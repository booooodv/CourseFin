package updater_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coursefin/internal/db"
	"coursefin/internal/updater"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newHandlerHTTPClient(h http.Handler) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			resp := rec.Result()
			resp.Request = req
			return resp, nil
		}),
	}
}

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }
func strPtr(v string) *string {
	return &v
}

func TestSemVer(t *testing.T) {
	if got := updater.NormalizeVersion("v1.2.3"); got != "1.2.3" {
		t.Fatalf("expected NormalizeVersion(\"v1.2.3\") == \"1.2.3\", got %q", got)
	}
	if got := updater.NormalizeVersion("  v1.2.3\n"); got != "1.2.3" {
		t.Fatalf("expected trimmed NormalizeVersion, got %q", got)
	}

	if cmp, err := updater.CompareVersion("1.0.2", "1.0.1"); err != nil || cmp <= 0 {
		t.Fatalf("expected 1.0.2 > 1.0.1, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := updater.CompareVersion("1.0.10", "1.0.2"); err != nil || cmp <= 0 {
		t.Fatalf("expected 1.0.10 > 1.0.2, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := updater.CompareVersion("1.0.2-rc.1", "1.0.2"); err != nil || cmp >= 0 {
		t.Fatalf("expected 1.0.2-rc.1 < 1.0.2, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := updater.CompareVersion("1.0.2-rc.2", "1.0.2-rc.10"); err != nil || cmp >= 0 {
		t.Fatalf("expected 1.0.2-rc.2 < 1.0.2-rc.10, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := updater.CompareVersion("1.0.2-1", "1.0.2-alpha"); err != nil || cmp >= 0 {
		t.Fatalf("expected numeric prerelease < alpha prerelease, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := updater.CompareVersion("1.0.2-alpha", "1.0.2-alpha.1"); err != nil || cmp >= 0 {
		t.Fatalf("expected shorter prerelease < longer prerelease, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := updater.CompareVersion("v1.0.1", "1.0.1"); err != nil || cmp != 0 {
		t.Fatalf("expected v1.0.1 == 1.0.1, got cmp=%d err=%v", cmp, err)
	}
	if cmp, err := updater.CompareVersion("1.0.1+build.1", "1.0.1+build.2"); err != nil || cmp != 0 {
		t.Fatalf("expected build metadata ignored in compare, got cmp=%d err=%v", cmp, err)
	}

	if updater.IsValidSemVer("dev") {
		t.Fatalf("expected IsValidSemVer(\"dev\") == false")
	}
	if updater.IsValidSemVer("1.2") {
		t.Fatalf("expected IsValidSemVer(\"1.2\") == false")
	}
	if updater.IsValidSemVer("01.2.3") {
		t.Fatalf("expected leading zero major version to be invalid")
	}
	if updater.IsValidSemVer("1.2.3-01") {
		t.Fatalf("expected leading zero numeric prerelease to be invalid")
	}
	if !updater.IsValidSemVer("v1.2.3") {
		t.Fatalf("expected IsValidSemVer(\"v1.2.3\") == true")
	}
	if !updater.IsValidSemVer("0.0.0-dev") {
		t.Fatalf("expected IsValidSemVer(\"0.0.0-dev\") == true")
	}
	if _, err := updater.ParseVersion("dev"); err == nil {
		t.Fatalf("expected ParseVersion(\"dev\") to return error")
	}
}

func TestCheckNowSuccessAndETag(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	managedDir := filepath.Join(t.TempDir(), "bin")
	goos := "linux"
	goarch := "amd64"
	expectedAsset := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", goos, goarch)

	var mu sync.Mutex
	var reqCount int
	var gotIfNoneMatch []string

	client := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqCount++
		n := reqCount
		gotIfNoneMatch = append(gotIfNoneMatch, r.Header.Get("If-None-Match"))
		mu.Unlock()

		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("unexpected Accept header: %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "CourseFin/1.0.1" {
			t.Errorf("unexpected User-Agent header: %q", got)
		}

		if n == 1 {
			w.Header().Set("ETag", `"etag-1"`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"tag_name": "v1.2.0",
				"name": "CourseFin v1.2.0",
				"body": "Release notes here",
				"html_url": "https://github.com/booooodv/CourseFin/releases/tag/v1.2.0",
				"published_at": "2026-11-24T12:00:00Z",
				"draft": false,
				"prerelease": false,
				"assets": [
					{"name": %q, "size": 14820352, "browser_download_url": "https://github.com/booooodv/CourseFin/releases/download/v1.2.0/%s"},
					{"name": "checksums.txt", "size": 512, "browser_download_url": "https://github.com/booooodv/CourseFin/releases/download/v1.2.0/checksums.txt"}
				]
			}`, expectedAsset, expectedAsset)
			return
		}

		if r.Header.Get("If-None-Match") != `"etag-1"` {
			t.Errorf("expected If-None-Match \"etag-1\", got %q", r.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))

	svc := updater.NewService(updater.Options{
		DB:             conn,
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ManagedBinDir:  managedDir,
		HTTPClient:     client,
		GOOS:           goos,
		GOARCH:         goarch,
	})

	ctx := context.Background()
	st1, err := svc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("first CheckNow failed: %v", err)
	}
	if !st1.UpdateAvailable {
		t.Fatalf("expected UpdateAvailable == true")
	}
	if st1.LatestVersion != "1.2.0" {
		t.Fatalf("expected LatestVersion 1.2.0, got %q", st1.LatestVersion)
	}
	if !st1.CanApply {
		t.Fatalf("expected CanApply == true, got reason %q", st1.CannotApplyReason)
	}
	if st1.AssetName != expectedAsset {
		t.Fatalf("expected AssetName %q, got %q", expectedAsset, st1.AssetName)
	}
	if st1.AssetSize != 14820352 {
		t.Fatalf("expected AssetSize 14820352, got %d", st1.AssetSize)
	}
	if st1.LastCheckedAt == "" || st1.LastError != "" || st1.State != "idle" {
		t.Fatalf("unexpected status metadata: %+v", st1)
	}

	// Verify exact JSON schema field names from Spec Section 5.5.
	rawJSON, err := json.Marshal(st1)
	if err != nil {
		t.Fatalf("json.Marshal(status): %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rawJSON, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	expectedKeys := []string{
		"current_version", "latest_version", "update_available",
		"release_name", "release_notes", "release_url", "published_at",
		"asset_name", "asset_size", "deploy_mode", "platform",
		"can_apply", "cannot_apply_reason", "auto_check_enabled",
		"auto_check_locked_by_env", "check_interval_hours", "mirror_url",
		"state", "target_version", "progress_bytes", "total_bytes",
		"progress_percent", "last_checked_at", "last_error",
	}
	for _, k := range expectedKeys {
		if _, ok := decoded[k]; !ok {
			t.Fatalf("missing JSON key %q in serialized Status: %s", k, string(rawJSON))
		}
	}

	st2, err := svc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("second CheckNow failed: %v", err)
	}
	if !st2.UpdateAvailable || st2.LatestVersion != "1.2.0" || st2.AssetName != expectedAsset {
		t.Fatalf("expected cached release data preserved on 304, got %+v", st2)
	}
	if st2.LastCheckedAt == "" || st2.LastError != "" {
		t.Fatalf("expected LastCheckedAt refreshed and LastError empty on 304, got %+v", st2)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(gotIfNoneMatch) != 2 || gotIfNoneMatch[0] != "" || gotIfNoneMatch[1] != `"etag-1"` {
		t.Fatalf("unexpected If-None-Match sequence: %v", gotIfNoneMatch)
	}
}

func TestCheckNowNoReleases404AndPrerelease(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var mode atomic.Int32
	client := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 0:
			w.WriteHeader(http.StatusNotFound)
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tag_name":"v2.0.0-rc.1","prerelease":true,"draft":false,"assets":[]}`))
		case 2:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tag_name":"v2.0.0","prerelease":false,"draft":true,"assets":[]}`))
		case 3:
			w.WriteHeader(http.StatusInternalServerError)
		case 4:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(strings.Repeat("a", (2<<20)+100)))
		}
	}))

	svc := updater.NewService(updater.Options{
		DB:             conn,
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ManagedBinDir:  t.TempDir(),
		HTTPClient:     client,
	})
	ctx := context.Background()

	// 404 -> no error, UpdateAvailable == false, LastError == ""
	mode.Store(0)
	st, err := svc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("expected nil error on 404, got %v", err)
	}
	if st.UpdateAvailable || st.LastError != "" || st.LastCheckedAt == "" {
		t.Fatalf("unexpected status on 404: %+v", st)
	}

	// prerelease: true -> ignored
	mode.Store(1)
	st, err = svc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("expected nil error on prerelease, got %v", err)
	}
	if st.UpdateAvailable || st.LatestVersion != "" {
		t.Fatalf("expected prerelease ignored, got %+v", st)
	}

	// draft: true -> ignored
	mode.Store(2)
	st, err = svc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("expected nil error on draft, got %v", err)
	}
	if st.UpdateAvailable || st.LatestVersion != "" {
		t.Fatalf("expected draft ignored, got %+v", st)
	}

	// 500 -> ErrUpstream
	mode.Store(3)
	st, err = svc.CheckNow(ctx)
	if !errors.Is(err, updater.ErrUpstream) {
		t.Fatalf("expected ErrUpstream on 500, got %v", err)
	}
	if st.LastError == "" {
		t.Fatalf("expected LastError populated on upstream 500")
	}

	// >2 MiB body -> ErrUpstream
	mode.Store(4)
	if _, err = svc.CheckNow(ctx); !errors.Is(err, updater.ErrUpstream) {
		t.Fatalf("expected ErrUpstream on oversized body, got %v", err)
	}
}

