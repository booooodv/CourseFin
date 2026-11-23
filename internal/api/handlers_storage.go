package api

import (
	"errors"
	"net/http"
	"strings"

	"coursefin/internal/catalog"
	"coursefin/internal/scanner"

	"github.com/go-chi/chi/v5"
)

func (h *handler) handleListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.cat.ListSources(r.Context())
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if sources == nil {
		sources = []catalog.StorageSource{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (h *handler) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Driver   string `json:"driver"`
		RootPath string `json:"root_path"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	src, err := h.cat.CreateSource(r.Context(), req.Name, req.Driver, req.RootPath)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, src)
}

func (h *handler) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	sourceID := normalizeID(chi.URLParam(r, "id"))
	if err := h.cat.DeleteSource(r.Context(), sourceID); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) handleScanSource(w http.ResponseWriter, r *http.Request) {
	sourceID := normalizeID(chi.URLParam(r, "id"))

	asyncRaw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("async")))
	if asyncRaw == "1" || asyncRaw == "true" {
		st, err := h.scanner.StartScanAsync(h.lifecycleCtx, sourceID)
		if err != nil {
			switch {
			case errors.Is(err, scanner.ErrSourceNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, scanner.ErrScanInProgress):
				writeError(w, http.StatusConflict, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusAccepted, st)
		return
	}

	rep, err := h.scanner.ScanSource(r.Context(), sourceID)
	if err != nil {
		switch {
		case errors.Is(err, scanner.ErrSourceNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, scanner.ErrScanInProgress):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (h *handler) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	sourceID := normalizeID(chi.URLParam(r, "id"))
	if _, err := h.cat.GetSource(r.Context(), sourceID); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.scanner.Status(sourceID))
}

func (h *handler) handleListDirs(w http.ResponseWriter, r *http.Request) {
	sourceID := normalizeID(chi.URLParam(r, "id"))
	prefix := r.URL.Query().Get("prefix")
	dirs, err := h.cat.ListIndexedDirs(r.Context(), sourceID, prefix)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if dirs == nil {
		dirs = []catalog.IndexedDir{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"dirs": dirs})
}

func (h *handler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	sourceID := normalizeID(chi.URLParam(r, "id"))
	if _, err := h.cat.GetSource(r.Context(), sourceID); err != nil {
		writeCatalogError(w, err)
		return
	}
	q := r.URL.Query()
	files, err := h.cat.ListMediaFiles(r.Context(), sourceID, q.Get("prefix"), q.Get("search"), q.Get("status"))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if files == nil {
		files = []catalog.MediaFile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (h *handler) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceID   string `json:"source_id"`
		DirRelPath string `json:"dir_rel_path"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	preview, err := h.cat.PreviewDirectoryImport(r.Context(), req.SourceID, req.DirRelPath)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *handler) handleImportCommit(w http.ResponseWriter, r *http.Request) {
	var req catalog.CommitImportInput
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	course, err := h.cat.CommitDirectoryImport(r.Context(), req)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, course)
}
