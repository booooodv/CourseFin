package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"coursefin/internal/id"
)

type PreviewLesson struct {
	MediaFileID  string `json:"media_file_id"`
	RelativePath string `json:"relative_path"`
	FileName     string `json:"file_name"`
	Title        string `json:"title"`
	DurationMs   int64  `json:"duration_ms"`
	SortOrder    int    `json:"sort_order"`
}

type PreviewChapter struct {
	Title     string          `json:"title"`
	SortOrder int             `json:"sort_order"`
	Lessons   []PreviewLesson `json:"lessons"`
}

type ImportPreview struct {
	SourceID     string           `json:"source_id"`
	DirRelPath   string           `json:"dir_rel_path"`
	CourseTitle  string           `json:"course_title"`
	CoverRelPath string           `json:"cover_rel_path,omitempty"`
	Chapters     []PreviewChapter `json:"chapters"`
	TotalFiles   int              `json:"total_files"`
}

type CommitImportLesson struct {
	MediaFileID string `json:"media_file_id"`
	Title       string `json:"title"`
}

type CommitImportChapter struct {
	Title   string               `json:"title"`
	Lessons []CommitImportLesson `json:"lessons"`
}

type CommitImportInput struct {
	SourceID     string                `json:"source_id"`
	DirRelPath   string                `json:"dir_rel_path"`
	LibraryID    string                `json:"library_id"`
	Title        string                `json:"title"`
	Overview     string                `json:"overview"`
	Instructor   string                `json:"instructor"`
	Tags         []string              `json:"tags"`
	CoverRelPath string                `json:"cover_rel_path"`
	Chapters     []CommitImportChapter `json:"chapters"`
}

var coverBaseOrder = map[string]int{
	"cover":  1,
	"poster": 2,
	"folder": 3,
	"thumb":  4,
}

var coverExtOrder = map[string]int{
	".jpg":  1,
	".jpeg": 2,
	".png":  3,
	".webp": 4,
}

func relWithinRoot(rootRelPath, fileRelPath string) (string, bool) {
	if rootRelPath == "" {
		return fileRelPath, true
	}
	prefix := rootRelPath + "/"
	if !strings.HasPrefix(fileRelPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(fileRelPath, prefix), true
}

func mapFileToChapterAndTitle(courseRootRelPath, fileRelPath, fileName string) (chapterTitle, lessonTitle string) {
	rel, ok := relWithinRoot(courseRootRelPath, fileRelPath)
	if !ok {
		return defaultChapterTitle, stripExt(fileName)
	}
	slashIdx := strings.IndexByte(rel, '/')
	if slashIdx < 0 {
		return defaultChapterTitle, stripExt(rel)
	}
	chapterTitle = rel[:slashIdx]
	subRel := rel[slashIdx+1:]
	subNoExt := stripExt(subRel)
	parts := strings.Split(subNoExt, "/")
	lessonTitle = strings.Join(parts, " / ")
	return chapterTitle, lessonTitle
}

func (s *Service) detectCoverImage(ctx context.Context, src *StorageSource, dirRelPath string) string {
	provider, err := s.registry.ProviderFor(src.Driver, src.RootPath, src.ConfigJSON)
	if err != nil {
		return ""
	}
	entries, err := provider.Walk(ctx, dirRelPath)
	if err != nil {
		return ""
	}

	targetDir := dirRelPath
	if targetDir == "" {
		targetDir = "."
	}

	bestScore := 999
	bestRelPath := ""
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		if path.Dir(entry.RelPath) != targetDir {
			continue
		}
		ext := strings.ToLower(path.Ext(entry.Name))
		extRank, okExt := coverExtOrder[ext]
		if !okExt {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(entry.Name, path.Ext(entry.Name)))
		baseRank, okBase := coverBaseOrder[stem]
		if !okBase {
			continue
		}
		score := baseRank*10 + extRank
		if score < bestScore {
			bestScore = score
			bestRelPath = entry.RelPath
		}
	}
	return bestRelPath
}

