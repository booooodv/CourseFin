package api

import (
	"errors"
	"net/http"

	"coursefin/internal/updater"
)

func (h *handler) handleGetUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if h.updater == nil {
		writeError(w, http.StatusServiceUnavailable, "updater service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, h.updater.Status(r.Context()))
}

func (h *handler) handleUpdateUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if h.updater == nil {
		writeError(w, http.StatusServiceUnavailable, "updater service unavailable")
		return
	}
	var req updater.ConfigInput
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	st, err := h.updater.UpdateConfig(r.Context(), req)
	if err != nil {
		if errors.Is(err, updater.ErrInvalidConfig) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *handler) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	if h.updater == nil {
		writeError(w, http.StatusServiceUnavailable, "updater service unavailable")
		return
	}
	st, err := h.updater.CheckNow(r.Context())
	if err != nil {
		switch {
		case errors.Is(err, updater.ErrBusy):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, updater.ErrUpstream):
			writeError(w, http.StatusBadGateway, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *handler) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if h.updater == nil {
		writeError(w, http.StatusServiceUnavailable, "updater service unavailable")
		return
	}
	st, err := h.updater.StartApply(h.lifecycleCtx)
	if err != nil {
		switch {
		case errors.Is(err, updater.ErrBusy):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, updater.ErrCannotApply):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, st)
}
