package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"coursefin/internal/auth"
	"coursefin/internal/catalog"
	"coursefin/internal/id"
	"coursefin/internal/media"
	"coursefin/internal/scanner"
	"coursefin/internal/storage"
	"coursefin/internal/updater"

	"github.com/go-chi/chi/v5"
)

const maxJSONBodyBytes = 2 << 20 // 2 MiB

type ctxKey int

const authCtxKey ctxKey = iota

type authContext struct {
	Session *auth.Session
	User    *auth.User
	Token   string
}

// Deps holds all dependencies required by the CourseFin Management REST API.
type Deps struct {
	DB           *sql.DB
	DataDir      string
	BaseURLHint  string
	Registry     *storage.Registry
	Auth         *auth.Service
	Scanner      *scanner.Service
	Catalog      *catalog.Service
	Media        *media.Service
	Updater      *updater.Service
	AIHTTPClient *http.Client
	LifecycleCtx context.Context
}

type handler struct {
	db           *sql.DB
	dataDir      string
	baseURLHint  string
	registry     *storage.Registry
	auth         *auth.Service
	scanner      *scanner.Service
	cat          *catalog.Service
	media        *media.Service
	updater      *updater.Service
	aiHTTPClient *http.Client
	lifecycleCtx context.Context
}

// NewHandler builds an http.Handler serving all CourseFin /api/v1 endpoints.
func NewHandler(deps Deps) http.Handler {
	reg := deps.Registry
	if reg == nil {
		reg = storage.NewRegistry()
	}
	lctx := deps.LifecycleCtx
	if lctx == nil {
		lctx = context.Background()
	}
	h := &handler{
		db:           deps.DB,
		dataDir:      deps.DataDir,
		baseURLHint:  strings.TrimRight(strings.TrimSpace(deps.BaseURLHint), "/"),
		registry:     reg,
		auth:         deps.Auth,
		scanner:      deps.Scanner,
		cat:          deps.Catalog,
		media:        deps.Media,
		updater:      deps.Updater,
		aiHTTPClient: deps.AIHTTPClient,
		lifecycleCtx: lctx,
	}

	r := chi.NewRouter()
	r.Route("/api/v1", func(apiR chi.Router) {
		// Public setup & login endpoints
		apiR.Get("/setup/status", h.handleSetupStatus)
		apiR.Post("/setup", h.handleSetup)
		apiR.Post("/auth/login", h.handleLogin)

		// Protected management endpoints
		apiR.Group(func(pr chi.Router) {
			pr.Use(h.requireAuth)

			// Auth & sessions
			pr.Post("/auth/logout", h.handleLogout)
			pr.Get("/auth/me", h.handleMe)
			pr.Post("/auth/password", h.handleChangePassword)
			pr.Get("/sessions", h.handleListSessions)
			pr.Delete("/sessions/{token}", h.handleRevokeSession)

			// Storage sources & scanner
			pr.Get("/fs/dirs", h.handleBrowseFSDirs)
			pr.Get("/sources", h.handleListSources)
			pr.Post("/sources", h.handleCreateSource)
			pr.Delete("/sources/{id}", h.handleDeleteSource)
			pr.Post("/sources/{id}/scan", h.handleScanSource)
			pr.Get("/sources/{id}/scan/status", h.handleScanStatus)
			pr.Get("/sources/{id}/dirs", h.handleListDirs)
			pr.Get("/sources/{id}/files", h.handleListFiles)
			pr.Get("/sources/{id}/subtitles", h.handleListSourceSubtitles)
			pr.Put("/subtitles/{id}/bind", h.handleBindSubtitle)
			pr.Post("/import/preview", h.handleImportPreview)
			pr.Post("/import/commit", h.handleImportCommit)

			// Libraries
			pr.Get("/libraries", h.handleListLibraries)
			pr.Post("/libraries", h.handleCreateLibrary)
			pr.Put("/libraries/{id}", h.handleUpdateLibrary)
			pr.Delete("/libraries/{id}", h.handleDeleteLibrary)

			// Courses & structure
			pr.Get("/courses", h.handleListCourses)
			pr.Post("/courses", h.handleCreateCourse)
			pr.Get("/courses/{id}", h.handleGetCourse)
			pr.Put("/courses/{id}", h.handleUpdateCourse)
			pr.Delete("/courses/{id}", h.handleDeleteCourse)
			pr.Post("/courses/{id}/cover", h.handleUploadCourseCover)
			pr.Post("/courses/{id}/backdrop", h.handleUploadCourseBackdrop)
			pr.Get("/courses/{id}/new-files", h.handleGetCourseNewFiles)
			pr.Post("/courses/{id}/new-files", h.handleAppendCourseNewFiles)

			pr.Post("/courses/{id}/chapters", h.handleCreateChapter)
			pr.Put("/chapters/{id}", h.handleUpdateChapter)
			pr.Delete("/chapters/{id}", h.handleDeleteChapter)
			pr.Put("/courses/{id}/chapters/reorder", h.handleReorderChapters)

			pr.Post("/chapters/{id}/lessons", h.handleAddLesson)
			pr.Put("/lessons/{id}", h.handleUpdateLesson)
			pr.Delete("/lessons/{id}", h.handleDeleteLesson)
			pr.Put("/courses/{id}/lessons/reorder", h.handleReorderLessons)

			// Playback & home
			pr.Get("/lessons/{id}/playback", h.handleLessonPlayback)
			pr.Post("/lessons/{id}/progress", h.handleReportProgress)
			pr.Post("/lessons/{id}/played", h.handleSetPlayed)
			pr.Get("/tags", h.handleListTags)
			pr.Get("/home", h.handleHome)

			// Settings & AI
			pr.Get("/settings", h.handleGetSettings)
			pr.Put("/settings", h.handleUpdateSettings)
			pr.Post("/settings/ai/test", h.handleTestAI)
			pr.Post("/courses/{id}/ai/organize", h.handleAIOrganize)
			pr.Post("/courses/{id}/ai/apply", h.handleAIApply)

			// System updates
			pr.Get("/system/update", h.handleGetUpdateStatus)
			pr.Put("/system/update/config", h.handleUpdateUpdateConfig)
			pr.Post("/system/update/check", h.handleCheckUpdate)
			pr.Post("/system/update/apply", h.handleApplyUpdate)
		})
	})

	return r
}

