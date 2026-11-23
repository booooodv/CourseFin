package jellyfin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"coursefin/internal/auth"
	"coursefin/internal/catalog"
	"coursefin/internal/id"
	"coursefin/internal/media"

	"github.com/go-chi/chi/v5"
)

type ctxKey int

const (
	origPathKey ctxKey = iota
	authCtxKey
)

type authContext struct {
	Session *auth.Session
	User    *auth.User
	Token   string
}

// Handler exposes the Jellyfin-compatible HTTP and WebSocket API.
type Handler struct {
	db    *sql.DB
	auth  *auth.Service
	cat   *catalog.Service
	media *media.Service
}

// NewHandler creates an http.Handler serving all Jellyfin-compatible endpoints.
func NewHandler(db *sql.DB, authSvc *auth.Service, catSvc *catalog.Service, mediaSvc *media.Service) http.Handler {
	h := &Handler{
		db:    db,
		auth:  authSvc,
		cat:   catSvc,
		media: mediaSvc,
	}

	r := chi.NewRouter()
	r.Use(normalizeJellyfinPathMiddleware)

	// Public system, branding, QuickConnect, auth, WebSocket, and anonymous image endpoints.
	r.Get("/system/info/public", h.handlePublicSystemInfo)
	r.Get("/system/ping", h.handlePing)
	r.Post("/system/ping", h.handlePing)
	r.Get("/branding/configuration", h.handleBrandingConfig)
	r.Get("/branding/css", h.handleBrandingCSS)
	r.Get("/branding/css.css", h.handleBrandingCSS)
	r.Get("/quickconnect/enabled", h.handleQuickConnectEnabled)
	r.Get("/users/public", h.handlePublicUsers)
	r.Post("/users/authenticatebyname", h.handleAuthenticateByName)
	r.Get("/socket", h.handleSocket)

	r.Get("/items/{itemId}/images/{imageType}", h.handleItemImage)
	r.Head("/items/{itemId}/images/{imageType}", h.handleItemImage)
	r.Get("/items/{itemId}/images/{imageType}/{imageIndex}", h.handleItemImage)
	r.Head("/items/{itemId}/images/{imageType}/{imageIndex}", h.handleItemImage)

	// Protected Jellyfin endpoints.
	r.Group(func(pr chi.Router) {
		pr.Use(h.requireAuth)

		// System & DisplayPreferences & Counts
		pr.Get("/system/info", h.handleSystemInfo)
		pr.Get("/system/endpoint", h.handleEndpoint)
		pr.Get("/displaypreferences/{displayPreferencesId}", h.handleGetDisplayPreferences)
		pr.Post("/displaypreferences/{displayPreferencesId}", h.handlePostDisplayPreferences)
		pr.Get("/items/counts", h.handleItemCounts)

		// Users & Sessions
		pr.Get("/users/me", h.handleGetCurrentUser)
		pr.Get("/users/{userId}", h.handleGetUser)
		pr.Post("/sessions/logout", h.handleLogout)
		pr.Post("/sessions/capabilities", h.handleCapabilities)
		pr.Post("/sessions/capabilities/full", h.handleCapabilities)
		pr.Get("/sessions", h.handleGetSessions)

		// Catalog Views, Items, Shows, Genres, Tags, Filters, Search
		pr.Get("/users/{userId}/views", h.handleUserViews)
		pr.Get("/users/{userId}/items", h.handleItems)
		pr.Get("/items", h.handleItems)
		pr.Get("/users/{userId}/items/resume", h.handleResume)
		pr.Get("/users/{userId}/items/latest", h.handleLatest)
		pr.Get("/users/{userId}/items/{itemId}", h.handleGetItem)
		pr.Get("/items/{itemId}", h.handleGetItem)

		pr.Get("/users/{userId}/items/{itemId}/similar", h.handleSimilar)
		pr.Get("/items/{itemId}/similar", h.handleSimilar)
		pr.Get("/users/{userId}/items/{itemId}/intros", h.handleIntros)
		pr.Get("/items/{itemId}/intros", h.handleIntros)
		pr.Get("/users/{userId}/items/{itemId}/specialfeatures", h.handleSpecialFeatures)
		pr.Get("/items/{itemId}/specialfeatures", h.handleSpecialFeatures)

		pr.Get("/shows/nextup", h.handleNextUp)
		pr.Get("/shows/{seriesId}/seasons", h.handleSeasons)
		pr.Get("/shows/{seriesId}/episodes", h.handleEpisodes)

		pr.Get("/genres", h.handleGenres)
		pr.Get("/tags", h.handleTags)
		pr.Get("/items/filters", h.handleFilters)
		pr.Get("/items/filters2", h.handleFilters2)
		pr.Get("/search/hints", h.handleSearchHints)

		// PlaybackInfo, Streaming, HLS Remux, Subtitles
		pr.Post("/items/{itemId}/playbackinfo", h.handlePlaybackInfo)
		pr.Get("/items/{itemId}/playbackinfo", h.handlePlaybackInfo)
		pr.Post("/videos/{itemId}/playbackinfo", h.handlePlaybackInfo)
		pr.Get("/videos/{itemId}/playbackinfo", h.handlePlaybackInfo)

		pr.Get("/videos/{itemId}/stream", h.handleVideoStream)
		pr.Head("/videos/{itemId}/stream", h.handleVideoStream)
		pr.Get("/videos/{itemId}/stream.{container}", h.handleVideoStream)
		pr.Head("/videos/{itemId}/stream.{container}", h.handleVideoStream)

		pr.Get("/videos/{itemId}/master.m3u8", h.handleMasterPlaylist)
		pr.Head("/videos/{itemId}/master.m3u8", h.handleMasterPlaylist)
		pr.Get("/videos/{itemId}/main.m3u8", h.handleMediaPlaylist)
		pr.Head("/videos/{itemId}/main.m3u8", h.handleMediaPlaylist)
		pr.Get("/videos/{itemId}/hls1/main/{segment}", h.handleHLSSegment)
		pr.Head("/videos/{itemId}/hls1/main/{segment}", h.handleHLSSegment)
		pr.Delete("/videos/activeencodings", h.handleDeleteActiveEncodings)

		pr.Get("/videos/{itemId}/{mediaSourceId}/subtitles/{index}/stream.{format}", h.handleSubtitleStream)
		pr.Head("/videos/{itemId}/{mediaSourceId}/subtitles/{index}/stream.{format}", h.handleSubtitleStream)
		pr.Get("/videos/{itemId}/{mediaSourceId}/subtitles/{index}/{offset}/stream.{format}", h.handleSubtitleStream)
		pr.Head("/videos/{itemId}/{mediaSourceId}/subtitles/{index}/{offset}/stream.{format}", h.handleSubtitleStream)

		// Progress & UserData Sync
		pr.Post("/sessions/playing", h.handlePlaybackStart)
		pr.Post("/sessions/playing/progress", h.handlePlaybackProgress)
		pr.Post("/sessions/playing/stopped", h.handlePlaybackStopped)
		pr.Post("/users/{userId}/playeditems/{itemId}", h.handleMarkPlayed)
		pr.Delete("/users/{userId}/playeditems/{itemId}", h.handleMarkUnplayed)
		pr.Get("/users/{userId}/items/{itemId}/userdata", h.handleGetUserData)
		pr.Post("/users/{userId}/items/{itemId}/userdata", h.handlePostUserData)
	})

	return r
}

func normalizeJellyfinPathMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		rawPath := ""
		if rctx != nil && rctx.RoutePath != "" {
			rawPath = rctx.RoutePath
		} else {
			rawPath = r.URL.Path
		}

		cleaned := cleanSegmentSlashes(rawPath)
		stripped := stripEmbyPrefix(cleaned)
		if rctx != nil {
			rctx.RoutePath = strings.ToLower(stripped)
		}

		ctx := context.WithValue(r.Context(), origPathKey, stripped)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func cleanSegmentSlashes(raw string) string {
	parts := strings.Split(raw, "/")
	kept := make([]string, 0, len(parts))
	for _, seg := range parts {
		if seg == "" {
			continue
		}
		kept = append(kept, seg)
	}
	if len(kept) == 0 {
		return "/"
	}
	return "/" + strings.Join(kept, "/")
}

func stripEmbyPrefix(p string) string {
	if strings.EqualFold(p, "/emby") {
		return "/"
	}
	if len(p) > len("/emby/") && strings.EqualFold(p[:len("/emby/")], "/emby/") {
		return p[len("/emby"):]
	}
	return p
}

// pathParam returns the named route parameter with its original request casing preserved.
func pathParam(r *http.Request, name string) string {
	origPath, _ := r.Context().Value(origPathKey).(string)
	rctx := chi.RouteContext(r.Context())
	if origPath != "" && rctx != nil && len(rctx.RoutePatterns) > 0 {
		pattern := rctx.RoutePatterns[len(rctx.RoutePatterns)-1]
		patSegs := splitTrimSlashes(pattern)
		origSegs := splitTrimSlashes(origPath)
		if len(patSegs) == len(origSegs) {
			token := "{" + name + "}"
			for i, pseg := range patSegs {
				if pseg == token {
					return origSegs[i]
				}
				if idx := strings.Index(pseg, token); idx >= 0 {
					prefix := pseg[:idx]
					suffix := pseg[idx+len(token):]
					oseg := origSegs[i]
					if len(oseg) >= len(prefix)+len(suffix) &&
						strings.EqualFold(oseg[:len(prefix)], prefix) &&
						strings.EqualFold(oseg[len(oseg)-len(suffix):], suffix) {
						return oseg[len(prefix) : len(oseg)-len(suffix)]
					}
				}
			}
		}
	}
	return chi.URLParam(r, name)
}

