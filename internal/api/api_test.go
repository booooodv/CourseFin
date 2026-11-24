package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"coursefin/internal/api"
	"coursefin/internal/auth"
	"coursefin/internal/catalog"
	"coursefin/internal/db"
	"coursefin/internal/id"
	"coursefin/internal/media"
	"coursefin/internal/scanner"
	"coursefin/internal/storage"
)

type fakeProber struct{}

func (f *fakeProber) Probe(ctx context.Context, absPath string) (*scanner.ProbeResult, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(absPath)), ".")
	res := &scanner.ProbeResult{
		DurationMs:  120000,
		Container:   ext,
		VideoCodec:  "h264",
		AudioCodec:  "aac",
		Width:       1920,
		Height:      1080,
		Bitrate:     2500000,
		StreamsJSON: `[{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080},{"index":1,"codec_type":"audio","codec_name":"aac","channels":2,"sample_rate":"48000"}]`,
	}
	if ext == "mkv" {
		res.AudioCodec = "ac3"
	} else if ext == "wmv" {
		res.VideoCodec = "wmv3"
		res.AudioCodec = "wmav2"
	}
	return res, nil
}

func (f *fakeProber) ExtractThumbnail(ctx context.Context, absPath string, seekSec float64, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, 16, 9))
	img.Set(1, 1, color.RGBA{R: 255, G: 100, B: 50, A: 255})
	file, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return jpeg.Encode(file, img, &jpeg.Options{Quality: 80})
}

type apiTestEnv struct {
	db       *sql.DB
	dataDir  string
	mediaDir string
	registry *storage.Registry
	authSvc  *auth.Service
	scanSvc  *scanner.Service
	catSvc   *catalog.Service
	mediaSvc *media.Service
	handler  http.Handler
	server   *httptest.Server
}

