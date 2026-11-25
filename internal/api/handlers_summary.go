package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"coursefin/internal/ai"
	"coursefin/internal/summary"

	"github.com/go-chi/chi/v5"
)

func (h *handler) handleTestASR(w http.ResponseWriter, r *http.Request) {
	settings, err := h.loadSettingsMap(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req struct {
		ASRBaseURL       *string `json:"asr_base_url"`
		AIASRBaseURL     *string `json:"ai_asr_base_url"`
		ASRAPIKey        *string `json:"asr_api_key"`
		AIASRAPIKey      *string `json:"ai_asr_api_key"`
		ClearAIASRAPIKey bool    `json:"clear_ai_asr_api_key"`
		ClearASRAPIKey   bool    `json:"clear_asr_api_key"`
		ASRModel         *string `json:"asr_model"`
		AIASRModel       *string `json:"ai_asr_model"`
		ASRLanguage      *string `json:"asr_language"`
		AIASRLanguage    *string `json:"ai_asr_language"`
		BaseURL          *string `json:"base_url"`
		AIBaseURL        *string `json:"ai_base_url"`
		APIKey           *string `json:"api_key"`
		AIAPIKey         *string `json:"ai_api_key"`
		ClearAIAPIKey    bool    `json:"clear_ai_api_key"`
		ClearAPIKey      bool    `json:"clear_api_key"`
	}
	if err := decodeJSON(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if v, ok := firstProvidedString(req.BaseURL, req.AIBaseURL); ok {
		settings["ai_base_url"] = v
	}
	applyOptionalAPIKeyUpdate(settings, "ai_api_key", firstPreferredStringPtr(req.APIKey, req.AIAPIKey), req.ClearAIAPIKey || req.ClearAPIKey)

	if v, ok := firstProvidedString(req.ASRBaseURL, req.AIASRBaseURL); ok {
		settings["ai_asr_base_url"] = v
	}
	applyOptionalAPIKeyUpdate(settings, "ai_asr_api_key", firstPreferredStringPtr(req.ASRAPIKey, req.AIASRAPIKey), req.ClearAIASRAPIKey || req.ClearASRAPIKey)

	if v, ok := firstProvidedString(req.ASRModel, req.AIASRModel); ok {
		settings["ai_asr_model"] = v
	}
	if v, ok := firstProvidedString(req.ASRLanguage, req.AIASRLanguage); ok {
		settings["ai_asr_language"] = v
	}

	resolved := summary.ResolveEffectiveSettings(settings)
	if !resolved.ASRConfigured {
		writeErrorWithCode(w, http.StatusBadRequest, "asr_not_configured", "ASR provider is not configured (effective ASR base URL and ai_asr_model are required)")
		return
	}

	transcriber := ai.NewOpenAICompatibleTranscriber(resolved.ASR, h.aiHTTPClient)
	if err := transcriber.TestConnection(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleGetLessonSummary(w http.ResponseWriter, r *http.Request) {
	if h.summary == nil {
		writeError(w, http.StatusServiceUnavailable, "summary service unavailable")
		return
	}
	lessonID := normalizeID(chi.URLParam(r, "id"))
	state, err := h.summary.GetLessonSummaryState(r.Context(), lessonID)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) handleGenerateLessonSummary(w http.ResponseWriter, r *http.Request) {
	if h.summary == nil {
		writeError(w, http.StatusServiceUnavailable, "summary service unavailable")
		return
	}
	lessonID := normalizeID(chi.URLParam(r, "id"))
	var opts summary.GenerateLessonOptions
	if err := decodeJSON(r, &opts, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	job, err := h.summary.StartLessonSummary(r.Context(), lessonID, opts)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *handler) handleCancelLessonSummary(w http.ResponseWriter, r *http.Request) {
	if h.summary == nil {
		writeError(w, http.StatusServiceUnavailable, "summary service unavailable")
		return
	}
	lessonID := normalizeID(chi.URLParam(r, "id"))
	if lessonID == "" {
		writeError(w, http.StatusBadRequest, "lesson id is required")
		return
	}
	if h.cat != nil {
		if _, err := h.cat.GetLesson(r.Context(), lessonID); err != nil {
			writeCatalogError(w, err)
			return
		}
	}
	job, err := h.summary.CancelLessonSummary(lessonID)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *handler) handleDeleteLessonSummary(w http.ResponseWriter, r *http.Request) {
	if h.summary == nil {
		writeError(w, http.StatusServiceUnavailable, "summary service unavailable")
		return
	}
	lessonID := normalizeID(chi.URLParam(r, "id"))
	delRaw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("delete_transcript")))
	deleteTranscript := delRaw == "true" || delRaw == "1"

	if err := h.summary.DeleteLessonSummary(r.Context(), lessonID, deleteTranscript); err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleGetCourseSummariesStatus(w http.ResponseWriter, r *http.Request) {
	if h.summary == nil {
		writeError(w, http.StatusServiceUnavailable, "summary service unavailable")
		return
	}
	courseID := normalizeID(chi.URLParam(r, "id"))
	st, err := h.summary.GetCourseSummaryStatus(r.Context(), courseID)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *handler) handleGenerateCourseSummaries(w http.ResponseWriter, r *http.Request) {
	if h.summary == nil {
		writeError(w, http.StatusServiceUnavailable, "summary service unavailable")
		return
	}
	courseID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	batch, err := h.summary.StartCourseBatch(r.Context(), courseID, req.Mode)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	status := http.StatusAccepted
	if !batch.Running {
		status = http.StatusOK
	}
	writeJSON(w, status, batch)
}

func (h *handler) handleCancelCourseSummaries(w http.ResponseWriter, r *http.Request) {
	if h.summary == nil {
		writeError(w, http.StatusServiceUnavailable, "summary service unavailable")
		return
	}
	courseID := normalizeID(chi.URLParam(r, "id"))
	if courseID == "" {
		writeError(w, http.StatusBadRequest, "course id is required")
		return
	}
	if h.db != nil {
		var exists int
		err := h.db.QueryRowContext(r.Context(), `SELECT 1 FROM courses WHERE id = ?`, courseID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "course not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	batch, err := h.summary.CancelCourseBatch(courseID)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func writeErrorWithCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{
		"error": msg,
		"code":  code,
	})
}

func writeSummaryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, summary.ErrAINotConfigured):
		writeErrorWithCode(w, http.StatusBadRequest, "ai_not_configured", err.Error())
	case errors.Is(err, summary.ErrASRNotConfigured):
		writeErrorWithCode(w, http.StatusBadRequest, "asr_not_configured", err.Error())
	case errors.Is(err, summary.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, summary.ErrBatchAlreadyRunning):
		writeErrorWithCode(w, http.StatusConflict, "batch_already_running", err.Error())
	case errors.Is(err, summary.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func firstProvidedString(ptrs ...*string) (string, bool) {
	provided := false
	for _, p := range ptrs {
		if p != nil {
			provided = true
			if v := strings.TrimSpace(*p); v != "" {
				return v, true
			}
		}
	}
	return "", provided
}

func firstPreferredStringPtr(ptrs ...*string) *string {
	var fallback *string
	for _, p := range ptrs {
		if p == nil {
			continue
		}
		if fallback == nil {
			fallback = p
		}
		if strings.TrimSpace(*p) != "" {
			return p
		}
	}
	return fallback
}
