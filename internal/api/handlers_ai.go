package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"coursefin/internal/ai"
	"coursefin/internal/catalog"
	"coursefin/internal/id"
	"coursefin/internal/media"
	"coursefin/internal/summary"

	"github.com/go-chi/chi/v5"
)

const (
	defaultScanIntervalMinutes = 60
	maskedAPIKeyPlaceholder    = "****"
	maxSubtitleExcerptRead     = 8 << 10 // 8 KiB per sidecar file
)

type settingsPayload struct {
	ServerName          string `json:"server_name"`
	ScanIntervalMinutes int    `json:"scan_interval_minutes"`
	BaseURLHint         string `json:"base_url_hint"`
	JellyfinUsername    string `json:"jellyfin_username"`
	AIBaseURL           string `json:"ai_base_url"`
	AIAPIKey            string `json:"ai_api_key"`
	AIAPIKeySet         bool   `json:"ai_api_key_set"`
	AIModel             string `json:"ai_model"`
	AIMaxSubtitleChars  int    `json:"ai_max_subtitle_chars"`
	AIASRBaseURL        string `json:"ai_asr_base_url"`
	AIASRAPIKey         string `json:"ai_asr_api_key"`
	AIASRAPIKeySet      bool   `json:"ai_asr_api_key_set"`
	AIASRModel          string `json:"ai_asr_model"`
	AIASRLanguage       string `json:"ai_asr_language"`
	AIConfigured        bool   `json:"ai_configured"`
	AIASRConfigured     bool   `json:"ai_asr_configured"`
}

