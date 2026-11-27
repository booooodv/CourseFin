package api_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	"sync/atomic"
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
	"coursefin/internal/summary"
	"coursefin/internal/updater"

	"golang.org/x/text/encoding/simplifiedchinese"
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
		"ai_base_url":           mockAI.URL + "/v1",
		"ai_api_key":            "sk-secret-12345",
		"ai_model":              "gpt-4o-mini",
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
		Mode        string `json:"mode"`
		StreamURL   string `json:"stream_url"`
		Transcoding bool   `json:"transcoding"`
		VideoCodec  string `json:"video_codec"`
		AudioCodec  string `json:"audio_codec"`
		Container   string `json:"container"`
		Subtitles   []struct {
			Language string `json:"language"`
			URL      string `json:"url"`
		} `json:"subtitles"`
	}

	hasFFmpeg := env.mediaSvc.HasFFmpeg()

	// 1. MP4 (h264/aac) -> direct
	resp, raw := env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lessons[0].ID+"/playback", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET playback direct = %d: %s", resp.StatusCode, raw)
	}
	var p1 playbackResp
	if err := json.Unmarshal(raw, &p1); err != nil {
		t.Fatalf("unmarshal p1: %v", err)
	}
	if p1.Mode != "direct" || p1.Transcoding || !strings.Contains(p1.StreamURL, "/Videos/"+lessons[0].ID+"/stream.mp4") || !strings.Contains(p1.StreamURL, "Static=true") {
		t.Fatalf("expected direct mode and stream.mp4 URL, got %+v", p1)
	}
	if len(p1.Subtitles) != 1 || !strings.Contains(p1.Subtitles[0].URL, "Stream.vtt") {
		t.Fatalf("expected 1 VTT subtitle URL, got %+v", p1.Subtitles)
	}

	// 1b. MP4 (h264/aac) with force_transcode=1 -> hls + transcoding when FFmpeg available
	if hasFFmpeg {
		resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lessons[0].ID+"/playback?force_transcode=1", nil, token)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET playback force_transcode = %d: %s", resp.StatusCode, raw)
		}
		var pForce playbackResp
		if err := json.Unmarshal(raw, &pForce); err != nil {
			t.Fatalf("unmarshal pForce: %v", err)
		}
		if pForce.Mode != "hls" || !pForce.Transcoding || !strings.Contains(pForce.StreamURL, "TranscodeVideo=1") {
			t.Fatalf("expected force_transcode hls+TranscodeVideo=1, got %+v", pForce)
		}
	}

	// 2. MKV (h264/ac3) -> hls (remux, transcoding=false) when FFmpeg available
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lessons[1].ID+"/playback", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET playback hls = %d: %s", resp.StatusCode, raw)
	}
	var p2 playbackResp
	if err := json.Unmarshal(raw, &p2); err != nil {
		t.Fatalf("unmarshal p2: %v", err)
	}
	if hasFFmpeg {
		if p2.Mode != "hls" || p2.Transcoding || !strings.Contains(p2.StreamURL, "/Videos/"+lessons[1].ID+"/master.m3u8") || strings.Contains(p2.StreamURL, "TranscodeVideo=1") {
			t.Fatalf("expected hls remux mode without TranscodeVideo=1, got %+v", p2)
		}
	}

	// 3. WMV (wmv3/wmav2) -> hls transcoding when FFmpeg available, else unsupported
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lessons[2].ID+"/playback", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET playback wmv = %d: %s", resp.StatusCode, raw)
	}
	var p3 playbackResp
	if err := json.Unmarshal(raw, &p3); err != nil {
		t.Fatalf("unmarshal p3: %v", err)
	}
	if hasFFmpeg {
		if p3.Mode != "hls" || !p3.Transcoding || !strings.Contains(p3.StreamURL, "TranscodeVideo=1") {
			t.Fatalf("expected wmv3 to fall back to hls transcoding when ffmpeg available, got %+v", p3)
		}
	} else if p3.Mode != "unsupported" || p3.StreamURL != "" {
		t.Fatalf("expected unsupported mode without ffmpeg, got %+v", p3)
	}

	// 4. Three-tier codec + browser capability + pix_fmt matrix
	wantTranscodeMode := "unsupported"
	wantTranscodeFlag := false
	if hasFFmpeg {
		wantTranscodeMode = "hls"
		wantTranscodeFlag = true
	}

	extraCases := []struct {
		name          string
		relPath       string
		container     string
		vcodec        string
		acodec        string
		streamsJSON   string
		query         string
		wantMode      string
		wantTranscode bool
	}{
		{name: "webm_vp9_opus", relPath: "CodecTest/04_vp9.webm", container: "webm", vcodec: "vp9", acodec: "opus", streamsJSON: "[]", wantMode: "direct", wantTranscode: false},
		{name: "mkv_vp9_opus", relPath: "CodecTest/05_vp9.mkv", container: "mkv", vcodec: "vp9", acodec: "opus", streamsJSON: "[]", wantMode: wantTranscodeMode, wantTranscode: wantTranscodeFlag},
		{name: "mkv_hevc_aac", relPath: "CodecTest/06_hevc.mkv", container: "mkv", vcodec: "hevc", acodec: "aac", streamsJSON: "[]", wantMode: wantTranscodeMode, wantTranscode: wantTranscodeFlag},
		{name: "mkv_av1_aac", relPath: "CodecTest/08_av1.mkv", container: "mkv", vcodec: "av1", acodec: "aac", streamsJSON: "[]", wantMode: wantTranscodeMode, wantTranscode: wantTranscodeFlag},
		{name: "mkv_h264_opus", relPath: "CodecTest/07_h264_opus.mkv", container: "mkv", vcodec: "h264", acodec: "opus", streamsJSON: "[]", wantMode: wantTranscodeMode, wantTranscode: false},
		{name: "mp4_hevc_direct_when_supported", relPath: "CodecTest/09_hevc.mp4", container: "mp4", vcodec: "hevc", acodec: "aac", streamsJSON: `[{"index":0,"codec_type":"video","codec_name":"hevc","pix_fmt":"yuv420p"}]`, query: "?codecs=h264,hevc,vp9,av1", wantMode: "direct", wantTranscode: false},
		{name: "mp4_hevc_transcode_when_not_supported", relPath: "CodecTest/10_hevc.mp4", container: "mp4", vcodec: "hevc", acodec: "aac", streamsJSON: `[{"index":0,"codec_type":"video","codec_name":"hevc","pix_fmt":"yuv420p"}]`, query: "?codecs=h264,vp9,av1", wantMode: wantTranscodeMode, wantTranscode: wantTranscodeFlag},
		{name: "mp4_hevc_yuv444p_forces_transcode", relPath: "CodecTest/11_hevc444.mp4", container: "mp4", vcodec: "hevc", acodec: "aac", streamsJSON: `[{"index":0,"codec_type":"video","codec_name":"hevc","pix_fmt":"yuv444p"}]`, query: "?codecs=h264,hevc", wantMode: wantTranscodeMode, wantTranscode: wantTranscodeFlag},
	}
	for _, tc := range extraCases {
		mfID := id.New()
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := env.db.Exec(`
			INSERT INTO media_files (id, source_id, relative_path, file_name, file_size, mod_time, status, duration_ms, container, video_codec, audio_codec, streams_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, 100, ?, 'active', 10000, ?, ?, ?, ?, ?, ?)`,
			mfID, src.ID, tc.relPath, filepath.Base(tc.relPath), now, tc.container, tc.vcodec, tc.acodec, tc.streamsJSON, now, now,
		); err != nil {
			t.Fatalf("insert media_file %s: %v", tc.name, err)
		}
		lsn, err := env.catSvc.AddLesson(context.Background(), course.Chapters[0].ID, mfID, tc.name)
		if err != nil {
			t.Fatalf("AddLesson %s: %v", tc.name, err)
		}
		cResp, cRaw := env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lsn.ID+"/playback"+tc.query, nil, token)
		if cResp.StatusCode != http.StatusOK {
			t.Fatalf("GET playback %s = %d: %s", tc.name, cResp.StatusCode, cRaw)
		}
		var cp playbackResp
		if err := json.Unmarshal(cRaw, &cp); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.name, err)
		}
		if cp.Mode != tc.wantMode || cp.Transcoding != tc.wantTranscode {
			t.Fatalf("%s: got mode=%q transcoding=%v, want mode=%q transcoding=%v", tc.name, cp.Mode, cp.Transcoding, tc.wantMode, tc.wantTranscode)
		}
		if cp.Transcoding && !strings.Contains(cp.StreamURL, "TranscodeVideo=1") {
			t.Fatalf("%s: expected TranscodeVideo=1 in stream_url %q", tc.name, cp.StreamURL)
		}
	}

	// 5. Without FFmpeg binary, non-direct video degrades cleanly to unsupported
	noFFmpegMedia := media.NewService(env.db, env.dataDir, env.registry, "/nonexistent/coursefin-ffmpeg")
	defer noFFmpegMedia.Close()
	noFFmpegHandler := api.NewHandler(api.Deps{
		DB:       env.db,
		DataDir:  env.dataDir,
		Registry: env.registry,
		Auth:     env.authSvc,
		Scanner:  env.scanSvc,
		Catalog:  env.catSvc,
		Media:    noFFmpegMedia,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lessons/"+lessons[2].ID+"/playback", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	noFFmpegHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-ffmpeg playback status = %d: %s", rec.Code, rec.Body.String())
	}
	var pNoFF playbackResp
	if err := json.Unmarshal(rec.Body.Bytes(), &pNoFF); err != nil {
		t.Fatalf("unmarshal pNoFF: %v", err)
	}
	if pNoFF.Mode != "unsupported" || pNoFF.StreamURL != "" || pNoFF.Transcoding {
		t.Fatalf("expected unsupported without ffmpeg, got %+v", pNoFF)
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

func TestDeleteLibraryAPIModes(t *testing.T) {
	env := setupAPITestEnv(t)

	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "lib-del"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	lib1, err := env.catSvc.CreateLibrary(context.Background(), "后端")
	if err != nil {
		t.Fatalf("CreateLibrary 1: %v", err)
	}
	lib2, err := env.catSvc.CreateLibrary(context.Background(), "全栈")
	if err != nil {
		t.Fatalf("CreateLibrary 2: %v", err)
	}
	course, err := env.catSvc.CreateCourse(context.Background(), catalog.CreateCourseInput{
		LibraryID: lib1.ID,
		Title:     "分布式系统",
	})
	if err != nil {
		t.Fatalf("CreateCourse: %v", err)
	}

	// 1. Default delete with courses -> 409 Conflict
	resp, raw := env.doJSON(t, http.MethodDelete, "/api/v1/libraries/"+lib1.ID, nil, token)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("DELETE non-empty library = %d, want 409: %s", resp.StatusCode, raw)
	}

	// 2. Both move_to_library_id & force=true -> 400 Bad Request
	resp, raw = env.doJSON(t, http.MethodDelete, "/api/v1/libraries/"+lib1.ID+"?move_to_library_id="+lib2.ID+"&force=true", nil, token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE move+force = %d, want 400: %s", resp.StatusCode, raw)
	}

	// 3. Migrate courses to lib2 and delete lib1 -> 200
	resp, raw = env.doJSON(t, http.MethodDelete, "/api/v1/libraries/"+lib1.ID+"?move_to_library_id="+lib2.ID, nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE move_to_library_id = %d, want 200: %s", resp.StatusCode, raw)
	}
	moved, err := env.catSvc.GetCourseDetail(context.Background(), course.ID)
	if err != nil {
		t.Fatalf("GetCourseDetail after migrate: %v", err)
	}
	if moved.LibraryID != lib2.ID {
		t.Fatalf("moved.LibraryID = %q, want %q", moved.LibraryID, lib2.ID)
	}

	// 4. Force delete lib2 -> 200 and course removed
	resp, raw = env.doJSON(t, http.MethodDelete, "/api/v1/libraries/"+lib2.ID+"?force=true", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE force=true = %d, want 200: %s", resp.StatusCode, raw)
	}
	resp, _ = env.doJSON(t, http.MethodGet, "/api/v1/courses/"+course.ID, nil, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET course after force delete = %d, want 404", resp.StatusCode)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func buildTestReleaseTarGz(t *testing.T, script []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	hdr := &tar.Header{
		Name:     "coursefin",
		Mode:     0o755,
		Size:     int64(len(script)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar WriteHeader: %v", err)
	}
	if _, err := tw.Write(script); err != nil {
		t.Fatalf("tar Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	return buf.Bytes()
}

func TestSystemUpdateEndpoints(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("self-update apply test requires linux or darwin")
	}

	dataDir := t.TempDir()
	managedDir := filepath.Join(t.TempDir(), "managed-bin")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "coursefin"), []byte("#!/bin/sh\necho '1.0.1'\n"), 0o755); err != nil {
		t.Fatalf("write initial binary stub: %v", err)
	}

	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	authSvc := auth.NewService(conn)
	if _, err := authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "upd-test"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	assetName := fmt.Sprintf("coursefin_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := buildTestReleaseTarGz(t, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo \"1.2.0\"\n  exit 0\nfi\nexit 1\n"))
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)

	releaseURL := "https://api.github.example/repos/booooodv/CourseFin/releases/latest"
	assetURL := "https://github.example/booooodv/CourseFin/releases/download/v1.2.0/" + assetName
	checksumsURL := "https://github.example/booooodv/CourseFin/releases/download/v1.2.0/checksums.txt"

	var failUpstream atomic.Bool
	failUpstream.Store(true)
	downloadStarted := make(chan struct{})
	releaseDownload := make(chan struct{})
	var downloadStartedOnce sync.Once
	var releaseDownloadOnce sync.Once

	mockGitHubClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			rec := httptest.NewRecorder()
			switch req.URL.String() {
			case releaseURL:
				if failUpstream.Load() {
					rec.WriteHeader(http.StatusBadGateway)
					_, _ = rec.WriteString("upstream unavailable")
				} else {
					rec.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(rec, `{
						"tag_name": "v1.2.0",
						"name": "CourseFin v1.2.0",
						"body": "Release notes v1.2.0",
						"html_url": "https://github.example/booooodv/CourseFin/releases/tag/v1.2.0",
						"published_at": "2026-11-24T12:00:00Z",
						"draft": false,
						"prerelease": false,
						"assets": [
							{"name": %q, "size": %d, "browser_download_url": %q},
							{"name": "checksums.txt", "size": %d, "browser_download_url": %q}
						]
					}`, assetName, len(archive), assetURL, len(checksums), checksumsURL)
				}
			case checksumsURL:
				rec.Header().Set("Content-Type", "text/plain")
				_, _ = rec.WriteString(checksums)
			case assetURL:
				downloadStartedOnce.Do(func() { close(downloadStarted) })
				select {
				case <-releaseDownload:
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
				rec.Header().Set("Content-Type", "application/octet-stream")
				rec.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
				_, _ = rec.Write(archive)
			default:
				rec.WriteHeader(http.StatusNotFound)
			}
			resp := rec.Result()
			resp.Request = req
			return resp, nil
		}),
	}

	updaterSvc := updater.NewService(updater.Options{
		DB:                conn,
		DataDir:           dataDir,
		CurrentVersion:    "1.0.1",
		APIBase:           "https://api.github.example",
		ManagedBinDir:     managedDir,
		HTTPClient:        mockGitHubClient,
		RestartGraceDelay: 5 * time.Millisecond,
	})

	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	t.Cleanup(func() {
		releaseDownloadOnce.Do(func() { close(releaseDownload) })
		cancelLifecycle()
		updaterSvc.Wait()
	})

	h := api.NewHandler(api.Deps{
		DB:           conn,
		DataDir:      dataDir,
		Auth:         authSvc,
		Updater:      updaterSvc,
		LifecycleCtx: lifecycleCtx,
	})

	doReq := func(method, reqPath string, body any, authToken string) (*httptest.ResponseRecorder, []byte) {
		t.Helper()
		var r io.Reader
		if body != nil {
			switch v := body.(type) {
			case string:
				r = strings.NewReader(v)
			default:
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatalf("marshal body: %v", err)
				}
				r = bytes.NewReader(b)
			}
		}
		req := httptest.NewRequest(method, reqPath, r)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if authToken != "" {
			req.Header.Set("Authorization", "Bearer "+authToken)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec, rec.Body.Bytes()
	}

	// 1. Unauthenticated GET /api/v1/system/update returns 401
	rec, raw := doReq(http.MethodGet, "/api/v1/system/update", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/v1/system/update = %d, want 401: %s", rec.Code, raw)
	}

	// 2. Authenticated GET /api/v1/system/update returns 200 with current_version
	rec, raw = doReq(http.MethodGet, "/api/v1/system/update", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/system/update = %d, want 200: %s", rec.Code, raw)
	}
	var st updater.Status
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal status: %v (%s)", err, raw)
	}
	if st.CurrentVersion != "1.0.1" {
		t.Fatalf("current_version = %q, want 1.0.1", st.CurrentVersion)
	}
	if st.UpdateAvailable {
		t.Fatalf("expected initial update_available=false")
	}

	// 3. PUT /api/v1/system/update/config validates and persists settings
	rec, raw = doReq(http.MethodPut, "/api/v1/system/update/config", map[string]any{
		"auto_check_enabled":   false,
		"check_interval_hours": 12,
		"mirror_url":           "https://gh-proxy.example/",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/v1/system/update/config valid = %d, want 200: %s", rec.Code, raw)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal config status: %v", err)
	}
	if st.AutoCheckEnabled || st.CheckIntervalHours != 12 || st.MirrorURL != "https://gh-proxy.example" {
		t.Fatalf("unexpected persisted config response: %+v", st)
	}

	// Confirm persisted on subsequent GET
	rec, raw = doReq(http.MethodGet, "/api/v1/system/update", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET after PUT config = %d: %s", rec.Code, raw)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal GET after PUT: %v", err)
	}
	if st.AutoCheckEnabled || st.CheckIntervalHours != 12 || st.MirrorURL != "https://gh-proxy.example" {
		t.Fatalf("config did not persist across GET: %+v", st)
	}

	// Invalid mirror scheme -> 400
	rec, raw = doReq(http.MethodPut, "/api/v1/system/update/config", map[string]any{
		"mirror_url": "ftp://bad",
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT invalid mirror scheme = %d, want 400: %s", rec.Code, raw)
	}

	// Invalid interval 0 -> 400
	rec, raw = doReq(http.MethodPut, "/api/v1/system/update/config", map[string]any{
		"check_interval_hours": 0,
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT interval 0 = %d, want 400: %s", rec.Code, raw)
	}

	// Malformed JSON -> 400
	rec, raw = doReq(http.MethodPut, "/api/v1/system/update/config", "{bad-json", token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT malformed JSON = %d, want 400: %s", rec.Code, raw)
	}

	// Clear mirror_url so subsequent check hits mock releaseURL cleanly
	rec, raw = doReq(http.MethodPut, "/api/v1/system/update/config", map[string]any{
		"mirror_url": "",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT clear mirror_url = %d: %s", rec.Code, raw)
	}

	// 4a. POST /api/v1/system/update/apply before any update available -> 400
	rec, raw = doReq(http.MethodPost, "/api/v1/system/update/apply", nil, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /apply before update available = %d, want 400: %s", rec.Code, raw)
	}

	// 4b. POST /api/v1/system/update/check returns 502 when upstream fails
	rec, raw = doReq(http.MethodPost, "/api/v1/system/update/check", nil, token)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST /check upstream error = %d, want 502: %s", rec.Code, raw)
	}

	// 4c. POST /api/v1/system/update/check returns 200 with update_available=true
	failUpstream.Store(false)
	rec, raw = doReq(http.MethodPost, "/api/v1/system/update/check", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /check success = %d, want 200: %s", rec.Code, raw)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal check response: %v", err)
	}
	if !st.UpdateAvailable || st.LatestVersion != "1.2.0" || !st.CanApply {
		t.Fatalf("expected update_available=true, latest_version=1.2.0, can_apply=true, got %+v", st)
	}

	// 5. POST /api/v1/system/update/apply returns 202 and enforces 409 while busy
	rec, raw = doReq(http.MethodPost, "/api/v1/system/update/apply", nil, token)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /apply = %d, want 202: %s", rec.Code, raw)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal apply response: %v", err)
	}
	if st.State != "downloading" || st.TargetVersion != "1.2.0" {
		t.Fatalf("expected downloading state for 1.2.0, got %+v", st)
	}

	select {
	case <-downloadStarted:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for mock download to start")
	}

	// Concurrent apply and check while downloading -> 409
	rec, raw = doReq(http.MethodPost, "/api/v1/system/update/apply", nil, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("concurrent POST /apply = %d, want 409: %s", rec.Code, raw)
	}
	rec, raw = doReq(http.MethodPost, "/api/v1/system/update/check", nil, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("concurrent POST /check = %d, want 409: %s", rec.Code, raw)
	}

	releaseDownloadOnce.Do(func() { close(releaseDownload) })

	select {
	case req := <-updaterSvc.RestartCh():
		if req.ExecPath != filepath.Join(managedDir, "coursefin") {
			t.Fatalf("unexpected RestartRequest ExecPath: %q", req.ExecPath)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for RestartCh")
	}
	updaterSvc.Wait()

	rec, raw = doReq(http.MethodGet, "/api/v1/system/update", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET final status = %d: %s", rec.Code, raw)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal final status: %v", err)
	}
	if st.State != "restarting" || st.ProgressPercent != 100 {
		t.Fatalf("expected restarting state with 100%%, got %+v", st)
	}
}

