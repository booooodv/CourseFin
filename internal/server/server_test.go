package server_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coursefin/internal/config"
	"coursefin/internal/server"
)

func TestServerRoutingNoCollisionBetweenWebAPIAndJellyfin(t *testing.T) {
	cfg := config.Config{
		Port:     "0",
		DataDir:  t.TempDir(),
		LogLevel: "info",
		BaseURL:  "http://localhost:8096",
	}

	appSrv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer func() {
		if err := appSrv.Close(); err != nil {
			t.Fatalf("appSrv.Close: %v", err)
		}
	}()

	ts := httptest.NewServer(appSrv.Handler())
	defer ts.Close()

	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. GET /health -> 200 OK
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("GET /health = %d %s, want 200 ok", resp.StatusCode, body)
	}

	// 2. GET / with Accept: text/html -> 302 to /web/
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err = noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("GET / html: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/web/" {
		t.Fatalf("GET / with Accept text/html = %d Location=%q, want 302 /web/", resp.StatusCode, resp.Header.Get("Location"))
	}

	// 3. GET /web/ -> serves SPA HTML
	resp, err = http.Get(ts.URL + "/web/")
	if err != nil {
		t.Fatalf("GET /web/: %v", err)
	}
	webBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(strings.ToLower(string(webBody)), "<html") {
		t.Fatalf("GET /web/ = %d %s, want 200 HTML", resp.StatusCode, webBody)
	}
	if !strings.Contains(string(webBody), "/web/favicon.svg") {
		t.Fatalf("GET /web/ HTML missing /web/favicon.svg link: %s", webBody)
	}

	// 3b. GET /web/favicon.svg, /favicon.svg, and /favicon.ico -> 200 SVG
	for _, favPath := range []string{"/web/favicon.svg", "/favicon.svg", "/favicon.ico"} {
		resp, err = http.Get(ts.URL + favPath)
		if err != nil {
			t.Fatalf("GET %s: %v", favPath, err)
		}
		favBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", favPath, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "image/svg+xml") {
			t.Fatalf("GET %s Content-Type = %q, want image/svg+xml", favPath, ct)
		}
		if !strings.Contains(string(favBody), "<svg") {
			t.Fatalf("GET %s body does not look like SVG: %s", favPath, favBody)
		}
	}

	// 4. GET /web/courses/abc -> SPA fallback serves index.html
	resp, err = http.Get(ts.URL + "/web/courses/abc")
	if err != nil {
		t.Fatalf("GET /web/courses/abc: %v", err)
	}
	spaBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(strings.ToLower(string(spaBody)), "<html") {
		t.Fatalf("GET /web/courses/abc = %d %s, want 200 HTML", resp.StatusCode, spaBody)
	}

	// 5. GET /web/assets/nonexistent.js -> 404
	resp, err = http.Get(ts.URL + "/web/assets/nonexistent.js")
	if err != nil {
		t.Fatalf("GET /web/assets/nonexistent.js: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /web/assets/nonexistent.js = %d, want 404", resp.StatusCode)
	}

	// 6. GET /api/v1/setup/status -> CourseFin API
	resp, err = http.Get(ts.URL + "/api/v1/setup/status")
	if err != nil {
		t.Fatalf("GET /api/v1/setup/status: %v", err)
	}
	apiBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/setup/status = %d %s", resp.StatusCode, apiBody)
	}
	var setupStatus struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	if err := json.Unmarshal(apiBody, &setupStatus); err != nil || !setupStatus.NeedsSetup {
		t.Fatalf("expected needs_setup=true, got %s", apiBody)
	}

	// 7. GET /System/Info/Public and /emby/System/Info/Public -> Jellyfin handler
	for _, path := range []string{"/System/Info/Public", "/emby/System/Info/Public"} {
		resp, err = http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		jfBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, resp.StatusCode, jfBody)
		}
		var pub struct {
			ProductName string `json:"ProductName"`
			ServerName  string `json:"ServerName"`
		}
		if err := json.Unmarshal(jfBody, &pub); err != nil || pub.ProductName != "Jellyfin Server" {
			t.Fatalf("expected Jellyfin PublicSystemInfo on %s, got %s", path, jfBody)
		}
	}
}