func setupAPITestEnv(t *testing.T) *apiTestEnv {
	t.Helper()

	dataDir := t.TempDir()
	mediaDir := t.TempDir()

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

	h := api.NewHandler(api.Deps{
		DB:          conn,
		DataDir:     dataDir,
		BaseURLHint: "http://localhost:8096",
		Registry:    reg,
		Auth:        authSvc,
		Scanner:     scanSvc,
		Catalog:     catSvc,
		Media:       mediaSvc,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return &apiTestEnv{
		db:       conn,
		dataDir:  dataDir,
		mediaDir: mediaDir,
		registry: reg,
		authSvc:  authSvc,
		scanSvc:  scanSvc,
		catSvc:   catSvc,
		mediaSvc: mediaSvc,
		handler:  h,
		server:   srv,
	}
}

func (e *apiTestEnv) doJSON(t *testing.T, method, path string, body any, token string) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.server.URL+path, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp, raw
}

func writeDummyFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeDummyPNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	img.Set(5, 5, color.RGBA{R: 30, G: 144, B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

func TestAPISetupLoginAndCourseLifecycle(t *testing.T) {
	env := setupAPITestEnv(t)

	// 1. Setup status before admin creation
	resp, raw := env.doJSON(t, http.MethodGet, "/api/v1/setup/status", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /setup/status = %d, want 200: %s", resp.StatusCode, raw)
	}
	var setupStatus struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	if err := json.Unmarshal(raw, &setupStatus); err != nil || !setupStatus.NeedsSetup {
		t.Fatalf("expected needs_setup=true, got %s", raw)
	}

	// Protected routes reject unauthenticated requests
	resp, _ = env.doJSON(t, http.MethodGet, "/api/v1/courses", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /courses = %d, want 401", resp.StatusCode)
	}

	// 2. Setup admin
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"username":    "admin",
		"password":    "pass1234",
		"server_name": "My Course Server",
	}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /setup = %d, want 201: %s", resp.StatusCode, raw)
	}

	// Duplicate setup returns 409
	resp, _ = env.doJSON(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"username": "admin2",
		"password": "secret",
	}, "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate POST /setup = %d, want 409", resp.StatusCode)
	}

	// 3. Login
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username":    "admin",
		"password":    "pass1234",
		"device_id":   "web-dev-1",
		"device_name": "Chrome",
	}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /auth/login = %d, want 200: %s", resp.StatusCode, raw)
	}
	var loginRes struct {
		Token string `json:"token"`
		User  struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &loginRes); err != nil || loginRes.Token == "" {
		t.Fatalf("unexpected login response: %s", raw)
	}
	token := loginRes.Token

	// Check /auth/me with Bearer token
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/auth/me", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, want 200: %s", resp.StatusCode, raw)
	}

	// 4. Create storage source & scan
	courseDir := filepath.Join(env.mediaDir, "Go Mastery")
	writeDummyFile(t, filepath.Join(courseDir, "01 Intro.mp4"), []byte("video1"))
	writeDummyFile(t, filepath.Join(courseDir, "01 Intro.srt"), []byte("1\n00:00:00,000 --> 00:00:02,000\nWelcome to Go\n"))
	writeDummyFile(t, filepath.Join(courseDir, "Chapter 1", "02 Interfaces.mp4"), []byte("video2"))
	writeDummyPNG(t, filepath.Join(courseDir, "cover.png"))

	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/sources", map[string]string{
		"name":      "Courses Disk",
		"driver":    "local",
		"root_path": env.mediaDir,
	}, token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /sources = %d, want 201: %s", resp.StatusCode, raw)
	}
	var src catalog.StorageSource
	if err := json.Unmarshal(raw, &src); err != nil || src.ID == "" {
		t.Fatalf("unmarshal source: %v (%s)", err, raw)
	}

	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/sources/"+src.ID+"/scan", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /sources/%s/scan = %d, want 200: %s", src.ID, resp.StatusCode, raw)
	}
	var rep scanner.ScanReport
	if err := json.Unmarshal(raw, &rep); err != nil || rep.TotalVideos != 2 {
		t.Fatalf("expected TotalVideos=2, got %s", raw)
	}

	// Browse dirs & files
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/sources/"+src.ID+"/dirs", nil, token)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "Go Mastery") {
		t.Fatalf("GET /dirs failed (%d): %s", resp.StatusCode, raw)
	}

	// 5. Import preview & commit
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/import/preview", map[string]string{
		"source_id":    src.ID,
		"dir_rel_path": "Go Mastery",
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /import/preview = %d, want 200: %s", resp.StatusCode, raw)
	}
	var preview catalog.ImportPreview
	if err := json.Unmarshal(raw, &preview); err != nil || preview.TotalFiles != 2 {
		t.Fatalf("expected preview TotalFiles=2, got %s", raw)
	}

	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/import/commit", catalog.CommitImportInput{
		SourceID:     src.ID,
		DirRelPath:   "Go Mastery",
		Title:        "Go Mastery",
		CoverRelPath: preview.CoverRelPath,
	}, token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /import/commit = %d, want 201: %s", resp.StatusCode, raw)
	}
	var course catalog.CourseDetail
	if err := json.Unmarshal(raw, &course); err != nil || course.ID == "" {
		t.Fatalf("unmarshal course: %v (%s)", err, raw)
	}
	if len(course.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(course.Chapters))
	}

	// 6. Update course title and tags
	newTitle := "Go Mastery Revised"
	tags := []string{"golang", "backend"}
	resp, raw = env.doJSON(t, http.MethodPut, "/api/v1/courses/"+course.ID, catalog.UpdateCourseInput{
		Title: &newTitle,
		Tags:  &tags,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /courses = %d, want 200: %s", resp.StatusCode, raw)
	}

	// 7. Reorder lessons (swap lessons across chapters or within chapter)
	l1 := course.Chapters[0].Lessons[0]
	l2 := course.Chapters[1].Lessons[0]
	resp, raw = env.doJSON(t, http.MethodPut, "/api/v1/courses/"+course.ID+"/lessons/reorder", map[string]any{
		"placements": []catalog.LessonPlacement{
			{LessonID: l2.ID, ChapterID: course.Chapters[0].ID, SortOrder: 1},
			{LessonID: l1.ID, ChapterID: course.Chapters[0].ID, SortOrder: 2},
		},
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /lessons/reorder = %d, want 200: %s", resp.StatusCode, raw)
	}

	// 8. Report playback progress on l1
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/lessons/"+l1.ID+"/progress", map[string]int64{
		"position_ticks": 300000000, // 30s
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /lessons/%s/progress = %d, want 200: %s", l1.ID, resp.StatusCode, raw)
	}

	// 9. Verify /home reflects resume, libraries, and updated course
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/home", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /home = %d, want 200: %s", resp.StatusCode, raw)
	}
	var homeRes struct {
		Resume    []catalog.LessonWithContext `json:"resume"`
		Libraries []catalog.Library           `json:"libraries"`
		Courses   []catalog.CourseSummary     `json:"courses"`
	}
	if err := json.Unmarshal(raw, &homeRes); err != nil {
		t.Fatalf("unmarshal home: %v", err)
	}
	if len(homeRes.Resume) != 1 || homeRes.Resume[0].ID != l1.ID || homeRes.Resume[0].PositionTicks != 300000000 {
		t.Fatalf("expected resume lesson %s with ticks=300000000, got %+v", l1.ID, homeRes.Resume)
	}
	if len(homeRes.Libraries) == 0 || len(homeRes.Courses) != 1 || homeRes.Courses[0].Title != newTitle {
		t.Fatalf("unexpected home libraries/courses: %+v", homeRes)
	}

	// 10. Logout invalidates token
	resp, _ = env.doJSON(t, http.MethodPost, "/api/v1/auth/logout", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /auth/logout = %d, want 200", resp.StatusCode)
	}
	resp, _ = env.doJSON(t, http.MethodGet, "/api/v1/auth/me", nil, token)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /auth/me after logout = %d, want 401", resp.StatusCode)
	}
}