func (h *handler) loadSettingsMap(ctx context.Context) (map[string]string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

func (h *handler) buildSettingsResponse(ctx context.Context) (*settingsPayload, error) {
	m, err := h.loadSettingsMap(ctx)
	if err != nil {
		return nil, err
	}

	serverName := strings.TrimSpace(m["server_name"])
	if serverName == "" {
		serverName = "CourseFin"
	}

	scanInterval := defaultScanIntervalMinutes
	if raw := strings.TrimSpace(m["scan_interval_minutes"]); raw != "" {
		if v, convErr := strconv.Atoi(raw); convErr == nil && v > 0 {
			scanInterval = v
		}
	}

	resolved := summary.ResolveEffectiveSettings(m)

	maskedKey := ""
	if resolved.AI.APIKey != "" {
		maskedKey = maskedAPIKeyPlaceholder
	}

	rawASRKey := strings.TrimSpace(m["ai_asr_api_key"])
	maskedASRKey := ""
	if rawASRKey != "" {
		maskedASRKey = maskedAPIKeyPlaceholder
	}

	jfUsername := ""
	if u, err := h.auth.GetAdminUser(ctx); err == nil && u != nil {
		jfUsername = u.Username
	}

	return &settingsPayload{
		ServerName:          serverName,
		ScanIntervalMinutes: scanInterval,
		BaseURLHint:         h.baseURLHint,
		JellyfinUsername:    jfUsername,
		AIBaseURL:           resolved.AI.BaseURL,
		AIAPIKey:            maskedKey,
		AIAPIKeySet:         resolved.AI.APIKey != "",
		AIModel:             resolved.AI.Model,
		AIMaxSubtitleChars:  resolved.AI.MaxSubtitleChars,
		AIASRBaseURL:        strings.TrimSpace(m["ai_asr_base_url"]),
		AIASRAPIKey:         maskedASRKey,
		AIASRAPIKeySet:      rawASRKey != "",
		AIASRModel:          resolved.ASR.Model,
		AIASRLanguage:       resolved.ASR.Language,
		AIConfigured:        resolved.AIConfigured,
		AIASRConfigured:     resolved.ASRConfigured,
	}, nil
}

func (h *handler) loadAIConfig(ctx context.Context) (ai.Config, error) {
	m, err := h.loadSettingsMap(ctx)
	if err != nil {
		return ai.Config{}, err
	}
	return summary.ResolveEffectiveSettings(m).AI, nil
}

func (h *handler) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := h.buildSettingsResponse(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerName          *string `json:"server_name"`
		ScanIntervalMinutes *int    `json:"scan_interval_minutes"`
		AIBaseURL           *string `json:"ai_base_url"`
		AIAPIKey            *string `json:"ai_api_key"`
		ClearAIAPIKey       bool    `json:"clear_ai_api_key"`
		AIModel             *string `json:"ai_model"`
		AIMaxSubtitleChars  *int    `json:"ai_max_subtitle_chars"`
		AIASRBaseURL        *string `json:"ai_asr_base_url"`
		AIASRAPIKey         *string `json:"ai_asr_api_key"`
		ClearAIASRAPIKey    bool    `json:"clear_ai_asr_api_key"`
		AIASRModel          *string `json:"ai_asr_model"`
		AIASRLanguage       *string `json:"ai_asr_language"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updates := make(map[string]string)
	if req.ServerName != nil {
		v := strings.TrimSpace(*req.ServerName)
		if v == "" {
			writeError(w, http.StatusBadRequest, "server_name cannot be empty")
			return
		}
		updates["server_name"] = v
	}
	if req.ScanIntervalMinutes != nil {
		if *req.ScanIntervalMinutes < 1 {
			writeError(w, http.StatusBadRequest, "scan_interval_minutes must be at least 1")
			return
		}
		updates["scan_interval_minutes"] = strconv.Itoa(*req.ScanIntervalMinutes)
	}
	if req.AIBaseURL != nil {
		updates["ai_base_url"] = strings.TrimSpace(*req.AIBaseURL)
	}
	applyOptionalAPIKeyUpdate(updates, "ai_api_key", req.AIAPIKey, req.ClearAIAPIKey)
	if req.AIModel != nil {
		updates["ai_model"] = strings.TrimSpace(*req.AIModel)
	}
	if req.AIMaxSubtitleChars != nil {
		if *req.AIMaxSubtitleChars < 0 {
			writeError(w, http.StatusBadRequest, "ai_max_subtitle_chars must be non-negative")
			return
		}
		updates["ai_max_subtitle_chars"] = strconv.Itoa(*req.AIMaxSubtitleChars)
	}
	if req.AIASRBaseURL != nil {
		updates["ai_asr_base_url"] = strings.TrimSpace(*req.AIASRBaseURL)
	}
	applyOptionalAPIKeyUpdate(updates, "ai_asr_api_key", req.AIASRAPIKey, req.ClearAIASRAPIKey)
	if req.AIASRModel != nil {
		updates["ai_asr_model"] = strings.TrimSpace(*req.AIASRModel)
	}
	if req.AIASRLanguage != nil {
		updates["ai_asr_language"] = strings.TrimSpace(*req.AIASRLanguage)
	}

	if len(updates) > 0 {
		tx, err := h.db.BeginTx(r.Context(), nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer tx.Rollback()

		now := time.Now().UTC().Format(time.RFC3339)
		for k, v := range updates {
			if _, err := tx.ExecContext(r.Context(), `
				INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
				k, v, now,
			); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	resp, err := h.buildSettingsResponse(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) handleTestAI(w http.ResponseWriter, r *http.Request) {
	stored, err := h.loadAIConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req struct {
		BaseURL          string `json:"base_url"`
		AIBaseURL        string `json:"ai_base_url"`
		APIKey           string `json:"api_key"`
		AIAPIKey         string `json:"ai_api_key"`
		Model            string `json:"model"`
		AIModel          string `json:"ai_model"`
		MaxSubtitleChars *int   `json:"max_subtitle_chars"`
	}
	if err := decodeJSON(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if v := firstNonEmpty(req.BaseURL, req.AIBaseURL); v != "" {
		stored.BaseURL = v
	}
	if v := firstNonEmpty(req.APIKey, req.AIAPIKey); v != "" && v != maskedAPIKeyPlaceholder {
		stored.APIKey = v
	}
	if v := firstNonEmpty(req.Model, req.AIModel); v != "" {
		stored.Model = v
	}
	if req.MaxSubtitleChars != nil && *req.MaxSubtitleChars >= 0 {
		stored.MaxSubtitleChars = *req.MaxSubtitleChars
	}

	if stored.BaseURL == "" || stored.Model == "" {
		writeError(w, http.StatusBadRequest, "ai_base_url and ai_model are required")
		return
	}

	provider := ai.NewOpenAICompatibleProvider(stored, h.aiHTTPClient)
	if err := provider.TestConnection(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleAIOrganize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	courseID := normalizeID(chi.URLParam(r, "id"))

	course, err := h.cat.GetCourseDetail(ctx, courseID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}

	aiCfg, err := h.loadAIConfig(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if aiCfg.BaseURL == "" || aiCfg.Model == "" {
		writeError(w, http.StatusBadRequest, "AI provider is not configured (ai_base_url and ai_model required)")
		return
	}

	mfMeta, err := h.loadLessonMediaAndSubtitles(ctx, course, aiCfg.MaxSubtitleChars > 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	input := ai.CourseOrganizeInput{
		CourseID:       course.ID,
		CurrentTitle:   course.Title,
		SourceRootPath: course.SourceRootPath,
		Overview:       course.Overview,
		Chapters:       make([]ai.OrganizeChapterInput, 0, len(course.Chapters)),
	}

	remainingBudget := aiCfg.MaxSubtitleChars
	if remainingBudget < 0 {
		remainingBudget = 0
	}

	for _, ch := range course.Chapters {
		chIn := ai.OrganizeChapterInput{
			ChapterID: ch.ID,
			Title:     ch.Title,
			Lessons:   make([]ai.OrganizeLessonInput, 0, len(ch.Lessons)),
		}
		for _, l := range ch.Lessons {
			meta := mfMeta[l.MediaFileID]
			excerpt := ""
			if remainingBudget > 0 && meta.hasSub {
				excerpt = h.readSubtitleExcerpt(ctx, meta, ai.DefaultPerLessonSubtitleChars)
				if excerpt != "" {
					remainingBudget -= utf8.RuneCountInString(excerpt)
				}
			}
			chIn.Lessons = append(chIn.Lessons, ai.OrganizeLessonInput{
				LessonID:        l.ID,
				Title:           l.Title,
				FileName:        meta.fileName,
				RelativePath:    meta.relativePath,
				SubtitleExcerpt: excerpt,
			})
		}
		input.Chapters = append(input.Chapters, chIn)
	}

	provider := ai.NewOpenAICompatibleProvider(aiCfg, h.aiHTTPClient)
	proposal, err := provider.OrganizeCourse(ctx, input)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, proposal)
}

type importProposalLesson struct {
	MediaFileID string `json:"media_file_id"`
	Title       string `json:"title"`
}

type importProposalChapter struct {
	Title   string                 `json:"title"`
	Lessons []importProposalLesson `json:"lessons"`
}

type importOrganizeProposal struct {
	Title    string                  `json:"title"`
	Overview string                  `json:"overview"`
	Tags     []string                `json:"tags"`
	Chapters []importProposalChapter `json:"chapters"`
}

func (h *handler) handleImportAIOrganize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		SourceID   string `json:"source_id"`
		DirRelPath string `json:"dir_rel_path"`
		Title      string `json:"title"`
		Overview   string `json:"overview"`
		Chapters   []struct {
			Title   string `json:"title"`
			Lessons []struct {
				MediaFileID string `json:"media_file_id"`
				Title       string `json:"title"`
			} `json:"lessons"`
		} `json:"chapters"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	aiCfg, err := h.loadAIConfig(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if aiCfg.BaseURL == "" || aiCfg.Model == "" {
		writeError(w, http.StatusBadRequest, "AI provider is not configured (ai_base_url and ai_model required)")
		return
	}

	sourceID := normalizeID(req.SourceID)
	if sourceID == "" {
		writeError(w, http.StatusBadRequest, "source_id is required")
		return
	}
	if _, err := h.cat.GetSource(ctx, sourceID); err != nil {
		writeCatalogError(w, err)
		return
	}

	var mfIDs []string
	seen := make(map[string]bool)
	for _, ch := range req.Chapters {
		for _, l := range ch.Lessons {
			mid := normalizeID(l.MediaFileID)
			if mid == "" {
				writeError(w, http.StatusBadRequest, "media_file_id cannot be empty")
				return
			}
			if seen[mid] {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("duplicate media_file_id %q", l.MediaFileID))
				return
			}
			seen[mid] = true
			mfIDs = append(mfIDs, mid)
		}
	}
	if len(mfIDs) == 0 {
		writeError(w, http.StatusBadRequest, "chapters must contain at least one lesson")
		return
	}

	mfMeta, err := h.loadMediaAndSubtitlesByIDs(ctx, mfIDs, aiCfg.MaxSubtitleChars > 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, mid := range mfIDs {
		meta, ok := mfMeta[mid]
		if !ok || meta.sourceID != sourceID {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("media_file_id %q does not belong to source %s", mid, sourceID))
			return
		}
	}

	input := ai.CourseOrganizeInput{
		CurrentTitle:   strings.TrimSpace(req.Title),
		SourceRootPath: strings.TrimSpace(req.DirRelPath),
		Overview:       strings.TrimSpace(req.Overview),
		Chapters:       make([]ai.OrganizeChapterInput, 0, len(req.Chapters)),
	}

	remainingBudget := aiCfg.MaxSubtitleChars
	if remainingBudget < 0 {
		remainingBudget = 0
	}

	for _, ch := range req.Chapters {
		chIn := ai.OrganizeChapterInput{
			Title:   ch.Title,
			Lessons: make([]ai.OrganizeLessonInput, 0, len(ch.Lessons)),
		}
		for _, l := range ch.Lessons {
			mid := normalizeID(l.MediaFileID)
			meta := mfMeta[mid]
			excerpt := ""
			if remainingBudget > 0 && meta.hasSub {
				excerpt = h.readSubtitleExcerpt(ctx, meta, ai.DefaultPerLessonSubtitleChars)
				if excerpt != "" {
					remainingBudget -= utf8.RuneCountInString(excerpt)
				}
			}
			chIn.Lessons = append(chIn.Lessons, ai.OrganizeLessonInput{
				LessonID:        mid,
				Title:           l.Title,
				FileName:        meta.fileName,
				RelativePath:    meta.relativePath,
				SubtitleExcerpt: excerpt,
			})
		}
		input.Chapters = append(input.Chapters, chIn)
	}

	provider := ai.NewOpenAICompatibleProvider(aiCfg, h.aiHTTPClient)
	proposal, err := provider.OrganizeCourse(ctx, input)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	out := importOrganizeProposal{
		Title:    proposal.Title,
		Overview: proposal.Overview,
		Tags:     proposal.Tags,
		Chapters: make([]importProposalChapter, 0, len(proposal.Chapters)),
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	for _, ch := range proposal.Chapters {
		ich := importProposalChapter{
			Title:   ch.Title,
			Lessons: make([]importProposalLesson, 0, len(ch.Lessons)),
		}
		for _, l := range ch.Lessons {
			ich.Lessons = append(ich.Lessons, importProposalLesson{
				MediaFileID: l.LessonID,
				Title:       l.Title,
			})
		}
		out.Chapters = append(out.Chapters, ich)
	}

	writeJSON(w, http.StatusOK, out)
}

type lessonMediaSubtitleMeta struct {
	sourceID     string
	fileName     string
	relativePath string
	hasSub       bool
	driver       string
	rootPath     string
	configJSON   string
	subRelPath   string
}

func (h *handler) loadLessonMediaAndSubtitles(ctx context.Context, course *catalog.CourseDetail, includeSubtitles bool) (map[string]lessonMediaSubtitleMeta, error) {
	var mfIDs []string
	seen := make(map[string]bool)
	for _, ch := range course.Chapters {
		for _, l := range ch.Lessons {
			if !seen[l.MediaFileID] {
				seen[l.MediaFileID] = true
				mfIDs = append(mfIDs, l.MediaFileID)
			}
		}
	}
	return h.loadMediaAndSubtitlesByIDs(ctx, mfIDs, includeSubtitles)
}

func (h *handler) loadMediaAndSubtitlesByIDs(ctx context.Context, mfIDs []string, includeSubtitles bool) (map[string]lessonMediaSubtitleMeta, error) {
	out := make(map[string]lessonMediaSubtitleMeta)
	if len(mfIDs) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(mfIDs))
	args := make([]any, len(mfIDs))
	for i, mid := range mfIDs {
		placeholders[i] = "?"
		args[i] = mid
	}
	inClause := strings.Join(placeholders, ",")

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, source_id, file_name, relative_path
		FROM media_files
		WHERE id IN (`+inClause+`)`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var mid, sid, fn, rp string
		if err := rows.Scan(&mid, &sid, &fn, &rp); err != nil {
			rows.Close()
			return nil, err
		}
		out[mid] = lessonMediaSubtitleMeta{
			sourceID:     sid,
			fileName:     fn,
			relativePath: rp,
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if !includeSubtitles {
		return out, nil
	}

	sRows, err := h.db.QueryContext(ctx, `
		SELECT sf.media_file_id, sf.relative_path, ss.driver, ss.root_path, ss.config_json
		FROM subtitle_files sf
		JOIN storage_sources ss ON ss.id = sf.source_id
		WHERE sf.media_file_id IN (`+inClause+`) AND sf.status = 'active'
		ORDER BY sf.is_default DESC, sf.language ASC, sf.id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer sRows.Close()

	for sRows.Next() {
		var mid, subRel, driver, rootPath, cfgJSON string
		if err := sRows.Scan(&mid, &subRel, &driver, &rootPath, &cfgJSON); err != nil {
			return nil, err
		}
		m := out[mid]
		if !m.hasSub {
			m.hasSub = true
			m.subRelPath = subRel
			m.driver = driver
			m.rootPath = rootPath
			m.configJSON = cfgJSON
			out[mid] = m
		}
	}
	return out, sRows.Err()
}

func (h *handler) readSubtitleExcerpt(ctx context.Context, meta lessonMediaSubtitleMeta, maxRunes int) string {
	provider, err := h.registry.ProviderFor(meta.driver, meta.rootPath, meta.configJSON)
	if err != nil {
		return ""
	}
	rc, err := provider.Open(ctx, meta.subRelPath)
	if err != nil {
		return ""
	}
	defer rc.Close()

	raw, err := io.ReadAll(io.LimitReader(rc, maxSubtitleExcerptRead+1))
	if err != nil || len(raw) == 0 {
		return ""
	}
	truncated := len(raw) > maxSubtitleExcerptRead
	if truncated {
		raw = raw[:maxSubtitleExcerptRead]
		raw = trimTruncatedSubtitleBytes(raw)
	}

	text := media.DecodeSubtitleToUTF8(raw)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var parts []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.EqualFold(trimmed, "WEBVTT") || strings.Contains(trimmed, "-->") {
			continue
		}
		if _, err := strconv.Atoi(trimmed); err == nil {
			continue
		}
		parts = append(parts, trimmed)
	}
	joined := strings.Join(parts, " ")
	if utf8.RuneCountInString(joined) <= maxRunes {
		return joined
	}
	count := 0
	for idx := range joined {
		if count == maxRunes {
			return joined[:idx]
		}
		count++
	}
	return joined
}

// trimTruncatedSubtitleBytes trims trailing incomplete bytes caused by a fixed-size
// prefix read so a mid-rune UTF-8 cut does not fail utf8.Valid and fall back to GB18030.
func trimTruncatedSubtitleBytes(raw []byte) []byte {
	if len(raw) >= 2 && ((raw[0] == 0xFF && raw[1] == 0xFE) || (raw[0] == 0xFE && raw[1] == 0xFF)) {
		if len(raw)%2 == 1 {
			return raw[:len(raw)-1]
		}
		return raw
	}
	if utf8.Valid(raw) {
		return raw
	}
	for k := 1; k <= 3 && k < len(raw); k++ {
		tail := raw[len(raw)-k:]
		if !utf8.FullRune(tail) && utf8.Valid(raw[:len(raw)-k]) {
			return raw[:len(raw)-k]
		}
	}
	return raw
}

func (h *handler) handleAIApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	courseID := normalizeID(chi.URLParam(r, "id"))

	var req struct {
		ApplyTitle    bool                 `json:"apply_title"`
		ApplyOverview bool                 `json:"apply_overview"`
		ApplyTags     bool                 `json:"apply_tags"`
		ApplyChapters bool                 `json:"apply_chapters"`
		Title         string               `json:"title"`
		Overview      string               `json:"overview"`
		Tags          []string             `json:"tags"`
		Chapters      []ai.ProposalChapter `json:"chapters"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var exists int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM courses WHERE id = ?`, courseID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)

	if req.ApplyTitle {
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty when apply_title is true")
			return
		}
		if _, err := tx.ExecContext(ctx, `UPDATE courses SET title = ?, updated_at = ? WHERE id = ?`, title, now, courseID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.ApplyOverview {
		overview := strings.TrimSpace(req.Overview)
		if _, err := tx.ExecContext(ctx, `UPDATE courses SET overview = ?, updated_at = ? WHERE id = ?`, overview, now, courseID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.ApplyTags {
		if err := replaceCourseTagsTx(ctx, tx, courseID, req.Tags); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.ApplyChapters {
		if err := applyCourseChaptersTx(ctx, tx, courseID, req.Chapters, now); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE courses SET updated_at = ? WHERE id = ?`, now, courseID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	updated, err := h.cat.GetCourseDetail(ctx, courseID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func replaceCourseTagsTx(ctx context.Context, tx *sql.Tx, courseID string, tagNames []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM course_tags WHERE course_id = ?`, courseID); err != nil {
		return err
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
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO course_tags (course_id, tag_id) VALUES (?, ?)`, courseID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func applyCourseChaptersTx(ctx context.Context, tx *sql.Tx, courseID string, chapters []ai.ProposalChapter, now string) error {
	if len(chapters) == 0 {
		return errors.New("chapters cannot be empty when apply_chapters is true")
	}

	existingLessons := make(map[string]bool)
	lRows, err := tx.QueryContext(ctx, `SELECT id FROM lessons WHERE course_id = ?`, courseID)
	if err != nil {
		return err
	}
	for lRows.Next() {
		var lid string
		if err := lRows.Scan(&lid); err != nil {
			lRows.Close()
			return err
		}
		existingLessons[lid] = true
	}
	if err := lRows.Err(); err != nil {
		lRows.Close()
		return err
	}
	lRows.Close()

	var oldChapterIDs []string
	cRows, err := tx.QueryContext(ctx, `SELECT id FROM chapters WHERE course_id = ?`, courseID)
	if err != nil {
		return err
	}
	for cRows.Next() {
		var cid string
		if err := cRows.Scan(&cid); err != nil {
			cRows.Close()
			return err
		}
		oldChapterIDs = append(oldChapterIDs, cid)
	}
	if err := cRows.Err(); err != nil {
		cRows.Close()
		return err
	}
	cRows.Close()

	seenLessons := make(map[string]bool, len(existingLessons))
	for ci, ch := range chapters {
		if strings.TrimSpace(ch.Title) == "" {
			return fmt.Errorf("chapter %d title cannot be empty", ci+1)
		}
		for li, l := range ch.Lessons {
			lid := normalizeID(l.LessonID)
			if lid == "" || !existingLessons[lid] {
				return fmt.Errorf("lesson_id %q does not belong to course %s", l.LessonID, courseID)
			}
			if seenLessons[lid] {
				return fmt.Errorf("duplicate lesson_id %q", l.LessonID)
			}
			if strings.TrimSpace(l.Title) == "" {
				return fmt.Errorf("chapter %d lesson %d title cannot be empty", ci+1, li+1)
			}
			seenLessons[lid] = true
		}
	}
	if len(seenLessons) != len(existingLessons) {
		return fmt.Errorf("proposal accounts for %d of %d lessons in course", len(seenLessons), len(existingLessons))
	}

	for ci, ch := range chapters {
		newChapID := id.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chapters (id, course_id, title, sort_order, overview, created_at, updated_at)
			VALUES (?, ?, ?, ?, '', ?, ?)`,
			newChapID, courseID, strings.TrimSpace(ch.Title), ci+1, now, now,
		); err != nil {
			return err
		}

		for li, l := range ch.Lessons {
			lid := normalizeID(l.LessonID)
			if _, err := tx.ExecContext(ctx, `
				UPDATE lessons SET chapter_id = ?, title = ?, sort_order = ?, updated_at = ?
				WHERE id = ? AND course_id = ?`,
				newChapID, strings.TrimSpace(l.Title), li+1, now, lid, courseID,
			); err != nil {
				return err
			}
		}
	}

	// Safe to delete old chapters after all lessons have been reassigned to new chapters.
	for _, oldID := range oldChapterIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chapters WHERE id = ? AND course_id = ?`, oldID, courseID); err != nil {
			return err
		}
	}
	return nil
}

func applyOptionalAPIKeyUpdate(target map[string]string, key string, val *string, clear bool) {
	if clear {
		target[key] = ""
		return
	}
	if val == nil {
		return
	}
	k := strings.TrimSpace(*val)
	// Preserve existing stored key when blank or equal to masked placeholder.
	if k != "" && k != maskedAPIKeyPlaceholder {
		target[key] = k
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}