func TestServerCloseWaitsForInFlightAsyncScan(t *testing.T) {
	mediaDir := t.TempDir()
	for i := 0; i < 10; i++ {
		p := filepath.Join(mediaDir, "Course", "Lesson_"+string(rune('a'+i))+".mp4")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("not-a-real-mp4"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	cfg := config.Config{
		Port:      "0",
		DataDir:   t.TempDir(),
		AdminUser: "admin",
		AdminPass: "secret",
		LogLevel:  "error",
	}

	appSrv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	ts := httptest.NewServer(appSrv.Handler())
	defer ts.Close()

	doReq := func(method, path string, body any, token string) (*http.Response, []byte) {
		t.Helper()
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, ts.URL+path, r)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp, raw
	}

	resp, raw := doReq(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username":  "admin",
		"password":  "secret",
		"device_id": "srv-test-1",
	}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d: %s", resp.StatusCode, raw)
	}
	var loginRes struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(raw, &loginRes)

	resp, raw = doReq(http.MethodPost, "/api/v1/sources", map[string]string{
		"name":      "Media",
		"driver":    "local",
		"root_path": mediaDir,
	}, loginRes.Token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create source = %d: %s", resp.StatusCode, raw)
	}
	var src struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &src)

	resp, raw = doReq(http.MethodPost, "/api/v1/sources/"+src.ID+"/scan?async=true", nil, loginRes.Token)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("async scan = %d, want 202: %s", resp.StatusCode, raw)
	}
	var st struct {
		Running bool `json:"running"`
	}
	if err := json.Unmarshal(raw, &st); err != nil || !st.Running {
		t.Fatalf("expected running=true in 202 response, got %s", raw)
	}

	// Close immediately while async scan is running; must wait cleanly with no error or race.
	if err := appSrv.Close(); err != nil {
		t.Fatalf("appSrv.Close() returned error: %v", err)
	}
}