func TestAIOrganizeDoesNotMutateUntilApply(t *testing.T) {
	env := setupAPITestEnv(t)

	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret", auth.DeviceInfo{DeviceID: "d1"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	// Create media + subtitle + course
	courseDir := filepath.Join(env.mediaDir, "Messy_Course_[1080p]")
	writeDummyFile(t, filepath.Join(courseDir, "01_setup_1080p.mp4"), []byte("v1"))
	writeDummyFile(t, filepath.Join(courseDir, "01_setup_1080p.srt"), []byte("1\n00:00:01,000 --> 00:00:03,000\nHello Docker setup\n"))
	writeDummyFile(t, filepath.Join(courseDir, "02_containers_1080p.mp4"), []byte("v2"))

	src, err := env.catSvc.CreateSource(context.Background(), "Disk", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	course, err := env.catSvc.CommitDirectoryImport(context.Background(), catalog.CommitImportInput{
		SourceID:   src.ID,
		DirRelPath: "Messy_Course_[1080p]",
	})
	if err != nil {
		t.Fatalf("CommitDirectoryImport: %v", err)
	}

	l1ID := course.Chapters[0].Lessons[0].ID
	l2ID := course.Chapters[0].Lessons[1].ID
	origTitle := course.Title
	origChapterTitle := course.Chapters[0].Title

	// Save initial progress on lesson 1 to verify apply preserves it
	if _, err := env.catSvc.ReportProgress(context.Background(), l1ID, 420000000); err != nil {
		t.Fatalf("ReportProgress: %v", err)
	}

	// Mock OpenAI server
	var seenAuthHeader string
	var seenPromptBody string
	mockAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthHeader = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		seenPromptBody = string(b)

		var respText string
		if strings.Contains(seenPromptBody, "Reply with ok.") {
			respText = "ok"
		} else {
			prop := map[string]any{
				"title":    "Clean Docker Course",
				"overview": "Learn Docker from scratch.",
				"tags":     []string{"docker", "devops"},
				"chapters": []map[string]any{
					{
						"title": "Getting Started",
						"lessons": []map[string]any{
							{"lesson_id": l1ID, "title": "Environment Setup"},
						},
					},
					{
						"title": "Core Concepts",
						"lessons": []map[string]any{
							{"lesson_id": l2ID, "title": "Containers Deep Dive"},
						},
					},
				},
			}
			pb, _ := json.Marshal(prop)
			respText = string(pb)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": respText}},
			},
		})
	}))
	defer mockAI.Close()

	// Save settings with API key
	resp, raw := env.doJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_base_url":          mockAI.URL + "/v1",
		"ai_api_key":           "sk-secret-12345",
		"ai_model":             "gpt-4o-mini",
		"ai_max_subtitle_chars": 2000,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /settings = %d, want 200: %s", resp.StatusCode, raw)
	}

	// GET /settings masks API key
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/settings", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /settings = %d: %s", resp.StatusCode, raw)
	}
	var settingsView map[string]any
	if err := json.Unmarshal(raw, &settingsView); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if settingsView["ai_api_key"] == "sk-secret-12345" || settingsView["ai_api_key_set"] != true {
		t.Fatalf("expected masked ai_api_key and ai_api_key_set=true, got %s", raw)
	}

	// PUT /settings with masked placeholder preserves existing key
	resp, _ = env.doJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"server_name": "Updated Server",
		"ai_api_key":  settingsView["ai_api_key"],
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /settings preserve key = %d", resp.StatusCode)
	}

	// Test AI connection
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/settings/ai/test", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /settings/ai/test = %d: %s", resp.StatusCode, raw)
	}
	if seenAuthHeader != "Bearer sk-secret-12345" {
		t.Fatalf("expected preserved API key in Authorization header, got %q", seenAuthHeader)
	}

	// Call /ai/organize -> preview only, MUST NOT mutate DB
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/ai/organize", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /ai/organize = %d, want 200: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(seenPromptBody, "Hello Docker setup") {
		t.Fatalf("expected sidecar subtitle excerpt in AI prompt, got: %s", seenPromptBody)
	}

	// Setting ai_max_subtitle_chars = 0 persists as 0, returns 0 on GET, and omits all subtitle excerpts on /ai/organize
	resp, raw = env.doJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_max_subtitle_chars": 0,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /settings ai_max_subtitle_chars=0 = %d: %s", resp.StatusCode, raw)
	}
	var zeroPutView map[string]any
	if err := json.Unmarshal(raw, &zeroPutView); err != nil {
		t.Fatalf("unmarshal zeroPutView: %v", err)
	}
	if zeroPutView["ai_max_subtitle_chars"] != float64(0) {
		t.Fatalf("expected ai_max_subtitle_chars=0 after PUT, got %v (%s)", zeroPutView["ai_max_subtitle_chars"], raw)
	}

	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/settings", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /settings after zero = %d: %s", resp.StatusCode, raw)
	}
	var zeroGetView map[string]any
	if err := json.Unmarshal(raw, &zeroGetView); err != nil {
		t.Fatalf("unmarshal zeroGetView: %v", err)
	}
	if zeroGetView["ai_max_subtitle_chars"] != float64(0) {
		t.Fatalf("expected ai_max_subtitle_chars=0 on GET /settings, got %v (%s)", zeroGetView["ai_max_subtitle_chars"], raw)
	}

	seenPromptBody = ""
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/ai/organize", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /ai/organize with 0 subtitle chars = %d: %s", resp.StatusCode, raw)
	}
	if strings.Contains(seenPromptBody, "Hello Docker setup") {
		t.Fatalf("expected empty subtitle excerpts when ai_max_subtitle_chars=0, got: %s", seenPromptBody)
	}

	afterOrganize, err := env.catSvc.GetCourseDetail(context.Background(), course.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail: %v", err)
	}
	if afterOrganize.Title != origTitle || len(afterOrganize.Chapters) != 1 || afterOrganize.Chapters[0].Title != origChapterTitle {
		t.Fatalf("DB mutated during /ai/organize! got title=%q chapters=%d", afterOrganize.Title, len(afterOrganize.Chapters))
	}

	// Call /ai/apply with selective toggles: apply_title=true, apply_chapters=false
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/ai/apply", map[string]any{
		"apply_title":    true,
		"apply_overview": false,
		"apply_tags":     false,
		"apply_chapters": false,
		"title":          "Clean Docker Course",
		"overview":       "Learn Docker from scratch.",
		"tags":           []string{"docker", "devops"},
		"chapters": []map[string]any{
			{
				"title":   "Getting Started",
				"lessons": []map[string]any{{"lesson_id": l1ID, "title": "Environment Setup"}},
			},
			{
				"title":   "Core Concepts",
				"lessons": []map[string]any{{"lesson_id": l2ID, "title": "Containers Deep Dive"}},
			},
		},
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /ai/apply (title only) = %d, want 200: %s", resp.StatusCode, raw)
	}

	afterTitleApply, err := env.catSvc.GetCourseDetail(context.Background(), course.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail: %v", err)
	}
	if afterTitleApply.Title != "Clean Docker Course" {
		t.Fatalf("expected title updated, got %q", afterTitleApply.Title)
	}
	if afterTitleApply.Overview != "" || len(afterTitleApply.Tags) != 0 || len(afterTitleApply.Chapters) != 1 {
		t.Fatalf("expected overview/tags/chapters untouched, got overview=%q tags=%v chapters=%d",
			afterTitleApply.Overview, afterTitleApply.Tags, len(afterTitleApply.Chapters))
	}

	// Now apply chapters and tags and verify playback progress on l1ID is preserved
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/ai/apply", map[string]any{
		"apply_title":    false,
		"apply_overview": true,
		"apply_tags":     true,
		"apply_chapters": true,
		"overview":       "Learn Docker from scratch.",
		"tags":           []string{"docker", "devops"},
		"chapters": []map[string]any{
			{
				"title":   "Getting Started",
				"lessons": []map[string]any{{"lesson_id": l1ID, "title": "Environment Setup"}},
			},
			{
				"title":   "Core Concepts",
				"lessons": []map[string]any{{"lesson_id": l2ID, "title": "Containers Deep Dive"}},
			},
		},
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /ai/apply (chapters) = %d, want 200: %s", resp.StatusCode, raw)
	}

	afterFullApply, err := env.catSvc.GetCourseDetail(context.Background(), course.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail: %v", err)
	}
	if len(afterFullApply.Chapters) != 2 || afterFullApply.Chapters[0].Lessons[0].Title != "Environment Setup" {
		t.Fatalf("expected 2 reorganized chapters, got %+v", afterFullApply.Chapters)
	}
	if afterFullApply.Chapters[0].Lessons[0].PositionTicks != 420000000 {
		t.Fatalf("expected lesson progress preserved after chapter reorganization, got %d", afterFullApply.Chapters[0].Lessons[0].PositionTicks)
	}
}

