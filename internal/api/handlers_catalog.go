package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"coursefin/internal/catalog"
	"coursefin/internal/id"

	"github.com/go-chi/chi/v5"
)

const maxCoverUploadBytes = 20 << 20 // 20 MiB

var allowedCoverExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
	".gif":  true,
}

func (h *handler) handleListLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := h.cat.ListLibraries(r.Context())
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if libs == nil {
		libs = []catalog.Library{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": libs})
}

func (h *handler) handleCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	lib, err := h.cat.CreateLibrary(r.Context(), req.Name)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, lib)
}

func (h *handler) handleUpdateLibrary(w http.ResponseWriter, r *http.Request) {
	libID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	lib, err := h.cat.UpdateLibrary(r.Context(), libID, req.Name)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lib)
}

func (h *handler) handleDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	libID := normalizeID(chi.URLParam(r, "id"))
	q := r.URL.Query()
	moveTo := strings.TrimSpace(q.Get("move_to_library_id"))
	forceRaw := strings.ToLower(strings.TrimSpace(q.Get("force")))
	force := forceRaw == "true" || forceRaw == "1"

	if err := h.cat.DeleteLibraryWithOptions(r.Context(), libID, catalog.DeleteLibraryOptions{
		MoveToLibraryID: moveTo,
		Force:           force,
	}); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleListCourses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := catalog.CourseFilter{
		LibraryID: q.Get("library_id"),
		Tag:       q.Get("tag"),
		Search:    q.Get("search"),
		SortBy:    q.Get("sort_by"),
	}
	if raw := strings.TrimSpace(q.Get("is_played")); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			filter.IsPlayed = &b
		}
	}
	if raw := strings.TrimSpace(q.Get("sort_desc")); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			filter.SortDesc = b
		}
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			filter.Offset = n
		}
	}

	courses, err := h.cat.ListCourses(r.Context(), filter)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if courses == nil {
		courses = []catalog.CourseSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"courses": courses})
}

func (h *handler) handleCreateCourse(w http.ResponseWriter, r *http.Request) {
	var req catalog.CreateCourseInput
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	course, err := h.cat.CreateCourse(r.Context(), req)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, course)
}

func (h *handler) handleGetCourse(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	course, err := h.cat.GetCourseDetail(r.Context(), courseID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, course)
}

func (h *handler) handleUpdateCourse(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	var req catalog.UpdateCourseInput
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	course, err := h.cat.UpdateCourse(r.Context(), courseID, req)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, course)
}