func TestHealthReturnsVersionAndBootID(t *testing.T) {
	srv1, err := server.New(config.Config{
		Port:      "0",
		DataDir:   t.TempDir(),
		AdminUser: "admin",
		AdminPass: "secret123",
		LogLevel:  "error",
		Version:   "v1.2.3",
	})
	if err != nil {
		t.Fatalf("server.New srv1: %v", err)
	}
	defer func() {
		if err := srv1.Close(); err != nil {
			t.Fatalf("srv1.Close: %v", err)
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv1.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("GET /health Cache-Control = %q, want no-store", got)
	}

	var h1 struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		BootID  string `json:"boot_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &h1); err != nil {
		t.Fatalf("unmarshal /health: %v (%s)", err, rec.Body.String())
	}
	if h1.Status != "ok" {
		t.Fatalf("status = %q, want ok", h1.Status)
	}
	if h1.Version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", h1.Version)
	}
	if len(h1.BootID) != 16 {
		t.Fatalf("boot_id = %q (len=%d), want 16-hex", h1.BootID, len(h1.BootID))
	}
	if _, err := hex.DecodeString(h1.BootID); err != nil {
		t.Fatalf("boot_id %q is not valid hex: %v", h1.BootID, err)
	}

	headReq := httptest.NewRequest(http.MethodHead, "/health", nil)
	headRec := httptest.NewRecorder()
	srv1.Handler().ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("HEAD /health status = %d, want 200", headRec.Code)
	}
	if got := headRec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("HEAD /health Cache-Control = %q, want no-store", got)
	}
	if headRec.Body.Len() != 0 {
		t.Fatalf("HEAD /health expected empty body, got %q", headRec.Body.String())
	}

	srv2, err := server.New(config.Config{
		Port:     "0",
		DataDir:  t.TempDir(),
		LogLevel: "error",
	})
	if err != nil {
		t.Fatalf("server.New srv2: %v", err)
	}
	defer func() {
		if err := srv2.Close(); err != nil {
			t.Fatalf("srv2.Close: %v", err)
		}
	}()

	if srv2.RestartCh() == nil {
		t.Fatalf("expected non-nil RestartCh on server")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec2 := httptest.NewRecorder()
	srv2.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("srv2 GET /health = %d, want 200", rec2.Code)
	}
	var h2 struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		BootID  string `json:"boot_id"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &h2); err != nil {
		t.Fatalf("unmarshal srv2 /health: %v", err)
	}
	if h2.Version != "0.0.0-dev" {
		t.Fatalf("default version = %q, want 0.0.0-dev", h2.Version)
	}
	if len(h2.BootID) != 16 || h2.BootID == h1.BootID {
		t.Fatalf("expected unique 16-hex boot_id, got %q vs %q", h2.BootID, h1.BootID)
	}

	// Verify server.New wired Updater service & normalized version into /api/v1/system/update
	loginBody := strings.NewReader(`{"username":"admin","password":"secret123","device_id":"srv-health-test"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	srv1.Handler().ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/auth/login = %d: %s", loginRec.Code, loginRec.Body.String())
	}
	var loginRes struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginRes); err != nil || loginRes.Token == "" {
		t.Fatalf("unmarshal login: %v (%s)", err, loginRec.Body.String())
	}

	updReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/update", nil)
	updReq.Header.Set("Authorization", "Bearer "+loginRes.Token)
	updRec := httptest.NewRecorder()
	srv1.Handler().ServeHTTP(updRec, updReq)
	if updRec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/system/update = %d, want 200: %s", updRec.Code, updRec.Body.String())
	}
	var updStatus struct {
		CurrentVersion string `json:"current_version"`
		State          string `json:"state"`
	}
	if err := json.Unmarshal(updRec.Body.Bytes(), &updStatus); err != nil {
		t.Fatalf("unmarshal /api/v1/system/update: %v", err)
	}
	if updStatus.CurrentVersion != "1.2.3" || updStatus.State != "idle" {
		t.Fatalf("unexpected wired updater status: %+v", updStatus)
	}
}

func TestServerServesFaviconAssets(t *testing.T) {
	srv, err := server.New(config.Config{
		Port:     "0",
		DataDir:  t.TempDir(),
		LogLevel: "error",
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("srv.Close: %v", err)
		}
	}()

	// Verify /web/ index.html links to /web/favicon.svg
	webRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(webRec, httptest.NewRequest(http.MethodGet, "/web/", nil))
	if webRec.Code != http.StatusOK {
		t.Fatalf("GET /web/ status = %d, want 200", webRec.Code)
	}
	if !strings.Contains(webRec.Body.String(), "/web/favicon.svg") {
		t.Fatalf("GET /web/ HTML missing /web/favicon.svg link")
	}

	// Verify GET & HEAD for /web/favicon.svg, /favicon.svg, and /favicon.ico
	for _, favPath := range []string{"/web/favicon.svg", "/favicon.svg", "/favicon.ico"} {
		getRec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, favPath, nil))
		if getRec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", favPath, getRec.Code)
		}
		if ct := getRec.Header().Get("Content-Type"); !strings.Contains(ct, "image/svg+xml") {
			t.Fatalf("GET %s Content-Type = %q, want image/svg+xml", favPath, ct)
		}
		if !strings.Contains(getRec.Body.String(), "<svg") {
			t.Fatalf("GET %s body does not contain <svg", favPath)
		}

		headRec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(headRec, httptest.NewRequest(http.MethodHead, favPath, nil))
		if headRec.Code != http.StatusOK {
			t.Fatalf("HEAD %s status = %d, want 200", favPath, headRec.Code)
		}
		if headRec.Body.Len() != 0 {
			t.Fatalf("HEAD %s expected empty body, got %d bytes", favPath, headRec.Body.Len())
		}
	}
}