func TestLessonPlaybackEndpointReportsDirectHLSAndUnsupported(t *testing.T) {
	env := setupAPITestEnv(t)

	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret", auth.DeviceInfo{DeviceID: "d1"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	courseDir := filepath.Join(env.mediaDir, "Codecs")
	writeDummyFile(t, filepath.Join(courseDir, "01_direct.mp4"), []byte("mp4"))
	writeDummyFile(t, filepath.Join(courseDir, "01_direct.zh.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\n你好\n"))
	writeDummyFile(t, filepath.Join(courseDir, "02_hls.mkv"), []byte("mkv"))
	writeDummyFile(t, filepath.Join(courseDir, "03_legacy.wmv"), []byte("wmv"))

	src, err := env.catSvc.CreateSource(context.Background(), "Disk", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	course, err := env.catSvc.CommitDirectoryImport(context.Background(), catalog.CommitImportInput{
		SourceID:   src.ID,
		DirRelPath: "Codecs",
	})
	if err != nil {
		t.Fatalf("CommitDirectoryImport: %v", err)
	}

	lessons := course.Chapters[0].Lessons
	if len(lessons) != 3 {
		t.Fatalf("expected 3 lessons, got %d", len(lessons))
	}

	type playbackResp struct {
		Mode       string `json:"mode"`
		StreamURL  string `json:"stream_url"`
		VideoCodec string `json:"video_codec"`
		AudioCodec string `json:"audio_codec"`
		Container  string `json:"container"`
		Subtitles  []struct {
			Language string `json:"language"`
			URL      string `json:"url"`
		} `json:"subtitles"`
	}

	// 1. MP4 (h264/aac) -> direct
	resp, raw := env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lessons[0].ID+"/playback", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET playback direct = %d: %s", resp.StatusCode, raw)
	}
	var p1 playbackResp
	if err := json.Unmarshal(raw, &p1); err != nil {
		t.Fatalf("unmarshal p1: %v", err)
	}
	if p1.Mode != "direct" || !strings.Contains(p1.StreamURL, "/Videos/"+lessons[0].ID+"/stream.mp4") || !strings.Contains(p1.StreamURL, "Static=true") {
		t.Fatalf("expected direct mode and stream.mp4 URL, got %+v", p1)
	}
	if len(p1.Subtitles) != 1 || !strings.Contains(p1.Subtitles[0].URL, "Stream.vtt") {
		t.Fatalf("expected 1 VTT subtitle URL, got %+v", p1.Subtitles)
	}

	// 2. MKV (h264/ac3) -> hls
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lessons[1].ID+"/playback", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET playback hls = %d: %s", resp.StatusCode, raw)
	}
	var p2 playbackResp
	if err := json.Unmarshal(raw, &p2); err != nil {
		t.Fatalf("unmarshal p2: %v", err)
	}
	if p2.Mode != "hls" || !strings.Contains(p2.StreamURL, "/Videos/"+lessons[1].ID+"/master.m3u8") || !strings.Contains(p2.StreamURL, "PlaySessionId=") {
		t.Fatalf("expected hls mode and master.m3u8 URL, got %+v", p2)
	}

	// 3. WMV (wmv3/wmav2) -> unsupported
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lessons[2].ID+"/playback", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET playback unsupported = %d: %s", resp.StatusCode, raw)
	}
	var p3 playbackResp
	if err := json.Unmarshal(raw, &p3); err != nil {
		t.Fatalf("unmarshal p3: %v", err)
	}
	if p3.Mode != "unsupported" || p3.StreamURL != "" {
		t.Fatalf("expected unsupported mode with empty stream_url, got %+v", p3)
	}

	// 4. WebM (vp9/opus) -> direct, MKV (vp9/opus) -> unsupported, MKV (hevc/aac) -> unsupported, MKV (h264/opus) -> hls
	extraCases := []struct {
		name      string
		relPath   string
		container string
		vcodec    string
		acodec    string
		wantMode  string
	}{
		{name: "webm_vp9_opus", relPath: "CodecTest/04_vp9.webm", container: "webm", vcodec: "vp9", acodec: "opus", wantMode: "direct"},
		{name: "mkv_vp9_opus", relPath: "CodecTest/05_vp9.mkv", container: "mkv", vcodec: "vp9", acodec: "opus", wantMode: "unsupported"},
		{name: "mkv_hevc_aac", relPath: "CodecTest/06_hevc.mkv", container: "mkv", vcodec: "hevc", acodec: "aac", wantMode: "unsupported"},
		{name: "mkv_av1_aac", relPath: "CodecTest/08_av1.mkv", container: "mkv", vcodec: "av1", acodec: "aac", wantMode: "unsupported"},
		{name: "mkv_h264_opus", relPath: "CodecTest/07_h264_opus.mkv", container: "mkv", vcodec: "h264", acodec: "opus", wantMode: "hls"},
	}
	for _, tc := range extraCases {
		mfID := id.New()
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := env.db.Exec(`
			INSERT INTO media_files (id, source_id, relative_path, file_name, file_size, mod_time, status, duration_ms, container, video_codec, audio_codec, created_at, updated_at)
			VALUES (?, ?, ?, ?, 100, ?, 'active', 10000, ?, ?, ?, ?, ?)`,
			mfID, src.ID, tc.relPath, filepath.Base(tc.relPath), now, tc.container, tc.vcodec, tc.acodec, now, now,
		); err != nil {
			t.Fatalf("insert media_file %s: %v", tc.name, err)
		}
		lsn, err := env.catSvc.AddLesson(context.Background(), course.Chapters[0].ID, mfID, tc.name)
		if err != nil {
			t.Fatalf("AddLesson %s: %v", tc.name, err)
		}
		cResp, cRaw := env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lsn.ID+"/playback", nil, token)
		if cResp.StatusCode != http.StatusOK {
			t.Fatalf("GET playback %s = %d: %s", tc.name, cResp.StatusCode, cRaw)
		}
		var cp playbackResp
		if err := json.Unmarshal(cRaw, &cp); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.name, err)
		}
		if cp.Mode != tc.wantMode {
			t.Fatalf("%s: got mode=%q, want %q", tc.name, cp.Mode, tc.wantMode)
		}
	}
}