func TestSystemUpdateEndpointsUnavailableWhenNilUpdater(t *testing.T) {
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	authSvc := auth.NewService(conn)
	if _, err := authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "nil-upd"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	h := api.NewHandler(api.Deps{
		DB:      conn,
		DataDir: dataDir,
		Auth:    authSvc,
	})

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/system/update", ""},
		{http.MethodPut, "/api/v1/system/update/config", "{}"},
		{http.MethodPost, "/api/v1/system/update/check", ""},
		{http.MethodPost, "/api/v1/system/update/apply", ""},
	}
	for _, tc := range cases {
		var r io.Reader
		if tc.body != "" {
			r = strings.NewReader(tc.body)
		}
		req := httptest.NewRequest(tc.method, tc.path, r)
		req.Header.Set("Authorization", "Bearer "+sess.Token)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s with nil updater = %d, want 503: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestSourceSubtitlesAndManualBindEndpoints(t *testing.T) {
	env := setupAPITestEnv(t)
	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "sub-bind-test"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	// Create source with 1 video and 1 unmatched subtitle
	courseDir := filepath.Join(env.mediaDir, "BindCourse")
	if err := os.MkdirAll(courseDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(courseDir, "01_demo.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatalf("write mp4: %v", err)
	}
	if err := os.WriteFile(filepath.Join(courseDir, "unrelated_name.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nHi\n"), 0o644); err != nil {
		t.Fatalf("write srt: %v", err)
	}

	src, err := env.catSvc.CreateSource(context.Background(), "BindSrc", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}

	course, err := env.catSvc.CommitDirectoryImport(context.Background(), catalog.CommitImportInput{
		SourceID:   src.ID,
		DirRelPath: "BindCourse",
		Title:      "Bind Course",
	})
	if err != nil {
		t.Fatalf("CommitDirectoryImport: %v", err)
	}
	lesson := course.Chapters[0].Lessons[0]

	// List unmatched subtitles
	resp, raw := env.doJSON(t, http.MethodGet, "/api/v1/sources/"+src.ID+"/subtitles?unmatched=true", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sources/{id}/subtitles = %d: %s", resp.StatusCode, raw)
	}
	var listRes struct {
		Subtitles []catalog.IndexedSubtitle `json:"subtitles"`
	}
	if err := json.Unmarshal(raw, &listRes); err != nil {
		t.Fatalf("unmarshal subtitles: %v", err)
	}
	if len(listRes.Subtitles) != 1 {
		t.Fatalf("expected 1 unmatched subtitle, got %+v", listRes.Subtitles)
	}
	subID := listRes.Subtitles[0].ID

	// 404 on unknown subtitle
	resp, _ = env.doJSON(t, http.MethodPut, "/api/v1/subtitles/00000000000000000000000000000001/bind", map[string]any{
		"media_file_id": lesson.MediaFileID,
	}, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("bind unknown subtitle = %d, want 404", resp.StatusCode)
	}

	// 404 on unknown media_file_id
	resp, _ = env.doJSON(t, http.MethodPut, "/api/v1/subtitles/"+subID+"/bind", map[string]any{
		"media_file_id": "00000000000000000000000000000002",
	}, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("bind unknown media_file = %d, want 404", resp.StatusCode)
	}

	// Bind to lesson's media file
	resp, raw = env.doJSON(t, http.MethodPut, "/api/v1/subtitles/"+subID+"/bind", map[string]any{
		"media_file_id": lesson.MediaFileID,
		"title":         "手工关联字幕",
		"language":      "zh",
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT bind = %d: %s", resp.StatusCode, raw)
	}

	// Verify bound subtitle appears in playback endpoint
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lesson.ID+"/playback", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET playback = %d: %s", resp.StatusCode, raw)
	}
	var pb struct {
		Subtitles []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"subtitles"`
	}
	if err := json.Unmarshal(raw, &pb); err != nil {
		t.Fatalf("unmarshal playback: %v", err)
	}
	if len(pb.Subtitles) != 1 || pb.Subtitles[0].Title != "手工关联字幕" || pb.Subtitles[0].URL == "" {
		t.Fatalf("expected bound subtitle in playback response, got %+v", pb.Subtitles)
	}
}

func TestImportAIOrganizeEndpoint(t *testing.T) {
	env := setupAPITestEnv(t)
	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "import-ai-test"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	courseDir := filepath.Join(env.mediaDir, "RawGoCourse")
	writeDummyFile(t, filepath.Join(courseDir, "01_intro_1080p.mp4"), []byte("v1"))
	writeDummyFile(t, filepath.Join(courseDir, "02_chan_1080p.mp4"), []byte("v2"))

	// Write a GBK-encoded subtitle for 01_intro_1080p.mp4
	gbkSRT, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("1\n00:00:00,000 --> 00:00:03,000\n欢迎来到 Go 并发编程课程\n"))
	if err != nil {
		t.Fatalf("encode GB18030: %v", err)
	}
	writeDummyFile(t, filepath.Join(courseDir, "01_intro_1080p.zh.srt"), gbkSRT)

	src, err := env.catSvc.CreateSource(context.Background(), "ImportAISrc", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}

	preview, err := env.catSvc.PreviewDirectoryImport(context.Background(), src.ID, "RawGoCourse")
	if err != nil {
		t.Fatalf("PreviewDirectoryImport: %v", err)
	}
	if len(preview.Chapters) != 1 || len(preview.Chapters[0].Lessons) != 2 {
		t.Fatalf("unexpected preview chapters: %+v", preview.Chapters)
	}
	mf1 := preview.Chapters[0].Lessons[0].MediaFileID
	mf2 := preview.Chapters[0].Lessons[1].MediaFileID

	validBody := map[string]any{
		"source_id":    src.ID,
		"dir_rel_path": "RawGoCourse",
		"title":        "RawGoCourse",
		"overview":     "",
		"chapters": []map[string]any{
			{
				"title": "正片",
				"lessons": []map[string]any{
					{"media_file_id": mf1, "title": "01_intro_1080p"},
					{"media_file_id": mf2, "title": "02_chan_1080p"},
				},
			},
		},
	}

	// 1. Without AI config -> 400
	resp, raw := env.doJSON(t, http.MethodPost, "/api/v1/import/ai/organize", validBody, token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("import AI organize without config = %d, want 400: %s", resp.StatusCode, raw)
	}

	var seenPrompt string
	mockAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenPrompt = string(body)
		prop := map[string]any{
			"title":    "Go 并发编程实战",
			"overview": "系统讲解 Go 并发模型与通道机制",
			"tags":     []string{"Go", "并发"},
			"chapters": []map[string]any{
				{
					"title": "第一章：入门",
					"lessons": []map[string]any{
						{"lesson_id": mf1, "title": "课程导论"},
					},
				},
				{
					"title": "第二章：核心机制",
					"lessons": []map[string]any{
						{"lesson_id": mf2, "title": "Channel 深入解析"},
					},
				},
			},
		}
		pb, _ := json.Marshal(prop)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": string(pb)}},
			},
		})
	}))
	defer mockAI.Close()

	resp, raw = env.doJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_base_url":           mockAI.URL + "/v1",
		"ai_api_key":            "sk-import",
		"ai_model":              "gpt-4o-mini",
		"ai_max_subtitle_chars": 2000,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT settings = %d: %s", resp.StatusCode, raw)
	}

	// 2. Empty lessons -> 400
	resp, _ = env.doJSON(t, http.MethodPost, "/api/v1/import/ai/organize", map[string]any{
		"source_id":    src.ID,
		"dir_rel_path": "RawGoCourse",
		"title":        "RawGoCourse",
		"chapters":     []map[string]any{},
	}, token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty chapters = %d, want 400", resp.StatusCode)
	}

	// 3. Unknown media_file_id -> 400
	resp, _ = env.doJSON(t, http.MethodPost, "/api/v1/import/ai/organize", map[string]any{
		"source_id":    src.ID,
		"dir_rel_path": "RawGoCourse",
		"title":        "RawGoCourse",
		"chapters": []map[string]any{
			{"title": "Ch", "lessons": []map[string]any{{"media_file_id": "00000000000000000000000000000099", "title": "X"}}},
		},
	}, token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("foreign media_file_id = %d, want 400", resp.StatusCode)
	}

	// 4. Valid request -> 200 with clean UTF-8 GBK subtitle excerpt and media_file_id mapped
	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/import/ai/organize", validBody, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/import/ai/organize = %d, want 200: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(seenPrompt, "欢迎来到 Go 并发编程课程") {
		t.Fatalf("expected UTF-8 decoded GBK subtitle excerpt in AI prompt, got: %s", seenPrompt)
	}

	var out struct {
		Title    string   `json:"title"`
		Overview string   `json:"overview"`
		Tags     []string `json:"tags"`
		Chapters []struct {
			Title   string `json:"title"`
			Lessons []struct {
				MediaFileID string `json:"media_file_id"`
				Title       string `json:"title"`
			} `json:"lessons"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal import AI response: %v", err)
	}
	if out.Title != "Go 并发编程实战" || len(out.Chapters) != 2 || out.Chapters[0].Lessons[0].MediaFileID != mf1 || out.Chapters[1].Lessons[0].MediaFileID != mf2 {
		t.Fatalf("unexpected import AI proposal: %+v", out)
	}

	// Ensure no courses were mutated/created in DB
	courses, err := env.catSvc.ListCourses(context.Background(), catalog.CourseFilter{})
	if err != nil {
		t.Fatalf("ListCourses: %v", err)
	}
	if len(courses) != 0 {
		t.Fatalf("expected 0 courses in DB after preview AI organize, got %d", len(courses))
	}
}

func TestStorageSourcesDirsAndFilesAssignmentAPI(t *testing.T) {
	env := setupAPITestEnv(t)
	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "assign-api-test"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	writeDummyFile(t, filepath.Join(env.mediaDir, "Imported", "01.mp4"), []byte("v1"))
	writeDummyFile(t, filepath.Join(env.mediaDir, "Pending", "02.mp4"), []byte("v2"))

	src, err := env.catSvc.CreateSource(context.Background(), "AssignAPISrc", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	if _, err := env.catSvc.CommitDirectoryImport(context.Background(), catalog.CommitImportInput{
		SourceID:   src.ID,
		DirRelPath: "Imported",
		Title:      "Imported Course",
	}); err != nil {
		t.Fatalf("CommitDirectoryImport: %v", err)
	}

	// 1. /api/v1/sources reports assigned_count=1, unassigned_count=1
	resp, raw := env.doJSON(t, http.MethodGet, "/api/v1/sources", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sources = %d: %s", resp.StatusCode, raw)
	}
	var srcRes struct {
		Sources []catalog.StorageSource `json:"sources"`
	}
	if err := json.Unmarshal(raw, &srcRes); err != nil {
		t.Fatalf("unmarshal sources: %v", err)
	}
	if len(srcRes.Sources) != 1 || srcRes.Sources[0].AssignedCount != 1 || srcRes.Sources[0].UnassignedCount != 1 {
		t.Fatalf("unexpected source assignment counts: %+v", srcRes.Sources)
	}

	// 2. /api/v1/sources/{id}/dirs reports per-dir counts and courses
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/sources/"+src.ID+"/dirs", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /dirs = %d: %s", resp.StatusCode, raw)
	}
	var dirRes struct {
		Dirs []catalog.IndexedDir `json:"dirs"`
	}
	if err := json.Unmarshal(raw, &dirRes); err != nil {
		t.Fatalf("unmarshal dirs: %v", err)
	}
	if len(dirRes.Dirs) != 2 || dirRes.Dirs[0].AssignedCount != 1 || len(dirRes.Dirs[0].Courses) != 1 || dirRes.Dirs[1].UnassignedCount != 1 {
		t.Fatalf("unexpected dir assignment info: %+v", dirRes.Dirs)
	}

	// 3. /api/v1/sources/{id}/files?assigned=false filters to unassigned files only
	resp, raw = env.doJSON(t, http.MethodGet, "/api/v1/sources/"+src.ID+"/files?assigned=false", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /files?assigned=false = %d: %s", resp.StatusCode, raw)
	}
	var fileRes struct {
		Files []catalog.MediaFile `json:"files"`
	}
	if err := json.Unmarshal(raw, &fileRes); err != nil {
		t.Fatalf("unmarshal files: %v", err)
	}
	if len(fileRes.Files) != 1 || fileRes.Files[0].FileName != "02.mp4" || fileRes.Files[0].Assigned {
		t.Fatalf("unexpected unassigned files: %+v", fileRes.Files)
	}
}

func TestImportAIOrganizeLargeUTF8SubtitleBoundaryNoMojibake(t *testing.T) {
	env := setupAPITestEnv(t)
	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "utf8-boundary"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	// Craft a BOM-less UTF-8 Chinese SRT > 8192 bytes where byte 8192 slices mid-rune.
	header := "1\n00:00:01,000 --> 00:00:05,000\n微服务架构实战课程导学\n\n2\n00:00:06,000 --> 00:00:10,000\n"
	// Pad ASCII bytes so that the repeated 3-byte Chinese rune '中' straddles offset 8192.
	padLen := (8192 - len(header) - 1) % 3
	content := header + strings.Repeat("a", padLen) + strings.Repeat("中", 3000) + "\n"
	// Confirm byte 8192 really falls mid-rune!
	rawBytes := []byte(content)
	if len(rawBytes) <= 8192 {
		t.Fatalf("expected > 8192 bytes, got %d", len(rawBytes))
	}

	writeDummyFile(t, filepath.Join(env.mediaDir, "UTF8Course", "01.mp4"), []byte("v1"))
	writeDummyFile(t, filepath.Join(env.mediaDir, "UTF8Course", "01.zh.srt"), rawBytes)

	src, err := env.catSvc.CreateSource(context.Background(), "UTF8Src", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	files, err := env.catSvc.ListMediaFiles(context.Background(), src.ID, "", "", "")
	if err != nil || len(files) != 1 {
		t.Fatalf("ListMediaFiles: %v (len=%d)", err, len(files))
	}
	mfID := files[0].ID

	var capturedPrompt string
	fakeLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedPrompt = string(body)
		respObj := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": fmt.Sprintf(`{"title":"微服务课程","overview":"简介","tags":["Go"],"chapters":[{"title":"第一章","lessons":[{"lesson_id":%q,"title":"导学"}]}]}`, mfID),
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respObj)
	}))
	defer fakeLLM.Close()

	resp, raw := env.doJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_base_url":           fakeLLM.URL + "/v1",
		"ai_api_key":            "test-key",
		"ai_model":              "gpt-test",
		"ai_max_subtitle_chars": 2000,
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT settings = %d: %s", resp.StatusCode, raw)
	}

	resp, raw = env.doJSON(t, http.MethodPost, "/api/v1/import/ai/organize", map[string]any{
		"source_id":    src.ID,
		"dir_rel_path": "UTF8Course",
		"title":        "UTF8Course",
		"chapters": []map[string]any{
			{
				"title": "默认",
				"lessons": []map[string]any{
					{"media_file_id": mfID, "title": "01"},
				},
			},
		},
	}, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /import/ai/organize = %d: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(capturedPrompt, "微服务架构实战课程导学") {
		t.Fatalf("expected clean UTF-8 Chinese subtitle excerpt in AI prompt, got mojibake/missing: %s", capturedPrompt)
	}
}