func TestMirrorFallbackAndTokenIsolation(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var mu sync.Mutex
	var mirrorStatus int = http.StatusOK
	var mirrorAuths []string
	var mirrorPaths []string
	var directAuths []string
	var directIfNoneMatch []string

	client := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		releaseJSON := `{"tag_name":"v1.2.0","name":"v1.2.0","body":"ok","html_url":"https://github.com/booooodv/CourseFin/releases/tag/v1.2.0","published_at":"2026-11-24T12:00:00Z","assets":[]}`

		switch r.URL.Host {
		case "mirror.example":
			mirrorAuths = append(mirrorAuths, r.Header.Get("Authorization"))
			mirrorPaths = append(mirrorPaths, r.URL.Path)
			w.Header().Set("ETag", `"mirror-etag"`)
			if mirrorStatus != http.StatusOK {
				w.WriteHeader(mirrorStatus)
				return
			}
			_, _ = w.Write([]byte(releaseJSON))
		case "api.github.example":
			directAuths = append(directAuths, r.Header.Get("Authorization"))
			directIfNoneMatch = append(directIfNoneMatch, r.Header.Get("If-None-Match"))
			w.Header().Set("ETag", `"direct-etag"`)
			_, _ = w.Write([]byte(releaseJSON))
		default:
			t.Errorf("unexpected host %q", r.URL.Host)
			w.WriteHeader(http.StatusBadGateway)
		}
	}))

	svc := updater.NewService(updater.Options{
		DB:             conn,
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		Repo:           "booooodv/CourseFin",
		APIBase:        "https://api.github.example",
		GitHubToken:    "secret-tok",
		ManagedBinDir:  t.TempDir(),
		HTTPClient:     client,
	})

	ctx := context.Background()
	if _, err := svc.UpdateConfig(ctx, updater.ConfigInput{
		MirrorURL: strPtr("https://mirror.example/"),
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// 1. Mirror succeeds (200) -> mirror gets empty Authorization, direct not called.
	st, err := svc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("CheckNow via mirror failed: %v", err)
	}
	if !st.UpdateAvailable || st.LatestVersion != "1.2.0" {
		t.Fatalf("unexpected status via mirror: %+v", st)
	}

	mu.Lock()
	if len(mirrorAuths) != 1 || mirrorAuths[0] != "" {
		t.Fatalf("mirror must never receive Authorization header, got %v", mirrorAuths)
	}
	if len(mirrorPaths) != 1 || mirrorPaths[0] != "/https://api.github.example/repos/booooodv/CourseFin/releases/latest" {
		t.Fatalf("unexpected mirror request path: %v", mirrorPaths)
	}
	if len(directAuths) != 0 {
		t.Fatalf("direct server should not be called when mirror succeeds, got %v", directAuths)
	}
	mirrorStatus = http.StatusForbidden
	mu.Unlock()

	// 2. Mirror returns 403 -> mirror gets empty Authorization, direct fallback gets Bearer token and no stale mirror ETag.
	st, err = svc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("CheckNow with mirror fallback failed: %v", err)
	}
	if !st.UpdateAvailable || st.LatestVersion != "1.2.0" {
		t.Fatalf("unexpected status on fallback: %+v", st)
	}

	mu.Lock()
	if len(mirrorAuths) != 2 || mirrorAuths[1] != "" {
		t.Fatalf("mirror must never receive Authorization header on retry, got %v", mirrorAuths)
	}
	if len(directAuths) != 1 || directAuths[0] != "Bearer secret-tok" {
		t.Fatalf("expected direct server to receive Bearer secret-tok, got %v", directAuths)
	}
	if len(directIfNoneMatch) != 1 || directIfNoneMatch[0] != "" {
		t.Fatalf("expected no mirror ETag sent to direct server, got %v", directIfNoneMatch)
	}
	mu.Unlock()

	// 3. Changing mirror_url clears/invalidates previous ETag on next check.
	if _, err := svc.UpdateConfig(ctx, updater.ConfigInput{
		MirrorURL: strPtr(""),
	}); err != nil {
		t.Fatalf("UpdateConfig clear mirror: %v", err)
	}
	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("CheckNow after clearing mirror failed: %v", err)
	}
	mu.Lock()
	if len(directIfNoneMatch) != 2 || directIfNoneMatch[1] != "" {
		t.Fatalf("expected ETag not sent after mirror_url changed, got %v", directIfNoneMatch)
	}
	mu.Unlock()
}