func TestCourseCoverUploadAndLessonFramePick(t *testing.T) {
	env := setupAPITestEnv(t)

	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret", auth.DeviceInfo{DeviceID: "d1"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	courseDir := filepath.Join(env.mediaDir, "CoverCourse")
	writeDummyFile(t, filepath.Join(courseDir, "01.mp4"), []byte("mp4"))
	src, err := env.catSvc.CreateSource(context.Background(), "Disk", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	course, err := env.catSvc.CommitDirectoryImport(context.Background(), catalog.CommitImportInput{
		SourceID:   src.ID,
		DirRelPath: "CoverCourse",
	})
	if err != nil {
		t.Fatalf("CommitDirectoryImport: %v", err)
	}
	lessonID := course.Chapters[0].Lessons[0].ID

	// 1. Pick lesson thumbnail frame as cover
	resp, raw := env.doJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/cover", map[string]string{
		"lesson_id": lessonID,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /cover lesson_id = %d: %s", resp.StatusCode, raw)
	}
	var updated catalog.CourseDetail
	if err := json.Unmarshal(raw, &updated); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if updated.CoverPath == "" {
		t.Fatalf("expected non-empty cover_path after frame pick")
	}
	if _, err := os.Stat(filepath.Join(env.dataDir, "covers", updated.CoverPath)); err != nil {
		t.Fatalf("expected cover file in dataDir/covers: %v", err)
	}

	// 2. Upload multipart PNG cover
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "poster.png")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	_ = png.Encode(fw, img)
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/courses/"+course.ID+"/cover", &body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	upResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload cover: %v", err)
	}
	defer upResp.Body.Close()
	upRaw, _ := io.ReadAll(upResp.Body)
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("multipart POST /cover = %d: %s", upResp.StatusCode, upRaw)
	}
	if err := json.Unmarshal(upRaw, &updated); err != nil {
		t.Fatalf("unmarshal upload resp: %v", err)
	}
	if !strings.HasSuffix(updated.CoverPath, ".png") {
		t.Fatalf("expected .png cover_path, got %q", updated.CoverPath)
	}

	// 3. Reject disguised non-image file even with .png extension
	var badBody bytes.Buffer
	badMW := multipart.NewWriter(&badBody)
	badFW, err := badMW.CreateFormFile("file", "evil.png")
	if err != nil {
		t.Fatalf("CreateFormFile bad: %v", err)
	}
	_, _ = badFW.Write([]byte("<html><script>alert(1)</script></html>"))
	_ = badMW.Close()

	badReq, err := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/courses/"+course.ID+"/cover", &badBody)
	if err != nil {
		t.Fatalf("NewRequest bad: %v", err)
	}
	badReq.Header.Set("Content-Type", badMW.FormDataContentType())
	badReq.Header.Set("Authorization", "Bearer "+token)
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatalf("upload bad cover: %v", err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for disguised non-image cover, got %d", badResp.StatusCode)
	}

	// Ensure id package is used cleanly if needed
	_ = id.New()
}

