package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"coursefin/internal/auth"

	"github.com/go-chi/chi/v5"
)

func (h *handler) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	needs, err := h.auth.NeedsSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needs_setup": needs})
}

func (h *handler) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		ServerName string `json:"server_name"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.auth.SetupAdmin(r.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, auth.ErrAlreadySetup):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if name := strings.TrimSpace(req.ServerName); name != "" {
		now := time.Now().UTC().Format(time.RFC3339)
		_, _ = h.db.ExecContext(r.Context(), `
			INSERT INTO app_settings (key, value, updated_at) VALUES ('server_name', ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			name, now,
		)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user":        user,
		"needs_setup": false,
	})
}

func (h *handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		DeviceID      string `json:"device_id"`
		DeviceName    string `json:"device_name"`
		ClientName    string `json:"client_name"`
		ClientVersion string `json:"client_version"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	_, hdrDev := auth.ParseAuthHeader(r)
	dev := auth.DeviceInfo{
		DeviceID:      strings.TrimSpace(req.DeviceID),
		DeviceName:    strings.TrimSpace(req.DeviceName),
		ClientName:    strings.TrimSpace(req.ClientName),
		ClientVersion: strings.TrimSpace(req.ClientVersion),
	}
	if dev.DeviceID == "" {
		dev.DeviceID = hdrDev.DeviceID
	}
	if dev.DeviceName == "" {
		dev.DeviceName = hdrDev.DeviceName
	}
	if dev.ClientName == "" {
		dev.ClientName = hdrDev.ClientName
	}
	if dev.ClientVersion == "" {
		dev.ClientVersion = hdrDev.ClientVersion
	}
	if dev.ClientName == "" {
		dev.ClientName = "CourseFin Web"
	}

	sess, user, err := h.auth.Login(r.Context(), req.Username, req.Password, dev)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":   sess.Token,
		"user":    user,
		"session": sess,
	})
}

func (h *handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	if ac != nil && ac.Token != "" {
		if err := h.auth.RevokeSession(r.Context(), ac.Token); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleMe(w http.ResponseWriter, r *http.Request) {
	ac := currentAuth(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":    ac.User,
		"session": ac.Session,
	})
}

func (h *handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.auth.ChangePassword(r.Context(), req.OldPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusBadRequest, "current password is incorrect")
		case errors.Is(err, auth.ErrNotSetup):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.auth.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		sessions = []auth.Session{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *handler) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		writeError(w, http.StatusBadRequest, "session token is required")
		return
	}
	if err := h.auth.RevokeSession(r.Context(), token); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