func TestUpdateConfigValidationAndPersistence(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dataDir := t.TempDir()
	managedDir := t.TempDir()
	svc := updater.NewService(updater.Options{
		DB:             conn,
		DataDir:        dataDir,
		CurrentVersion: "1.0.1",
		ManagedBinDir:  managedDir,
	})

	ctx := context.Background()
	st := svc.Status(ctx)
	if !st.AutoCheckEnabled || st.AutoCheckLockedByEnv || st.CheckIntervalHours != 6 || st.MirrorURL != "" {
		t.Fatalf("unexpected initial defaults: %+v", st)
	}

	// Persist valid config.
	st, err = svc.UpdateConfig(ctx, updater.ConfigInput{
		AutoCheckEnabled:   boolPtr(false),
		CheckIntervalHours: intPtr(24),
		MirrorURL:          strPtr("https://gh-proxy.example/"),
	})
	if err != nil {
		t.Fatalf("UpdateConfig valid failed: %v", err)
	}
	if st.AutoCheckEnabled || st.CheckIntervalHours != 24 || st.MirrorURL != "https://gh-proxy.example" {
		t.Fatalf("unexpected updated config status: %+v", st)
	}

	// Verify persistence across a new Service instance reading the same DB.
	svc2 := updater.NewService(updater.Options{
		DB:             conn,
		DataDir:        dataDir,
		CurrentVersion: "1.0.1",
		ManagedBinDir:  managedDir,
	})
	st2 := svc2.Status(ctx)
	if st2.AutoCheckEnabled || st2.CheckIntervalHours != 24 || st2.MirrorURL != "https://gh-proxy.example" {
		t.Fatalf("expected persisted config in SQLite app_settings, got %+v", st2)
	}

	// Invalid interval & mirror URLs.
	for _, badHours := range []int{0, -1, 169} {
		if _, err := svc.UpdateConfig(ctx, updater.ConfigInput{CheckIntervalHours: intPtr(badHours)}); !errors.Is(err, updater.ErrInvalidConfig) {
			t.Fatalf("expected ErrInvalidConfig for hours=%d, got %v", badHours, err)
		}
	}
	for _, badMirror := range []string{"file:///etc/passwd", "ftp://mirror.local", "http://", "not-a-url", "https://user:pass@mirror.local", "https://mirror.local?x=1"} {
		if _, err := svc.UpdateConfig(ctx, updater.ConfigInput{MirrorURL: strPtr(badMirror)}); !errors.Is(err, updater.ErrInvalidConfig) {
			t.Fatalf("expected ErrInvalidConfig for mirror=%q, got %v", badMirror, err)
		}
	}

	// AutoCheckEnv locks and overrides DB value (DB currently has false).
	svcEnv := updater.NewService(updater.Options{
		DB:             conn,
		DataDir:        dataDir,
		CurrentVersion: "1.0.1",
		AutoCheckEnv:   boolPtr(true),
		ManagedBinDir:  managedDir,
	})
	stEnv := svcEnv.Status(ctx)
	if !stEnv.AutoCheckLockedByEnv || !stEnv.AutoCheckEnabled {
		t.Fatalf("expected AutoCheckLockedByEnv=true and AutoCheckEnabled=true overriding DB false, got %+v", stEnv)
	}
}

func TestEligibilityChecks(t *testing.T) {
	ctx := context.Background()

	// 1. DeployMode "docker" vs "binary".
	dockerSvc := updater.NewService(updater.Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ManagedBinDir:  t.TempDir(),
		GOOS:           "linux",
		GOARCH:         "amd64",
	})
	if st := dockerSvc.Status(ctx); st.DeployMode != "docker" || !st.CanApply || st.Platform != "linux/amd64" {
		t.Fatalf("unexpected docker mode status: %+v", st)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "coursefin")
	if err := os.WriteFile(binPath, []byte("stub"), 0o755); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
	binSvc := updater.NewService(updater.Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		GOOS:           "darwin",
		GOARCH:         "arm64",
		ExecutablePath: func() (string, error) { return binPath, nil },
	})
	if st := binSvc.Status(ctx); st.DeployMode != "binary" || !st.CanApply || st.Platform != "darwin/arm64" {
		t.Fatalf("unexpected binary mode status: %+v", st)
	}

	// 2. Dev version cannot apply.
	devSvc := updater.NewService(updater.Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "dev",
		ManagedBinDir:  t.TempDir(),
		GOOS:           "linux",
		GOARCH:         "amd64",
	})
	if st := devSvc.Status(ctx); st.CanApply || st.CannotApplyReason == "" {
		t.Fatalf("expected CanApply==false with reason for dev version, got %+v", st)
	}

	// 3. Unsupported OS (windows) cannot apply.
	winSvc := updater.NewService(updater.Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ManagedBinDir:  t.TempDir(),
		GOOS:           "windows",
		GOARCH:         "amd64",
	})
	if st := winSvc.Status(ctx); st.CanApply || st.CannotApplyReason == "" {
		t.Fatalf("expected CanApply==false with reason for windows, got %+v", st)
	}

	// 4. Unwritable target dir cannot apply (use a file path as parent so MkdirAll/CreateTemp fails even as root).
	blockerFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blockerFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	unwritableSvc := updater.NewService(updater.Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ManagedBinDir:  filepath.Join(blockerFile, "bin"),
		GOOS:           "linux",
		GOARCH:         "amd64",
	})
	if st := unwritableSvc.Status(ctx); st.CanApply || st.CannotApplyReason == "" {
		t.Fatalf("expected CanApply==false with reason for unwritable dir, got %+v", st)
	}

	// 5. Release missing current platform asset -> CanApply == false.
	clientMissingAsset := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.2.0",
			"name": "v1.2.0",
			"assets": [
				{"name": "coursefin_1.2.0_linux_amd64.tar.gz", "size": 100, "browser_download_url": "https://example.com/linux.tar.gz"},
				{"name": "checksums.txt", "size": 10, "browser_download_url": "https://example.com/checksums.txt"}
			]
		}`))
	}))
	missingAssetSvc := updater.NewService(updater.Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ManagedBinDir:  t.TempDir(),
		HTTPClient:     clientMissingAsset,
		GOOS:           "darwin",
		GOARCH:         "arm64",
	})
	st, err := missingAssetSvc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("CheckNow failed: %v", err)
	}
	if !st.UpdateAvailable || st.CanApply || st.CannotApplyReason == "" {
		t.Fatalf("expected UpdateAvailable=true, CanApply=false with reason when asset missing, got %+v", st)
	}

	// 6. SupportsHotExec matches runtime OS.
	expectedHot := runtime.GOOS == "linux" || runtime.GOOS == "darwin"
	if updater.SupportsHotExec() != expectedHot {
		t.Fatalf("SupportsHotExec() = %v, expected %v", updater.SupportsHotExec(), expectedHot)
	}
}

func TestStartPeriodicLoop(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	checkedCh := make(chan struct{}, 2)
	client := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case checkedCh <- struct{}{}:
		default:
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","assets":[]}`))
	}))

	svc := updater.NewService(updater.Options{
		DB:                conn,
		DataDir:           t.TempDir(),
		CurrentVersion:    "1.0.1",
		ManagedBinDir:     t.TempDir(),
		HTTPClient:        client,
		InitialCheckDelay: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.StartPeriodicLoop(ctx)
	}()

	select {
	case <-checkedCh:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatalf("timed out waiting for initial periodic check")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("StartPeriodicLoop did not exit on context cancel")
	}
}