func (h *handler) handleDeleteCourse(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	if err := h.cat.DeleteCourse(r.Context(), courseID); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleUploadCourseCover(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	course, err := h.cat.GetCourseDetail(r.Context(), courseID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}

	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(ct, "multipart/form-data") {
		fileName, err := h.saveUploadedCourseImage(r, courseID, "cover", course.CoverPath)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := h.cat.UpdateCourse(r.Context(), courseID, catalog.UpdateCourseInput{
			CoverPath: &fileName,
		})
		if err != nil {
			writeCatalogError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
		return
	}

	// JSON frame pick from a lesson thumbnail
	var req struct {
		LessonID string `json:"lesson_id"`
	}
	if err := decodeJSON(r, &req, false); err != nil || strings.TrimSpace(req.LessonID) == "" {
		writeError(w, http.StatusBadRequest, "multipart file or lesson_id is required")
		return
	}

	lesson, err := h.cat.GetLesson(r.Context(), req.LessonID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if lesson.CourseID != course.ID {
		writeError(w, http.StatusBadRequest, "lesson does not belong to this course")
		return
	}

	srcPath, err := h.resolveLessonThumbnailSource(lesson)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	ext := strings.ToLower(filepath.Ext(srcPath))
	if !allowedCoverExts[ext] {
		ext = ".jpg"
	}
	destName := courseID + "_cover" + ext
	if err := h.copyFileToCoversDir(srcPath, destName, course.CoverPath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	updated, err := h.cat.UpdateCourse(r.Context(), courseID, catalog.UpdateCourseInput{
		CoverPath: &destName,
	})
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *handler) handleUploadCourseBackdrop(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	course, err := h.cat.GetCourseDetail(r.Context(), courseID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}

	fileName, err := h.saveUploadedCourseImage(r, courseID, "backdrop", course.BackdropPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.cat.UpdateCourse(r.Context(), courseID, catalog.UpdateCourseInput{
		BackdropPath: &fileName,
	})
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *handler) saveUploadedCourseImage(r *http.Request, courseID, kind, prevFileName string) (string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxCoverUploadBytes)
	if err := r.ParseMultipartForm(maxCoverUploadBytes); err != nil {
		return "", fmt.Errorf("parse multipart upload: %w", err)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", fmt.Errorf("missing multipart field 'file': %w", err)
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxCoverUploadBytes))
	if err != nil {
		return "", fmt.Errorf("read uploaded file: %w", err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("uploaded file is empty")
	}

	detected := http.DetectContentType(raw)
	var detectedExt string
	switch detected {
	case "image/jpeg":
		detectedExt = ".jpg"
	case "image/png":
		detectedExt = ".png"
	case "image/webp":
		detectedExt = ".webp"
	case "image/gif":
		detectedExt = ".gif"
	default:
		return "", fmt.Errorf("unsupported image format (%s)", detected)
	}

	ext := strings.ToLower(path.Ext(header.Filename))
	if !allowedCoverExts[ext] {
		ext = detectedExt
	}

	coversDir := filepath.Join(h.dataDir, "covers")
	if err := os.MkdirAll(coversDir, 0o755); err != nil {
		return "", fmt.Errorf("create covers dir: %w", err)
	}

	destName := fmt.Sprintf("%s_%s%s", courseID, kind, ext)
	destPath := filepath.Join(coversDir, destName)
	if err := os.WriteFile(destPath, raw, 0o644); err != nil {
		return "", fmt.Errorf("save image: %w", err)
	}
	if prevFileName != "" && filepath.Base(prevFileName) != destName {
		_ = os.Remove(filepath.Join(coversDir, filepath.Base(prevFileName)))
	}
	return destName, nil
}

func (h *handler) resolveLessonThumbnailSource(lesson *catalog.LessonDetail) (string, error) {
	var candidates []string
	if lesson.CoverPath != "" {
		candidates = append(candidates, filepath.Join(h.dataDir, "covers", filepath.Base(lesson.CoverPath)))
	}
	if lesson.ThumbnailPath != "" {
		candidates = append(candidates, filepath.Join(h.dataDir, "thumbs", filepath.Base(lesson.ThumbnailPath)))
	}
	for _, cand := range candidates {
		if info, err := os.Stat(cand); err == nil && !info.IsDir() && info.Size() > 0 {
			return cand, nil
		}
	}
	return "", fmt.Errorf("lesson has no extracted thumbnail")
}

func (h *handler) copyFileToCoversDir(srcPath, destName, prevFileName string) error {
	coversDir := filepath.Join(h.dataDir, "covers")
	if err := os.MkdirAll(coversDir, 0o755); err != nil {
		return fmt.Errorf("create covers dir: %w", err)
	}
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read thumbnail: %w", err)
	}
	destPath := filepath.Join(coversDir, destName)
	if err := os.WriteFile(destPath, raw, 0o644); err != nil {
		return fmt.Errorf("write cover: %w", err)
	}
	if prevFileName != "" && filepath.Base(prevFileName) != destName {
		_ = os.Remove(filepath.Join(coversDir, filepath.Base(prevFileName)))
	}
	return nil
}

func (h *handler) handleGetCourseNewFiles(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	files, err := h.cat.CheckNewFiles(r.Context(), courseID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if files == nil {
		files = []catalog.MediaFile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (h *handler) handleAppendCourseNewFiles(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		MediaFileIDs []string `json:"media_file_ids"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.cat.AppendNewFiles(r.Context(), courseID, req.MediaFileIDs); err != nil {
		writeCatalogError(w, err)
		return
	}
	course, err := h.cat.GetCourseDetail(r.Context(), courseID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, course)
}

func (h *handler) handleCreateChapter(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ch, err := h.cat.CreateChapter(r.Context(), courseID, req.Title)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (h *handler) handleUpdateChapter(w http.ResponseWriter, r *http.Request) {
	chapterID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		Title    string `json:"title"`
		Overview string `json:"overview"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ch, err := h.cat.UpdateChapter(r.Context(), chapterID, req.Title, req.Overview)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (h *handler) handleDeleteChapter(w http.ResponseWriter, r *http.Request) {
	chapterID := normalizeID(chi.URLParam(r, "id"))
	if err := h.cat.DeleteChapter(r.Context(), chapterID); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleReorderChapters(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		ChapterIDs []string `json:"chapter_ids"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.cat.ReorderChapters(r.Context(), courseID, req.ChapterIDs); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleAddLesson(w http.ResponseWriter, r *http.Request) {
	chapterID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		MediaFileID string `json:"media_file_id"`
		Title       string `json:"title"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	lesson, err := h.cat.AddLesson(r.Context(), chapterID, req.MediaFileID, req.Title)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, lesson)
}

func (h *handler) handleUpdateLesson(w http.ResponseWriter, r *http.Request) {
	lessonID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		Title     string `json:"title"`
		Overview  string `json:"overview"`
		ChapterID string `json:"chapter_id"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	lesson, err := h.cat.UpdateLesson(r.Context(), lessonID, req.Title, req.Overview, req.ChapterID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lesson)
}

func (h *handler) handleDeleteLesson(w http.ResponseWriter, r *http.Request) {
	lessonID := normalizeID(chi.URLParam(r, "id"))
	if err := h.cat.DeleteLesson(r.Context(), lessonID); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleReorderLessons(w http.ResponseWriter, r *http.Request) {
	courseID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		Placements []catalog.LessonPlacement `json:"placements"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.cat.ReorderLessons(r.Context(), courseID, req.Placements); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type playbackSubtitleItem struct {
	Index      int    `json:"index"`
	Language   string `json:"language"`
	Title      string `json:"title"`
	Format     string `json:"format"`
	IsDefault  bool   `json:"is_default"`
	IsExternal bool   `json:"is_external"`
	IsText     bool   `json:"is_text"`
	URL        string `json:"url"`
}

type playbackProgressInfo struct {
	PositionTicks int64  `json:"position_ticks"`
	Played        bool   `json:"played"`
	PlayCount     int    `json:"play_count"`
	LastPlayedAt  string `json:"last_played_at,omitempty"`
}

func (h *handler) handleLessonPlayback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ac := currentAuth(r)
	lessonID := normalizeID(chi.URLParam(r, "id"))

	lesson, err := h.cat.GetLesson(ctx, lessonID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	mf, err := h.cat.GetMediaFile(ctx, lesson.MediaFileID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}

	container := resolveLessonContainer(mf)
	vcodec := strings.ToLower(strings.TrimSpace(mf.VideoCodec))
	acodec := strings.ToLower(strings.TrimSpace(mf.AudioCodec))
	playSessionID := id.New()

	clientCodecs := parseClientSupportedCodecs(r.URL.Query().Get("codecs"))
	forceTranscodeRaw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("force_transcode")))
	forceTranscode := forceTranscodeRaw == "1" || forceTranscodeRaw == "true"
	hasFFmpeg := h.media != nil && h.media.HasFFmpeg()

	var mode, streamURL string
	transcoding := false

	canDirect := !forceTranscode &&
		isBrowserDirectContainer(container) &&
		isBrowserDirectAudioCodec(acodec) &&
		isBrowserDirectVideoCodecForClient(vcodec, clientCodecs) &&
		hasBrowserSafePixFmt(mf.StreamsJSON)

	switch {
	case canDirect:
		mode = "direct"
		q := url.Values{}
		q.Set("Static", "true")
		q.Set("MediaSourceId", mf.ID)
		if ac != nil && ac.Token != "" {
			q.Set("api_key", ac.Token)
		}
		streamURL = fmt.Sprintf("/Videos/%s/stream.%s?%s", lesson.ID, container, q.Encode())
	case !forceTranscode && hasFFmpeg && (vcodec == "" || h.media.CanWebRemuxVideoCodec(vcodec)):
		mode = "hls"
		q := url.Values{}
		q.Set("PlaySessionId", playSessionID)
		q.Set("MediaSourceId", mf.ID)
		if ac != nil && ac.Token != "" {
			q.Set("api_key", ac.Token)
		}
		streamURL = fmt.Sprintf("/Videos/%s/master.m3u8?%s", lesson.ID, q.Encode())
	case hasFFmpeg:
		mode = "hls"
		transcoding = true
		q := url.Values{}
		q.Set("PlaySessionId", playSessionID)
		q.Set("MediaSourceId", mf.ID)
		q.Set("TranscodeVideo", "1")
		if ac != nil && ac.Token != "" {
			q.Set("api_key", ac.Token)
		}
		streamURL = fmt.Sprintf("/Videos/%s/master.m3u8?%s", lesson.ID, q.Encode())
	default:
		mode = "unsupported"
	}

	streams, err := h.media.ResolveStreams(ctx, mf.ID)
	subtitles := make([]playbackSubtitleItem, 0)
	if err == nil {
		for _, st := range streams {
			if !strings.EqualFold(st.Type, "subtitle") {
				continue
			}
			isText := !isBitmapSubtitleCodec(st.Codec)
			subURL := ""
			if isText {
				subURL = fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/Stream.vtt", lesson.ID, mf.ID, st.Index)
				if ac != nil && ac.Token != "" {
					subURL += "?api_key=" + url.QueryEscape(ac.Token)
				}
			}
			subtitles = append(subtitles, playbackSubtitleItem{
				Index:      st.Index,
				Language:   st.Language,
				Title:      st.Title,
				Format:     st.Format,
				IsDefault:  st.IsDefault,
				IsExternal: st.IsExternal,
				IsText:     isText,
				URL:        subURL,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"lesson_id":       lesson.ID,
		"course_id":       lesson.CourseID,
		"media_file_id":   mf.ID,
		"play_session_id": playSessionID,
		"mode":            mode,
		"stream_url":      streamURL,
		"transcoding":     transcoding,
		"subtitles":       subtitles,
		"progress": playbackProgressInfo{
			PositionTicks: lesson.PositionTicks,
			Played:        lesson.Played,
			PlayCount:     lesson.PlayCount,
			LastPlayedAt:  lesson.LastPlayedAt,
		},
		"video_codec": mf.VideoCodec,
		"audio_codec": mf.AudioCodec,
		"container":   container,
		"duration_ms": mf.DurationMs,
	})
}

func resolveLessonContainer(mf *catalog.MediaFile) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(mf.FileName)), ".")
	if ext == "" {
		ext = strings.TrimPrefix(strings.ToLower(path.Ext(mf.RelativePath)), ".")
	}
	c := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(mf.Container), "."))
	switch c {
	case "matroska":
		c = "mkv"
	case "mpegts":
		c = "ts"
	}
	if ext != "" && (c == "" || strings.Contains(c, ",")) {
		return ext
	}
	if c != "" {
		return c
	}
	return ext
}

func isBrowserDirectContainer(c string) bool {
	switch c {
	case "mp4", "m4v", "webm", "mov":
		return true
	default:
		return false
	}
}

func normalizeWebVideoCodec(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "h264", "avc", "avc1":
		return "h264"
	case "hevc", "h265", "hvc1", "hev1":
		return "hevc"
	case "vp9", "vp09":
		return "vp9"
	case "av1", "av01":
		return "av1"
	default:
		return strings.ToLower(strings.TrimSpace(c))
	}
}

func parseClientSupportedCodecs(raw string) map[string]bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]bool{
			"h264": true,
			"vp9":  true,
			"av1":  true,
		}
	}
	out := make(map[string]bool)
	for _, part := range strings.Split(trimmed, ",") {
		if norm := normalizeWebVideoCodec(part); norm != "" {
			out[norm] = true
		}
	}
	if len(out) == 0 {
		out["h264"] = true
	}
	return out
}

