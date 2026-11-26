package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"coursefin/internal/api"
	"coursefin/internal/auth"
	"coursefin/internal/catalog"
	"coursefin/internal/cover"
	"coursefin/internal/db"
	"coursefin/internal/jellyfin"
	"coursefin/internal/media"
	"coursefin/internal/scanner"
	"coursefin/internal/storage"
)

func encodeTestPNG(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestAICoverSettingsPreviewApplyAndBatchAPI(t *testing.T) {
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	reg := storage.NewRegistry()
	authSvc := auth.NewService(conn)
	scanSvc := scanner.NewService(conn, dataDir, reg, &fakeProber{})
	catSvc := catalog.NewService(conn, dataDir)
	catSvc.SetRegistry(reg)
	mediaSvc := media.NewService(conn, dataDir, reg, "")
	t.Cleanup(func() { _ = mediaSvc.Close() })

	lctx, lcancel := context.WithCancel(context.Background())
	coverSvc := cover.NewService(cover.Options{
		DB:           conn,
		DataDir:      dataDir,
		Catalog:      catSvc,
		LifecycleCtx: lctx,
	})
	t.Cleanup(func() {
		lcancel()
		coverSvc.Wait()
	})

	pngBytes := encodeTestPNG(t, 24, 110, 235)
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	var upstreamCalls atomic.Int32
	var lastAuthHeader atomic.Value
	lastAuthHeader.Store("")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		lastAuthHeader.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"b64_json": b64},
			},
		})
	}))
	defer upstream.Close()

	apiH := api.NewHandler(api.Deps{
		DB:           conn,
		DataDir:      dataDir,
		BaseURLHint:  "http://localhost:8096",
		Registry:     reg,
		Auth:         authSvc,
		Scanner:      scanSvc,
		Catalog:      catSvc,
		Media:        mediaSvc,
		Cover:        coverSvc,
		AIHTTPClient: upstream.Client(),
		LifecycleCtx: lctx,
	})
	jfH := jellyfin.NewHandler(conn, authSvc, catSvc, mediaSvc)

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/v1/", apiH)
	rootMux.Handle("/", jfH)

	srv := httptest.NewServer(rootMux)
	defer srv.Close()

	env := &apiTestEnv{
		db:       conn,
		dataDir:  dataDir,
		authSvc:  authSvc,
		catSvc:   catSvc,
		mediaSvc: mediaSvc,
		server:   srv,
	}

	// Setup admin user and sign in
	resp, _ := env.doJSON(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"username": "admin",
		"password": "password123",
	}, "")
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status=%d", resp.StatusCode)
	}
	resp, raw := env.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "password123",
	}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%s", resp.StatusCode, string(raw))
	}
	var loginRes struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(raw, &loginRes)
	token := loginRes.Token

	// 1. Styles endpoint before configuring image API
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/cover/styles", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("styles status=%d body=%s", resp.StatusCode, string(raw))
	}
	var stylesRes struct {
		Styles            []cover.StyleTemplate `json:"styles"`
		DefaultStyleID    string                `json:"default_style_id"`
		AIImageConfigured bool                  `json:"ai_image_configured"`
	}
	_ = json.Unmarshal(raw, &stylesRes)
	if len(stylesRes.Styles) != 6 || stylesRes.DefaultStyleID != cover.DefaultStyleID || stylesRes.AIImageConfigured {
		t.Fatalf("unexpected initial styles payload: %+v", stylesRes)
	}

	// 2. Test image endpoint with unsaved overrides (and invalid protocol validation)
	resp, _ = env.doJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_image_protocol": "bad_proto",
	}, token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid protocol, got %d", resp.StatusCode)
	}

	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/settings/ai/image/test", map[string]any{
		"ai_image_protocol": "openai_images",
		"ai_image_base_url": upstream.URL + "/v1",
		"ai_image_api_key":  "sk-override-img",
		"ai_image_model":    "gpt-image-1",
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("test image status=%d body=%s", resp.StatusCode, string(raw))
	}
	if lastAuthHeader.Load().(string) != "Bearer sk-override-img" {
		t.Fatalf("expected override key used in test, got %q", lastAuthHeader.Load().(string))
	}

	// 3. Persist settings and verify API key masking
	resp, raw = env.doJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_image_protocol":      "openai_images",
		"ai_image_base_url":      upstream.URL + "/v1",
		"ai_image_api_key":       "sk-saved-secret",
		"ai_image_model":         "gpt-image-1",
		"ai_image_size":          "1536x1024",
		"ai_cover_default_style": "dark_tech",
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update settings status=%d body=%s", resp.StatusCode, string(raw))
	}
	var settingsRes struct {
		AIImageProtocol     string `json:"ai_image_protocol"`
		AIImageAPIKey       string `json:"ai_image_api_key"`
		AIImageAPIKeySet    bool   `json:"ai_image_api_key_set"`
		AIImageConfigured   bool   `json:"ai_image_configured"`
		AICoverDefaultStyle string `json:"ai_cover_default_style"`
	}
	_ = json.Unmarshal(raw, &settingsRes)
	if settingsRes.AIImageAPIKey != "****" || !settingsRes.AIImageAPIKeySet || !settingsRes.AIImageConfigured || settingsRes.AICoverDefaultStyle != "dark_tech" {
		t.Fatalf("unexpected saved settings: %+v", settingsRes)
	}

	// 4. Create two courses, generate preview for course 1, fetch preview via ?api_key=, apply it, verify Jellyfin Primary image
	libs, _ := catSvc.ListLibraries(context.Background())
	c1, err := catSvc.CreateCourse(context.Background(), catalog.CreateCourseInput{
		LibraryID:  libs[0].ID,
		Title:      "Go 并发实战",
		Instructor: "陈默",
	})
	if err != nil {
		t.Fatalf("CreateCourse c1: %v", err)
	}
	c2, err := catSvc.CreateCourse(context.Background(), catalog.CreateCourseInput{
		LibraryID: libs[0].ID,
		Title:     "Rust 并发编程",
	})
	if err != nil {
		t.Fatalf("CreateCourse c2: %v", err)
	}

	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+c1.ID+"/cover/generate", map[string]any{
		"style_id": "blueprint_grid",
		"title":    "Go 并发实战",
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate preview status=%d body=%s", resp.StatusCode, string(raw))
	}
	var preview cover.CoverPreview
	_ = json.Unmarshal(raw, &preview)
	if preview.StyleID != "blueprint_grid" || preview.PreviewURL == "" {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	// Fetch preview image using ?api_key= query param (like <img> src with withApiKey)
	imgResp, err := http.Get(srv.URL + preview.PreviewURL + "?api_key=" + token)
	if err != nil {
		t.Fatalf("GET preview image: %v", err)
	}
	imgBytes, _ := io.ReadAll(imgResp.Body)
	imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK || !bytes.Equal(imgBytes, pngBytes) {
		t.Fatalf("preview image mismatch: status=%d len=%d", imgResp.StatusCode, len(imgBytes))
	}

	// Apply preview
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+c1.ID+"/cover/apply", map[string]string{
		"preview_id": preview.PreviewID,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply preview status=%d body=%s", resp.StatusCode, string(raw))
	}
	var updatedC1 catalog.CourseDetail
	_ = json.Unmarshal(raw, &updatedC1)
	if updatedC1.CoverPath != c1.ID+"_cover.png" {
		t.Fatalf("expected updated cover_path, got %q", updatedC1.CoverPath)
	}

	// Verify Jellyfin Primary image serves the applied cover
	jfImgResp, err := http.Get(srv.URL + "/Items/" + c1.ID + "/Images/Primary?api_key=" + token)
	if err != nil {
		t.Fatalf("GET jellyfin primary image: %v", err)
	}
	jfBytes, _ := io.ReadAll(jfImgResp.Body)
	jfImgResp.Body.Close()
	if jfImgResp.StatusCode != http.StatusOK || !bytes.Equal(jfBytes, pngBytes) {
		t.Fatalf("jellyfin primary image mismatch: status=%d len=%d", jfImgResp.StatusCode, len(jfBytes))
	}

	// 5. Batch generate missing covers (c1 already has cover -> skipped, c2 generated)
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/covers/batch/generate", map[string]any{
		"mode":     "missing_only",
		"style_id": "editorial_modern",
	}, token)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for batch start, got %d body=%s", resp.StatusCode, string(raw))
	}

	deadline := time.Now().Add(5 * time.Second)
	var finalBatch cover.BatchCoverStatus
	for time.Now().Before(deadline) {
		resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/courses/covers/batch/status", nil, token)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("batch status=%d", resp.StatusCode)
		}
		_ = json.Unmarshal(raw, &finalBatch)
		if !finalBatch.Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if finalBatch.Running || finalBatch.State != "completed" || finalBatch.Completed != 1 || finalBatch.Skipped != 1 {
		t.Fatalf("unexpected final batch status: %+v", finalBatch)
	}

	updatedC2, _ := catSvc.GetCourseDetail(context.Background(), c2.ID)
	if updatedC2.CoverPath != c2.ID+"_cover.png" {
		t.Fatalf("expected c2 generated cover, got %q", updatedC2.CoverPath)
	}

	// 6. Clear saved image API key
	resp, raw = env.doJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"clear_ai_image_api_key": true,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear image key status=%d", resp.StatusCode)
	}
	_ = json.Unmarshal(raw, &settingsRes)
	if settingsRes.AIImageAPIKeySet || settingsRes.AIImageAPIKey != "" {
		t.Fatalf("expected cleared image api key, got %+v", settingsRes)
	}
	if !strings.Contains(strings.ToLower(settingsRes.AIImageProtocol), "openai") {
		t.Fatalf("unexpected protocol: %q", settingsRes.AIImageProtocol)
	}
}
