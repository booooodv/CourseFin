package api

import (
	"errors"
	"net/http"

	"coursefin/internal/ai"
	"coursefin/internal/cover"

	"github.com/go-chi/chi/v5"
)

func (h *handler) handleTestAIImage(w http.ResponseWriter, r *http.Request) {
	settings, err := h.loadSettingsMap(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req struct {
		ImageProtocol      *string `json:"image_protocol"`
		AIImageProtocol    *string `json:"ai_image_protocol"`
		ImageBaseURL       *string `json:"image_base_url"`
		AIImageBaseURL     *string `json:"ai_image_base_url"`
		ImageAPIKey        *string `json:"image_api_key"`
		AIImageAPIKey      *string `json:"ai_image_api_key"`
		ClearAIImageAPIKey bool    `json:"clear_ai_image_api_key"`
		ClearImageAPIKey   bool    `json:"clear_image_api_key"`
		ImageModel         *string `json:"image_model"`
		AIImageModel       *string `json:"ai_image_model"`
		ImageSize          *string `json:"image_size"`
		AIImageSize        *string `json:"ai_image_size"`
		BaseURL            *string `json:"base_url"`
		AIBaseURL          *string `json:"ai_base_url"`
		APIKey             *string `json:"api_key"`
		AIAPIKey           *string `json:"ai_api_key"`
		ClearAIAPIKey      bool    `json:"clear_ai_api_key"`
		ClearAPIKey        bool    `json:"clear_api_key"`
	}
	if err := decodeJSON(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if v, ok := firstProvidedString(req.BaseURL, req.AIBaseURL); ok {
		settings["ai_base_url"] = v
	}
	applyOptionalAPIKeyUpdate(settings, "ai_api_key", firstPreferredStringPtr(req.APIKey, req.AIAPIKey), req.ClearAIAPIKey || req.ClearAPIKey)

	if v, ok := firstProvidedString(req.ImageProtocol, req.AIImageProtocol); ok {
		proto, perr := ai.ValidateImageProtocol(v)
		if perr != nil {
			writeError(w, http.StatusBadRequest, perr.Error())
			return
		}
		settings["ai_image_protocol"] = proto
	}
	if v, ok := firstProvidedString(req.ImageBaseURL, req.AIImageBaseURL); ok {
		settings["ai_image_base_url"] = v
	}
	applyOptionalAPIKeyUpdate(settings, "ai_image_api_key", firstPreferredStringPtr(req.ImageAPIKey, req.AIImageAPIKey), req.ClearAIImageAPIKey || req.ClearImageAPIKey)
	if v, ok := firstProvidedString(req.ImageModel, req.AIImageModel); ok {
		settings["ai_image_model"] = v
	}
	if v, ok := firstProvidedString(req.ImageSize, req.AIImageSize); ok {
		settings["ai_image_size"] = v
	}

	resolved := cover.ResolveEffectiveImageSettings(settings)
	if !resolved.Configured {
		writeErrorWithCode(w, http.StatusBadRequest, "image_not_configured", "AI image generator is not configured (base URL / API key and ai_image_model required)")
		return
	}

	gen := ai.NewHTTPImageGenerator(resolved.Image, h.aiHTTPClient)
	if err := gen.TestConnection(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleListCoverStyles(w http.ResponseWriter, r *http.Request) {
	var cfg cover.EffectiveImageSettings
	var err error
	if h.cover != nil {
		cfg, err = h.cover.LoadSettings(r.Context())
	} else {
		var m map[string]string
		m, err = h.loadSettingsMap(r.Context())
		if err == nil {
			cfg = cover.ResolveEffectiveImageSettings(m)
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"styles":              cover.ListStyles(),
		"default_style_id":    cfg.DefaultStyleID,
		"ai_image_configured": cfg.Configured,
	})
}

func (h *handler) handleGetCoverPreview(w http.ResponseWriter, r *http.Request) {
	if h.cover == nil {
		writeError(w, http.StatusServiceUnavailable, "cover service unavailable")
		return
	}
	previewID := chi.URLParam(r, "previewId")
	filePath, contentType, err := h.cover.GetPreviewFile(previewID)
	if err != nil {
		writeCoverError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filePath)
}

func (h *handler) handleGenerateCourseCover(w http.ResponseWriter, r *http.Request) {
	if h.cover == nil {
		writeError(w, http.StatusServiceUnavailable, "cover service unavailable")
		return
	}
	courseID := normalizeID(chi.URLParam(r, "id"))
	var req cover.GeneratePreviewInput
	if err := decodeJSON(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	preview, err := h.cover.GenerateCoursePreview(r.Context(), courseID, req)
	if err != nil {
		writeCoverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *handler) handleApplyCourseCover(w http.ResponseWriter, r *http.Request) {
	if h.cover == nil {
		writeError(w, http.StatusServiceUnavailable, "cover service unavailable")
		return
	}
	courseID := normalizeID(chi.URLParam(r, "id"))
	var req struct {
		PreviewID string `json:"preview_id"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.cover.ApplyCoursePreview(r.Context(), courseID, req.PreviewID)
	if err != nil {
		writeCoverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *handler) handleGetBatchCoverStatus(w http.ResponseWriter, r *http.Request) {
	if h.cover == nil {
		writeError(w, http.StatusServiceUnavailable, "cover service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, h.cover.GetBatchStatus())
}

func (h *handler) handleStartBatchCover(w http.ResponseWriter, r *http.Request) {
	if h.cover == nil {
		writeError(w, http.StatusServiceUnavailable, "cover service unavailable")
		return
	}
	var req cover.BatchCoverInput
	if err := decodeJSON(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status, err := h.cover.StartBatch(r.Context(), req)
	if err != nil {
		writeCoverError(w, err)
		return
	}
	code := http.StatusOK
	if status.Running {
		code = http.StatusAccepted
	}
	writeJSON(w, code, status)
}

func (h *handler) handleCancelBatchCover(w http.ResponseWriter, r *http.Request) {
	if h.cover == nil {
		writeError(w, http.StatusServiceUnavailable, "cover service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, h.cover.CancelBatch())
}

func writeCoverError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cover.ErrImageNotConfigured):
		writeErrorWithCode(w, http.StatusBadRequest, "image_not_configured", err.Error())
	case errors.Is(err, cover.ErrNotFound), errors.Is(err, cover.ErrPreviewNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, cover.ErrBatchAlreadyRunning):
		writeErrorWithCode(w, http.StatusConflict, "batch_already_running", err.Error())
	case errors.Is(err, cover.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cover.ErrUpstream):
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