func TestLessonPlayback10BitAnd444H264RoutesToTranscode(t *testing.T) {
	env := setupAPITestEnv(t)
	if !env.mediaSvc.HasFFmpeg() {
		t.Skip("ffmpeg not available")
	}
	if _, err := env.authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := env.authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "pixfmt-test"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	token := sess.Token

	src, err := env.catSvc.CreateSource(context.Background(), "PixFmtSrc", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	course, err := env.catSvc.CreateCourse(context.Background(), catalog.CreateCourseInput{Title: "Screen Recordings"})
	if err != nil {
		t.Fatalf("CreateCourse: %v", err)
	}
	chapID := course.Chapters[0].ID

	for _, tc := range []struct {
		name        string
		fileName    string
		container   string
		streamsJSON string
	}{
		{
			name:        "h264_10bit_mkv",
			fileName:    "screen_10bit.mkv",
			container:   "mkv",
			streamsJSON: `[{"index":0,"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p10le"}]`,
		},
		{
			name:        "h264_444_mp4",
			fileName:    "obs_444.mp4",
			container:   "mp4",
			streamsJSON: `[{"index":0,"codec_type":"video","codec_name":"h264","pix_fmt":"yuv444p"}]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeDummyFile(t, filepath.Join(env.mediaDir, tc.fileName), []byte("dummy"))
			mfID := id.New()
			now := time.Now().UTC().Format(time.RFC3339)
			if _, err := env.db.Exec(
				`INSERT INTO media_files (id, source_id, relative_path, file_name, file_size, mod_time, status, duration_ms, container, video_codec, audio_codec, width, height, bitrate, streams_json, created_at, updated_at)
				 VALUES (?, ?, ?, ?, 1024, ?, 'active', 60000, ?, 'h264', 'aac', 1920, 1080, 2000000, ?, ?, ?)`,
				mfID, src.ID, tc.fileName, tc.fileName, now, tc.container, tc.streamsJSON, now, now,
			); err != nil {
				t.Fatalf("insert media_file: %v", err)
			}
			lesson, err := env.catSvc.AddLesson(context.Background(), chapID, mfID, tc.name)
			if err != nil {
				t.Fatalf("AddLesson: %v", err)
			}

			resp, raw := env.doJSON(t, http.MethodGet, "/api/v1/lessons/"+lesson.ID+"/playback?codecs=h264,hevc,vp9,av1", nil, token)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /playback = %d: %s", resp.StatusCode, raw)
			}
			var pb struct {
				Mode        string `json:"mode"`
				Transcoding bool   `json:"transcoding"`
				StreamURL   string `json:"stream_url"`
			}
			if err := json.Unmarshal(raw, &pb); err != nil {
				t.Fatalf("unmarshal playback: %v", err)
			}
			if pb.Mode != "hls" || !pb.Transcoding || !strings.Contains(pb.StreamURL, "TranscodeVideo=1") {
				t.Fatalf("%s expected Tier 3 transcode (hls, transcoding=true, TranscodeVideo=1), got %+v", tc.name, pb)
			}
		})
	}
}

type apiRoundTripFunc func(*http.Request) (*http.Response, error)

func (f apiRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type apiRecorderEnv struct {
	db       *sql.DB
	dataDir  string
	mediaDir string
	registry *storage.Registry
	authSvc  *auth.Service
	scanSvc  *scanner.Service
	catSvc   *catalog.Service
	mediaSvc *media.Service
	handler  http.Handler
}

func setupRecorderAPITestEnv(t *testing.T, rt http.RoundTripper) *apiRecorderEnv {
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

	lctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var aiClient *http.Client
	if rt != nil {
		aiClient = &http.Client{Transport: rt}
	}

	h := api.NewHandler(api.Deps{
		DB:           conn,
		DataDir:      dataDir,
		BaseURLHint:  "http://localhost:8096",
		Registry:     reg,
		Auth:         authSvc,
		Scanner:      scanSvc,
		Catalog:      catSvc,
		Media:        mediaSvc,
		AIHTTPClient: aiClient,
		LifecycleCtx: lctx,
	})

	return &apiRecorderEnv{
		db:       conn,
		dataDir:  dataDir,
		mediaDir: mediaDir,
		registry: reg,
		authSvc:  authSvc,
		scanSvc:  scanSvc,
		catSvc:   catSvc,
		mediaSvc: mediaSvc,
		handler:  h,
	}
}

func (e *apiRecorderEnv) doRecJSON(t *testing.T, method, reqPath string, body any, token string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, reqPath, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec, rec.Body.Bytes()
}

func (e *apiRecorderEnv) loginAdmin(t *testing.T) string {
	t.Helper()
	if _, err := e.authSvc.SetupAdmin(context.Background(), "admin", "secret123"); err != nil {
		t.Fatalf("SetupAdmin: %v", err)
	}
	sess, _, err := e.authSvc.Login(context.Background(), "admin", "secret123", auth.DeviceInfo{DeviceID: "api-rec-test"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return sess.Token
}

func TestSettingsASRFieldsMaskingAndClear(t *testing.T) {
	env := setupRecorderAPITestEnv(t, nil)
	token := env.loginAdmin(t)

	// 1. Initial GET reports empty ASR fields and configured=false
	rec, raw := env.doRecJSON(t, http.MethodGet, "/api/v1/settings", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial GET /settings = %d: %s", rec.Code, raw)
	}
	var initial map[string]any
	if err := json.Unmarshal(raw, &initial); err != nil {
		t.Fatalf("unmarshal initial settings: %v", err)
	}
	for _, boolKey := range []string{"ai_asr_api_key_set", "ai_configured", "ai_asr_configured"} {
		v, exists := initial[boolKey]
		if !exists || v != false {
			t.Fatalf("expected %s=false in initial GET /settings, got %v (%s)", boolKey, v, raw)
		}
	}
	for _, strKey := range []string{"ai_asr_base_url", "ai_asr_api_key", "ai_asr_model", "ai_asr_language"} {
		v, exists := initial[strKey]
		if !exists || v != "" {
			t.Fatalf("expected %s=\"\" in initial GET /settings, got %v (%s)", strKey, v, raw)
		}
	}
	if initial["ai_asr_segment_seconds"] != float64(300) || initial["ai_asr_timeout_seconds"] != float64(900) {
		t.Fatalf("expected default ai_asr_segment_seconds=300 and ai_asr_timeout_seconds=900, got seg=%v timeout=%v", initial["ai_asr_segment_seconds"], initial["ai_asr_timeout_seconds"])
	}

	// 2. PUT saves ASR fields and masks API key
	rec, raw = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_asr_base_url":        "https://whisper.local/v1",
		"ai_asr_api_key":         "sk-asr-secret",
		"ai_asr_model":           "whisper-1",
		"ai_asr_language":        "zh",
		"ai_asr_segment_seconds": 180,
		"ai_asr_timeout_seconds": 1800,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings ASR = %d: %s", rec.Code, raw)
	}

	rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/settings", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings after ASR save = %d: %s", rec.Code, raw)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("unmarshal saved settings: %v", err)
	}
	if saved["ai_asr_base_url"] != "https://whisper.local/v1" ||
		saved["ai_asr_api_key"] != "****" ||
		saved["ai_asr_api_key_set"] != true ||
		saved["ai_asr_model"] != "whisper-1" ||
		saved["ai_asr_language"] != "zh" ||
		saved["ai_asr_segment_seconds"] != float64(180) ||
		saved["ai_asr_timeout_seconds"] != float64(1800) ||
		saved["ai_configured"] != false ||
		saved["ai_asr_configured"] != true {
		t.Fatalf("unexpected saved ASR settings: %s", raw)
	}

	// 2b. PUT rejects out-of-range ASR segment / timeout bounds
	rec, _ = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_asr_segment_seconds": 10,
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range ai_asr_segment_seconds, got %d", rec.Code)
	}
	rec, _ = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_asr_timeout_seconds": 5000,
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range ai_asr_timeout_seconds, got %d", rec.Code)
	}

	// 3. Updating with masked placeholder preserves stored key
	rec, raw = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_asr_api_key":  "****",
		"ai_asr_language": "en",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings with masked key = %d: %s", rec.Code, raw)
	}
	var storedKey string
	if err := env.db.QueryRow(`SELECT value FROM app_settings WHERE key = 'ai_asr_api_key'`).Scan(&storedKey); err != nil {
		t.Fatalf("query stored ai_asr_api_key: %v", err)
	}
	if storedKey != "sk-asr-secret" {
		t.Fatalf("expected preserved ai_asr_api_key='sk-asr-secret', got %q", storedKey)
	}
	var afterMask map[string]any
	_ = json.Unmarshal(raw, &afterMask)
	if afterMask["ai_asr_api_key"] != "****" || afterMask["ai_asr_api_key_set"] != true || afterMask["ai_asr_language"] != "en" {
		t.Fatalf("unexpected settings after masked update: %s", raw)
	}

	// 4. clear_ai_asr_api_key clears stored ASR key
	rec, raw = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"clear_ai_asr_api_key": true,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings clear ASR key = %d: %s", rec.Code, raw)
	}
	var afterClear map[string]any
	_ = json.Unmarshal(raw, &afterClear)
	if afterClear["ai_asr_api_key"] != "" || afterClear["ai_asr_api_key_set"] != false || afterClear["ai_asr_configured"] != true {
		t.Fatalf("unexpected settings after clear_ai_asr_api_key: %s", raw)
	}
	if err := env.db.QueryRow(`SELECT value FROM app_settings WHERE key = 'ai_asr_api_key'`).Scan(&storedKey); err != nil {
		t.Fatalf("query cleared ai_asr_api_key: %v", err)
	}
	if storedKey != "" {
		t.Fatalf("expected empty stored ai_asr_api_key after clear, got %q", storedKey)
	}

	// 5. Main AI configured + fallback ASR configured
	rec, raw = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_base_url":     "https://main.example/v1",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings main AI = %d: %s", rec.Code, raw)
	}
	var afterMain map[string]any
	_ = json.Unmarshal(raw, &afterMain)
	if afterMain["ai_configured"] != true || afterMain["ai_asr_configured"] != true || afterMain["ai_asr_base_url"] != "" {
		t.Fatalf("expected ai_configured=true, ai_asr_configured=true, ai_asr_base_url='', got %s", raw)
	}
}

func TestASRConfigDoesNotLeakMainKeyToDifferentHost(t *testing.T) {
	var (
		mu             sync.Mutex
		capturedURL    string
		capturedAuth   string
		upstreamStatus = http.StatusOK
		upstreamBody   = `{"text":"ok"}`
	)
	rt := apiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		capturedURL = r.URL.String()
		capturedAuth = r.Header.Get("Authorization")
		status := upstreamStatus
		body := upstreamBody
		mu.Unlock()
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	env := setupRecorderAPITestEnv(t, rt)
	token := env.loginAdmin(t)

	// Save main AI settings using an independent host and main secret key
	rec, raw := env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_base_url": "https://asr-and-main.example/v1",
		"ai_api_key":  "sk-main-secret",
		"ai_model":    "gpt-4o-mini",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings main = %d: %s", rec.Code, raw)
	}

	// 1. Independent asr_base_url + empty asr_api_key -> MUST NOT forward sk-main-secret
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/settings/ai/asr/test", map[string]any{
		"asr_base_url": "https://other-whisper.example/v1",
		"asr_api_key":  "",
		"asr_model":    "whisper-1",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings/ai/asr/test (independent host) = %d: %s", rec.Code, raw)
	}
	mu.Lock()
	gotURL, gotAuth := capturedURL, capturedAuth
	mu.Unlock()
	if gotURL != "https://other-whisper.example/v1/audio/transcriptions" {
		t.Fatalf("unexpected ASR test URL %q", gotURL)
	}
	if gotAuth != "" {
		t.Fatalf("main ai_api_key leaked to independent ASR host! Authorization=%q", gotAuth)
	}

	// 2. Empty asr_base_url (reusing main ai_base_url) + empty asr_api_key -> forwards Bearer sk-main-secret
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/settings/ai/asr/test", map[string]any{
		"asr_base_url": "",
		"asr_api_key":  "",
		"asr_model":    "whisper-1",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings/ai/asr/test (reuse main) = %d: %s", rec.Code, raw)
	}
	mu.Lock()
	gotURL, gotAuth = capturedURL, capturedAuth
	mu.Unlock()
	if gotURL != "https://asr-and-main.example/v1/audio/transcriptions" {
		t.Fatalf("unexpected fallback ASR test URL %q", gotURL)
	}
	if gotAuth != "Bearer sk-main-secret" {
		t.Fatalf("expected main Authorization 'Bearer sk-main-secret', got %q", gotAuth)
	}

	// 3. Unsaved base_url + api_key overrides & ignoring "****" & clear_ai_asr_api_key
	rec, raw = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_asr_base_url": "https://saved-asr.example/v1",
		"ai_asr_api_key":  "sk-asr-saved",
		"ai_asr_model":    "whisper-1",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings saved ASR = %d: %s", rec.Code, raw)
	}

	// Masked placeholder preserves saved ASR key
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/settings/ai/asr/test", map[string]any{
		"ai_asr_api_key": "****",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings/ai/asr/test with masked key = %d: %s", rec.Code, raw)
	}
	mu.Lock()
	gotAuth = capturedAuth
	mu.Unlock()
	if gotAuth != "Bearer sk-asr-saved" {
		t.Fatalf("expected preserved ASR key 'Bearer sk-asr-saved', got %q", gotAuth)
	}

	// Clearing saved ASR base URL & key in unsaved test falls back to main URL + unsaved main api_key
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/settings/ai/asr/test", map[string]any{
		"asr_base_url":         "",
		"clear_ai_asr_api_key": true,
		"base_url":             "https://unsaved-main.example/v1",
		"api_key":              "sk-unsaved-main",
		"asr_model":            "whisper-large-v3",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings/ai/asr/test with unsaved overrides = %d: %s", rec.Code, raw)
	}
	mu.Lock()
	gotURL, gotAuth = capturedURL, capturedAuth
	mu.Unlock()
	if gotURL != "https://unsaved-main.example/v1/audio/transcriptions" || gotAuth != "Bearer sk-unsaved-main" {
		t.Fatalf("unexpected unsaved override resolution: url=%q auth=%q", gotURL, gotAuth)
	}

	// Upstream error maps to 502 Bad Gateway
	mu.Lock()
	upstreamStatus = http.StatusUnauthorized
	upstreamBody = `{"error":{"message":"invalid asr key"}}`
	mu.Unlock()

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/settings/ai/asr/test", nil, token)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST /settings/ai/asr/test on upstream 401 = %d, want 502: %s", rec.Code, raw)
	}

	// Missing ASR model maps to 400 with code=asr_not_configured
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/settings/ai/asr/test", map[string]any{
		"asr_model": "",
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /settings/ai/asr/test with empty asr_model = %d, want 400: %s", rec.Code, raw)
	}
	var errBody struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(raw, &errBody); err != nil || errBody.Code != "asr_not_configured" {
		t.Fatalf("expected code=asr_not_configured on ASR test, got %s", raw)
	}
}

func TestLessonSummaryAPIEndToEndAndErrorCodes(t *testing.T) {
	var (
		mu          sync.Mutex
		gateCh      chan struct{}
		gateEntered chan struct{}
		gateOnce    sync.Once
	)

	rt := apiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		ch := gateCh
		entered := gateEntered
		mu.Unlock()
		if ch != nil {
			gateOnce.Do(func() {
				if entered != nil {
					close(entered)
				}
			})
			select {
			case <-ch:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}

		payload := map[string]any{
			"overview": "本节课讲解了核心架构与实战命令。",
			"outline": []map[string]any{
				{"title": "模块一：基础概念", "summary": "介绍背景与目标", "start_sec": 0},
				{"title": "模块二：代码实践", "summary": "演示核心实现", "start_sec": 30},
			},
			"timeline": []map[string]any{
				{
					"start_sec":  0,
					"end_sec":    30,
					"title":      "课程导入",
					"summary":    "梳理整体目录与关键术语",
					"key_points": []string{"明确学习目标", "配置基础环境"},
				},
				{
					"start_sec":  30,
					"end_sec":    90,
					"title":      "核心实践",
					"summary":    "编写代码并验证运行结果",
					"key_points": []string{"运行 go test 验证"},
				},
			},
		}
		b, _ := json.Marshal(payload)
		respObj := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": string(b)}},
			},
		}
		out, _ := json.Marshal(respObj)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(out)),
		}, nil
	})

	env := setupRecorderAPITestEnv(t, rt)
	token := env.loginAdmin(t)

	// Seed source, course, and 2 lessons (lesson 1 has no subtitle initially; lesson 2 has no subtitle initially)
	courseDir := filepath.Join(env.mediaDir, "SummaryCourse")
	writeDummyFile(t, filepath.Join(courseDir, "01_intro.mp4"), []byte("video1"))
	writeDummyFile(t, filepath.Join(courseDir, "02_deep.mp4"), []byte("video2"))

	src, err := env.catSvc.CreateSource(context.Background(), "SummaryDisk", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	course, err := env.catSvc.CommitDirectoryImport(context.Background(), catalog.CommitImportInput{
		SourceID:   src.ID,
		DirRelPath: "SummaryCourse",
	})
	if err != nil {
		t.Fatalf("CommitDirectoryImport: %v", err)
	}
	l1ID := course.Chapters[0].Lessons[0].ID
	l2ID := course.Chapters[0].Lessons[1].ID

	// 1. Unauthenticated requests return 401
	unauthEndpoints := [][2]string{
		{http.MethodPost, "/api/v1/settings/ai/asr/test"},
		{http.MethodGet, "/api/v1/lessons/" + l1ID + "/summary"},
		{http.MethodPost, "/api/v1/lessons/" + l1ID + "/summary/generate"},
		{http.MethodPost, "/api/v1/lessons/" + l1ID + "/summary/cancel"},
		{http.MethodDelete, "/api/v1/lessons/" + l1ID + "/summary"},
		{http.MethodPost, "/api/v1/lessons/" + l1ID + "/transcript/generate"},
		{http.MethodPost, "/api/v1/lessons/" + l1ID + "/transcript/cancel"},
		{http.MethodGet, "/api/v1/courses/" + course.ID + "/summaries/status"},
		{http.MethodPost, "/api/v1/courses/" + course.ID + "/summaries/generate"},
		{http.MethodPost, "/api/v1/courses/" + course.ID + "/summaries/cancel"},
		{http.MethodPost, "/api/v1/courses/" + course.ID + "/transcripts/generate"},
		{http.MethodPost, "/api/v1/courses/" + course.ID + "/transcripts/cancel"},
	}
	for _, ep := range unauthEndpoints {
		rec, _ := env.doRecJSON(t, ep[0], ep[1], nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s %s = %d, want 401", ep[0], ep[1], rec.Code)
		}
	}

	// 2. Unknown lesson/course returns 404
	notFoundEndpoints := [][2]string{
		{http.MethodGet, "/api/v1/lessons/nonexistent-lesson-id/summary"},
		{http.MethodPost, "/api/v1/lessons/nonexistent-lesson-id/summary/generate"},
		{http.MethodPost, "/api/v1/lessons/nonexistent-lesson-id/summary/cancel"},
		{http.MethodDelete, "/api/v1/lessons/nonexistent-lesson-id/summary"},
		{http.MethodPost, "/api/v1/lessons/nonexistent-lesson-id/transcript/generate"},
		{http.MethodPost, "/api/v1/lessons/nonexistent-lesson-id/transcript/cancel"},
		{http.MethodGet, "/api/v1/courses/nonexistent-course-id/summaries/status"},
		{http.MethodPost, "/api/v1/courses/nonexistent-course-id/summaries/generate"},
		{http.MethodPost, "/api/v1/courses/nonexistent-course-id/summaries/cancel"},
		{http.MethodPost, "/api/v1/courses/nonexistent-course-id/transcripts/generate"},
		{http.MethodPost, "/api/v1/courses/nonexistent-course-id/transcripts/cancel"},
	}
	for _, ep := range notFoundEndpoints {
		rec, raw := env.doRecJSON(t, ep[0], ep[1], nil, token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unknown ID %s %s = %d, want 404: %s", ep[0], ep[1], rec.Code, raw)
		}
	}

	// 3. AI unconfigured -> 400 with code == "ai_not_configured"
	rec, raw := env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+l1ID+"/summary/generate", nil, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /lessons/{id}/summary/generate (no AI) = %d, want 400: %s", rec.Code, raw)
	}
	var errPayload struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(raw, &errPayload); err != nil || errPayload.Code != "ai_not_configured" || errPayload.Error == "" {
		t.Fatalf("expected code=ai_not_configured, got %s", raw)
	}

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/summaries/generate", nil, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /courses/{id}/summaries/generate (no AI) = %d, want 400: %s", rec.Code, raw)
	}
	errPayload = struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}{}
	if err := json.Unmarshal(raw, &errPayload); err != nil || errPayload.Code != "ai_not_configured" {
		t.Fatalf("expected code=ai_not_configured on batch generate, got %s", raw)
	}

	// 4. Configure AI (no ASR) -> lesson without subtitle returns 400 with code == "asr_not_configured"
	rec, raw = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_base_url": "https://llm.example/v1",
		"ai_api_key":  "sk-llm",
		"ai_model":    "gpt-4o-mini",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings = %d: %s", rec.Code, raw)
	}

	// Verify capabilities via GET /lessons/{id}/summary (also test dashless ID normalization!)
	dashlessL1 := strings.ReplaceAll(l1ID, "-", "")
	rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/lessons/"+dashlessL1+"/summary", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /lessons/{dashless}/summary = %d: %s", rec.Code, raw)
	}
	var state summary.LessonSummaryState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal LessonSummaryState: %v (%s)", err, raw)
	}
	if state.LessonID != l1ID || state.Summary != nil || !state.Capabilities.AIConfigured || state.Capabilities.ASRConfigured || state.Capabilities.HasSubtitle || state.Capabilities.HasCachedTranscript {
		t.Fatalf("unexpected initial lesson summary state: %+v", state)
	}

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+l1ID+"/summary/generate", nil, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /lessons/{id}/summary/generate (no sub, no ASR) = %d, want 400: %s", rec.Code, raw)
	}
	errPayload = struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}{}
	if err := json.Unmarshal(raw, &errPayload); err != nil || errPayload.Code != "asr_not_configured" {
		t.Fatalf("expected code=asr_not_configured, got %s", raw)
	}

	// 5. Post manual transcript -> 202, poll until completed, verify summary & cached transcript
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+l1ID+"/summary/generate", map[string]any{
		"manual_transcript": "Welcome to Go interfaces. First we define an interface, then implement it.",
	}, token)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /lessons/{id}/summary/generate (manual) = %d, want 202: %s", rec.Code, raw)
	}
	var job summary.LessonJobStatus
	if err := json.Unmarshal(raw, &job); err != nil || job.LessonID != l1ID {
		t.Fatalf("unexpected 202 LessonJobStatus: %s", raw)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/lessons/"+l1ID+"/summary", nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("poll GET /summary = %d: %s", rec.Code, raw)
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatalf("unmarshal polled state: %v", err)
		}
		if state.Job != nil && !state.Job.Running {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if state.Job == nil || state.Job.Running || state.Job.Stage != summary.StageCompleted {
		t.Fatalf("expected completed summary job, got %+v", state.Job)
	}
	if state.Summary == nil || state.Summary.SourceType != "manual" || state.Summary.Overview == "" || len(state.Summary.Outline) == 0 || len(state.Summary.Timeline) == 0 {
		t.Fatalf("expected persisted manual summary, got %+v", state.Summary)
	}
	if !state.Capabilities.HasCachedTranscript || state.Capabilities.TranscriptSource != "manual" {
		t.Fatalf("expected cached manual transcript capability, got %+v", state.Capabilities)
	}

	// 6. DELETE without delete_transcript removes summary but retains cached transcript
	rec, raw = env.doRecJSON(t, http.MethodDelete, "/api/v1/lessons/"+l1ID+"/summary", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /summary = %d: %s", rec.Code, raw)
	}
	rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/lessons/"+l1ID+"/summary", nil, token)
	_ = json.Unmarshal(raw, &state)
	if state.Summary != nil || !state.Capabilities.HasCachedTranscript {
		t.Fatalf("expected summary=nil and cached transcript retained, got summary=%v caps=%+v", state.Summary, state.Capabilities)
	}

	// DELETE with ?delete_transcript=true clears cached transcript as well
	rec, raw = env.doRecJSON(t, http.MethodDelete, "/api/v1/lessons/"+l1ID+"/summary?delete_transcript=true", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /summary?delete_transcript=true = %d: %s", rec.Code, raw)
	}
	rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/lessons/"+l1ID+"/summary", nil, token)
	_ = json.Unmarshal(raw, &state)
	if state.Summary != nil || state.Capabilities.HasCachedTranscript {
		t.Fatalf("expected summary=nil and cached transcript deleted, got summary=%v caps=%+v", state.Summary, state.Capabilities)
	}

	// 7. Attach SRT subtitles to both lessons and test course batch lifecycle (202, 409 conflict, cancel, complete, 200 when 0 pending)
	writeDummyFile(t, filepath.Join(courseDir, "01_intro.srt"), []byte("1\n00:00:00,000 --> 00:00:05,000\nLesson 1 subtitle\n"))
	writeDummyFile(t, filepath.Join(courseDir, "02_deep.srt"), []byte("1\n00:00:00,000 --> 00:00:05,000\nLesson 2 subtitle\n"))
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("rescan subtitles: %v", err)
	}

	// Gate LLM call to deterministically test 409 conflict & cancel while running
	mu.Lock()
	gateCh = make(chan struct{})
	gateEntered = make(chan struct{})
	gateOnce = sync.Once{}
	mu.Unlock()

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/summaries/generate", map[string]any{"mode": "missing_only"}, token)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /courses/{id}/summaries/generate = %d, want 202: %s", rec.Code, raw)
	}
	var batch summary.CourseBatchStatus
	if err := json.Unmarshal(raw, &batch); err != nil || !batch.Running || batch.Total != 2 {
		t.Fatalf("unexpected 202 CourseBatchStatus: %s", raw)
	}

	<-gateEntered

	// Second batch generate while running -> 409 Conflict
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/summaries/generate", nil, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("concurrent POST /courses/{id}/summaries/generate = %d, want 409: %s", rec.Code, raw)
	}

	// Status endpoint shows running batch
	rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/courses/"+course.ID+"/summaries/status", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /courses/{id}/summaries/status = %d: %s", rec.Code, raw)
	}
	var courseStatus summary.CourseSummariesStatus
	if err := json.Unmarshal(raw, &courseStatus); err != nil || courseStatus.BatchJob == nil || !courseStatus.BatchJob.Running {
		t.Fatalf("expected running batch_job in status, got %s", raw)
	}

	// Cancel running batch -> 200
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/summaries/cancel", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /courses/{id}/summaries/cancel = %d: %s", rec.Code, raw)
	}

	// Wait for cancelled batch to settle
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/courses/"+course.ID+"/summaries/status", nil, token)
		_ = json.Unmarshal(raw, &courseStatus)
		if courseStatus.BatchJob != nil && !courseStatus.BatchJob.Running {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if courseStatus.BatchJob == nil || courseStatus.BatchJob.Running || courseStatus.BatchJob.CurrentStage != summary.StageCancelled {
		t.Fatalf("expected cancelled batch_job, got %+v", courseStatus.BatchJob)
	}

	// Remove gate and run full batch to completion
	mu.Lock()
	gateCh = nil
	gateEntered = nil
	mu.Unlock()

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/summaries/generate", map[string]any{"mode": "all"}, token)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /courses/{id}/summaries/generate (all) = %d, want 202: %s", rec.Code, raw)
	}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/courses/"+course.ID+"/summaries/status", nil, token)
		_ = json.Unmarshal(raw, &courseStatus)
		if courseStatus.BatchJob != nil && !courseStatus.BatchJob.Running {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if courseStatus.BatchJob == nil || courseStatus.BatchJob.Running || courseStatus.BatchJob.CurrentStage != summary.StageCompleted || courseStatus.BatchJob.Completed != 2 {
		t.Fatalf("expected completed batch of 2, got %+v", courseStatus.BatchJob)
	}
	if len(courseStatus.SummaryLessonIDs) != 2 || courseStatus.SummaryLessonIDs[0] != l1ID || courseStatus.SummaryLessonIDs[1] != l2ID {
		t.Fatalf("expected summary_lesson_ids=[%s, %s], got %v", l1ID, l2ID, courseStatus.SummaryLessonIDs)
	}

	// When 0 pending lessons in missing_only mode -> returns 200 OK (not 202)
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/summaries/generate", map[string]any{"mode": "missing_only"}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /courses/{id}/summaries/generate (0 pending) = %d, want 200: %s", rec.Code, raw)
	}
	if err := json.Unmarshal(raw, &batch); err != nil || batch.Running || batch.Skipped != 2 || batch.CurrentStage != summary.StageCompleted {
		t.Fatalf("expected non-running completed batch with skipped=2, got %+v (%s)", batch, raw)
	}

	// Invalid batch mode returns 400
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/summaries/generate", map[string]any{"mode": "invalid"}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /courses/{id}/summaries/generate (invalid mode) = %d, want 400: %s", rec.Code, raw)
	}

	// Single-lesson cancel while running settles as cancelled and preserves existing summary
	mu.Lock()
	gateCh = make(chan struct{})
	gateEntered = make(chan struct{})
	gateOnce = sync.Once{}
	mu.Unlock()

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+l1ID+"/summary/generate", map[string]any{"force": true}, token)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /lessons/{id}/summary/generate (for cancel) = %d, want 202: %s", rec.Code, raw)
	}
	<-gateEntered

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+l1ID+"/summary/cancel", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /lessons/{id}/summary/cancel = %d: %s", rec.Code, raw)
	}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/lessons/"+l1ID+"/summary", nil, token)
		_ = json.Unmarshal(raw, &state)
		if state.Job != nil && !state.Job.Running {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if state.Job == nil || state.Job.Running || state.Job.Stage != summary.StageCancelled {
		t.Fatalf("expected cancelled single lesson job, got %+v", state.Job)
	}
	if state.Summary == nil || state.Summary.Overview == "" {
		t.Fatalf("expected existing saved summary preserved after cancel, got %+v", state.Summary)
	}
}

type apiFakeAudioExtractor struct {
	dataDir string
}

func (f *apiFakeAudioExtractor) ExtractChunks(ctx context.Context, mediaPath string, jobID string) ([]summary.AudioChunk, func(), error) {
	workDir := filepath.Join(f.dataDir, "cache", "asr", jobID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(workDir) }
	chunkPath := filepath.Join(workDir, "chunk_000.mp3")
	if err := os.WriteFile(chunkPath, []byte("fake-mp3"), 0o644); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return []summary.AudioChunk{{Path: chunkPath, OffsetSec: 0, DurationSec: 60}}, cleanup, nil
}

func TestDecoupledTranscriptionAndChapterBatchAPIs(t *testing.T) {
	var (
		mu         sync.Mutex
		asrGateCh  chan struct{}
		asrEntered chan struct{}
		asrOnce    sync.Once
	)

	rt := apiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			mu.Lock()
			ch := asrGateCh
			entered := asrEntered
			mu.Unlock()
			if ch != nil {
				asrOnce.Do(func() {
					if entered != nil {
						close(entered)
					}
				})
				select {
				case <-ch:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			b, _ := json.Marshal(map[string]any{
				"language": "zh",
				"segments": []map[string]any{{"start": 0.0, "end": 5.0, "text": "API 转写片段"}},
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(b)),
			}, nil
		}

		payload := map[string]any{
			"overview": "章节总结概览",
			"outline":  []map[string]any{{"title": "大纲", "summary": "描述", "start_sec": 0}},
			"timeline": []map[string]any{{"start_sec": 0, "end_sec": 5, "title": "分段", "summary": "内容", "key_points": []string{"点"}}},
		}
		b, _ := json.Marshal(payload)
		respBytes, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": string(b)}}},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(respBytes)),
		}, nil
	})

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

	lctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	aiClient := &http.Client{Transport: rt}
	sumSvc := summary.NewService(summary.Options{
		DB:             conn,
		DataDir:        dataDir,
		Catalog:        catSvc,
		Media:          mediaSvc,
		LifecycleCtx:   lctx,
		HTTPClient:     aiClient,
		AudioExtractor: &apiFakeAudioExtractor{dataDir: dataDir},
	})
	t.Cleanup(sumSvc.Wait)

	env := &apiRecorderEnv{
		db:       conn,
		dataDir:  dataDir,
		mediaDir: mediaDir,
		registry: reg,
		authSvc:  authSvc,
		scanSvc:  scanSvc,
		catSvc:   catSvc,
		mediaSvc: mediaSvc,
		handler: api.NewHandler(api.Deps{
			DB:           conn,
			DataDir:      dataDir,
			BaseURLHint:  "http://localhost:8096",
			Registry:     reg,
			Auth:         authSvc,
			Scanner:      scanSvc,
			Catalog:      catSvc,
			Media:        mediaSvc,
			Summary:      sumSvc,
			AIHTTPClient: aiClient,
			LifecycleCtx: lctx,
		}),
	}
	token := env.loginAdmin(t)

	// Build 2-chapter course: Ch1 (1 subtitled lesson), Ch2 (2 audio-only lessons)
	courseDir := filepath.Join(env.mediaDir, "TwoStepCourse")
	writeDummyFile(t, filepath.Join(courseDir, "ch1", "01.mp4"), []byte("v1"))
	writeDummyFile(t, filepath.Join(courseDir, "ch1", "01.srt"), []byte("1\n00:00:00,000 --> 00:00:04,000\nCh1 Sub\n"))
	writeDummyFile(t, filepath.Join(courseDir, "ch2", "02.mp4"), []byte("v2"))
	writeDummyFile(t, filepath.Join(courseDir, "ch2", "03.mp4"), []byte("v3"))

	src, err := env.catSvc.CreateSource(context.Background(), "TwoStepDisk", "local", env.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if _, err := env.scanSvc.ScanSource(context.Background(), src.ID); err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	course, err := env.catSvc.CommitDirectoryImport(context.Background(), catalog.CommitImportInput{
		SourceID:   src.ID,
		DirRelPath: "TwoStepCourse",
	})
	if err != nil {
		t.Fatalf("CommitDirectoryImport: %v", err)
	}
	ch1ID := course.Chapters[0].ID
	ch2ID := course.Chapters[1].ID
	ch2L1 := course.Chapters[1].Lessons[0].ID
	ch2L2 := course.Chapters[1].Lessons[1].ID

	// 1. Before ASR configured: POST /lessons/{id}/transcript/generate -> 400 asr_not_configured
	rec, raw := env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+ch2L1+"/transcript/generate", nil, token)
	if rec.Code != http.StatusBadRequest || !strings.Contains(string(raw), "asr_not_configured") {
		t.Fatalf("expected 400 asr_not_configured, got %d: %s", rec.Code, raw)
	}

	// 2. Configure ONLY ASR (NO AI LLM!) -> single-lesson transcribe succeeds with 202 -> completed
	rec, raw = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_asr_base_url": "http://asr.example/v1",
		"ai_asr_model":    "whisper-1",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings ASR only = %d: %s", rec.Code, raw)
	}

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+ch2L1+"/transcript/generate", nil, token)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /lessons/{id}/transcript/generate = %d, want 202: %s", rec.Code, raw)
	}
	sumSvc.Wait()

	// Idempotent re-trigger when already transcribed returns 200 OK (non-running)
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+ch2L1+"/transcript/generate", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent POST /lessons/{id}/transcript/generate = %d, want 200: %s", rec.Code, raw)
	}

	// 3. Configure AI LLM as well
	rec, raw = env.doRecJSON(t, http.MethodPut, "/api/v1/settings", map[string]any{
		"ai_base_url": "https://llm.example/v1",
		"ai_api_key":  "sk-llm",
		"ai_model":    "gpt-4o-mini",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /settings AI = %d: %s", rec.Code, raw)
	}

	// 4. Invalid chapter_id returns 404
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/transcripts/generate", map[string]any{
		"chapter_id": "nonexistent-chapter-id",
	}, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("invalid chapter_id = %d, want 404: %s", rec.Code, raw)
	}

	// 5. Gate ASR on ch2L2 while starting chapter-scoped batch transcribe + chapter-scoped batch summarize simultaneously!
	mu.Lock()
	asrGateCh = make(chan struct{})
	asrEntered = make(chan struct{})
	asrOnce = sync.Once{}
	mu.Unlock()

	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/transcripts/generate", map[string]any{
		"mode":       "missing_only",
		"chapter_id": ch2ID,
	}, token)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /courses/{id}/transcripts/generate = %d, want 202: %s", rec.Code, raw)
	}
	<-asrEntered

	// Duplicate transcribe batch -> 409 batch_already_running
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/transcripts/generate", nil, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate batch transcribe = %d, want 409: %s", rec.Code, raw)
	}

	// Opposite single-lesson summarize on ch2L2 (currently transcribing in batch) -> 409 lesson_job_conflict!
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/lessons/"+ch2L2+"/summary/generate", nil, token)
	if rec.Code != http.StatusConflict || !strings.Contains(string(raw), "lesson_job_conflict") {
		t.Fatalf("opposite action while transcribing = %d, want 409 lesson_job_conflict: %s", rec.Code, raw)
	}

	// Simultaneously run summary batch scoped to ch1ID (succeeds concurrently without waiting for ASR gate!).
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/summaries/generate", map[string]any{
		"mode":       "missing_only",
		"chapter_id": ch1ID,
	}, token)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("concurrent ch1 summaries/generate = %d, want 202: %s", rec.Code, raw)
	}

	// Wait for ch1 summary batch to finish BEFORE releasing ASR gate!
	deadline := time.Now().Add(5 * time.Second)
	var status summary.CourseSummariesStatus
	for time.Now().Before(deadline) {
		rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/courses/"+course.ID+"/summaries/status", nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET summaries/status = %d: %s", rec.Code, raw)
		}
		_ = json.Unmarshal(raw, &status)
		if status.SummaryBatchJob != nil && !status.SummaryBatchJob.Running {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if status.SummaryBatchJob == nil || status.SummaryBatchJob.Running || status.SummaryBatchJob.Completed != 1 || status.SummaryBatchJob.ChapterID != ch1ID {
		t.Fatalf("expected ch1 summary batch completed concurrently, got %+v", status.SummaryBatchJob)
	}
	if status.TranscribeBatchJob == nil || !status.TranscribeBatchJob.Running || status.TranscribeBatchJob.ChapterID != ch2ID {
		t.Fatalf("expected ch2 transcribe batch still running, got %+v", status.TranscribeBatchJob)
	}

	// Cancel transcribe batch via /courses/{id}/transcripts/cancel
	rec, raw = env.doRecJSON(t, http.MethodPost, "/api/v1/courses/"+course.ID+"/transcripts/cancel", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /courses/{id}/transcripts/cancel = %d: %s", rec.Code, raw)
	}
	sumSvc.Wait()

	rec, raw = env.doRecJSON(t, http.MethodGet, "/api/v1/courses/"+course.ID+"/summaries/status", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET final summaries/status = %d: %s", rec.Code, raw)
	}
	_ = json.Unmarshal(raw, &status)
	if status.TranscribeBatchJob == nil || status.TranscribeBatchJob.Running || status.TranscribeBatchJob.CurrentStage != summary.StageCancelled {
		t.Fatalf("expected cancelled transcribe_batch_job, got %+v", status.TranscribeBatchJob)
	}
	if status.Counts.TotalLessons != 3 || status.Counts.TranscriptReady != 2 || status.Counts.SummarizedLessons != 1 {
		t.Fatalf("unexpected final counts: %+v", status.Counts)
	}
}
