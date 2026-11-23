package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxImageDimension   = 4096
	defaultImageQuality = 85
)

// ServeImage resolves the image for a Library, Course, Chapter, or Lesson (applying
// Lesson -> thumbnail and Course -> first-lesson fallbacks), optionally resizes it
// while preserving aspect ratio, caches resized JPEGs under {dataDir}/cache/images,
// and handles conditional If-None-Match (304 Not Modified) with source-hash cache busting.
func (s *Service) ServeImage(w http.ResponseWriter, r *http.Request, itemID, imageType string, maxWidth, maxHeight int) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	srcPath, err := s.resolveItemImagePath(r.Context(), itemID, imageType)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		writeLookupError(w, err)
		return
	}

	srcHash, err := hashFileSHA256(srcPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("hash image: %v", err), http.StatusInternalServerError)
		return
	}

	if maxWidth < 0 {
		maxWidth = 0
	}
	if maxHeight < 0 {
		maxHeight = 0
	}
	if maxWidth > maxImageDimension {
		maxWidth = maxImageDimension
	}
	if maxHeight > maxImageDimension {
		maxHeight = maxImageDimension
	}

	if maxWidth == 0 && maxHeight == 0 {
		etag := fmt.Sprintf(`"%s-orig"`, srcHash[:16])
		f, err := os.Open(srcPath)
		if err != nil {
			writeLookupError(w, err)
			return
		}
		defer f.Close()

		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Type", imageContentType(srcPath))
		http.ServeContent(w, r, filepath.Base(srcPath), srcInfo.ModTime(), f)
		return
	}

	quality := defaultImageQuality
	if r != nil && r.URL != nil {
		if qStr := strings.TrimSpace(r.URL.Query().Get("quality")); qStr != "" {
			if q, convErr := strconv.Atoi(qStr); convErr == nil {
				if q < 1 {
					q = 1
				} else if q > 100 {
					q = 100
				}
				quality = q
			}
		}
	}

	etag := fmt.Sprintf(`"%s-%dx%d-q%d"`, srcHash[:16], maxWidth, maxHeight, quality)
	cacheDir := filepath.Join(s.dataDir, "cache", "images")
	cacheName := fmt.Sprintf("%s_%dx%d_q%d.jpg", srcHash[:24], maxWidth, maxHeight, quality)
	cachePath := filepath.Join(cacheDir, cacheName)

	if _, err := os.Stat(cachePath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := buildAndCacheResizedJPEG(srcPath, cacheDir, cachePath, maxWidth, maxHeight, quality); err != nil {
			http.Error(w, fmt.Sprintf("resize image: %v", err), http.StatusInternalServerError)
			return
		}
	}

	f, err := os.Open(cachePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	cacheInfo, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, cacheName, cacheInfo.ModTime(), f)
}

