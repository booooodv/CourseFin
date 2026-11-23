package server_test

import (
	"bytes"
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