func TestStartPeriodicLoopAutoCheckDisabled(t *testing.T) {
	var called atomic.Int32
	client := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","assets":[]}`))
	}))

	svc := updater.NewService(updater.Options{
		DataDir:           t.TempDir(),
		CurrentVersion:    "1.0.1",
		AutoCheckEnv:      boolPtr(false),
		ManagedBinDir:     t.TempDir(),
		HTTPClient:        client,
		InitialCheckDelay: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.StartPeriodicLoop(ctx)
	}()

	time.Sleep(40 * time.Millisecond)
	cancel()
	<-done

	if called.Load() != 0 {
		t.Fatalf("expected zero HTTP checks when auto_check is disabled, got %d", called.Load())
	}
}

func TestCheckNowConcurrentBusy(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	client := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","assets":[]}`))
	}))

	svc := updater.NewService(updater.Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ManagedBinDir:  t.TempDir(),
		HTTPClient:     client,
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.CheckNow(context.Background())
		errCh <- err
	}()

	<-started
	_, err := svc.CheckNow(context.Background())
	if !errors.Is(err, updater.ErrBusy) {
		close(release)
		t.Fatalf("expected ErrBusy for concurrent CheckNow, got %v", err)
	}
	close(release)

	if err := <-errCh; err != nil {
		t.Fatalf("first CheckNow failed: %v", err)
	}
}

type tarTestEntry struct {
	name     string
	content  []byte
	mode     int64
	typeflag byte
	linkname string
}

func buildReleaseTarGz(t *testing.T, entryName string, content []byte, mode int64) []byte {
	t.Helper()
	return buildTarGzWithEntries(t, []tarTestEntry{
		{
			name:     entryName,
			content:  content,
			mode:     mode,
			typeflag: tar.TypeReg,
		},
	})
}

