package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