func splitTrimSlashes(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := extractToken(r)
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

func extractToken(r *http.Request) (string, auth.DeviceInfo) {
	token, dev := auth.ParseAuthHeader(r)
	if token == "" {
		if q := queryParam(r, "api_key"); q != "" {
			token = q
		} else if q := queryParam(r, "ApiKey"); q != "" {
			token = q
		}
	}
	return strings.TrimSpace(token), dev
}

func resolveUserMatch(r *http.Request, user *auth.User) bool {
	if user == nil {
		return false
	}
	raw := strings.TrimSpace(pathParam(r, "userId"))
	if raw == "" {
		raw = strings.TrimSpace(queryParam(r, "UserId"))
	}
	if raw == "" || strings.EqualFold(raw, "me") {
		return true
	}
	norm, ok := id.Normalize(raw)
	if !ok {
		return false
	}
	return norm == "00000000000000000000000000000000" || norm == user.ID
}

func normalizeID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if norm, ok := id.Normalize(trimmed); ok {
		return norm
	}
	return trimmed
}

func queryParam(r *http.Request, key string) string {
	if r == nil || r.URL == nil {
		return ""
	}
	q := r.URL.Query()
	if v := strings.TrimSpace(q.Get(key)); v != "" {
		return v
	}
	for k, vs := range q {
		if strings.EqualFold(k, key) && len(vs) > 0 {
			if v := strings.TrimSpace(vs[0]); v != "" {
				return v
			}
		}
	}
	return ""
}

func queryInt(r *http.Request, key string, def int) int {
	raw := queryParam(r, key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func queryInt64(r *http.Request, key string) *int64 {
	raw := queryParam(r, key)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func queryBool(r *http.Request, key string) *bool {
	raw := queryParam(r, key)
	if raw == "" {
		return nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}
	return &b
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