func buildTarGzWithEntries(t *testing.T, entries []tarTestEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for _, entry := range entries {
		tf := entry.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     entry.name,
			Mode:     entry.mode,
			Size:     int64(len(entry.content)),
			Typeflag: tf,
			Linkname: entry.linkname,
		}
		if tf == tar.TypeSymlink || tf == tar.TypeLink || tf == tar.TypeDir {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar WriteHeader(%q): %v", entry.name, err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write(entry.content); err != nil {
				t.Fatalf("tar Write(%q): %v", entry.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	return buf.Bytes()
}

func versionStubScript(version string) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo %q\n  exit 0\nfi\nexit 1\n", version))
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func runBinaryVersion(t *testing.T, binPath string) string {
	t.Helper()
	out, err := exec.Command(binPath, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("exec %q --version failed: %v (output: %s)", binPath, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func assertNoMatchingEntry(t *testing.T, dir, prefix string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			t.Fatalf("unexpected leftover temp file %q in %q", entry.Name(), dir)
		}
	}
}

func newApplyMockClient(t *testing.T, assetName string, archiveBytes []byte, checksumsContent string) *http.Client {
	t.Helper()
	releaseURL := "https://api.github.example/repos/booooodv/CourseFin/releases/latest"
	assetURL := "https://github.example/booooodv/CourseFin/releases/download/v1.2.0/" + assetName
	checksumsURL := "https://github.example/booooodv/CourseFin/releases/download/v1.2.0/checksums.txt"

	return newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case releaseURL:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"tag_name": "v1.2.0",
				"name": "CourseFin v1.2.0",
				"body": "Notes",
				"html_url": "https://github.example/booooodv/CourseFin/releases/tag/v1.2.0",
				"published_at": "2026-11-24T12:00:00Z",
				"draft": false,
				"prerelease": false,
				"assets": [
					{"name": %q, "size": %d, "browser_download_url": %q},
					{"name": "checksums.txt", "size": %d, "browser_download_url": %q}
				]
			}`, assetName, len(archiveBytes), assetURL, len(checksumsContent), checksumsURL)
		case checksumsURL:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(checksumsContent))
		case assetURL:
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archiveBytes)))
			_, _ = w.Write(archiveBytes)
		default:
			t.Errorf("unexpected HTTP request URL: %s", r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestStartApplyDockerModeSuccess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	dataDir := t.TempDir()
	managedDir := filepath.Join(t.TempDir(), "managed-bin")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	activeBin := filepath.Join(managedDir, "coursefin")
	if err := os.WriteFile(activeBin, versionStubScript("1.0.1"), 0o755); err != nil {
		t.Fatalf("write initial stub: %v", err)
	}

	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildReleaseTarGz(t, "coursefin", versionStubScript("1.2.0"), 0o755)
	checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

	svc := updater.NewService(updater.Options{
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		ManagedBinDir:     managedDir,
		HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
		RestartGraceDelay: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("CheckNow failed: %v", err)
	}

	startSt, err := svc.StartApply(ctx)
	if err != nil {
		t.Fatalf("StartApply failed: %v", err)
	}
	if startSt.State != "downloading" || startSt.TargetVersion != "1.2.0" {
		t.Fatalf("unexpected initial StartApply status: %+v", startSt)
	}

	var req updater.RestartRequest
	select {
	case req = <-svc.RestartCh():
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for RestartCh; current status: %+v", svc.Status(ctx))
	}
	svc.Wait()

	if req.ExecPath != activeBin {
		t.Fatalf("expected ExecPath %q, got %q", activeBin, req.ExecPath)
	}
	backupBin := filepath.Join(managedDir, "coursefin.bak")
	if req.BackupPath != backupBin {
		t.Fatalf("expected BackupPath %q, got %q", backupBin, req.BackupPath)
	}
	if got := runBinaryVersion(t, activeBin); got != "1.2.0" {
		t.Fatalf("expected new active binary --version 1.2.0, got %q", got)
	}
	if got := runBinaryVersion(t, backupBin); got != "1.0.1" {
		t.Fatalf("expected backup binary --version 1.0.1, got %q", got)
	}

	pendingBytes, err := os.ReadFile(filepath.Join(managedDir, ".pending_update"))
	if err != nil {
		t.Fatalf("read .pending_update: %v", err)
	}
	if strings.TrimSpace(string(pendingBytes)) != "tried" {
		t.Fatalf("expected .pending_update to contain %q, got %q", "tried", string(pendingBytes))
	}

	finalSt := svc.Status(ctx)
	if finalSt.State != "restarting" || finalSt.ProgressPercent != 100 || finalSt.LastError != "" {
		t.Fatalf("unexpected final status: %+v", finalSt)
	}
	assertNoMatchingEntry(t, managedDir, ".coursefin.new.")
	assertNoMatchingEntry(t, filepath.Join(dataDir, "updates", "staging"), assetName+".part")
}

func TestStartApplyStandaloneBinarySuccess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	dataDir := t.TempDir()
	binDir := t.TempDir()
	exePath := filepath.Join(binDir, "coursefin")
	if err := os.WriteFile(exePath, versionStubScript("1.0.1"), 0o755); err != nil {
		t.Fatalf("write initial binary: %v", err)
	}
	resolvedExePath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", exePath, err)
	}

	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildReleaseTarGz(t, "coursefin_1.2.0/coursefin", versionStubScript("1.2.0"), 0o644)
	checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

	svc := updater.NewService(updater.Options{
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
		ExecutablePath:    func() (string, error) { return exePath, nil },
		RestartGraceDelay: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("CheckNow failed: %v", err)
	}
	if _, err := svc.StartApply(ctx); err != nil {
		t.Fatalf("StartApply failed: %v", err)
	}

	var req updater.RestartRequest
	select {
	case req = <-svc.RestartCh():
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for RestartCh; status: %+v", svc.Status(ctx))
	}
	svc.Wait()

	if req.ExecPath != resolvedExePath {
		t.Fatalf("expected ExecPath %q, got %q", resolvedExePath, req.ExecPath)
	}
	if req.BackupPath != resolvedExePath+".bak" {
		t.Fatalf("expected BackupPath %q, got %q", resolvedExePath+".bak", req.BackupPath)
	}
	if got := runBinaryVersion(t, resolvedExePath); got != "1.2.0" {
		t.Fatalf("expected updated binary --version 1.2.0, got %q", got)
	}
	if got := runBinaryVersion(t, resolvedExePath+".bak"); got != "1.0.1" {
		t.Fatalf("expected backup binary --version 1.0.1, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(resolvedExePath), ".pending_update")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no .pending_update in standalone mode, got err=%v", err)
	}
	finalSt := svc.Status(ctx)
	if finalSt.State != "restarting" || finalSt.ProgressPercent != 100 {
		t.Fatalf("unexpected final status: %+v", finalSt)
	}
}

func TestStartApplyRejectsTamperedChecksum(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	dataDir := t.TempDir()
	managedDir := t.TempDir()
	activeBin := filepath.Join(managedDir, "coursefin")
	if err := os.WriteFile(activeBin, versionStubScript("1.0.1"), 0o755); err != nil {
		t.Fatalf("write initial binary: %v", err)
	}

	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildReleaseTarGz(t, "coursefin", versionStubScript("1.2.0"), 0o755)
	badChecksums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), assetName)

	svc := updater.NewService(updater.Options{
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		ManagedBinDir:     managedDir,
		HTTPClient:        newApplyMockClient(t, assetName, archive, badChecksums),
		RestartGraceDelay: 5 * time.Millisecond,
	})

	ctx := context.Background()
	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("CheckNow failed: %v", err)
	}
	if _, err := svc.StartApply(ctx); err != nil {
		t.Fatalf("StartApply failed: %v", err)
	}
	svc.Wait()

	select {
	case req := <-svc.RestartCh():
		t.Fatalf("unexpected RestartRequest on tampered checksum: %+v", req)
	default:
	}

	st := svc.Status(ctx)
	if st.State != "failed" {
		t.Fatalf("expected State == failed, got %+v", st)
	}
	if !strings.Contains(strings.ToLower(st.LastError), "checksum mismatch") {
		t.Fatalf("expected LastError to mention checksum mismatch, got %q", st.LastError)
	}
	if got := runBinaryVersion(t, activeBin); got != "1.0.1" {
		t.Fatalf("active binary must remain untouched (1.0.1), got %q", got)
	}
	assertNoMatchingEntry(t, filepath.Join(dataDir, "updates", "staging"), assetName+".part")
}

func TestStartApplyRejectsTarTraversalAndSymlink(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	cases := []struct {
		name    string
		entries []tarTestEntry
	}{
		{
			name: "path traversal entry",
			entries: []tarTestEntry{
				{name: "coursefin", content: versionStubScript("1.2.0"), mode: 0o755, typeflag: tar.TypeReg},
				{name: "../evil", content: []byte("boom"), mode: 0o755, typeflag: tar.TypeReg},
			},
		},
		{
			name: "symlink coursefin entry",
			entries: []tarTestEntry{
				{name: "coursefin", mode: 0o777, typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
			},
		},
		{
			name: "hardlink entry",
			entries: []tarTestEntry{
				{name: "coursefin", content: versionStubScript("1.2.0"), mode: 0o755, typeflag: tar.TypeReg},
				{name: "extra", mode: 0o755, typeflag: tar.TypeLink, linkname: "coursefin"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			managedDir := t.TempDir()
			activeBin := filepath.Join(managedDir, "coursefin")
			if err := os.WriteFile(activeBin, versionStubScript("1.0.1"), 0o755); err != nil {
				t.Fatalf("write initial binary: %v", err)
			}

			assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
			archive := buildTarGzWithEntries(t, tc.entries)
			checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

			svc := updater.NewService(updater.Options{
				DataDir:           dataDir,
				CurrentVersion:    "1.0.1",
				APIBase:           "https://api.github.example",
				ManagedBinDir:     managedDir,
				HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
				RestartGraceDelay: 5 * time.Millisecond,
			})

			ctx := context.Background()
			if _, err := svc.CheckNow(ctx); err != nil {
				t.Fatalf("CheckNow failed: %v", err)
			}
			if _, err := svc.StartApply(ctx); err != nil {
				t.Fatalf("StartApply failed: %v", err)
			}
			svc.Wait()

			select {
			case req := <-svc.RestartCh():
				t.Fatalf("unexpected RestartRequest on unsafe tar: %+v", req)
			default:
			}

			st := svc.Status(ctx)
			if st.State != "failed" || st.LastError == "" {
				t.Fatalf("expected failed state with error, got %+v", st)
			}
			if got := runBinaryVersion(t, activeBin); got != "1.0.1" {
				t.Fatalf("active binary must remain untouched (1.0.1), got %q", got)
			}
			assertNoMatchingEntry(t, managedDir, ".coursefin.new.")
			assertNoMatchingEntry(t, filepath.Join(dataDir, "updates", "staging"), assetName+".part")
		})
	}
}

func TestStartApplyRejectsFailedVersionPreflight(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	cases := []struct {
		name   string
		script []byte
	}{
		{
			name:   "mismatched version output",
			script: versionStubScript("9.9.9"),
		},
		{
			name:   "non-zero exit code",
			script: []byte("#!/bin/sh\necho '1.2.0'\nexit 1\n"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			managedDir := t.TempDir()
			activeBin := filepath.Join(managedDir, "coursefin")
			if err := os.WriteFile(activeBin, versionStubScript("1.0.1"), 0o755); err != nil {
				t.Fatalf("write initial binary: %v", err)
			}

			assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
			archive := buildReleaseTarGz(t, "coursefin", tc.script, 0o755)
			checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

			svc := updater.NewService(updater.Options{
				DataDir:           dataDir,
				CurrentVersion:    "1.0.1",
				APIBase:           "https://api.github.example",
				ManagedBinDir:     managedDir,
				HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
				RestartGraceDelay: 5 * time.Millisecond,
			})

			ctx := context.Background()
			if _, err := svc.CheckNow(ctx); err != nil {
				t.Fatalf("CheckNow failed: %v", err)
			}
			if _, err := svc.StartApply(ctx); err != nil {
				t.Fatalf("StartApply failed: %v", err)
			}
			svc.Wait()

			select {
			case req := <-svc.RestartCh():
				t.Fatalf("unexpected RestartRequest on failed preflight: %+v", req)
			default:
			}

			st := svc.Status(ctx)
			if st.State != "failed" || st.LastError == "" {
				t.Fatalf("expected failed state with error, got %+v", st)
			}
			if got := runBinaryVersion(t, activeBin); got != "1.0.1" {
				t.Fatalf("active binary must remain untouched (1.0.1), got %q", got)
			}
			assertNoMatchingEntry(t, managedDir, ".coursefin.new.")
			if _, err := os.Stat(activeBin + ".bak"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("backup file must not be created when preflight fails, got err=%v", err)
			}
		})
	}
}

func TestStartApplyConcurrencyBusy(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	dataDir := t.TempDir()
	managedDir := t.TempDir()
	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildReleaseTarGz(t, "coursefin", versionStubScript("1.2.0"), 0o755)
	checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

	releaseURL := "https://api.github.example/repos/booooodv/CourseFin/releases/latest"
	assetURL := "https://github.example/booooodv/CourseFin/releases/download/v1.2.0/" + assetName
	checksumsURL := "https://github.example/booooodv/CourseFin/releases/download/v1.2.0/checksums.txt"

	downloadStarted := make(chan struct{})
	releaseDownload := make(chan struct{})

	client := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case releaseURL:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"tag_name": "v1.2.0",
				"name": "CourseFin v1.2.0",
				"assets": [
					{"name": %q, "size": %d, "browser_download_url": %q},
					{"name": "checksums.txt", "size": %d, "browser_download_url": %q}
				]
			}`, assetName, len(archive), assetURL, len(checksums), checksumsURL)
		case checksumsURL:
			_, _ = w.Write([]byte(checksums))
		case assetURL:
			close(downloadStarted)
			select {
			case <-releaseDownload:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
			_, _ = w.Write(archive)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	svc := updater.NewService(updater.Options{
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		ManagedBinDir:     managedDir,
		HTTPClient:        client,
		RestartGraceDelay: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("CheckNow failed: %v", err)
	}
	if _, err := svc.StartApply(ctx); err != nil {
		t.Fatalf("StartApply failed: %v", err)
	}

	select {
	case <-downloadStarted:
	case <-time.After(3 * time.Second):
		close(releaseDownload)
		t.Fatalf("timed out waiting for download to start")
	}

	if st := svc.Status(ctx); st.State != "downloading" {
		close(releaseDownload)
		t.Fatalf("expected Status.State == downloading while paused, got %+v", st)
	}
	if _, err := svc.StartApply(ctx); !errors.Is(err, updater.ErrBusy) {
		close(releaseDownload)
		t.Fatalf("expected ErrBusy for second StartApply, got %v", err)
	}
	if _, err := svc.CheckNow(ctx); !errors.Is(err, updater.ErrBusy) {
		close(releaseDownload)
		t.Fatalf("expected ErrBusy for CheckNow during apply, got %v", err)
	}

	close(releaseDownload)

	select {
	case <-svc.RestartCh():
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for RestartCh after unblocking")
	}
	svc.Wait()
}

func TestStartApplyCannotApplyAndMirrorFallback(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	ctx := context.Background()

	// 1. CannotApply before any update is found.
	noUpdateSvc := updater.NewService(updater.Options{
		DataDir:        t.TempDir(),
		CurrentVersion: "1.0.1",
		ManagedBinDir:  t.TempDir(),
	})
	if _, err := noUpdateSvc.StartApply(ctx); !errors.Is(err, updater.ErrCannotApply) {
		t.Fatalf("expected ErrCannotApply when no update is available, got %v", err)
	}

	// 2. Mirror-first downloads with direct fallback and no GitHub token sent to mirror.
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	dataDir := t.TempDir()
	managedDir := t.TempDir()
	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildReleaseTarGz(t, "coursefin", versionStubScript("1.2.0"), 0o755)
	checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

	var mu sync.Mutex
	var mirrorPaths []string
	var mirrorAuths []string
	var directPaths []string
	var directAuths []string

	client := newHandlerHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Host {
		case "mirror.example":
			mirrorPaths = append(mirrorPaths, r.URL.Path)
			mirrorAuths = append(mirrorAuths, r.Header.Get("Authorization"))
			if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
				// Simulate mirror failure on checksums.txt so direct fallback runs.
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/"+assetName) {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
				_, _ = w.Write(archive)
				return
			}
			_, _ = fmt.Fprintf(w, `{
				"tag_name": "v1.2.0",
				"name": "v1.2.0",
				"assets": [
					{"name": %q, "size": %d, "browser_download_url": "https://github.example/booooodv/CourseFin/releases/download/v1.2.0/%s"},
					{"name": "checksums.txt", "size": %d, "browser_download_url": "https://github.example/booooodv/CourseFin/releases/download/v1.2.0/checksums.txt"}
				]
			}`, assetName, len(archive), assetName, len(checksums))
		case "github.example":
			directPaths = append(directPaths, r.URL.Path)
			directAuths = append(directAuths, r.Header.Get("Authorization"))
			if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
				_, _ = w.Write([]byte(checksums))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected host %q", r.URL.Host)
			w.WriteHeader(http.StatusBadGateway)
		}
	}))

	svc := updater.NewService(updater.Options{
		DB:                conn,
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		GitHubToken:       "secret-tok",
		ManagedBinDir:     managedDir,
		HTTPClient:        client,
		RestartGraceDelay: 5 * time.Millisecond,
	})
	if _, err := svc.UpdateConfig(ctx, updater.ConfigInput{MirrorURL: strPtr("https://mirror.example")}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if _, err := svc.StartApply(ctx); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	select {
	case <-svc.RestartCh():
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for RestartCh; status: %+v", svc.Status(ctx))
	}
	svc.Wait()

	mu.Lock()
	defer mu.Unlock()
	for _, auth := range mirrorAuths {
		if auth != "" {
			t.Fatalf("mirror must never receive Authorization header, got %v", mirrorAuths)
		}
	}
	for _, auth := range directAuths {
		if auth != "" {
			t.Fatalf("asset download direct host must not receive API Authorization token, got %v", directAuths)
		}
	}
	if len(directPaths) != 1 || !strings.HasSuffix(directPaths[0], "/checksums.txt") {
		t.Fatalf("expected direct fallback only for checksums.txt, got %v", directPaths)
	}
	if len(mirrorPaths) != 3 {
		t.Fatalf("expected mirror tried for release, checksums, and asset, got %v", mirrorPaths)
	}
}

