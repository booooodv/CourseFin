package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coursefin/internal/id"

	_ "golang.org/x/image/webp"
)

const (
	CoverSourceAI      = "ai"
	CoverSourceAIBatch = "ai_batch"
	CoverSourceUpload  = "upload"
	CoverSourceFrame   = "frame"
	CoverSourceImport  = "import"
	CoverSourceLegacy  = "legacy"
)

// CourseCover represents an image associated with a course.
type CourseCover struct {
	ID        string `json:"id"`
	CourseID  string `json:"course_id"`
	FilePath  string `json:"file_path"`
	Source    string `json:"source"`
	StyleID   string `json:"style_id,omitempty"`
	StyleName string `json:"style_name,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	IsActive  bool   `json:"is_active"`
	ImageURL  string `json:"image_url"`
	CreatedAt string `json:"created_at"`
}

// AddCourseCoverInput specifies image bytes and metadata to append as a new course cover.
type AddCourseCoverInput struct {
	Ext       string
	Data      []byte
	Source    string
	StyleID   string
	StyleName string
	Prompt    string
	Width     int
	Height    int
}

// ListCourseCovers returns all covers associated with a course, ordered newest-first.
func (s *Service) ListCourseCovers(ctx context.Context, courseID string) ([]CourseCover, error) {
	courseID = normalizeOrRaw(courseID)
	if courseID == "" {
		return nil, ErrInvalidInput
	}

	var currentCoverPath, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT cover_path, updated_at FROM courses WHERE id = ?`, courseID).
		Scan(&currentCoverPath, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup course for covers: %w", err)
	}

	if strings.TrimSpace(currentCoverPath) != "" {
		legacyTime := updatedAt
		if parsed, perr := time.Parse(time.RFC3339Nano, strings.TrimSpace(updatedAt)); perr == nil {
			legacyTime = coverTimeUTC(parsed)
		} else if strings.TrimSpace(legacyTime) == "" {
			legacyTime = coverTimeUTC(time.Now().UTC())
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO course_covers (id, course_id, file_path, source, style_id, style_name, prompt, width, height, created_at)
			VALUES (?, ?, ?, ?, '', '', '', 0, 0, ?)`,
			id.New(), courseID, strings.TrimSpace(currentCoverPath), CoverSourceLegacy, legacyTime,
		); err != nil {
			return nil, fmt.Errorf("backfill active course cover: %w", err)
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, course_id, file_path, source, style_id, style_name, prompt, width, height, created_at
		FROM course_covers
		WHERE course_id = ?
		ORDER BY created_at DESC, rowid DESC`, courseID)
	if err != nil {
		return nil, fmt.Errorf("list course covers: %w", err)
	}
	defer rows.Close()

	covers := make([]CourseCover, 0)
	for rows.Next() {
		var cc CourseCover
		if err := rows.Scan(
			&cc.ID, &cc.CourseID, &cc.FilePath, &cc.Source,
			&cc.StyleID, &cc.StyleName, &cc.Prompt,
			&cc.Width, &cc.Height, &cc.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan course cover: %w", err)
		}
		cc.IsActive = currentCoverPath != "" && cc.FilePath == currentCoverPath
		cc.ImageURL = courseCoverImageURL(cc.CourseID, cc.ID)
		covers = append(covers, cc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return covers, nil
}

// GetCourseCover loads a single cover owned by the specified course.
func (s *Service) GetCourseCover(ctx context.Context, courseID, coverID string) (*CourseCover, error) {
	courseID = normalizeOrRaw(courseID)
	coverID = normalizeOrRaw(coverID)
	if courseID == "" || coverID == "" {
		return nil, ErrNotFound
	}

	var cc CourseCover
	var currentCoverPath string
	err := s.db.QueryRowContext(ctx, `
		SELECT cc.id, cc.course_id, cc.file_path, cc.source, cc.style_id, cc.style_name, cc.prompt, cc.width, cc.height, cc.created_at, c.cover_path
		FROM course_covers cc
		JOIN courses c ON c.id = cc.course_id
		WHERE cc.id = ? AND cc.course_id = ?`, coverID, courseID).
		Scan(
			&cc.ID, &cc.CourseID, &cc.FilePath, &cc.Source,
			&cc.StyleID, &cc.StyleName, &cc.Prompt,
			&cc.Width, &cc.Height, &cc.CreatedAt, &currentCoverPath,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get course cover: %w", err)
	}
	cc.IsActive = currentCoverPath != "" && cc.FilePath == currentCoverPath
	cc.ImageURL = courseCoverImageURL(cc.CourseID, cc.ID)
	return &cc, nil
}

// AddCourseCover persists a new immutable cover image for a course and sets it as the active cover.
func (s *Service) AddCourseCover(ctx context.Context, courseID string, input AddCourseCoverInput) (*CourseCover, *CourseDetail, error) {
	courseID = normalizeOrRaw(courseID)
	if courseID == "" || len(input.Data) == 0 {
		return nil, nil, fmt.Errorf("%w: course_id and image data are required", ErrInvalidInput)
	}

	ext, err := normalizeCoverExt(input.Ext)
	if err != nil {
		return nil, nil, err
	}

	source := normalizeCoverSource(input.Source)
	w, h := input.Width, input.Height
	if w <= 0 || h <= 0 {
		dw, dh := detectImageDimensions(input.Data)
		if w <= 0 {
			w = dw
		}
		if h <= 0 {
			h = dh
		}
	}

	var existingCover, existingUpdated string
	err = s.db.QueryRowContext(ctx, `SELECT cover_path, updated_at FROM courses WHERE id = ?`, courseID).
		Scan(&existingCover, &existingUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("lookup course before adding cover: %w", err)
	}

	coverID := id.New()
	fileName := fmt.Sprintf("%s_cover_%s%s", courseID, coverID, ext)
	var destPath string
	if s.dataDir != "" {
		coversDir := filepath.Join(s.dataDir, "covers")
		if err := os.MkdirAll(coversDir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create covers dir: %w", err)
		}
		destPath = filepath.Join(coversDir, fileName)
		if err := writeCoverFileAtomically(coversDir, destPath, input.Data); err != nil {
			return nil, nil, fmt.Errorf("write cover file: %w", err)
		}
	}

	committed := false
	defer func() {
		if !committed && destPath != "" {
			_ = os.Remove(destPath)
		}
	}()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx, `SELECT cover_path, updated_at FROM courses WHERE id = ?`, courseID).
		Scan(&existingCover, &existingUpdated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("lock course for cover insert: %w", err)
	}

	nowTime := time.Now().UTC()
	if err := ensureLegacyCoverTx(ctx, tx, courseID, existingCover, existingUpdated, nowTime); err != nil {
		return nil, nil, err
	}

	nowStr := coverTimeUTC(nowTime)
	styleID := strings.TrimSpace(input.StyleID)
	styleName := strings.TrimSpace(input.StyleName)
	prompt := strings.TrimSpace(input.Prompt)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO course_covers (id, course_id, file_path, source, style_id, style_name, prompt, width, height, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		coverID, courseID, fileName, source, styleID, styleName, prompt, w, h, nowStr,
	); err != nil {
		return nil, nil, fmt.Errorf("insert course cover: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE courses SET cover_path = ?, updated_at = ? WHERE id = ?`, fileName, nowStr, courseID); err != nil {
		return nil, nil, fmt.Errorf("activate new course cover: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit new course cover: %w", err)
	}
	committed = true

	detail, err := s.GetCourseDetail(ctx, courseID)
	if err != nil {
		return nil, nil, err
	}
	created := &CourseCover{
		ID:        coverID,
		CourseID:  courseID,
		FilePath:  fileName,
		Source:    source,
		StyleID:   styleID,
		StyleName: styleName,
		Prompt:    prompt,
		Width:     w,
		Height:    h,
		IsActive:  true,
		ImageURL:  courseCoverImageURL(courseID, coverID),
		CreatedAt: nowStr,
	}
	return created, detail, nil
}

// ActivateCourseCover sets an existing course cover as the course's active cover.
func (s *Service) ActivateCourseCover(ctx context.Context, courseID, coverID string) (*CourseDetail, error) {
	courseID = normalizeOrRaw(courseID)
	coverID = normalizeOrRaw(coverID)
	if courseID == "" || coverID == "" {
		return nil, ErrNotFound
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var filePath string
	err = tx.QueryRowContext(ctx, `SELECT file_path FROM course_covers WHERE id = ? AND course_id = ?`, coverID, courseID).
		Scan(&filePath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup cover to activate: %w", err)
	}

	nowStr := coverTimeUTC(time.Now().UTC())
	res, err := tx.ExecContext(ctx, `UPDATE courses SET cover_path = ?, updated_at = ? WHERE id = ?`, filePath, nowStr, courseID)
	if err != nil {
		return nil, fmt.Errorf("update course active cover: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit cover activation: %w", err)
	}
	return s.GetCourseDetail(ctx, courseID)
}

// DeleteCourseCover deletes a cover record and its disk file; if it was active, the newest remaining cover becomes active.
func (s *Service) DeleteCourseCover(ctx context.Context, courseID, coverID string) (*CourseDetail, error) {
	courseID = normalizeOrRaw(courseID)
	coverID = normalizeOrRaw(coverID)
	if courseID == "" || coverID == "" {
		return nil, ErrNotFound
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var deletedFilePath, currentCoverPath string
	err = tx.QueryRowContext(ctx, `
		SELECT cc.file_path, c.cover_path
		FROM course_covers cc
		JOIN courses c ON c.id = cc.course_id
		WHERE cc.id = ? AND cc.course_id = ?`, coverID, courseID).
		Scan(&deletedFilePath, &currentCoverPath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup cover to delete: %w", err)
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM course_covers WHERE id = ? AND course_id = ?`, coverID, courseID)
	if err != nil {
		return nil, fmt.Errorf("delete course cover: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}

	if deletedFilePath == currentCoverPath {
		var nextCoverPath string
		err := tx.QueryRowContext(ctx, `
			SELECT file_path
			FROM course_covers
			WHERE course_id = ?
			ORDER BY created_at DESC, rowid DESC
			LIMIT 1`, courseID).Scan(&nextCoverPath)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("find fallback course cover: %w", err)
		}
		nowStr := coverTimeUTC(time.Now().UTC())
		if _, err := tx.ExecContext(ctx, `UPDATE courses SET cover_path = ?, updated_at = ? WHERE id = ?`, nextCoverPath, nowStr, courseID); err != nil {
			return nil, fmt.Errorf("update fallback course cover: %w", err)
		}
	}

	var coverRefs, courseRefs int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM course_covers WHERE file_path = ?`, deletedFilePath).Scan(&coverRefs); err != nil {
		return nil, fmt.Errorf("count remaining cover refs: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM courses WHERE cover_path = ?`, deletedFilePath).Scan(&courseRefs); err != nil {
		return nil, fmt.Errorf("count remaining course refs: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit delete course cover: %w", err)
	}

	if coverRefs == 0 && courseRefs == 0 && s.dataDir != "" && strings.TrimSpace(deletedFilePath) != "" {
		cleanBase := filepath.Base(strings.TrimSpace(deletedFilePath))
		if cleanBase != "" && cleanBase != "." && cleanBase != ".." {
			_ = os.Remove(filepath.Join(s.dataDir, "covers", cleanBase))
		}
	}

	return s.GetCourseDetail(ctx, courseID)
}

func ensureLegacyCoverTx(ctx context.Context, tx *sql.Tx, courseID, currentCoverPath, existingUpdatedAt string, beforeTime time.Time) error {
	trimmed := strings.TrimSpace(currentCoverPath)
	if trimmed == "" {
		return nil
	}

	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM course_covers WHERE course_id = ? AND file_path = ? LIMIT 1`, courseID, trimmed).Scan(&exists)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check existing legacy cover: %w", err)
	}

	beforeStr := coverTimeUTC(beforeTime)
	legacyTime := coverTimeUTC(beforeTime.Add(-time.Millisecond))
	if parsed, perr := time.Parse(time.RFC3339Nano, strings.TrimSpace(existingUpdatedAt)); perr == nil {
		cand := coverTimeUTC(parsed)
		if cand < beforeStr {
			legacyTime = cand
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO course_covers (id, course_id, file_path, source, style_id, style_name, prompt, width, height, created_at)
		VALUES (?, ?, ?, ?, '', '', '', 0, 0, ?)`,
		id.New(), courseID, trimmed, CoverSourceLegacy, legacyTime,
	); err != nil {
		return fmt.Errorf("backfill legacy course cover: %w", err)
	}
	return nil
}

func coverTimeUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func courseCoverImageURL(courseID, coverID string) string {
	return "/api/v1/courses/" + courseID + "/covers/" + coverID + "/image"
}

func normalizeCoverExt(rawExt string) (string, error) {
	ext := strings.ToLower(strings.TrimSpace(rawExt))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return ext, nil
	default:
		return "", fmt.Errorf("%w: unsupported cover extension %q", ErrInvalidInput, rawExt)
	}
}

func normalizeCoverSource(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case CoverSourceAI:
		return CoverSourceAI
	case CoverSourceAIBatch:
		return CoverSourceAIBatch
	case CoverSourceUpload:
		return CoverSourceUpload
	case CoverSourceFrame:
		return CoverSourceFrame
	case CoverSourceImport:
		return CoverSourceImport
	case CoverSourceLegacy:
		return CoverSourceLegacy
	default:
		return CoverSourceUpload
	}
}

func detectImageDimensions(data []byte) (int, int) {
	if len(data) == 0 {
		return 0, 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func writeCoverFileAtomically(dir, destPath string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".cover-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