func (h *handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := extractAPIToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		sess, user, err := h.auth.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), authCtxKey, &authContext{
			Session: sess,
			User:    user,
			Token:   token,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentAuth(r *http.Request) *authContext {
	ac, _ := r.Context().Value(authCtxKey).(*authContext)
	return ac
}

func extractAPIToken(r *http.Request) (string, auth.DeviceInfo) {
	if r == nil {
		return "", auth.DeviceInfo{}
	}
	if rawAuth := strings.TrimSpace(r.Header.Get("Authorization")); rawAuth != "" {
		if len(rawAuth) > 7 && strings.EqualFold(rawAuth[:7], "bearer ") {
			return strings.TrimSpace(rawAuth[7:]), auth.DeviceInfo{}
		}
	}
	token, dev := auth.ParseAuthHeader(r)
	if token == "" && r.URL != nil {
		for k, vs := range r.URL.Query() {
			if (strings.EqualFold(k, "api_key") || strings.EqualFold(k, "ApiKey")) && len(vs) > 0 {
				if v := strings.TrimSpace(vs[0]); v != "" {
					token = v
					break
				}
			}
		}
	}
	return strings.TrimSpace(token), dev
}

func normalizeID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if norm, ok := id.Normalize(trimmed); ok {
		return norm
	}
	return trimmed
}

func decodeJSON(r *http.Request, dst any, allowEmpty bool) error {
	if r.Body == nil {
		if allowEmpty {
			return nil
		}
		return errors.New("request body is required")
	}
	lr := io.LimitReader(r.Body, maxJSONBodyBytes)
	err := json.NewDecoder(lr).Decode(dst)
	if errors.Is(err, io.EOF) && allowEmpty {
		return nil
	}
	return err
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeCatalogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, catalog.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, catalog.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