func TestStartApplyChecksumAndArchiveEdgeCases(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	ctx := context.Background()
	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)

	// 1. Uppercase hex in checksums.txt succeeds (case-insensitive verify).
	t.Run("uppercase checksum accepted", func(t *testing.T) {
		managedDir := t.TempDir()
		archive := buildReleaseTarGz(t, "./coursefin", versionStubScript("1.2.0"), 0o755)
		checksums := fmt.Sprintf("%s  *%s\n", strings.ToUpper(sha256HexBytes(archive)), assetName)

		svc := updater.NewService(updater.Options{
			DataDir:           t.TempDir(),
			CurrentVersion:    "1.0.1",
			APIBase:           "https://api.github.example",
			ManagedBinDir:     managedDir,
			HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
			RestartGraceDelay: 5 * time.Millisecond,
		})
		if _, err := svc.CheckNow(ctx); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		if _, err := svc.StartApply(ctx); err != nil {
			t.Fatalf("StartApply: %v", err)
		}
		select {
		case <-svc.RestartCh():
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for RestartCh; status: %+v", svc.Status(ctx))
		}
		svc.Wait()
	})

	// 2. Missing entry in checksums.txt fails before download completes.
	t.Run("missing checksum entry", func(t *testing.T) {
		managedDir := t.TempDir()
		archive := buildReleaseTarGz(t, "coursefin", versionStubScript("1.2.0"), 0o755)
		checksums := fmt.Sprintf("%s  other_platform.tar.gz\n", sha256HexBytes(archive))

		svc := updater.NewService(updater.Options{
			DataDir:           t.TempDir(),
			CurrentVersion:    "1.0.1",
			APIBase:           "https://api.github.example",
			ManagedBinDir:     managedDir,
			HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
			RestartGraceDelay: 5 * time.Millisecond,
		})
		if _, err := svc.CheckNow(ctx); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		if _, err := svc.StartApply(ctx); err != nil {
			t.Fatalf("StartApply: %v", err)
		}
		svc.Wait()
		if st := svc.Status(ctx); st.State != "failed" || st.LastError == "" {
			t.Fatalf("expected failed state when checksum entry missing, got %+v", st)
		}
	})

	// 3. Archive without coursefin entry fails cleanly.
	t.Run("missing coursefin binary in tar", func(t *testing.T) {
		managedDir := t.TempDir()
		archive := buildReleaseTarGz(t, "README.md", []byte("hello"), 0o644)
		checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

		svc := updater.NewService(updater.Options{
			DataDir:           t.TempDir(),
			CurrentVersion:    "1.0.1",
			APIBase:           "https://api.github.example",
			ManagedBinDir:     managedDir,
			HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
			RestartGraceDelay: 5 * time.Millisecond,
		})
		if _, err := svc.CheckNow(ctx); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		if _, err := svc.StartApply(ctx); err != nil {
			t.Fatalf("StartApply: %v", err)
		}
		svc.Wait()
		if st := svc.Status(ctx); st.State != "failed" || st.LastError == "" {
			t.Fatalf("expected failed state when binary missing, got %+v", st)
		}
	})

	// 4. Oversized (>128 MiB) decompressed entry inside compressed tar is rejected.
	t.Run("rejects oversized decompressed entry", func(t *testing.T) {
		const oversized = (128 << 20) + 1024
		var buf bytes.Buffer
		gzw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gzw)
		if err := tw.WriteHeader(&tar.Header{
			Name:     "coursefin",
			Mode:     0o755,
			Size:     oversized,
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		zeroChunk := make([]byte, 1<<20)
		rem := int64(oversized)
		for rem > 0 {
			n := int64(len(zeroChunk))
			if rem < n {
				n = rem
			}
			if _, err := tw.Write(zeroChunk[:n]); err != nil {
				t.Fatalf("tar Write: %v", err)
			}
			rem -= n
		}
		if err := tw.Close(); err != nil {
			t.Fatalf("tar Close: %v", err)
		}
		if err := gzw.Close(); err != nil {
			t.Fatalf("gzip Close: %v", err)
		}

		archive := buf.Bytes()
		checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)
		managedDir := t.TempDir()

		svc := updater.NewService(updater.Options{
			DataDir:           t.TempDir(),
			CurrentVersion:    "1.0.1",
			APIBase:           "https://api.github.example",
			ManagedBinDir:     managedDir,
			HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
			RestartGraceDelay: 5 * time.Millisecond,
		})
		if _, err := svc.CheckNow(ctx); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		if _, err := svc.StartApply(ctx); err != nil {
			t.Fatalf("StartApply: %v", err)
		}
		svc.Wait()
		if st := svc.Status(ctx); st.State != "failed" || st.LastError == "" {
			t.Fatalf("expected failed state for >128 MiB tar entry, got %+v", st)
		}
		assertNoMatchingEntry(t, managedDir, ".coursefin.new.")
	})
}