func isBrowserDirectVideoCodecForClient(c string, supported map[string]bool) bool {
	norm := normalizeWebVideoCodec(c)
	switch norm {
	case "h264", "hevc", "vp9", "av1":
		return supported[norm]
	default:
		return false
	}
}

func hasBrowserSafePixFmt(streamsJSON string) bool {
	trimmed := strings.TrimSpace(streamsJSON)
	if trimmed == "" || trimmed == "[]" {
		return true
	}
	var streams []struct {
		CodecType   string `json:"codec_type"`
		PixFmt      string `json:"pix_fmt"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(trimmed), &streams); err != nil {
		return true
	}
	for _, st := range streams {
		if !strings.EqualFold(st.CodecType, "video") || st.Disposition.AttachedPic != 0 {
			continue
		}
		pf := strings.ToLower(strings.TrimSpace(st.PixFmt))
		if pf == "" {
			return true
		}
		if strings.Contains(pf, "444") || strings.Contains(pf, "422") || strings.Contains(pf, "gbr") || strings.Contains(pf, "10le") || strings.Contains(pf, "10be") || strings.Contains(pf, "12le") || strings.Contains(pf, "12be") {
			return false
		}
		return true
	}
	return true
}

func isBrowserDirectAudioCodec(c string) bool {
	switch c {
	case "", "aac", "mp3", "opus":
		return true
	default:
		return false
	}
}

func isBitmapSubtitleCodec(c string) bool {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "dvd_subtitle", "hdmv_pgs_subtitle", "pgssub", "dvb_subtitle", "xsub", "dvdsub":
		return true
	default:
		return false
	}
}

func (h *handler) handleReportProgress(w http.ResponseWriter, r *http.Request) {
	lessonID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		PositionTicks int64 `json:"position_ticks"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prog, err := h.cat.ReportProgress(r.Context(), lessonID, req.PositionTicks)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prog)
}

func (h *handler) handleSetPlayed(w http.ResponseWriter, r *http.Request) {
	lessonID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		Played *bool `json:"played"`
	}
	if err := decodeJSON(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	played := true
	if req.Played != nil {
		played = *req.Played
	}
	prog, err := h.cat.SetPlayed(r.Context(), lessonID, played)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prog)
}

func (h *handler) handleListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.cat.ListTags(r.Context())
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if tags == nil {
		tags = []catalog.Tag{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (h *handler) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	resume, err := h.cat.GetResumeLessons(ctx, 20)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if resume == nil {
		resume = []catalog.LessonWithContext{}
	}

	nextUp, err := h.cat.GetNextUpLessons(ctx, 20)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if nextUp == nil {
		nextUp = []catalog.LessonWithContext{}
	}

	libs, err := h.cat.ListLibraries(ctx)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if libs == nil {
		libs = []catalog.Library{}
	}

	courses, err := h.cat.GetLatestCourses(ctx, "", 20)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if courses == nil {
		courses = []catalog.CourseSummary{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"resume":    resume,
		"next_up":   nextUp,
		"libraries": libs,
		"courses":   courses,
	})
}
