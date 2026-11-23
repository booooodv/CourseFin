package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	ticksPerMillisecond   int64   = 10000
	autoCompleteThreshold float64 = 0.90
	defaultQueryLimit     int     = 20
)

type Progress struct {
	LessonID      string `json:"lesson_id"`
	PositionTicks int64  `json:"position_ticks"`
	Played        bool   `json:"played"`
	PlayCount     int    `json:"play_count"`
	LastPlayedAt  string `json:"last_played_at,omitempty"`
	UpdatedAt     string `json:"updated_at"`
}

type LessonWithContext struct {
	LessonDetail
	CourseTitle      string `json:"course_title"`
	CourseCoverPath  string `json:"course_cover_path"`
	LibraryID        string `json:"library_id"`
	ChapterTitle     string `json:"chapter_title"`
	ChapterSortOrder int    `json:"chapter_sort_order"`
}

func (s *Service) ReportProgress(ctx context.Context, lessonID string, positionTicks int64) (*Progress, error) {
	lessonID = normalizeOrRaw(lessonID)
	if positionTicks < 0 {
		positionTicks = 0
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin progress tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var durationMs int64
	err = tx.QueryRowContext(ctx, `
		SELECT mf.duration_ms
		FROM lessons l
		JOIN media_files mf ON mf.id = l.media_file_id
		WHERE l.id = ?`, lessonID).Scan(&durationMs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load lesson duration: %w", err)
	}

	var (
		prevPositionTicks int64
		playedInt         int
		playCount         int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT position_ticks, played, play_count FROM playback_progress WHERE lesson_id = ?`, lessonID).
		Scan(&prevPositionTicks, &playedInt, &playCount)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load playback_progress: %w", err)
	}

	prevPlayed := playedInt != 0
	played := prevPlayed
	finalPos := positionTicks

	if durationMs > 0 && float64(positionTicks)/float64(durationMs*ticksPerMillisecond) >= autoCompleteThreshold {
		played = true
		if !prevPlayed || prevPositionTicks > 0 {
			playCount++
		}
		finalPos = 0
	}

	now := nowUTC()
	if err := upsertProgressWith(ctx, tx, lessonID, finalPos, played, playCount, now, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit progress tx: %w", err)
	}

	return &Progress{
		LessonID:      lessonID,
		PositionTicks: finalPos,
		Played:        played,
		PlayCount:     playCount,
		LastPlayedAt:  now,
		UpdatedAt:     now,
	}, nil
}

func (s *Service) SetPlayed(ctx context.Context, lessonID string, played bool) (*Progress, error) {
	lessonID = normalizeOrRaw(lessonID)

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM lessons WHERE id = ?`, lessonID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var (
		existingPlayedInt int
		playCount         int
		existingLastPlay  sql.NullString
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT played, play_count, last_played_at FROM playback_progress WHERE lesson_id = ?`, lessonID).
		Scan(&existingPlayedInt, &playCount, &existingLastPlay)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load playback_progress: %w", err)
	}

	now := nowUTC()
	lastPlayedAt := existingLastPlay.String
	if played {
		if existingPlayedInt == 0 {
			playCount++
		} else if playCount == 0 {
			playCount = 1
		}
		lastPlayedAt = now
	} else {
		playCount = 0
	}

	if err := s.upsertProgress(ctx, lessonID, 0, played, playCount, lastPlayedAt, now); err != nil {
		return nil, err
	}

	return &Progress{
		LessonID:      lessonID,
		PositionTicks: 0,
		Played:        played,
		PlayCount:     playCount,
		LastPlayedAt:  lastPlayedAt,
		UpdatedAt:     now,
	}, nil
}

func (s *Service) upsertProgress(ctx context.Context, lessonID string, positionTicks int64, played bool, playCount int, lastPlayedAt, updatedAt string) error {
	return upsertProgressWith(ctx, s.db, lessonID, positionTicks, played, playCount, lastPlayedAt, updatedAt)
}

type progressExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func upsertProgressWith(ctx context.Context, execer progressExecer, lessonID string, positionTicks int64, played bool, playCount int, lastPlayedAt, updatedAt string) error {
	playedInt := 0
	if played {
		playedInt = 1
	}
	var lastPlayVal any
	if strings.TrimSpace(lastPlayedAt) != "" {
		lastPlayVal = lastPlayedAt
	}

	_, err := execer.ExecContext(ctx, `
		INSERT INTO playback_progress (lesson_id, position_ticks, played, play_count, last_played_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(lesson_id) DO UPDATE SET
			position_ticks = excluded.position_ticks,
			played         = excluded.played,
			play_count     = excluded.play_count,
			last_played_at = COALESCE(excluded.last_played_at, playback_progress.last_played_at),
			updated_at     = excluded.updated_at`,
		lessonID, positionTicks, playedInt, playCount, lastPlayVal, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert playback_progress: %w", err)
	}
	return nil
}

const lessonWithContextSelect = `
	SELECT l.id, l.course_id, l.chapter_id, l.media_file_id, l.title, l.overview, l.sort_order, l.cover_path,
	       mf.duration_ms, mf.container, mf.video_codec, mf.audio_codec, mf.width, mf.height, mf.bitrate, mf.thumbnail_path, mf.status,
	       COALESCE(pp.position_ticks, 0), COALESCE(pp.played, 0), COALESCE(pp.play_count, 0), COALESCE(pp.last_played_at, ''),
	       l.created_at, l.updated_at,
	       c.title, c.cover_path, c.library_id, ch.title, ch.sort_order
	FROM lessons l
	JOIN chapters ch ON ch.id = l.chapter_id
	JOIN courses c ON c.id = l.course_id
	JOIN media_files mf ON mf.id = l.media_file_id
	LEFT JOIN playback_progress pp ON pp.lesson_id = l.id`

func scanLessonWithContext(rows *sql.Rows) (LessonWithContext, error) {
	var item LessonWithContext
	var playedInt int
	err := rows.Scan(
		&item.ID, &item.CourseID, &item.ChapterID, &item.MediaFileID, &item.Title, &item.Overview, &item.SortOrder, &item.CoverPath,
		&item.DurationMs, &item.Container, &item.VideoCodec, &item.AudioCodec, &item.Width, &item.Height, &item.Bitrate, &item.ThumbnailPath, &item.MediaStatus,
		&item.PositionTicks, &playedInt, &item.PlayCount, &item.LastPlayedAt,
		&item.CreatedAt, &item.UpdatedAt,
		&item.CourseTitle, &item.CourseCoverPath, &item.LibraryID, &item.ChapterTitle, &item.ChapterSortOrder,
	)
	item.Played = playedInt != 0
	return item, err
}

func (s *Service) GetResumeLessons(ctx context.Context, limit int) ([]LessonWithContext, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	rows, err := s.db.QueryContext(ctx, lessonWithContextSelect+`
		WHERE pp.position_ticks > 0 AND pp.played = 0
		ORDER BY COALESCE(pp.last_played_at, pp.updated_at) DESC, pp.updated_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("get resume lessons: %w", err)
	}
	defer rows.Close()

	var items []LessonWithContext
	for rows.Next() {
		item, err := scanLessonWithContext(rows)
		if err != nil {
			return nil, fmt.Errorf("scan resume lesson: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetNextUpLessons(ctx context.Context, limit int) ([]LessonWithContext, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	rows, err := s.db.QueryContext(ctx, lessonWithContextSelect+`
		WHERE EXISTS (
			SELECT 1
			FROM lessons l2
			JOIN playback_progress pp2 ON pp2.lesson_id = l2.id
			WHERE l2.course_id = c.id AND pp2.played = 1
		)
		ORDER BY c.id ASC, ch.sort_order ASC, ch.created_at ASC, l.sort_order ASC, l.created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("get next up lessons: %w", err)
	}
	defer rows.Close()

	type courseGroup struct {
		lessons         []LessonWithContext
		maxLastPlayedAt string
	}
	groups := make(map[string]*courseGroup)
	var courseOrder []string

	for rows.Next() {
		item, err := scanLessonWithContext(rows)
		if err != nil {
			return nil, fmt.Errorf("scan next up candidate: %w", err)
		}
		g, ok := groups[item.CourseID]
		if !ok {
			g = &courseGroup{}
			groups[item.CourseID] = g
			courseOrder = append(courseOrder, item.CourseID)
		}
		g.lessons = append(g.lessons, item)
		if item.LastPlayedAt > g.maxLastPlayedAt {
			g.maxLastPlayedAt = item.LastPlayedAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type candidate struct {
		lesson       LessonWithContext
		lastPlayedAt string
	}
	var candidates []candidate

	for _, cid := range courseOrder {
		g := groups[cid]
		lastPlayedIdx := -1
		for i, l := range g.lessons {
			if l.Played {
				lastPlayedIdx = i
			}
		}

		var picked *LessonWithContext
		for i := lastPlayedIdx + 1; i < len(g.lessons); i++ {
			if !g.lessons[i].Played && g.lessons[i].PositionTicks == 0 {
				picked = &g.lessons[i]
				break
			}
		}
		if picked == nil {
			for i := 0; i < lastPlayedIdx; i++ {
				if !g.lessons[i].Played && g.lessons[i].PositionTicks == 0 {
					picked = &g.lessons[i]
					break
				}
			}
		}
		if picked != nil {
			candidates = append(candidates, candidate{
				lesson:       *picked,
				lastPlayedAt: g.maxLastPlayedAt,
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].lastPlayedAt > candidates[j].lastPlayedAt
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	out := make([]LessonWithContext, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.lesson)
	}
	return out, nil
}

func (s *Service) GetLatestCourses(ctx context.Context, libraryID string, limit int) ([]CourseSummary, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	return s.ListCourses(ctx, CourseFilter{
		LibraryID: libraryID,
		SortBy:    "created_at",
		SortDesc:  true,
		Limit:     limit,
	})
}