func TestStartApplyPendingUpdateWriteFailureKeepsBinaryUntouched(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	dataDir := t.TempDir()
	managedDir := t.TempDir()
	activeBin := filepath.Join(managedDir, "coursefin")
	if err := os.WriteFile(activeBin, versionStubScript("1.0.1"), 0o755); err != nil {
		t.Fatalf("write initial binary: %v", err)
	}
	// Block .pending_update atomic rename by placing a non-empty directory there.
	pendingBlocker := filepath.Join(managedDir, ".pending_update")
	if err := os.MkdirAll(filepath.Join(pendingBlocker, "keep"), 0o755); err != nil {
		t.Fatalf("mkdir pending blocker: %v", err)
	}

	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildReleaseTarGz(t, "coursefin", versionStubScript("1.2.0"), 0o755)
	checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

	svc := updater.NewService(updater.Options{
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		ManagedBinDir:     managedDir,
		HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
		RestartGraceDelay: 5 * time.Millisecond,
	})

	ctx := context.Background()
	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if _, err := svc.StartApply(ctx); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	svc.Wait()

	select {
	case req := <-svc.RestartCh():
		t.Fatalf("unexpected RestartRequest when .pending_update cannot be written: %+v", req)
	default:
	}

	st := svc.Status(ctx)
	if st.State != "failed" || !strings.Contains(st.LastError, "pending update") {
		t.Fatalf("expected failed state mentioning pending update, got %+v", st)
	}
	if got := runBinaryVersion(t, activeBin); got != "1.0.1" {
		t.Fatalf("active binary must remain untouched at 1.0.1 when .pending_update fails, got %q", got)
	}
	assertNoMatchingEntry(t, managedDir, ".coursefin.new.")
	assertNoMatchingEntry(t, filepath.Join(dataDir, "updates", "staging"), assetName+".part")
}

