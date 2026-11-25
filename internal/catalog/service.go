package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"coursefin/internal/id"
	"coursefin/internal/storage"
)

const defaultChapterTitle = "正片"

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidInput = errors.New("invalid input")
	ErrConflict     = errors.New("resource conflict")
)

// Service manages logical course catalogs, storage browsing, directory import,
// and unified playback progress.
type Service struct {
	db       *sql.DB
	dataDir  string
	registry *storage.Registry
}

// NewService creates a catalog Service backed by db and dataDir.
func NewService(db *sql.DB, dataDir string) *Service {
	return &Service{
		db:       db,
		dataDir:  dataDir,
		registry: storage.NewRegistry(),
	}
}

// SetRegistry overrides the storage driver registry used by the importer and source validation.
func (s *Service) SetRegistry(reg *storage.Registry) {
	if reg != nil {
		s.registry = reg
	}
}

type Library struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CoverPath   string `json:"cover_path"`
	SortOrder   int    `json:"sort_order"`
	CourseCount int    `json:"course_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Tag struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CourseCount int    `json:"course_count"`
}

type CourseFilter struct {
	LibraryID string
	Tag       string
	Search    string
	IDs       []string
	IsPlayed  *bool
	SortBy    string
	SortDesc  bool
	Limit     int
	Offset    int
}

type CourseSummary struct {
	ID                string   `json:"id"`
	LibraryID         string   `json:"library_id"`
	Title             string   `json:"title"`
	SortTitle         string   `json:"sort_title"`
	Overview          string   `json:"overview"`
	Instructor        string   `json:"instructor"`
	CoverPath         string   `json:"cover_path"`
	BackdropPath      string   `json:"backdrop_path"`
	SourceID          string   `json:"source_id,omitempty"`
	SourceRootPath    string   `json:"source_root_path,omitempty"`
	Tags              []string `json:"tags"`
	ChapterCount      int      `json:"chapter_count"`
	LessonCount       int      `json:"lesson_count"`
	PlayedCount       int      `json:"played_count"`
	UnplayedItemCount int      `json:"unplayed_item_count"`
	PlayedPercentage  float64  `json:"played_percentage"`
	Played            bool     `json:"played"`
	TotalDurationMs   int64    `json:"total_duration_ms"`
	FirstLessonID     string   `json:"first_lesson_id,omitempty"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

type CourseDetail struct {
	CourseSummary
	Chapters []ChapterDetail `json:"chapters"`
}