func (s *Service) PreviewDirectoryImport(ctx context.Context, sourceID, dirRelPath string) (*ImportPreview, error) {
	src, err := s.GetSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	cleanDir := cleanRelPrefix(dirRelPath)
	courseTitle := src.Name
	if cleanDir != "" {
		courseTitle = path.Base(cleanDir)
	}

	files, err := s.ListMediaFiles(ctx, src.ID, cleanDir, "", "active")
	if err != nil {
		return nil, err
	}

	type groupedItem struct {
		mf           MediaFile
		subRel       string
		lessonTitle  string
		chapterTitle string
	}

	var rootItems []groupedItem
	subMap := make(map[string][]groupedItem)

	for _, mf := range files {
		rel, ok := relWithinRoot(cleanDir, mf.RelativePath)
		if !ok {
			continue
		}
		chTitle, lTitle := mapFileToChapterAndTitle(cleanDir, mf.RelativePath, mf.FileName)
		slashIdx := strings.IndexByte(rel, '/')
		if slashIdx < 0 {
			rootItems = append(rootItems, groupedItem{
				mf:           mf,
				subRel:       rel,
				lessonTitle:  lTitle,
				chapterTitle: chTitle,
			})
		} else {
			subRel := rel[slashIdx+1:]
			subMap[chTitle] = append(subMap[chTitle], groupedItem{
				mf:           mf,
				subRel:       subRel,
				lessonTitle:  lTitle,
				chapterTitle: chTitle,
			})
		}
	}

	var chapters []PreviewChapter
	chapterOrder := 1

	if len(rootItems) > 0 {
		sort.Slice(rootItems, func(i, j int) bool {
			return NaturalCompare(rootItems[i].subRel, rootItems[j].subRel) < 0
		})
		lessons := make([]PreviewLesson, 0, len(rootItems))
		for i, item := range rootItems {
			lessons = append(lessons, PreviewLesson{
				MediaFileID:  item.mf.ID,
				RelativePath: item.mf.RelativePath,
				FileName:     item.mf.FileName,
				Title:        item.lessonTitle,
				DurationMs:   item.mf.DurationMs,
				SortOrder:    i + 1,
			})
		}
		chapters = append(chapters, PreviewChapter{
			Title:     defaultChapterTitle,
			SortOrder: chapterOrder,
			Lessons:   lessons,
		})
		chapterOrder++
	}

	subNames := make([]string, 0, len(subMap))
	for name := range subMap {
		subNames = append(subNames, name)
	}
	sort.Slice(subNames, func(i, j int) bool {
		return NaturalCompare(subNames[i], subNames[j]) < 0
	})

	for _, chName := range subNames {
		items := subMap[chName]
		sort.Slice(items, func(i, j int) bool {
			return NaturalCompare(items[i].subRel, items[j].subRel) < 0
		})
		lessons := make([]PreviewLesson, 0, len(items))
		for i, item := range items {
			lessons = append(lessons, PreviewLesson{
				MediaFileID:  item.mf.ID,
				RelativePath: item.mf.RelativePath,
				FileName:     item.mf.FileName,
				Title:        item.lessonTitle,
				DurationMs:   item.mf.DurationMs,
				SortOrder:    i + 1,
			})
		}
		chapters = append(chapters, PreviewChapter{
			Title:     chName,
			SortOrder: chapterOrder,
			Lessons:   lessons,
		})
		chapterOrder++
	}

	return &ImportPreview{
		SourceID:     src.ID,
		DirRelPath:   cleanDir,
		CourseTitle:  courseTitle,
		CoverRelPath: s.detectCoverImage(ctx, src, cleanDir),
		Chapters:     chapters,
		TotalFiles:   len(files),
	}, nil
}