func TestStartApplyRenameFailureCleansUpPendingUpdateMarker(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	dataDir := t.TempDir()
	managedDir := t.TempDir()
	activeBin := filepath.Join(managedDir, "coursefin")
	if err := os.WriteFile(activeBin, versionStubScript("1.0.1"), 0o755); err != nil {
		t.Fatalf("write initial binary: %v", err)
	}
	pendingPath := filepath.Join(managedDir, ".pending_update")

	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildReleaseTarGz(t, "coursefin", versionStubScript("1.2.0"), 0o755)
	checksums := fmt.Sprintf("%s  %s\n", sha256HexBytes(archive), assetName)

	svc := updater.NewService(updater.Options{
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		ManagedBinDir:     managedDir,
		HTTPClient:        newApplyMockClient(t, assetName, archive, checksums),
		RestartGraceDelay: 5 * time.Millisecond,
	})

	var pendingExistedDuringRename bool
	updater.SetRenameFuncForTest(svc, func(oldpath, newpath string) error {
		if data, err := os.ReadFile(pendingPath); err == nil && string(data) == "tried\n" {
			pendingExistedDuringRename = true
		}
		return errors.New("simulated rename failure")
	})

	ctx := context.Background()
	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if _, err := svc.StartApply(ctx); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	svc.Wait()

	if !pendingExistedDuringRename {
		t.Fatalf("expected .pending_update marker to be written before os.Rename")
	}
	if _, err := os.Stat(pendingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected .pending_update to be cleaned up after rename failure, got err=%v", err)
	}
	st := svc.Status(ctx)
	if st.State != "failed" || !strings.Contains(st.LastError, "simulated rename failure") {
		t.Fatalf("expected failed state with rename error, got %+v", st)
	}
	if got := runBinaryVersion(t, activeBin); got != "1.0.1" {
		t.Fatalf("active binary must remain untouched at 1.0.1 on rename failure, got %q", got)
	}
	assertNoMatchingEntry(t, managedDir, ".coursefin.new.")
	assertNoMatchingEntry(t, filepath.Join(dataDir, "updates", "staging"), assetName+".part")
}

func TestCheckNowClearsStaleFailedApplyProgress(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("hot-exec apply requires linux or darwin")
	}

	dataDir := t.TempDir()
	managedDir := t.TempDir()
	activeBin := filepath.Join(managedDir, "coursefin")
	if err := os.WriteFile(activeBin, versionStubScript("1.0.1"), 0o755); err != nil {
		t.Fatalf("write initial binary: %v", err)
	}

	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildReleaseTarGz(t, "coursefin", versionStubScript("1.2.0"), 0o755)
	badChecksums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), assetName)

	svc := updater.NewService(updater.Options{
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		ManagedBinDir:     managedDir,
		HTTPClient:        newApplyMockClient(t, assetName, archive, badChecksums),
		RestartGraceDelay: 5 * time.Millisecond,
	})

	ctx := context.Background()
	if _, err := svc.CheckNow(ctx); err != nil {
		t.Fatalf("initial CheckNow: %v", err)
	}
	if _, err := svc.StartApply(ctx); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	svc.Wait()

	failedSt := svc.Status(ctx)
	if failedSt.State != "failed" || failedSt.TargetVersion != "1.2.0" || failedSt.ProgressBytes == 0 || failedSt.TotalBytes == 0 || failedSt.ProgressPercent == 0 {
		t.Fatalf("expected failed apply to populate progress fields first, got %+v", failedSt)
	}

	checkedSt, err := svc.CheckNow(ctx)
	if err != nil {
		t.Fatalf("second CheckNow: %v", err)
	}
	if checkedSt.State != "idle" {
		t.Fatalf("expected State == idle after CheckNow, got %+v", checkedSt)
	}
	if checkedSt.TargetVersion != "" || checkedSt.ProgressBytes != 0 || checkedSt.TotalBytes != 0 || checkedSt.ProgressPercent != 0 || checkedSt.LastError != "" {
		t.Fatalf("expected stale apply progress fields cleared on CheckNow, got %+v", checkedSt)
	}
}
