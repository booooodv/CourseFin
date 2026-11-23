package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"coursefin/internal/api"
	"coursefin/internal/auth"
	"coursefin/internal/catalog"
	"coursefin/internal/config"
	"coursefin/internal/db"
	"coursefin/internal/jellyfin"
	"coursefin/internal/media"
	"coursefin/internal/scanner"
	"coursefin/internal/storage"
)

const defaultScanInterval = 60 * time.Minute

// Server assembles SQLite, storage, scanner, catalog, media, AI, REST API,
// embedded Web SPA, and Jellyfin-compatible HTTP/WebSocket handlers.
type Server struct {
	cfg       config.Config
	db        *sql.DB
	media     *media.Service
	scanner   *scanner.Service
	handler   http.Handler
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// New initializes all CourseFin backend services and builds the unified router.
func New(cfg config.Config) (*Server, error) {
	conn, err := db.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	reg := storage.NewRegistry()
	authSvc := auth.NewService(conn)
	if cfg.AdminUser != "" && cfg.AdminPass != "" {
		if err := authSvc.EnsureEnvAdmin(context.Background(), cfg.AdminUser, cfg.AdminPass); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("ensure env admin: %w", err)
		}
	}

	prober := scanner.NewFFmpegProber("", "")
	scanSvc := scanner.NewService(conn, cfg.DataDir, reg, prober)
	catSvc := catalog.NewService(conn, cfg.DataDir)
	catSvc.SetRegistry(reg)
	mediaSvc := media.NewService(conn, cfg.DataDir, reg, "")

	ctx, cancel := context.WithCancel(context.Background())

	apiHandler := api.NewHandler(api.Deps{
		DB:           conn,
		DataDir:      cfg.DataDir,
		BaseURLHint:  cfg.BaseURL,
		Registry:     reg,
		Auth:         authSvc,
		Scanner:      scanSvc,
		Catalog:      catSvc,
		Media:        mediaSvc,
		LifecycleCtx: ctx,
	})
	jfHandler := jellyfin.NewHandler(conn, authSvc, catSvc, mediaSvc)

	webSubFS, err := fs.Sub(webDistFS, "webdist")
	if err != nil {
		cancel()
		_ = mediaSvc.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("sub webdist fs: %w", err)
	}

	srv := &Server{
		cfg:     cfg,
		db:      conn,
		media:   mediaSvc,
		scanner: scanSvc,
		cancel:  cancel,
	}

	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		scanSvc.StartPeriodicLoop(ctx, func() time.Duration {
			return readScanInterval(ctx, conn)
		})
	}()

	srv.handler = buildTopHandler(apiHandler, jfHandler, webSubFS)
	return srv, nil
}

// Handler returns the unified root http.Handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Close cancels background tasks (including periodic scans and active FFmpeg remuxers)
// and closes the SQLite database connection.
func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
		if s.scanner != nil {
			s.scanner.Wait()
		}

		var errs []error
		if s.media != nil {
			if err := s.media.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if s.db != nil {
			if err := s.db.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		closeErr = errors.Join(errs...)
	})
	return closeErr
}

func readScanInterval(ctx context.Context, conn *sql.DB) time.Duration {
	var raw string
	err := conn.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = 'scan_interval_minutes'`).Scan(&raw)
	if err != nil {
		return defaultScanInterval
	}
	if mins, convErr := strconv.Atoi(strings.TrimSpace(raw)); convErr == nil && mins > 0 {
		return time.Duration(mins) * time.Minute
	}
	return defaultScanInterval
}

func buildTopHandler(apiHandler, jfHandler http.Handler, webSubFS fs.FS) http.Handler {
	fileServer := http.FileServerFS(webSubFS)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path

		if reqPath == "/health" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
			}
			return
		}

		if reqPath == "/api/v1" || strings.HasPrefix(reqPath, "/api/v1/") {
			apiHandler.ServeHTTP(w, r)
			return
		}

		if reqPath == "/web" {
			http.Redirect(w, r, "/web/", http.StatusFound)
			return
		}

		if strings.HasPrefix(reqPath, "/web/") {
			serveWebSPA(w, r, webSubFS, fileServer)
			return
		}

		if reqPath == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
				http.Redirect(w, r, "/web/", http.StatusFound)
				return
			}
		}

		jfHandler.ServeHTTP(w, r)
	})
}

func serveWebSPA(w http.ResponseWriter, r *http.Request, webSubFS fs.FS, fileServer http.Handler) {
	rel := strings.TrimPrefix(r.URL.Path, "/web/")
	cleaned := strings.TrimPrefix(path.Clean("/"+rel), "/")

	if cleaned == "" || cleaned == "." || cleaned == "index.html" {
		serveIndexHTML(w, r, webSubFS)
		return
	}

	if info, err := fs.Stat(webSubFS, cleaned); err == nil && !info.IsDir() {
		r2 := r.Clone(r.Context())
		u := *r.URL
		u.Path = "/" + cleaned
		r2.URL = &u
		fileServer.ServeHTTP(w, r2)
		return
	}

	if isMissingStaticAsset(cleaned) {
		http.NotFound(w, r)
		return
	}

	serveIndexHTML(w, r, webSubFS)
}

func serveIndexHTML(w http.ResponseWriter, r *http.Request, webSubFS fs.FS) {
	raw, err := fs.ReadFile(webSubFS, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(raw))
}

func isMissingStaticAsset(cleaned string) bool {
	if strings.HasPrefix(cleaned, "assets/") {
		return true
	}
	ext := strings.ToLower(path.Ext(cleaned))
	switch ext {
	case ".js", ".mjs", ".css", ".map", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".json":
		return true
	default:
		return false
	}
}