func (s *Service) resolveItemImagePath(ctx context.Context, rawItemID, rawImageType string) (string, error) {
	itemID := normalizeOrRaw(rawItemID)
	if itemID == "" {
		return "", sql.ErrNoRows
	}
	imgType := strings.ToLower(strings.TrimSpace(rawImageType))
	if imgType == "" {
		imgType = "primary"
	}

	// 1. Check libraries
	var libCover string
	err := s.db.QueryRowContext(ctx, `SELECT cover_path FROM libraries WHERE id = ?`, itemID).Scan(&libCover)
	if err == nil {
		if imgType == "primary" || imgType == "thumb" {
			if p, ok := resolveDataImagePath(s.dataDir, "covers", libCover); ok {
				return p, nil
			}
		}
		return "", os.ErrNotExist
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	// 2. Check courses
	var courseCover, courseBackdrop string
	err = s.db.QueryRowContext(ctx, `SELECT cover_path, backdrop_path FROM courses WHERE id = ?`, itemID).Scan(&courseCover, &courseBackdrop)
	if err == nil {
		if imgType == "backdrop" {
			if p, ok := resolveDataImagePath(s.dataDir, "covers", courseBackdrop); ok {
				return p, nil
			}
			return "", os.ErrNotExist
		}
		if imgType == "primary" || imgType == "thumb" {
			if p, ok := resolveDataImagePath(s.dataDir, "covers", courseCover); ok {
				return p, nil
			}
			return s.resolveFirstCourseLessonImage(ctx, itemID)
		}
		return "", os.ErrNotExist
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	// 3. Check chapters
	var chapCourseID string
	err = s.db.QueryRowContext(ctx, `SELECT course_id FROM chapters WHERE id = ?`, itemID).Scan(&chapCourseID)
	if err == nil {
		if imgType == "backdrop" {
			var bd string
			if qerr := s.db.QueryRowContext(ctx, `SELECT backdrop_path FROM courses WHERE id = ?`, chapCourseID).Scan(&bd); qerr == nil {
				if p, ok := resolveDataImagePath(s.dataDir, "covers", bd); ok {
					return p, nil
				}
			}
			return "", os.ErrNotExist
		}
		if imgType == "primary" || imgType == "thumb" {
			var cp string
			if qerr := s.db.QueryRowContext(ctx, `SELECT cover_path FROM courses WHERE id = ?`, chapCourseID).Scan(&cp); qerr == nil {
				if p, ok := resolveDataImagePath(s.dataDir, "covers", cp); ok {
					return p, nil
				}
			}
			if p, ferr := s.resolveFirstChapterLessonImage(ctx, itemID); ferr == nil {
				return p, nil
			}
			return s.resolveFirstCourseLessonImage(ctx, chapCourseID)
		}
		return "", os.ErrNotExist
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	// 4. Check lessons
	var lessonCover, thumbPath, lessonCourseID string
	err = s.db.QueryRowContext(ctx, `
		SELECT l.cover_path, mf.thumbnail_path, l.course_id
		FROM lessons l
		JOIN media_files mf ON mf.id = l.media_file_id
		WHERE l.id = ?`, itemID).Scan(&lessonCover, &thumbPath, &lessonCourseID)
	if err == nil {
		if imgType == "backdrop" {
			var bd string
			if qerr := s.db.QueryRowContext(ctx, `SELECT backdrop_path FROM courses WHERE id = ?`, lessonCourseID).Scan(&bd); qerr == nil {
				if p, ok := resolveDataImagePath(s.dataDir, "covers", bd); ok {
					return p, nil
				}
			}
			return "", os.ErrNotExist
		}
		if imgType == "primary" || imgType == "thumb" {
			if p, ok := resolveDataImagePath(s.dataDir, "covers", lessonCover); ok {
				return p, nil
			}
			if p, ok := resolveDataImagePath(s.dataDir, "thumbs", thumbPath); ok {
				return p, nil
			}
		}
		return "", os.ErrNotExist
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	return "", sql.ErrNoRows
}

func (s *Service) resolveFirstCourseLessonImage(ctx context.Context, courseID string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.cover_path, mf.thumbnail_path
		FROM lessons l
		JOIN chapters ch ON ch.id = l.chapter_id
		JOIN media_files mf ON mf.id = l.media_file_id
		WHERE l.course_id = ?
		ORDER BY ch.sort_order ASC, ch.created_at ASC, l.sort_order ASC, l.created_at ASC`, courseID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		var lCover, mfThumb string
		if err := rows.Scan(&lCover, &mfThumb); err != nil {
			return "", err
		}
		if p, ok := resolveDataImagePath(s.dataDir, "covers", lCover); ok {
			return p, nil
		}
		if p, ok := resolveDataImagePath(s.dataDir, "thumbs", mfThumb); ok {
			return p, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", os.ErrNotExist
}

func (s *Service) resolveFirstChapterLessonImage(ctx context.Context, chapterID string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.cover_path, mf.thumbnail_path
		FROM lessons l
		JOIN media_files mf ON mf.id = l.media_file_id
		WHERE l.chapter_id = ?
		ORDER BY l.sort_order ASC, l.created_at ASC`, chapterID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		var lCover, mfThumb string
		if err := rows.Scan(&lCover, &mfThumb); err != nil {
			return "", err
		}
		if p, ok := resolveDataImagePath(s.dataDir, "covers", lCover); ok {
			return p, nil
		}
		if p, ok := resolveDataImagePath(s.dataDir, "thumbs", mfThumb); ok {
			return p, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", os.ErrNotExist
}

func resolveDataImagePath(dataDir, defaultSubdir, storedPath string) (string, bool) {
	trimmed := strings.TrimSpace(filepath.ToSlash(storedPath))
	if trimmed == "" || dataDir == "" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "/") || filepath.IsAbs(trimmed) {
		return "", false
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == ".." {
			return "", false
		}
	}

	var candidates []string
	if strings.HasPrefix(trimmed, "covers/") || strings.HasPrefix(trimmed, "thumbs/") {
		candidates = append(candidates, filepath.Join(dataDir, filepath.FromSlash(trimmed)))
	}
	candidates = append(candidates,
		filepath.Join(dataDir, defaultSubdir, filepath.Base(trimmed)),
		filepath.Join(dataDir, filepath.FromSlash(trimmed)),
	)

	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return "", false
	}
	if resolvedData, rerr := filepath.EvalSymlinks(absData); rerr == nil {
		absData = resolvedData
	}
	for _, cand := range candidates {
		absCand, err := filepath.Abs(cand)
		if err != nil {
			continue
		}
		resolvedCand, rerr := filepath.EvalSymlinks(absCand)
		if rerr != nil {
			continue
		}
		if resolvedCand != absData && !strings.HasPrefix(resolvedCand, absData+string(filepath.Separator)) {
			continue
		}
		if info, err := os.Stat(resolvedCand); err == nil && !info.IsDir() && info.Size() > 0 {
			return resolvedCand, true
		}
	}
	return "", false
}

func buildAndCacheResizedJPEG(srcPath, cacheDir, cachePath string, maxWidth, maxHeight, quality int) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	srcImg, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return err
	}

	bounds := srcImg.Bounds()
	dstW, dstH := computeFitSize(bounds.Dx(), bounds.Dy(), maxWidth, maxHeight)

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), srcImg, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(cacheDir, ".img-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, cachePath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func computeFitSize(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return 1, 1
	}
	scale := 1.0
	if maxW > 0 {
		sw := float64(maxW) / float64(srcW)
		if sw < scale {
			scale = sw
		}
	}
	if maxH > 0 {
		sh := float64(maxH) / float64(srcH)
		if sh < scale {
			scale = sh
		}
	}
	dstW := int(math.Round(float64(srcW) * scale))
	dstH := int(math.Round(float64(srcH) * scale))
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	return dstW, dstH
}

func hashFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func imageContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}