type blockingProber struct {
	fakeProber
	startedOnce sync.Once
	startedCh   chan struct{}
	releaseCh   chan struct{}
}

func (b *blockingProber) Probe(ctx context.Context, absPath string) (*scanner.ProbeResult, error) {
	b.startedOnce.Do(func() {
		close(b.startedCh)
	})
	select {
	case <-b.releaseCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.fakeProber.Probe(ctx, absPath)
}

func TestAsyncScanDeterministicRunningAndConflict(t *testing.T) {
	dataDir := t.TempDir()
	mediaDir := t.TempDir()

	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	reg := storage.NewRegistry()
	authSvc := auth.NewService(conn)
	prober := &blockingProber{
		startedCh: make(chan struct{}),
		releaseCh: make(chan struct{}),
	}
	scanSvc := scanner.NewService(conn, dataDir, reg, prober)
	catSvc := catalog.NewService(conn, dataDir)
	catSvc.SetRegistry(reg)
	mediaSvc := media.NewService(conn, dataDir, reg, "")
	defer mediaSvc.Close()

	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	defer cancelLifecycle()

	h := api.NewHandler(api.Deps{
		DB:           conn,
		DataDir:      dataDir,
		Registry:     reg,
		Auth:         authSvc,
		Scanner:      scanSvc,
		Catalog:      catSvc,
		Media:        mediaSvc,
		LifecycleCtx: lifecycleCtx,
	})
	ts := httptest.NewServer(h)
	defer ts.Close()

	if _, err := authSvc.SetupAdmin(context.Background(), "admin", "secret"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := authSvc.Login(context.Background(), "admin", "secret", auth.DeviceInfo{DeviceID: "d1"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	writeDummyFile(t, filepath.Join(mediaDir, "Course", "01.mp4"), []byte("video"))
	src, err := catSvc.CreateSource(context.Background(), "Disk", "local", mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	doReq := func(method, p string) (*http.Response, []byte) {
		req, _ := http.NewRequest(method, ts.URL+p, nil)
		req.Header.Set("Authorization", "Bearer "+sess.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, p, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp, raw
	}

	// 1. First async scan returns 202 deterministically with running: true
	resp, raw := doReq(http.MethodPost, "/api/v1/sources/"+src.ID+"/scan?async=true")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /scan?async=true = %d, want 202: %s", resp.StatusCode, raw)
	}
	var st scanner.ScanState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if !st.Running || st.StartedAt == nil {
		t.Fatalf("expected immediate running=true and started_at in 202 response, got %s", raw)
	}

	// Wait until probe worker enters Probe (blocked on releaseCh)
	select {
	case <-prober.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for async scan to enter Probe")
	}

	// 2. Concurrent second async scan returns 409 Conflict
	resp2, raw2 := doReq(http.MethodPost, "/api/v1/sources/"+src.ID+"/scan?async=true")
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second POST /scan?async=true = %d, want 409: %s", resp2.StatusCode, raw2)
	}

	// Status endpoint also shows running: true while blocked
	respStatus, rawStatus := doReq(http.MethodGet, "/api/v1/sources/"+src.ID+"/scan/status")
	if respStatus.StatusCode != http.StatusOK {
		t.Fatalf("GET /scan/status = %d: %s", respStatus.StatusCode, rawStatus)
	}
	if err := json.Unmarshal(rawStatus, &st); err != nil || !st.Running {
		t.Fatalf("expected running=true from /scan/status, got %s", rawStatus)
	}

	// Release prober and wait for completion
	close(prober.releaseCh)
	scanSvc.Wait()

	finalState := scanSvc.Status(src.ID)
	if finalState.Running || finalState.LastReport == nil || finalState.LastReport.TotalVideos != 1 {
		t.Fatalf("expected completed scan with TotalVideos=1, got %+v", finalState)
	}

	// 3. Also test cancelling lifecycleCtx while a second async scan is blocked, then Wait() before closing DB
	prober2 := &blockingProber{
		startedCh: make(chan struct{}),
		releaseCh: make(chan struct{}),
	}
	scanSvc2 := scanner.NewService(conn, dataDir, reg, prober2)
	lc2, cancelLC2 := context.WithCancel(context.Background())
	writeDummyFile(t, filepath.Join(mediaDir, "Course", "02.mp4"), []byte("video2"))
	if _, err := scanSvc2.StartScanAsync(lc2, src.ID); err != nil {
		t.Fatalf("StartScanAsync: %v", err)
	}
	select {
	case <-prober2.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for scanSvc2 probe")
	}
	cancelLC2()
	scanSvc2.Wait()
	if st2 := scanSvc2.Status(src.ID); st2.Running {
		t.Fatalf("expected scanSvc2 not running after Wait()")
	}
}

func TestBrowseFSDirs(t *testing.T) {
	env := setupAPITestEnv(t)

	// Setup admin & login
	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "fs-test"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	// 1. Unauthenticated request returns 401
	resp, raw := env.doJSON(t, http.MethodGet, "/api/v1/fs/dirs", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/v1/fs/dirs = %d, want 401: %s", resp.StatusCode, raw)
	}

	type browseResp struct {
		CurrentPath string `json:"current_path"`
		ParentPath  string `json:"parent_path"`
		Entries     []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"entries"`
		Roots []struct {
			Label string `json:"label"`
			Path  string `json:"path"`
		} `json:"roots"`
	}

	// 2. Authenticated default browse returns 200 with non-empty current_path & roots
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/fs/dirs", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/fs/dirs = %d, want 200: %s", resp.StatusCode, raw)
	}
	var def browseResp
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("unmarshal default browse: %v", err)
	}
	if def.CurrentPath == "" || !filepath.IsAbs(def.CurrentPath) {
		t.Fatalf("expected absolute current_path, got %q", def.CurrentPath)
	}
	if len(def.Roots) == 0 {
		t.Fatalf("expected non-empty roots, got %s", raw)
	}

	// 3. Explicit temp directory with subdirs, hidden dir, file, symlink to dir, broken symlink
	tmp := t.TempDir()
	for _, sub := range []string{"10_go", "2_rust", ".hidden"} {
		if err := os.Mkdir(filepath.Join(tmp, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	filePath := filepath.Join(tmp, "readme.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	linkTarget := t.TempDir()
	if err := os.Symlink(linkTarget, filepath.Join(tmp, "link_dir")); err != nil {
		t.Fatalf("symlink valid: %v", err)
	}
	if err := os.Symlink(filepath.Join(tmp, "does-not-exist-target"), filepath.Join(tmp, "broken_link")); err != nil {
		t.Fatalf("symlink broken: %v", err)
	}

	q := "/api/v1/fs/dirs?path=" + url.QueryEscape(tmp)
	resp, raw = env.doJSON(t, http.MethodGet, q, nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200: %s", q, resp.StatusCode, raw)
	}
	var listed browseResp
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatalf("unmarshal listed: %v", err)
	}
	if listed.CurrentPath != filepath.Clean(tmp) {
		t.Fatalf("current_path = %q, want %q", listed.CurrentPath, filepath.Clean(tmp))
	}
	if listed.ParentPath != filepath.Dir(filepath.Clean(tmp)) {
		t.Fatalf("parent_path = %q, want %q", listed.ParentPath, filepath.Dir(filepath.Clean(tmp)))
	}
	var names []string
	for _, e := range listed.Entries {
		names = append(names, e.Name)
		if e.Path != filepath.Join(filepath.Clean(tmp), e.Name) {
			t.Fatalf("entry %q path = %q, want %q", e.Name, e.Path, filepath.Join(filepath.Clean(tmp), e.Name))
		}
	}
	wantNames := []string{"2_rust", "10_go", "link_dir"}
	if strings.Join(names, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("entries = %v, want %v", names, wantNames)
	}

	// show_hidden=true includes .hidden
	resp, raw = env.doJSON(t, http.MethodGet, q+"&show_hidden=true", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET show_hidden = %d: %s", resp.StatusCode, raw)
	}
	var listedHidden browseResp
	if err := json.Unmarshal(raw, &listedHidden); err != nil {
		t.Fatalf("unmarshal hidden: %v", err)
	}
	foundHidden := false
	for _, e := range listedHidden.Entries {
		if e.Name == ".hidden" {
			foundHidden = true
		}
	}
	if !foundHidden {
		t.Fatalf("expected .hidden in entries with show_hidden=true, got %+v", listedHidden.Entries)
	}

	// 4. Browsing filesystem root returns parent_path == ""
	if runtime.GOOS != "windows" {
		resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/fs/dirs?path="+url.QueryEscape("/"), nil, token)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET root = %d: %s", resp.StatusCode, raw)
		}
		var rootResp browseResp
		if err := json.Unmarshal(raw, &rootResp); err != nil {
			t.Fatalf("unmarshal root: %v", err)
		}
		if rootResp.ParentPath != "" {
			t.Fatalf("root parent_path = %q, want empty", rootResp.ParentPath)
		}
	}

	// 5. Validation & error cases
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/fs/dirs?path="+url.QueryEscape("relative/dir"), nil, token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("relative path = %d, want 400: %s", resp.StatusCode, raw)
	}
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/fs/dirs?path="+url.QueryEscape(filePath), nil, token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("file path = %d, want 400: %s", resp.StatusCode, raw)
	}
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/fs/dirs?path="+url.QueryEscape(filepath.Join(tmp, "missing-dir")), nil, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing path = %d, want 404: %s", resp.StatusCode, raw)
	}

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		noPermDir := filepath.Join(tmp, "no_perm")
		if err := os.Mkdir(noPermDir, 0o755); err != nil {
			t.Fatalf("mkdir no_perm: %v", err)
		}
		if err := os.Chmod(noPermDir, 0o000); err != nil {
			t.Fatalf("chmod no_perm: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(noPermDir, 0o755) })

		resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/fs/dirs?path="+url.QueryEscape(noPermDir), nil, token)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("unreadable dir = %d, want 403: %s", resp.StatusCode, raw)
		}
	}
}