type ChapterDetail struct {
	ID                string         `json:"id"`
	CourseID          string         `json:"course_id"`
	Title             string         `json:"title"`
	SortOrder         int            `json:"sort_order"`
	Overview          string         `json:"overview"`
	LessonCount       int            `json:"lesson_count"`
	PlayedCount       int            `json:"played_count"`
	UnplayedItemCount int            `json:"unplayed_item_count"`
	PlayedPercentage  float64        `json:"played_percentage"`
	Played            bool           `json:"played"`
	Lessons           []LessonDetail `json:"lessons"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type SubtitleInfo struct {
	ID        string `json:"id"`
	Language  string `json:"language"`
	Title     string `json:"title"`
	Format    string `json:"format"`
	IsDefault bool   `json:"is_default"`
	Status    string `json:"status"`
}

type LessonDetail struct {
	ID            string         `json:"id"`
	CourseID      string         `json:"course_id"`
	ChapterID     string         `json:"chapter_id"`
	MediaFileID   string         `json:"media_file_id"`
	SourceID      string         `json:"source_id,omitempty"`
	Title         string         `json:"title"`
	Overview      string         `json:"overview"`
	SortOrder     int            `json:"sort_order"`
	CoverPath     string         `json:"cover_path"`
	DurationMs    int64          `json:"duration_ms"`
	Container     string         `json:"container"`
	VideoCodec    string         `json:"video_codec"`
	AudioCodec    string         `json:"audio_codec"`
	Width         int            `json:"width"`
	Height        int            `json:"height"`
	Bitrate       int64          `json:"bitrate"`
	ThumbnailPath string         `json:"thumbnail_path"`
	MediaStatus   string         `json:"media_status"`
	PositionTicks int64          `json:"position_ticks"`
	Played        bool           `json:"played"`
	PlayCount     int            `json:"play_count"`
	LastPlayedAt  string         `json:"last_played_at,omitempty"`
	Subtitles     []SubtitleInfo `json:"subtitles,omitempty"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

type CreateCourseInput struct {
	LibraryID  string   `json:"library_id"`
	Title      string   `json:"title"`
	SortTitle  string   `json:"sort_title"`
	Overview   string   `json:"overview"`
	Instructor string   `json:"instructor"`
	Tags       []string `json:"tags"`
}

type UpdateCourseInput struct {
	LibraryID    *string   `json:"library_id,omitempty"`
	Title        *string   `json:"title,omitempty"`
	SortTitle    *string   `json:"sort_title,omitempty"`
	Overview     *string   `json:"overview,omitempty"`
	Instructor   *string   `json:"instructor,omitempty"`
	CoverPath    *string   `json:"cover_path,omitempty"`
	BackdropPath *string   `json:"backdrop_path,omitempty"`
	Tags         *[]string `json:"tags,omitempty"`
}

type LessonPlacement struct {
	LessonID  string `json:"lesson_id"`
	ChapterID string `json:"chapter_id"`
	SortOrder int    `json:"sort_order"`
}

type StorageSource struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Driver        string `json:"driver"`
	RootPath      string `json:"root_path"`
	ConfigJSON    string `json:"config_json"`
	Enabled       bool   `json:"enabled"`
	LastScannedAt string `json:"last_scanned_at,omitempty"`
	MediaCount    int    `json:"media_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type IndexedDir struct {
	RelPath     string `json:"rel_path"`
	Name        string `json:"name"`
	FileCount   int    `json:"file_count"`
	DirectFiles int    `json:"direct_files"`
	HasSubdirs  bool   `json:"has_subdirs"`
}

type IndexedSubtitle struct {
	ID            string `json:"id"`
	SourceID      string `json:"source_id"`
	RelativePath  string `json:"relative_path"`
	FileName      string `json:"file_name"`
	MediaFileID   string `json:"media_file_id,omitempty"`
	Language      string `json:"language"`
	Title         string `json:"title"`
	Format        string `json:"format"`
	IsDefault     bool   `json:"is_default"`
	ManuallyBound bool   `json:"manually_bound"`
	Status        string `json:"status"`
}

type BindSubtitleInput struct {
	MediaFileID *string `json:"media_file_id"`
	Title       *string `json:"title"`
	Language    *string `json:"language"`
	IsDefault   *bool   `json:"is_default"`
	ResetAuto   bool    `json:"reset_auto"`
}

type MediaFile struct {
	ID            string `json:"id"`
	SourceID      string `json:"source_id"`
	RelativePath  string `json:"relative_path"`
	FileName      string `json:"file_name"`
	FileSize      int64  `json:"file_size"`
	ModTime       string `json:"mod_time"`
	Status        string `json:"status"`
	DurationMs    int64  `json:"duration_ms"`
	Container     string `json:"container"`
	VideoCodec    string `json:"video_codec"`
	AudioCodec    string `json:"audio_codec"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	Bitrate       int64  `json:"bitrate"`
	StreamsJSON   string `json:"-"`
	ThumbnailPath string `json:"thumbnail_path"`
	ProbedAt      string `json:"probed_at,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func normalizeOrRaw(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if norm, ok := id.Normalize(trimmed); ok {
		return norm
	}
	return trimmed
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func isConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "FOREIGN KEY constraint failed") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}

func computeWatchStats(lessonCount, playedCount int) (unplayed int, pct float64, played bool) {
	if lessonCount <= 0 {
		return 0, 0, false
	}
	unplayed = lessonCount - playedCount
	if unplayed < 0 {
		unplayed = 0
	}
	pct = float64(playedCount) * 100.0 / float64(lessonCount)
	played = playedCount == lessonCount
	return unplayed, pct, played
}

// --- Libraries ---

func (s *Service) ListLibraries(ctx context.Context) ([]Library, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.name, l.cover_path, l.sort_order, COUNT(c.id) AS course_count, l.created_at, l.updated_at
		FROM libraries l
		LEFT JOIN courses c ON c.library_id = l.id
		GROUP BY l.id
		ORDER BY l.sort_order ASC, l.created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}
	defer rows.Close()

	var libs []Library
	for rows.Next() {
		var lib Library
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.CoverPath, &lib.SortOrder, &lib.CourseCount, &lib.CreatedAt, &lib.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan library: %w", err)
		}
		libs = append(libs, lib)
	}
	return libs, rows.Err()
}

func (s *Service) CreateLibrary(ctx context.Context, name string) (*Library, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: library name is required", ErrInvalidInput)
	}

	var maxOrder int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM libraries`).Scan(&maxOrder); err != nil {
		return nil, fmt.Errorf("query max library sort_order: %w", err)
	}

	now := nowUTC()
	lib := &Library{
		ID:        id.New(),
		Name:      name,
		SortOrder: maxOrder + 1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO libraries (id, name, cover_path, sort_order, created_at, updated_at)
		VALUES (?, ?, '', ?, ?, ?)`,
		lib.ID, lib.Name, lib.SortOrder, lib.CreatedAt, lib.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert library: %w", err)
	}
	return lib, nil
}

func (s *Service) UpdateLibrary(ctx context.Context, libraryID, name string) (*Library, error) {
	libraryID = normalizeOrRaw(libraryID)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: library name is required", ErrInvalidInput)
	}

	now := nowUTC()
	res, err := s.db.ExecContext(ctx, `UPDATE libraries SET name = ?, updated_at = ? WHERE id = ?`, name, now, libraryID)
	if err != nil {
		return nil, fmt.Errorf("update library: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}

	var lib Library
	err = s.db.QueryRowContext(ctx, `
		SELECT l.id, l.name, l.cover_path, l.sort_order, COUNT(c.id), l.created_at, l.updated_at
		FROM libraries l
		LEFT JOIN courses c ON c.library_id = l.id
		WHERE l.id = ?
		GROUP BY l.id`, libraryID).
		Scan(&lib.ID, &lib.Name, &lib.CoverPath, &lib.SortOrder, &lib.CourseCount, &lib.CreatedAt, &lib.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load updated library: %w", err)
	}
	return &lib, nil
}

type DeleteLibraryOptions struct {
	MoveToLibraryID string
	Force           bool
}

func (s *Service) DeleteLibrary(ctx context.Context, libraryID string) error {
	return s.DeleteLibraryWithOptions(ctx, libraryID, DeleteLibraryOptions{})
}

func (s *Service) DeleteLibraryWithOptions(ctx context.Context, libraryID string, opts DeleteLibraryOptions) error {
	libraryID = normalizeOrRaw(libraryID)
	moveToID := normalizeOrRaw(opts.MoveToLibraryID)

	if moveToID != "" && opts.Force {
		return fmt.Errorf("%w: move_to_library_id and force cannot be used together", ErrInvalidInput)
	}

	if moveToID == "" && !opts.Force {
		res, err := s.db.ExecContext(ctx, `DELETE FROM libraries WHERE id = ?`, libraryID)
		if err != nil {
			if isConstraintErr(err) {
				return fmt.Errorf("%w: library contains courses", ErrConflict)
			}
			return fmt.Errorf("delete library: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNotFound
		}
		return nil
	}

	if moveToID != "" && moveToID == libraryID {
		return fmt.Errorf("%w: target library must differ from library being deleted", ErrInvalidInput)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete library tx: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM libraries WHERE id = ?`, libraryID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("check source library: %w", err)
	}

	var coversToClean []string

	if moveToID != "" {
		var targetExists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM libraries WHERE id = ?`, moveToID).Scan(&targetExists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: target library not found", ErrNotFound)
			}
			return fmt.Errorf("check target library: %w", err)
		}
		now := nowUTC()
		if _, err := tx.ExecContext(ctx, `UPDATE courses SET library_id = ?, updated_at = ? WHERE library_id = ?`, moveToID, now, libraryID); err != nil {
			return fmt.Errorf("migrate library courses: %w", err)
		}
	} else if opts.Force {
		rows, err := tx.QueryContext(ctx, `SELECT cover_path, backdrop_path FROM courses WHERE library_id = ?`, libraryID)
		if err != nil {
			return fmt.Errorf("query library course covers: %w", err)
		}
		for rows.Next() {
			var coverPath, backdropPath string
			if err := rows.Scan(&coverPath, &backdropPath); err != nil {
				rows.Close()
				return fmt.Errorf("scan library course covers: %w", err)
			}
			if coverPath != "" {
				coversToClean = append(coversToClean, coverPath)
			}
			if backdropPath != "" {
				coversToClean = append(coversToClean, backdropPath)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate library course covers: %w", err)
		}
		rows.Close()

		if _, err := tx.ExecContext(ctx, `DELETE FROM courses WHERE library_id = ?`, libraryID); err != nil {
			return fmt.Errorf("delete library courses: %w", err)
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM libraries WHERE id = ?`, libraryID)
	if err != nil {
		return fmt.Errorf("delete library: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete library: %w", err)
	}

	// Clean up app-managed cover/backdrop artifacts inside dataDir/covers only (never touch media volumes).
	if s.dataDir != "" && len(coversToClean) > 0 {
		coversDir := filepath.Join(s.dataDir, "covers")
		for _, p := range coversToClean {
			_ = os.Remove(filepath.Join(coversDir, filepath.Base(p)))
		}
	}
	return nil
}

// --- Courses & Tags ---