func (s *Service) CommitDirectoryImport(ctx context.Context, input CommitImportInput) (*CourseDetail, error) {
	src, err := s.GetSource(ctx, input.SourceID)
	if err != nil {
		return nil, err
	}
	libID, err := s.resolveLibraryID(ctx, input.LibraryID)
	if err != nil {
		return nil, err
	}

	cleanDir := cleanRelPrefix(input.DirRelPath)
	chapters := input.Chapters
	coverRelPath := strings.TrimSpace(input.CoverRelPath)

	if len(chapters) == 0 {
		preview, err := s.PreviewDirectoryImport(ctx, src.ID, cleanDir)
		if err != nil {
			return nil, err
		}
		for _, pch := range preview.Chapters {
			var ls []CommitImportLesson
			for _, pl := range pch.Lessons {
				ls = append(ls, CommitImportLesson{
					MediaFileID: pl.MediaFileID,
					Title:       pl.Title,
				})
			}
			chapters = append(chapters, CommitImportChapter{
				Title:   pch.Title,
				Lessons: ls,
			})
		}
		if coverRelPath == "" {
			coverRelPath = preview.CoverRelPath
		}
		if strings.TrimSpace(input.Title) == "" {
			input.Title = preview.CourseTitle
		}
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		if cleanDir != "" {
			title = path.Base(cleanDir)
		} else {
			title = src.Name
		}
	}

	courseID := id.New()
	var copiedCoverName string
	var copiedCoverAbs string
	if coverRelPath != "" {
		name, abs, err := s.copyCoverFromStorage(ctx, src, courseID, coverRelPath)
		if err != nil {
			return nil, err
		}
		copiedCoverName = name
		copiedCoverAbs = abs
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		if copiedCoverAbs != "" {
			_ = os.Remove(copiedCoverAbs)
		}
		return nil, err
	}
	committed := false
	defer func() {
		tx.Rollback()
		if !committed && copiedCoverAbs != "" {
			_ = os.Remove(copiedCoverAbs)
		}
	}()

	now := nowUTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO courses (
			id, library_id, title, sort_title, overview, instructor,
			cover_path, backdrop_path, source_id, source_root_path, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
		courseID, libID, title, title, strings.TrimSpace(input.Overview), strings.TrimSpace(input.Instructor),
		copiedCoverName, src.ID, cleanDir, now, now,
	); err != nil {
		return nil, fmt.Errorf("insert imported course: %w", err)
	}

	if len(chapters) == 0 {
		chapters = []CommitImportChapter{{Title: defaultChapterTitle}}
	}

	for ci, ch := range chapters {
		chTitle := strings.TrimSpace(ch.Title)
		if chTitle == "" {
			chTitle = fmt.Sprintf("Chapter %d", ci+1)
		}
		chID := id.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chapters (id, course_id, title, sort_order, overview, created_at, updated_at)
			VALUES (?, ?, ?, ?, '', ?, ?)`,
			chID, courseID, chTitle, ci+1, now, now,
		); err != nil {
			return nil, fmt.Errorf("insert chapter %q: %w", chTitle, err)
		}

		for li, l := range ch.Lessons {
			mfID := normalizeOrRaw(l.MediaFileID)
			var fileName string
			err := tx.QueryRowContext(ctx, `SELECT file_name FROM media_files WHERE id = ?`, mfID).Scan(&fileName)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: media_file %s", ErrNotFound, mfID)
			}
			if err != nil {
				return nil, err
			}

			lTitle := strings.TrimSpace(l.Title)
			if lTitle == "" {
				lTitle = stripExt(fileName)
			}

			if _, err := tx.ExecContext(ctx, `
				INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, overview, sort_order, cover_path, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, '', ?, '', ?, ?)`,
				id.New(), courseID, chID, mfID, lTitle, li+1, now, now,
			); err != nil {
				return nil, fmt.Errorf("insert lesson %q: %w", lTitle, err)
			}
		}
	}

	if len(input.Tags) > 0 {
		if err := setCourseTagsTx(ctx, tx, courseID, input.Tags); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	return s.GetCourseDetail(ctx, courseID)
}

func (s *Service) copyCoverFromStorage(ctx context.Context, src *StorageSource, courseID, coverRelPath string) (string, string, error) {
	ext := strings.ToLower(path.Ext(coverRelPath))
	if _, ok := coverExtOrder[ext]; !ok {
		return "", "", fmt.Errorf("%w: unsupported cover extension %q", ErrInvalidInput, ext)
	}

	provider, err := s.registry.ProviderFor(src.Driver, src.RootPath, src.ConfigJSON)
	if err != nil {
		return "", "", fmt.Errorf("resolve provider for cover: %w", err)
	}

	rc, err := provider.Open(ctx, coverRelPath)
	if err != nil {
		return "", "", fmt.Errorf("open cover %q: %w", coverRelPath, err)
	}
	defer rc.Close()

	coversDir := filepath.Join(s.dataDir, "covers")
	if err := os.MkdirAll(coversDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create covers dir: %w", err)
	}

	coverName := courseID + ext
	destPath := filepath.Join(coversDir, coverName)
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", "", fmt.Errorf("create cover file: %w", err)
	}

	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		_ = os.Remove(destPath)
		return "", "", fmt.Errorf("copy cover file: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(destPath)
		return "", "", fmt.Errorf("close cover file: %w", err)
	}

	return coverName, destPath, nil
}

func (s *Service) CheckNewFiles(ctx context.Context, courseID string) ([]MediaFile, error) {
	courseID = normalizeOrRaw(courseID)

	var sourceID, sourceRoot sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT source_id, source_root_path FROM courses WHERE id = ?`, courseID).
		Scan(&sourceID, &sourceRoot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !sourceID.Valid || strings.TrimSpace(sourceID.String) == "" {
		return []MediaFile{}, nil
	}

	allFiles, err := s.ListMediaFiles(ctx, sourceID.String, sourceRoot.String, "", "active")
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT media_file_id FROM lessons WHERE course_id = ?`, courseID)
	if err != nil {
		return nil, fmt.Errorf("query existing course media files: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var mfID string
		if err := rows.Scan(&mfID); err != nil {
			return nil, err
		}
		existing[mfID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	newFiles := make([]MediaFile, 0)
	for _, mf := range allFiles {
		if !existing[mf.ID] {
			newFiles = append(newFiles, mf)
		}
	}
	return newFiles, nil
}

func (s *Service) AppendNewFiles(ctx context.Context, courseID string, mediaFileIDs []string) error {
	courseID = normalizeOrRaw(courseID)
	if len(mediaFileIDs) == 0 {
		return nil
	}

	var sourceRoot sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT source_root_path FROM courses WHERE id = ?`, courseID).Scan(&sourceRoot)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	courseRoot := ""
	if sourceRoot.Valid {
		courseRoot = cleanRelPrefix(sourceRoot.String)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	existingMF := make(map[string]bool)
	lRows, err := tx.QueryContext(ctx, `SELECT DISTINCT media_file_id FROM lessons WHERE course_id = ?`, courseID)
	if err != nil {
		return err
	}
	for lRows.Next() {
		var mid string
		if err := lRows.Scan(&mid); err != nil {
			lRows.Close()
			return err
		}
		existingMF[mid] = true
	}
	if err := lRows.Err(); err != nil {
		lRows.Close()
		return err
	}
	lRows.Close()

	var toAppend []MediaFile
	seenReq := make(map[string]bool, len(mediaFileIDs))
	for _, rawID := range mediaFileIDs {
		mfID := normalizeOrRaw(rawID)
		if mfID == "" || seenReq[mfID] || existingMF[mfID] {
			continue
		}
		seenReq[mfID] = true

		var mf MediaFile
		err := tx.QueryRowContext(ctx, `
			SELECT id, source_id, relative_path, file_name
			FROM media_files WHERE id = ?`, mfID).
			Scan(&mf.ID, &mf.SourceID, &mf.RelativePath, &mf.FileName)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: media_file %s", ErrNotFound, mfID)
		}
		if err != nil {
			return err
		}
		toAppend = append(toAppend, mf)
	}

	if len(toAppend) == 0 {
		return nil
	}

	sort.Slice(toAppend, func(i, j int) bool {
		return NaturalCompare(toAppend[i].RelativePath, toAppend[j].RelativePath) < 0
	})

	chapterByTitle := make(map[string]string)
	maxLessonOrder := make(map[string]int)
	maxChapterOrder := 0

	chRows, err := tx.QueryContext(ctx, `
		SELECT ch.id, ch.title, ch.sort_order, COALESCE(MAX(l.sort_order), 0)
		FROM chapters ch
		LEFT JOIN lessons l ON l.chapter_id = ch.id
		WHERE ch.course_id = ?
		GROUP BY ch.id
		ORDER BY ch.sort_order ASC, ch.created_at ASC`, courseID)
	if err != nil {
		return err
	}
	for chRows.Next() {
		var cid, ctitle string
		var corder, lmax int
		if err := chRows.Scan(&cid, &ctitle, &corder, &lmax); err != nil {
			chRows.Close()
			return err
		}
		if _, exists := chapterByTitle[ctitle]; !exists {
			chapterByTitle[ctitle] = cid
		}
		maxLessonOrder[cid] = lmax
		if corder > maxChapterOrder {
			maxChapterOrder = corder
		}
	}
	if err := chRows.Err(); err != nil {
		chRows.Close()
		return err
	}
	chRows.Close()

	now := nowUTC()
	for _, mf := range toAppend {
		chTitle, lTitle := mapFileToChapterAndTitle(courseRoot, mf.RelativePath, mf.FileName)
		chapID, ok := chapterByTitle[chTitle]
		if !ok {
			maxChapterOrder++
			chapID = id.New()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO chapters (id, course_id, title, sort_order, overview, created_at, updated_at)
				VALUES (?, ?, ?, ?, '', ?, ?)`,
				chapID, courseID, chTitle, maxChapterOrder, now, now,
			); err != nil {
				return fmt.Errorf("create chapter %q during append: %w", chTitle, err)
			}
			chapterByTitle[chTitle] = chapID
			maxLessonOrder[chapID] = 0
		}

		maxLessonOrder[chapID]++
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, overview, sort_order, cover_path, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, '', ?, '', ?, ?)`,
			id.New(), courseID, chapID, mf.ID, lTitle, maxLessonOrder[chapID], now, now,
		); err != nil {
			return fmt.Errorf("append lesson %q: %w", lTitle, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE courses SET updated_at = ? WHERE id = ?`, now, courseID); err != nil {
		return err
	}

	return tx.Commit()
}
