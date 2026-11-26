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
	"mime/multipart"
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

func TestAICoverMultiCoverAndBatchAPI(t *testing.T) {
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

	png1 := encodeTestPNG(t, 24, 110, 235)
	png2 := encodeTestPNG(t, 220, 60, 40)
	png3 := encodeTestPNG(t, 30, 180, 90)
	b64One := base64.StdEncoding.EncodeToString(png1)
	b64Two := base64.StdEncoding.EncodeToString(png2)
	var upstreamCalls atomic.Int32
	var lastAuthHeader atomic.Value
	lastAuthHeader.Store("")
	var activeB64 atomic.Value
	activeB64.Store(b64One)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		lastAuthHeader.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"b64_json": activeB64.Load().(string)},
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

	// 4. Create two courses; generate AI cover #1 (directly persisted & active) and verify Jellyfin Primary image
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
		t.Fatalf("generate cover #1 status=%d body=%s", resp.StatusCode, string(raw))
	}
	var gen1 cover.GeneratedCourseCover
	_ = json.Unmarshal(raw, &gen1)
	if gen1.Cover == nil || gen1.Course == nil || !gen1.Cover.IsActive || gen1.Cover.StyleID != "blueprint_grid" {
		t.Fatalf("unexpected gen1 response: %+v", gen1)
	}
	if gen1.Course.CoverPath != gen1.Cover.FilePath || len(gen1.Course.Covers) != 1 {
		t.Fatalf("expected 1 active cover after gen1, path=%q len=%d", gen1.Course.CoverPath, len(gen1.Course.Covers))
	}

	// Fetch cover #1 image via ?api_key= and verify Jellyfin Primary image serves png1
	imgResp, err := http.Get(srv.URL + gen1.Cover.ImageURL + "?api_key=" + token)
	if err != nil {
		t.Fatalf("GET cover #1 image: %v", err)
	}
	imgBytes, _ := io.ReadAll(imgResp.Body)
	imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK || !bytes.Equal(imgBytes, png1) {
		t.Fatalf("cover #1 image mismatch: status=%d len=%d", imgResp.StatusCode, len(imgBytes))
	}

	jfImgResp, err := http.Get(srv.URL + "/Items/" + c1.ID + "/Images/Primary?api_key=" + token)
	if err != nil {
		t.Fatalf("GET jellyfin primary image: %v", err)
	}
	jfBytes, _ := io.ReadAll(jfImgResp.Body)
	jfImgResp.Body.Close()
	if jfImgResp.StatusCode != http.StatusOK || !bytes.Equal(jfBytes, png1) {
		t.Fatalf("jellyfin primary image mismatch: status=%d len=%d", jfImgResp.StatusCode, len(jfBytes))
	}

	// 5. Generate AI cover #2 (distinct image png2) -> appended & set active, cover #1 preserved
	activeB64.Store(b64Two)
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+c1.ID+"/cover/generate", map[string]any{
		"style_id": "dark_tech",
		"title":    "Go 并发实战 V2",
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate cover #2 status=%d body=%s", resp.StatusCode, string(raw))
	}
	var gen2 cover.GeneratedCourseCover
	_ = json.Unmarshal(raw, &gen2)
	if gen2.Cover.ID == gen1.Cover.ID || len(gen2.Course.Covers) != 2 || gen2.Course.CoverPath != gen2.Cover.FilePath {
		t.Fatalf("expected gen2 appended & active, got %+v", gen2)
	}

	// List covers endpoint & cross-course IDOR checks
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/courses/"+c1.ID+"/covers", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET covers status=%d body=%s", resp.StatusCode, string(raw))
	}
	var listRes struct {
		Covers []catalog.CourseCover `json:"covers"`
	}
	_ = json.Unmarshal(raw, &listRes)
	if len(listRes.Covers) != 2 || listRes.Covers[0].ID != gen2.Cover.ID || !listRes.Covers[0].IsActive || listRes.Covers[1].ID != gen1.Cover.ID || listRes.Covers[1].IsActive {
		t.Fatalf("unexpected covers list: %+v", listRes.Covers)
	}

	crossImgResp, err := http.Get(srv.URL + "/api/v1/courses/" + c2.ID + "/covers/" + gen1.Cover.ID + "/image?api_key=" + token)
	if err != nil {
		t.Fatalf("GET cross-course cover image: %v", err)
	}
	crossImgResp.Body.Close()
	if crossImgResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-course cover image, got %d", crossImgResp.StatusCode)
	}

	resp, _ = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+c2.ID+"/covers/"+gen1.Cover.ID+"/activate", nil, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-course activate, got %d", resp.StatusCode)
	}

	// 6. Activate cover #1 again -> Jellyfin Primary reverts to png1
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+c1.ID+"/covers/"+gen1.Cover.ID+"/activate", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activate cover #1 status=%d body=%s", resp.StatusCode, string(raw))
	}
	var activatedC1 catalog.CourseDetail
	_ = json.Unmarshal(raw, &activatedC1)
	if activatedC1.CoverPath != gen1.Cover.FilePath {
		t.Fatalf("expected CoverPath=%q after activate, got %q", gen1.Cover.FilePath, activatedC1.CoverPath)
	}

	jfRevertedResp, err := http.Get(srv.URL + "/Items/" + c1.ID + "/Images/Primary?api_key=" + token)
	if err != nil {
		t.Fatalf("GET jellyfin reverted primary image: %v", err)
	}
	jfRevertedBytes, _ := io.ReadAll(jfRevertedResp.Body)
	jfRevertedResp.Body.Close()
	if jfRevertedResp.StatusCode != http.StatusOK || !bytes.Equal(jfRevertedBytes, png1) {
		t.Fatalf("expected jellyfin primary image reverted to png1, status=%d", jfRevertedResp.StatusCode)
	}

	// 7. Delete active cover #1 -> promotes cover #2, and cover #1 image endpoint returns 404
	resp, raw = env.doJSON(t, http.MethodDelete, "/api/v1/courses/"+c1.ID+"/covers/"+gen1.Cover.ID, nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE cover #1 status=%d body=%s", resp.StatusCode, string(raw))
	}
	var afterDel catalog.CourseDetail
	_ = json.Unmarshal(raw, &afterDel)
	if afterDel.CoverPath != gen2.Cover.FilePath || len(afterDel.Covers) != 1 || !afterDel.Covers[0].IsActive {
		t.Fatalf("expected cover #2 promoted after deleting active cover #1: %+v", afterDel)
	}
	delImgResp, err := http.Get(srv.URL + gen1.Cover.ImageURL + "?api_key=" + token)
	if err != nil {
		t.Fatalf("GET deleted cover #1 image: %v", err)
	}
	delImgResp.Body.Close()
	if delImgResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for deleted cover #1 image, got %d", delImgResp.StatusCode)
	}

	// 8. Manual multipart upload appends cover #3 (source="upload") while keeping cover #2
	var upBuf bytes.Buffer
	mw := multipart.NewWriter(&upBuf)
	fw, err := mw.CreateFormFile("file", "manual.png")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = fw.Write(png3)
	_ = mw.Close()
	upReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/courses/"+c1.ID+"/cover", &upBuf)
	upReq.Header.Set("Content-Type", mw.FormDataContentType())
	upReq.Header.Set("Authorization", "Bearer "+token)
	upResp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		t.Fatalf("POST multipart upload: %v", err)
	}
	upRaw, _ := io.ReadAll(upResp.Body)
	upResp.Body.Close()
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("multipart upload status=%d body=%s", upResp.StatusCode, string(upRaw))
	}
	var afterUpload catalog.CourseDetail
	_ = json.Unmarshal(upRaw, &afterUpload)
	if len(afterUpload.Covers) != 2 || afterUpload.Covers[0].Source != catalog.CoverSourceUpload || !afterUpload.Covers[0].IsActive {
		t.Fatalf("expected upload cover appended & active alongside cover #2: %+v", afterUpload.Covers)
	}

	// 9. Batch generate missing covers (c1 already has cover -> skipped, c2 generated)
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
	if !strings.HasPrefix(updatedC2.CoverPath, c2.ID+"_cover_") || len(updatedC2.Covers) != 1 || updatedC2.Covers[0].Source != catalog.CoverSourceAIBatch {
		t.Fatalf("expected c2 generated ai_batch cover, got path=%q covers=%+v", updatedC2.CoverPath, updatedC2.Covers)
	}

	// 10. Batch mode "all" appends new covers while preserving existing ones
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/covers/batch/generate", map[string]any{
		"mode":     "all",
		"style_id": "clay_3d",
	}, token)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for batch mode=all, got %d body=%s", resp.StatusCode, string(raw))
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/courses/covers/batch/status", nil, token)
		_ = json.Unmarshal(raw, &finalBatch)
		if !finalBatch.Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	c1AfterAll, _ := catSvc.GetCourseDetail(context.Background(), c1.ID)
	c2AfterAll, _ := catSvc.GetCourseDetail(context.Background(), c2.ID)
	if len(c1AfterAll.Covers) != 3 || len(c2AfterAll.Covers) != 2 {
		t.Fatalf("expected batch mode=all to preserve old covers (c1=%d, c2=%d)", len(c1AfterAll.Covers), len(c2AfterAll.Covers))
	}

	// 11. Clear saved image API key
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