func (s *Service) ListCourses(ctx context.Context, filter CourseFilter) ([]CourseSummary, error) {
	where := []string{"1=1"}
	var args []any

	if filter.LibraryID != "" {
		where = append(where, "c.library_id = ?")
		args = append(args, normalizeOrRaw(filter.LibraryID))
	}
	if strings.TrimSpace(filter.Tag) != "" {
		where = append(where, `EXISTS (
			SELECT 1 FROM course_tags ct
			JOIN tags t ON t.id = ct.tag_id
			WHERE ct.course_id = c.id AND t.name = ?
		)`)
		args = append(args, strings.TrimSpace(filter.Tag))
	}
	if strings.TrimSpace(filter.Search) != "" {
		term := "%" + strings.ToLower(strings.TrimSpace(filter.Search)) + "%"
		where = append(where, "(LOWER(c.title) LIKE ? OR LOWER(c.overview) LIKE ? OR LOWER(c.instructor) LIKE ?)")
		args = append(args, term, term, term)
	}
	if len(filter.IDs) > 0 {
		placeholders := make([]string, len(filter.IDs))
		for i, raw := range filter.IDs {
			placeholders[i] = "?"
			args = append(args, normalizeOrRaw(raw))
		}
		where = append(where, "c.id IN ("+strings.Join(placeholders, ",")+")")
	}

	orderCol := "c.created_at"
	orderDir := "DESC"
	switch strings.ToLower(strings.TrimSpace(filter.SortBy)) {
	case "title":
		orderCol = "c.title"
		orderDir = "ASC"
	case "sort_title":
		orderCol = "CASE WHEN c.sort_title != '' THEN c.sort_title ELSE c.title END"
		orderDir = "ASC"
	case "updated_at":
		orderCol = "c.updated_at"
		orderDir = "DESC"
	case "created_at":
		orderCol = "c.created_at"
		orderDir = "DESC"
	}
	if filter.SortBy != "" {
		if filter.SortDesc {
			orderDir = "DESC"
		} else {
			orderDir = "ASC"
		}
	}

	query := fmt.Sprintf(`
		SELECT c.id, c.library_id, c.title, c.sort_title, c.overview, c.instructor,
		       c.cover_path, c.backdrop_path, COALESCE(c.source_id, ''), COALESCE(c.source_root_path, ''),
		       c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM chapters ch WHERE ch.course_id = c.id) AS chapter_count,
		       (SELECT COUNT(*) FROM lessons l WHERE l.course_id = c.id) AS lesson_count,
		       (SELECT COUNT(*) FROM lessons l JOIN playback_progress pp ON pp.lesson_id = l.id WHERE l.course_id = c.id AND pp.played = 1) AS played_count,
		       (SELECT COALESCE(SUM(mf.duration_ms), 0) FROM lessons l JOIN media_files mf ON mf.id = l.media_file_id WHERE l.course_id = c.id) AS total_duration_ms,
		       COALESCE((SELECT l.id FROM lessons l JOIN chapters ch ON ch.id = l.chapter_id WHERE l.course_id = c.id ORDER BY ch.sort_order ASC, ch.created_at ASC, l.sort_order ASC, l.created_at ASC LIMIT 1), '') AS first_lesson_id
		FROM courses c
		WHERE %s
		ORDER BY %s %s, c.id ASC`, strings.Join(where, " AND "), orderCol, orderDir)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list courses: %w", err)
	}
	defer rows.Close()

	var courses []CourseSummary
	var courseIDs []string
	for rows.Next() {
		var cs CourseSummary
		if err := rows.Scan(
			&cs.ID, &cs.LibraryID, &cs.Title, &cs.SortTitle, &cs.Overview, &cs.Instructor,
			&cs.CoverPath, &cs.BackdropPath, &cs.SourceID, &cs.SourceRootPath,
			&cs.CreatedAt, &cs.UpdatedAt,
			&cs.ChapterCount, &cs.LessonCount, &cs.PlayedCount, &cs.TotalDurationMs, &cs.FirstLessonID,
		); err != nil {
			return nil, fmt.Errorf("scan course summary: %w", err)
		}
		cs.UnplayedItemCount, cs.PlayedPercentage, cs.Played = computeWatchStats(cs.LessonCount, cs.PlayedCount)
		cs.Tags = []string{}
		if filter.IsPlayed != nil && cs.Played != *filter.IsPlayed {
			continue
		}
		courses = append(courses, cs)
		courseIDs = append(courseIDs, cs.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.populateCourseTags(ctx, courses, courseIDs); err != nil {
		return nil, err
	}

	if filter.Offset > 0 {
		if filter.Offset >= len(courses) {
			return []CourseSummary{}, nil
		}
		courses = courses[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(courses) {
		courses = courses[:filter.Limit]
	}

	return courses, nil
}

func (s *Service) populateCourseTags(ctx context.Context, courses []CourseSummary, courseIDs []string) error {
	if len(courseIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(courseIDs))
	args := make([]any, len(courseIDs))
	for i, cid := range courseIDs {
		placeholders[i] = "?"
		args[i] = cid
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ct.course_id, t.name
		 FROM course_tags ct
		 JOIN tags t ON t.id = ct.tag_id
		 WHERE ct.course_id IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY t.name ASC`, args...)
	if err != nil {
		return fmt.Errorf("load course tags: %w", err)
	}
	defer rows.Close()

	tagMap := make(map[string][]string, len(courseIDs))
	for rows.Next() {
		var cid, tname string
		if err := rows.Scan(&cid, &tname); err != nil {
			return err
		}
		tagMap[cid] = append(tagMap[cid], tname)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range courses {
		if tags, ok := tagMap[courses[i].ID]; ok {
			courses[i].Tags = tags
		}
	}
	return nil
}

func (s *Service) GetCourseDetail(ctx context.Context, courseID string) (*CourseDetail, error) {
	courseID = normalizeOrRaw(courseID)
	summaries, err := s.ListCourses(ctx, CourseFilter{IDs: []string{courseID}})
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, ErrNotFound
	}

	detail := &CourseDetail{
		CourseSummary: summaries[0],
		Chapters:      []ChapterDetail{},
	}

	chRows, err := s.db.QueryContext(ctx, `
		SELECT id, course_id, title, sort_order, overview, created_at, updated_at
		FROM chapters
		WHERE course_id = ?
		ORDER BY sort_order ASC, created_at ASC`, courseID)
	if err != nil {
		return nil, fmt.Errorf("load course chapters: %w", err)
	}
	defer chRows.Close()

	chIndex := make(map[string]int)
	for chRows.Next() {
		var ch ChapterDetail
		if err := chRows.Scan(&ch.ID, &ch.CourseID, &ch.Title, &ch.SortOrder, &ch.Overview, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan chapter: %w", err)
		}
		ch.Lessons = []LessonDetail{}
		chIndex[ch.ID] = len(detail.Chapters)
		detail.Chapters = append(detail.Chapters, ch)
	}
	if err := chRows.Err(); err != nil {
		return nil, err
	}

	lRows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.course_id, l.chapter_id, l.media_file_id, mf.source_id, l.title, l.overview, l.sort_order, l.cover_path,
		       mf.duration_ms, mf.container, mf.video_codec, mf.audio_codec, mf.width, mf.height, mf.bitrate, mf.thumbnail_path, mf.status,
		       COALESCE(pp.position_ticks, 0), COALESCE(pp.played, 0), COALESCE(pp.play_count, 0), COALESCE(pp.last_played_at, ''),
		       l.created_at, l.updated_at
		FROM lessons l
		JOIN media_files mf ON mf.id = l.media_file_id
		LEFT JOIN playback_progress pp ON pp.lesson_id = l.id
		WHERE l.course_id = ?
		ORDER BY l.sort_order ASC, l.created_at ASC`, courseID)
	if err != nil {
		return nil, fmt.Errorf("load course lessons: %w", err)
	}
	defer lRows.Close()

	var mediaFileIDs []string
	seenMF := make(map[string]bool)
	for lRows.Next() {
		var ld LessonDetail
		var playedInt int
		if err := lRows.Scan(
			&ld.ID, &ld.CourseID, &ld.ChapterID, &ld.MediaFileID, &ld.SourceID, &ld.Title, &ld.Overview, &ld.SortOrder, &ld.CoverPath,
			&ld.DurationMs, &ld.Container, &ld.VideoCodec, &ld.AudioCodec, &ld.Width, &ld.Height, &ld.Bitrate, &ld.ThumbnailPath, &ld.MediaStatus,
			&ld.PositionTicks, &playedInt, &ld.PlayCount, &ld.LastPlayedAt,
			&ld.CreatedAt, &ld.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan lesson: %w", err)
		}
		ld.Played = playedInt != 0
		if !seenMF[ld.MediaFileID] {
			seenMF[ld.MediaFileID] = true
			mediaFileIDs = append(mediaFileIDs, ld.MediaFileID)
		}
		if idx, ok := chIndex[ld.ChapterID]; ok {
			detail.Chapters[idx].Lessons = append(detail.Chapters[idx].Lessons, ld)
			detail.Chapters[idx].LessonCount++
			if ld.Played {
				detail.Chapters[idx].PlayedCount++
			}
		}
	}
	if err := lRows.Err(); err != nil {
		return nil, err
	}

	subMap, err := s.loadSubtitlesForMediaFiles(ctx, mediaFileIDs)
	if err != nil {
		return nil, err
	}

	for i := range detail.Chapters {
		ch := &detail.Chapters[i]
		ch.UnplayedItemCount, ch.PlayedPercentage, ch.Played = computeWatchStats(ch.LessonCount, ch.PlayedCount)
		for j := range ch.Lessons {
			if subs, ok := subMap[ch.Lessons[j].MediaFileID]; ok {
				ch.Lessons[j].Subtitles = subs
			}
		}
	}

	return detail, nil
}

func (s *Service) loadSubtitlesForMediaFiles(ctx context.Context, mediaFileIDs []string) (map[string][]SubtitleInfo, error) {
	res := make(map[string][]SubtitleInfo)
	if len(mediaFileIDs) == 0 {
		return res, nil
	}
	placeholders := make([]string, len(mediaFileIDs))
	args := make([]any, len(mediaFileIDs))
	for i, mid := range mediaFileIDs {
		placeholders[i] = "?"
		args[i] = mid
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, media_file_id, language, title, format, is_default, status
		 FROM subtitle_files
		 WHERE media_file_id IN (`+strings.Join(placeholders, ",")+`) AND status = 'active'
		 ORDER BY is_default DESC, language ASC, id ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("load subtitles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var mfID string
		var sub SubtitleInfo
		var isDef int
		if err := rows.Scan(&sub.ID, &mfID, &sub.Language, &sub.Title, &sub.Format, &isDef, &sub.Status); err != nil {
			return nil, err
		}
		sub.IsDefault = isDef != 0
		res[mfID] = append(res[mfID], sub)
	}
	return res, rows.Err()
}

func (s *Service) resolveLibraryID(ctx context.Context, libID string) (string, error) {
	libID = strings.TrimSpace(libID)
	if libID == "" {
		var first string
		err := s.db.QueryRowContext(ctx, `SELECT id FROM libraries ORDER BY sort_order ASC, created_at ASC LIMIT 1`).Scan(&first)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: no library configured", ErrNotFound)
		}
		if err != nil {
			return "", err
		}
		return first, nil
	}
	libID = normalizeOrRaw(libID)
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM libraries WHERE id = ?`, libID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: library %s", ErrNotFound, libID)
	}
	if err != nil {
		return "", err
	}
	return libID, nil
}

func (s *Service) CreateCourse(ctx context.Context, input CreateCourseInput) (*CourseDetail, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: course title is required", ErrInvalidInput)
	}

	libID, err := s.resolveLibraryID(ctx, input.LibraryID)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := nowUTC()
	courseID := id.New()
	sortTitle := strings.TrimSpace(input.SortTitle)
	if sortTitle == "" {
		sortTitle = title
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO courses (id, library_id, title, sort_title, overview, instructor, cover_path, backdrop_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, '', '', ?, ?)`,
		courseID, libID, title, sortTitle, strings.TrimSpace(input.Overview), strings.TrimSpace(input.Instructor), now, now,
	); err != nil {
		return nil, fmt.Errorf("insert course: %w", err)
	}

	// Enforce default chapter ("正片", sort_order = 1) on every newly created empty course.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chapters (id, course_id, title, sort_order, overview, created_at, updated_at)
		VALUES (?, ?, ?, 1, '', ?, ?)`,
		id.New(), courseID, defaultChapterTitle, now, now,
	); err != nil {
		return nil, fmt.Errorf("insert default chapter: %w", err)
	}

	if len(input.Tags) > 0 {
		if err := setCourseTagsTx(ctx, tx, courseID, input.Tags); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetCourseDetail(ctx, courseID)
}

func (s *Service) UpdateCourse(ctx context.Context, courseID string, input UpdateCourseInput) (*CourseDetail, error) {
	courseID = normalizeOrRaw(courseID)

	var (
		libID        string
		title        string
		sortTitle    string
		overview     string
		instructor   string
		coverPath    string
		backdropPath string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT library_id, title, sort_title, overview, instructor, cover_path, backdrop_path
		FROM courses WHERE id = ?`, courseID).
		Scan(&libID, &title, &sortTitle, &overview, &instructor, &coverPath, &backdropPath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if input.LibraryID != nil {
		resolved, err := s.resolveLibraryID(ctx, *input.LibraryID)
		if err != nil {
			return nil, err
		}
		libID = resolved
	}
	if input.Title != nil {
		t := strings.TrimSpace(*input.Title)
		if t == "" {
			return nil, fmt.Errorf("%w: course title cannot be empty", ErrInvalidInput)
		}
		title = t
	}
	if input.SortTitle != nil {
		sortTitle = strings.TrimSpace(*input.SortTitle)
	}
	if input.Overview != nil {
		overview = strings.TrimSpace(*input.Overview)
	}
	if input.Instructor != nil {
		instructor = strings.TrimSpace(*input.Instructor)
	}
	if input.CoverPath != nil {
		coverPath = strings.TrimSpace(*input.CoverPath)
	}
	if input.BackdropPath != nil {
		backdropPath = strings.TrimSpace(*input.BackdropPath)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := nowUTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE courses
		SET library_id = ?, title = ?, sort_title = ?, overview = ?, instructor = ?,
		    cover_path = ?, backdrop_path = ?, updated_at = ?
		WHERE id = ?`,
		libID, title, sortTitle, overview, instructor, coverPath, backdropPath, now, courseID,
	); err != nil {
		return nil, fmt.Errorf("update course: %w", err)
	}

	if input.Tags != nil {
		if err := setCourseTagsTx(ctx, tx, courseID, *input.Tags); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCourseDetail(ctx, courseID)
}

func (s *Service) DeleteCourse(ctx context.Context, courseID string) error {
	courseID = normalizeOrRaw(courseID)

	var coverPath, backdropPath string
	err := s.db.QueryRowContext(ctx, `SELECT cover_path, backdrop_path FROM courses WHERE id = ?`, courseID).
		Scan(&coverPath, &backdropPath)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM courses WHERE id = ?`, courseID)
	if err != nil {
		return fmt.Errorf("delete course: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	// Clean up app-managed cover/backdrop artifacts inside dataDir/covers only (never touch media volumes).
	if s.dataDir != "" {
		coversDir := filepath.Join(s.dataDir, "covers")
		for _, p := range []string{coverPath, backdropPath} {
			if p != "" {
				_ = os.Remove(filepath.Join(coversDir, filepath.Base(p)))
			}
		}
	}
	return nil
}

func (s *Service) SetCourseTags(ctx context.Context, courseID string, tagNames []string) error {
	courseID = normalizeOrRaw(courseID)

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM courses WHERE id = ?`, courseID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := setCourseTagsTx(ctx, tx, courseID, tagNames); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE courses SET updated_at = ? WHERE id = ?`, nowUTC(), courseID); err != nil {
		return err
	}
	return tx.Commit()
}

func setCourseTagsTx(ctx context.Context, tx *sql.Tx, courseID string, tagNames []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM course_tags WHERE course_id = ?`, courseID); err != nil {
		return fmt.Errorf("clear course tags: %w", err)
	}

	seen := make(map[string]bool)
	for _, raw := range tagNames {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		var tagID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, name).Scan(&tagID)
		if errors.Is(err, sql.ErrNoRows) {
			tagID = id.New()
			if _, err := tx.ExecContext(ctx, `INSERT INTO tags (id, name) VALUES (?, ?)`, tagID, name); err != nil {
				return fmt.Errorf("insert tag %q: %w", name, err)
			}
		} else if err != nil {
			return fmt.Errorf("lookup tag %q: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO course_tags (course_id, tag_id) VALUES (?, ?)`, courseID, tagID); err != nil {
			return fmt.Errorf("link course tag %q: %w", name, err)
		}
	}
	return nil
}

func (s *Service) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, COUNT(ct.course_id) AS course_count
		FROM tags t
		JOIN course_tags ct ON ct.tag_id = t.id
		GROUP BY t.id
		HAVING COUNT(ct.course_id) > 0
		ORDER BY t.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.CourseCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// --- Chapters & Lessons ---

func (s *Service) CreateChapter(ctx context.Context, courseID, title string) (*ChapterDetail, error) {
	courseID = normalizeOrRaw(courseID)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("%w: chapter title is required", ErrInvalidInput)
	}

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM courses WHERE id = ?`, courseID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var maxOrder int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM chapters WHERE course_id = ?`, courseID).Scan(&maxOrder); err != nil {
		return nil, err
	}

	now := nowUTC()
	ch := &ChapterDetail{
		ID:        id.New(),
		CourseID:  courseID,
		Title:     title,
		SortOrder: maxOrder + 1,
		Lessons:   []LessonDetail{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO chapters (id, course_id, title, sort_order, overview, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, ?)`,
		ch.ID, ch.CourseID, ch.Title, ch.SortOrder, ch.CreatedAt, ch.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert chapter: %w", err)
	}
	return ch, nil
}

func (s *Service) UpdateChapter(ctx context.Context, chapterID, title, overview string) (*ChapterDetail, error) {
	chapterID = normalizeOrRaw(chapterID)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("%w: chapter title is required", ErrInvalidInput)
	}

	now := nowUTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE chapters SET title = ?, overview = ?, updated_at = ? WHERE id = ?`,
		title, strings.TrimSpace(overview), now, chapterID,
	)
	if err != nil {
		return nil, fmt.Errorf("update chapter: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.GetChapter(ctx, chapterID)
}

func (s *Service) GetChapter(ctx context.Context, chapterID string) (*ChapterDetail, error) {
	chapterID = normalizeOrRaw(chapterID)
	var courseID string
	err := s.db.QueryRowContext(ctx, `SELECT course_id FROM chapters WHERE id = ?`, chapterID).Scan(&courseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	detail, err := s.GetCourseDetail(ctx, courseID)
	if err != nil {
		return nil, err
	}
	for i := range detail.Chapters {
		if detail.Chapters[i].ID == chapterID {
			ch := detail.Chapters[i]
			return &ch, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) DeleteChapter(ctx context.Context, chapterID string) error {
	chapterID = normalizeOrRaw(chapterID)
	res, err := s.db.ExecContext(ctx, `DELETE FROM chapters WHERE id = ?`, chapterID)
	if err != nil {
		return fmt.Errorf("delete chapter: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ReorderChapters(ctx context.Context, courseID string, chapterIDs []string) error {
	courseID = normalizeOrRaw(courseID)
	if len(chapterIDs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowUTC()
	seen := make(map[string]bool, len(chapterIDs))
	for i, rawID := range chapterIDs {
		cid := normalizeOrRaw(rawID)
		if seen[cid] {
			return fmt.Errorf("%w: duplicate chapter id %s", ErrInvalidInput, cid)
		}
		seen[cid] = true

		res, err := tx.ExecContext(ctx, `
			UPDATE chapters SET sort_order = ?, updated_at = ? WHERE id = ? AND course_id = ?`,
			i+1, now, cid, courseID,
		)
		if err != nil {
			return fmt.Errorf("reorder chapter %s: %w", cid, err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("%w: chapter %s in course %s", ErrNotFound, cid, courseID)
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE courses SET updated_at = ? WHERE id = ?`, now, courseID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) AddLesson(ctx context.Context, chapterID, mediaFileID, title string) (*LessonDetail, error) {
	chapterID = normalizeOrRaw(chapterID)
	mediaFileID = normalizeOrRaw(mediaFileID)

	var courseID string
	err := s.db.QueryRowContext(ctx, `SELECT course_id FROM chapters WHERE id = ?`, chapterID).Scan(&courseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: chapter %s", ErrNotFound, chapterID)
	}
	if err != nil {
		return nil, err
	}

	var fileName string
	err = s.db.QueryRowContext(ctx, `SELECT file_name FROM media_files WHERE id = ?`, mediaFileID).Scan(&fileName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: media_file %s", ErrNotFound, mediaFileID)
	}
	if err != nil {
		return nil, err
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = stripExt(fileName)
	}

	var maxOrder int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM lessons WHERE chapter_id = ?`, chapterID).Scan(&maxOrder); err != nil {
		return nil, err
	}

	lessonID := id.New()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, overview, sort_order, cover_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', ?, '', ?, ?)`,
		lessonID, courseID, chapterID, mediaFileID, title, maxOrder+1, now, now,
	); err != nil {
		return nil, fmt.Errorf("insert lesson: %w", err)
	}

	return s.GetLesson(ctx, lessonID)
}

func (s *Service) UpdateLesson(ctx context.Context, lessonID, title, overview, chapterID string) (*LessonDetail, error) {
	lessonID = normalizeOrRaw(lessonID)

	var curCourseID, curChapterID, curTitle string
	err := s.db.QueryRowContext(ctx, `SELECT course_id, chapter_id, title FROM lessons WHERE id = ?`, lessonID).
		Scan(&curCourseID, &curChapterID, &curTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	newTitle := strings.TrimSpace(title)
	if newTitle == "" {
		newTitle = curTitle
	}
	newOverview := strings.TrimSpace(overview)

	targetChapterID := curChapterID
	moveChapter := false
	if strings.TrimSpace(chapterID) != "" {
		normChap := normalizeOrRaw(chapterID)
		if normChap != curChapterID {
			var targetCourseID string
			err := s.db.QueryRowContext(ctx, `SELECT course_id FROM chapters WHERE id = ?`, normChap).Scan(&targetCourseID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: chapter %s", ErrNotFound, normChap)
			}
			if err != nil {
				return nil, err
			}
			if targetCourseID != curCourseID {
				return nil, fmt.Errorf("%w: chapter %s belongs to a different course", ErrInvalidInput, normChap)
			}
			targetChapterID = normChap
			moveChapter = true
		}
	}

	now := nowUTC()
	if moveChapter {
		var maxOrder int
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM lessons WHERE chapter_id = ?`, targetChapterID).Scan(&maxOrder); err != nil {
			return nil, err
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE lessons SET title = ?, overview = ?, chapter_id = ?, sort_order = ?, updated_at = ? WHERE id = ?`,
			newTitle, newOverview, targetChapterID, maxOrder+1, now, lessonID,
		); err != nil {
			return nil, fmt.Errorf("update lesson: %w", err)
		}
	} else {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE lessons SET title = ?, overview = ?, updated_at = ? WHERE id = ?`,
			newTitle, newOverview, now, lessonID,
		); err != nil {
			return nil, fmt.Errorf("update lesson: %w", err)
		}
	}

	return s.GetLesson(ctx, lessonID)
}

func (s *Service) GetLesson(ctx context.Context, lessonID string) (*LessonDetail, error) {
	lessonID = normalizeOrRaw(lessonID)
	var ld LessonDetail
	var playedInt int
	err := s.db.QueryRowContext(ctx, `
		SELECT l.id, l.course_id, l.chapter_id, l.media_file_id, mf.source_id, l.title, l.overview, l.sort_order, l.cover_path,
		       mf.duration_ms, mf.container, mf.video_codec, mf.audio_codec, mf.width, mf.height, mf.bitrate, mf.thumbnail_path, mf.status,
		       COALESCE(pp.position_ticks, 0), COALESCE(pp.played, 0), COALESCE(pp.play_count, 0), COALESCE(pp.last_played_at, ''),
		       l.created_at, l.updated_at
		FROM lessons l
		JOIN media_files mf ON mf.id = l.media_file_id
		LEFT JOIN playback_progress pp ON pp.lesson_id = l.id
		WHERE l.id = ?`, lessonID).
		Scan(
			&ld.ID, &ld.CourseID, &ld.ChapterID, &ld.MediaFileID, &ld.SourceID, &ld.Title, &ld.Overview, &ld.SortOrder, &ld.CoverPath,
			&ld.DurationMs, &ld.Container, &ld.VideoCodec, &ld.AudioCodec, &ld.Width, &ld.Height, &ld.Bitrate, &ld.ThumbnailPath, &ld.MediaStatus,
			&ld.PositionTicks, &playedInt, &ld.PlayCount, &ld.LastPlayedAt,
			&ld.CreatedAt, &ld.UpdatedAt,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get lesson: %w", err)
	}
	ld.Played = playedInt != 0

	subs, err := s.loadSubtitlesForMediaFiles(ctx, []string{ld.MediaFileID})
	if err != nil {
		return nil, err
	}
	ld.Subtitles = subs[ld.MediaFileID]
	return &ld, nil
}

func (s *Service) DeleteLesson(ctx context.Context, lessonID string) error {
	lessonID = normalizeOrRaw(lessonID)
	res, err := s.db.ExecContext(ctx, `DELETE FROM lessons WHERE id = ?`, lessonID)
	if err != nil {
		return fmt.Errorf("delete lesson: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ReorderLessons(ctx context.Context, courseID string, placements []LessonPlacement) error {
	courseID = normalizeOrRaw(courseID)
	if len(placements) == 0 {
		return nil
	}

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM courses WHERE id = ?`, courseID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowUTC()
	seenLesson := make(map[string]bool, len(placements))
	validChapters := make(map[string]bool)

	for i, p := range placements {
		lid := normalizeOrRaw(p.LessonID)
		cid := normalizeOrRaw(p.ChapterID)
		if seenLesson[lid] {
			return fmt.Errorf("%w: duplicate lesson id %s", ErrInvalidInput, lid)
		}
		seenLesson[lid] = true

		if !validChapters[cid] {
			var chOK int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM chapters WHERE id = ? AND course_id = ?`, cid, courseID).Scan(&chOK)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: chapter %s in course %s", ErrNotFound, cid, courseID)
			}
			if err != nil {
				return err
			}
			validChapters[cid] = true
		}

		sortOrder := p.SortOrder
		if sortOrder <= 0 {
			sortOrder = i + 1
		}

		res, err := tx.ExecContext(ctx, `
			UPDATE lessons SET chapter_id = ?, sort_order = ?, updated_at = ?
			WHERE id = ? AND course_id = ?`,
			cid, sortOrder, now, lid, courseID,
		)
		if err != nil {
			return fmt.Errorf("reorder lesson %s: %w", lid, err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("%w: lesson %s in course %s", ErrNotFound, lid, courseID)
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE courses SET updated_at = ? WHERE id = ?`, now, courseID); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Storage Browser ---

func (s *Service) ListSources(ctx context.Context) ([]StorageSource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ss.id, ss.name, ss.driver, ss.root_path, ss.config_json, ss.enabled,
		       COALESCE(ss.last_scanned_at, ''), COUNT(mf.id) AS media_count,
		       ss.created_at, ss.updated_at
		FROM storage_sources ss
		LEFT JOIN media_files mf ON mf.source_id = ss.id AND mf.status = 'active'
		GROUP BY ss.id
		ORDER BY ss.created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list storage sources: %w", err)
	}
	defer rows.Close()

	var sources []StorageSource
	for rows.Next() {
		var src StorageSource
		var enabledInt int
		if err := rows.Scan(
			&src.ID, &src.Name, &src.Driver, &src.RootPath, &src.ConfigJSON, &enabledInt,
			&src.LastScannedAt, &src.MediaCount, &src.CreatedAt, &src.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan storage source: %w", err)
		}
		src.Enabled = enabledInt != 0
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *Service) GetSource(ctx context.Context, sourceID string) (*StorageSource, error) {
	sourceID = normalizeOrRaw(sourceID)
	var src StorageSource
	var enabledInt int
	err := s.db.QueryRowContext(ctx, `
		SELECT ss.id, ss.name, ss.driver, ss.root_path, ss.config_json, ss.enabled,
		       COALESCE(ss.last_scanned_at, ''), COUNT(mf.id) AS media_count,
		       ss.created_at, ss.updated_at
		FROM storage_sources ss
		LEFT JOIN media_files mf ON mf.source_id = ss.id AND mf.status = 'active'
		WHERE ss.id = ?
		GROUP BY ss.id`, sourceID).
		Scan(
			&src.ID, &src.Name, &src.Driver, &src.RootPath, &src.ConfigJSON, &enabledInt,
			&src.LastScannedAt, &src.MediaCount, &src.CreatedAt, &src.UpdatedAt,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	src.Enabled = enabledInt != 0
	return &src, nil
}

func (s *Service) CreateSource(ctx context.Context, name, driver, rootPath string) (*StorageSource, error) {
	name = strings.TrimSpace(name)
	driver = strings.ToLower(strings.TrimSpace(driver))
	rootPath = strings.TrimSpace(rootPath)
	if driver == "" {
		driver = "local"
	}
	if name == "" || rootPath == "" {
		return nil, fmt.Errorf("%w: name and root_path are required", ErrInvalidInput)
	}

	if _, err := s.registry.ProviderFor(driver, rootPath, "{}"); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	now := nowUTC()
	src := &StorageSource{
		ID:         id.New(),
		Name:       name,
		Driver:     driver,
		RootPath:   rootPath,
		ConfigJSON: "{}",
		Enabled:    true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO storage_sources (id, name, driver, root_path, config_json, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		src.ID, src.Name, src.Driver, src.RootPath, src.ConfigJSON, src.CreatedAt, src.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert storage source: %w", err)
	}
	return src, nil
}

func (s *Service) DeleteSource(ctx context.Context, sourceID string) error {
	sourceID = normalizeOrRaw(sourceID)
	res, err := s.db.ExecContext(ctx, `DELETE FROM storage_sources WHERE id = ?`, sourceID)
	if err != nil {
		if isConstraintErr(err) {
			return fmt.Errorf("%w: storage source has media files referenced by courses", ErrConflict)
		}
		return fmt.Errorf("delete storage source: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func cleanRelPrefix(raw string) string {
	s := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	s = strings.Trim(s, "/")
	if s == "" || s == "." {
		return ""
	}
	cleaned := path.Clean(s)
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

func (s *Service) ListIndexedDirs(ctx context.Context, sourceID, prefix string) ([]IndexedDir, error) {
	sourceID = normalizeOrRaw(sourceID)
	if _, err := s.GetSource(ctx, sourceID); err != nil {
		return nil, err
	}

	cleanPrefix := cleanRelPrefix(prefix)

	rows, err := s.db.QueryContext(ctx, `
		SELECT relative_path FROM media_files
		WHERE source_id = ? AND status = 'active'`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("list indexed dirs: %w", err)
	}
	defer rows.Close()

	dirMap := make(map[string]*IndexedDir)
	for rows.Next() {
		var relPath string
		if err := rows.Scan(&relPath); err != nil {
			return nil, err
		}
		var rest string
		if cleanPrefix == "" {
			rest = relPath
		} else {
			if !strings.HasPrefix(relPath, cleanPrefix+"/") {
				continue
			}
			rest = strings.TrimPrefix(relPath, cleanPrefix+"/")
		}
		slashIdx := strings.IndexByte(rest, '/')
		if slashIdx < 0 {
			continue
		}
		dirName := rest[:slashIdx]
		fullDir := dirName
		if cleanPrefix != "" {
			fullDir = cleanPrefix + "/" + dirName
		}

		entry, ok := dirMap[dirName]
		if !ok {
			entry = &IndexedDir{
				RelPath: fullDir,
				Name:    dirName,
			}
			dirMap[dirName] = entry
		}
		entry.FileCount++
		if strings.ContainsRune(rest[slashIdx+1:], '/') {
			entry.HasSubdirs = true
		} else {
			entry.DirectFiles++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dirs := make([]IndexedDir, 0, len(dirMap))
	for _, d := range dirMap {
		dirs = append(dirs, *d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return NaturalCompare(dirs[i].Name, dirs[j].Name) < 0
	})
	return dirs, nil
}

func (s *Service) ListMediaFiles(ctx context.Context, sourceID, dirPrefix, search, status string) ([]MediaFile, error) {
	where := []string{"1=1"}
	var args []any

	if strings.TrimSpace(sourceID) != "" {
		where = append(where, "source_id = ?")
		args = append(args, normalizeOrRaw(sourceID))
	}
	if strings.TrimSpace(status) != "" {
		where = append(where, "status = ?")
		args = append(args, strings.TrimSpace(status))
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_id, relative_path, file_name, file_size, mod_time, status,
		       duration_ms, container, video_codec, audio_codec, width, height, bitrate,
		       thumbnail_path, COALESCE(probed_at, ''), created_at, updated_at
		FROM media_files
		WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, fmt.Errorf("list media files: %w", err)
	}
	defer rows.Close()

	cleanPrefix := cleanRelPrefix(dirPrefix)
	searchLower := strings.ToLower(strings.TrimSpace(search))

	var files []MediaFile
	for rows.Next() {
		var mf MediaFile
		if err := rows.Scan(
			&mf.ID, &mf.SourceID, &mf.RelativePath, &mf.FileName, &mf.FileSize, &mf.ModTime, &mf.Status,
			&mf.DurationMs, &mf.Container, &mf.VideoCodec, &mf.AudioCodec, &mf.Width, &mf.Height, &mf.Bitrate,
			&mf.ThumbnailPath, &mf.ProbedAt, &mf.CreatedAt, &mf.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan media file: %w", err)
		}
		if cleanPrefix != "" && !strings.HasPrefix(mf.RelativePath, cleanPrefix+"/") {
			continue
		}
		if searchLower != "" && !strings.Contains(strings.ToLower(mf.RelativePath), searchLower) {
			continue
		}
		files = append(files, mf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return NaturalCompare(files[i].RelativePath, files[j].RelativePath) < 0
	})
	return files, nil
}

func (s *Service) GetMediaFile(ctx context.Context, mediaFileID string) (*MediaFile, error) {
	mediaFileID = normalizeOrRaw(mediaFileID)
	var mf MediaFile
	err := s.db.QueryRowContext(ctx, `
		SELECT id, source_id, relative_path, file_name, file_size, mod_time, status,
		       duration_ms, container, video_codec, audio_codec, width, height, bitrate,
		       streams_json, thumbnail_path, COALESCE(probed_at, ''), created_at, updated_at
		FROM media_files WHERE id = ?`, mediaFileID).
		Scan(
			&mf.ID, &mf.SourceID, &mf.RelativePath, &mf.FileName, &mf.FileSize, &mf.ModTime, &mf.Status,
			&mf.DurationMs, &mf.Container, &mf.VideoCodec, &mf.AudioCodec, &mf.Width, &mf.Height, &mf.Bitrate,
			&mf.StreamsJSON, &mf.ThumbnailPath, &mf.ProbedAt, &mf.CreatedAt, &mf.UpdatedAt,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &mf, nil
}

func (s *Service) ListSourceSubtitles(ctx context.Context, sourceID, dirPrefix, search string, unmatchedOnly *bool) ([]IndexedSubtitle, error) {
	sourceID = normalizeOrRaw(sourceID)
	if _, err := s.GetSource(ctx, sourceID); err != nil {
		return nil, err
	}

	where := []string{"source_id = ?", "status = 'active'"}
	args := []any{sourceID}
	if unmatchedOnly != nil {
		if *unmatchedOnly {
			where = append(where, "(media_file_id IS NULL OR media_file_id = '')")
		} else {
			where = append(where, "(media_file_id IS NOT NULL AND media_file_id != '')")
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_id, relative_path, COALESCE(media_file_id, ''), language, title, format, is_default, manually_bound, status
		FROM subtitle_files
		WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, fmt.Errorf("list source subtitles: %w", err)
	}
	defer rows.Close()

	cleanPrefix := cleanRelPrefix(dirPrefix)
	searchLower := strings.ToLower(strings.TrimSpace(search))

	var subs []IndexedSubtitle
	for rows.Next() {
		var sub IndexedSubtitle
		var isDefInt, manualInt int
		if err := rows.Scan(
			&sub.ID, &sub.SourceID, &sub.RelativePath, &sub.MediaFileID,
			&sub.Language, &sub.Title, &sub.Format, &isDefInt, &manualInt, &sub.Status,
		); err != nil {
			return nil, fmt.Errorf("scan source subtitle: %w", err)
		}
		if cleanPrefix != "" && !strings.HasPrefix(sub.RelativePath, cleanPrefix+"/") {
			continue
		}
		if searchLower != "" && !strings.Contains(strings.ToLower(sub.RelativePath), searchLower) && !strings.Contains(strings.ToLower(sub.Title), searchLower) {
			continue
		}
		sub.FileName = path.Base(sub.RelativePath)
		sub.IsDefault = isDefInt != 0
		sub.ManuallyBound = manualInt != 0
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(subs, func(i, j int) bool {
		return NaturalCompare(subs[i].RelativePath, subs[j].RelativePath) < 0
	})
	return subs, nil
}

func (s *Service) BindSubtitle(ctx context.Context, subtitleID string, input BindSubtitleInput) (*IndexedSubtitle, error) {
	subtitleID = normalizeOrRaw(subtitleID)

	var sub IndexedSubtitle
	var isDefInt, manualInt int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, source_id, relative_path, COALESCE(media_file_id, ''), language, title, format, is_default, manually_bound, status
		FROM subtitle_files
		WHERE id = ?`, subtitleID).
		Scan(
			&sub.ID, &sub.SourceID, &sub.RelativePath, &sub.MediaFileID,
			&sub.Language, &sub.Title, &sub.Format, &isDefInt, &manualInt, &sub.Status,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load subtitle %s: %w", subtitleID, err)
	}

	nextMediaID := sql.NullString{}
	if sub.MediaFileID != "" {
		nextMediaID = sql.NullString{String: sub.MediaFileID, Valid: true}
	}
	nextManual := manualInt

	if input.ResetAuto {
		nextMediaID = sql.NullString{}
		nextManual = 0
	} else if input.MediaFileID != nil {
		trimmed := strings.TrimSpace(*input.MediaFileID)
		if trimmed == "" {
			nextMediaID = sql.NullString{}
			nextManual = 1
		} else {
			normMF := normalizeOrRaw(trimmed)
			var exists int
			err := s.db.QueryRowContext(ctx, `SELECT 1 FROM media_files WHERE id = ?`, normMF).Scan(&exists)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: media_file %s", ErrNotFound, normMF)
			}
			if err != nil {
				return nil, fmt.Errorf("verify media_file %s: %w", normMF, err)
			}
			nextMediaID = sql.NullString{String: normMF, Valid: true}
			nextManual = 1
		}
	}

	nextLang := sub.Language
	if input.Language != nil {
		if l := strings.TrimSpace(*input.Language); l != "" {
			nextLang = l
		}
	}
	nextTitle := sub.Title
	if input.Title != nil {
		nextTitle = strings.TrimSpace(*input.Title)
	}
	nextIsDef := isDefInt
	if input.IsDefault != nil {
		if *input.IsDefault {
			nextIsDef = 1
		} else {
			nextIsDef = 0
		}
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE subtitle_files
		SET media_file_id = ?, language = ?, title = ?, is_default = ?, manually_bound = ?
		WHERE id = ?`,
		nextMediaID, nextLang, nextTitle, nextIsDef, nextManual, subtitleID,
	); err != nil {
		return nil, fmt.Errorf("update subtitle binding %s: %w", subtitleID, err)
	}

	sub.FileName = path.Base(sub.RelativePath)
	sub.MediaFileID = ""
	if nextMediaID.Valid {
		sub.MediaFileID = nextMediaID.String
	}
	sub.Language = nextLang
	sub.Title = nextTitle
	sub.IsDefault = nextIsDef != 0
	sub.ManuallyBound = nextManual != 0
	return &sub, nil
}

func stripExt(name string) string {
	ext := path.Ext(name)
	if ext == "" {
		return name
	}
	return strings.TrimSuffix(name, ext)
}
