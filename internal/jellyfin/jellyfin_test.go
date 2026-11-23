package jellyfin_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coursefin/internal/auth"
	"coursefin/internal/catalog"
	"coursefin/internal/db"
	"coursefin/internal/id"
	"coursefin/internal/jellyfin"
	"coursefin/internal/media"
	"coursefin/internal/storage"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type testEnv struct {
	db       *sql.DB
	dataDir  string
	mediaDir string
	authSvc  *auth.Service
	catSvc   *catalog.Service
	mediaSvc *media.Service
	handler  http.Handler
	server   *httptest.Server
	admin    *auth.User
	libID    string
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dataDir := t.TempDir()
	mediaDir := t.TempDir()

	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	reg := storage.NewRegistry()
	authSvc := auth.NewService(conn)
	catSvc := catalog.NewService(conn, dataDir)
	catSvc.SetRegistry(reg)
	mediaSvc := media.NewService(conn, dataDir, reg, "")
	t.Cleanup(func() { _ = mediaSvc.Close() })

	admin, err := authSvc.SetupAdmin(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatalf("SetupAdmin failed: %v", err)
	}

	var libID string
	if err := conn.QueryRow(`SELECT id FROM libraries LIMIT 1`).Scan(&libID); err != nil {
		t.Fatalf("query default library failed: %v", err)
	}

	h := jellyfin.NewHandler(conn, authSvc, catSvc, mediaSvc)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return &testEnv{
		db:       conn,
		dataDir:  dataDir,
		mediaDir: mediaDir,
		authSvc:  authSvc,
		catSvc:   catSvc,
		mediaSvc: mediaSvc,
		handler:  h,
		server:   srv,
		admin:    admin,
		libID:    libID,
	}
}

func (e *testEnv) doJSON(t *testing.T, method, path string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reqBody)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, path, err)
	}
	return resp, data
}

func (e *testEnv) authenticate(t *testing.T) (token string, userID string, serverID string) {
	t.Helper()
	resp, body := e.doJSON(t, http.MethodPost, "/Users/AuthenticateByName", map[string]string{
		"Username": "admin",
		"Pw":       "secret",
	}, map[string]string{
		"Authorization": `MediaBrowser Client="Infuse", Device="iPad Pro", DeviceId="test-device-1", Version="7.7"`,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AuthenticateByName status = %d, body = %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken string `json:"AccessToken"`
		ServerId    string `json:"ServerId"`
		User        struct {
			Id   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"User"`
		SessionInfo struct {
			Id         string `json:"Id"`
			UserId     string `json:"UserId"`
			Client     string `json:"Client"`
			DeviceName string `json:"DeviceName"`
			DeviceId   string `json:"DeviceId"`
		} `json:"SessionInfo"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode auth response: %v (body=%s)", err, string(body))
	}
	if out.AccessToken == "" || out.ServerId == "" || out.User.Id == "" {
		t.Fatalf("missing auth fields: %+v", out)
	}
	return out.AccessToken, out.User.Id, out.ServerId
}

func (e *testEnv) seedSource(t *testing.T) string {
	t.Helper()
	src, err := e.catSvc.CreateSource(context.Background(), "Test Media", "local", e.mediaDir)
	if err != nil {
		t.Fatalf("CreateSource failed: %v", err)
	}
	return src.ID
}

func (e *testEnv) seedMediaFile(t *testing.T, sourceID, relPath, container, videoCodec, audioCodec string, durationMs int64, streamsJSON, thumbFile string) string {
	t.Helper()
	full := filepath.Join(e.mediaDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir media: %v", err)
	}
	payload := []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-mock-video-bytes-0123456789")
	if err := os.WriteFile(full, payload, 0o644); err != nil {
		t.Fatalf("write media file: %v", err)
	}

	mfID := id.New()
	now := time.Now().UTC().Format(time.RFC3339)
	if streamsJSON == "" {
		streamsJSON = "[]"
	}
	if _, err := e.db.Exec(
		`INSERT INTO media_files (
			id, source_id, relative_path, file_name, file_size, mod_time, status,
			duration_ms, container, video_codec, audio_codec, width, height, bitrate,
			streams_json, thumbnail_path, probed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, 1920, 1080, 2500000, ?, ?, ?, ?, ?)`,
		mfID, sourceID, relPath, filepath.Base(relPath), len(payload), now,
		durationMs, container, videoCodec, audioCodec, streamsJSON, thumbFile, now, now, now,
	); err != nil {
		t.Fatalf("insert media_file: %v", err)
	}
	return mfID
}

func (e *testEnv) seedExternalSubtitle(t *testing.T, sourceID, mediaFileID, relPath, lang, title, format string, isDefault bool) string {
	t.Helper()
	full := filepath.Join(e.mediaDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	srt := "1\n00:00:01,500 --> 00:00:03,500\n你好 CourseFin\n"
	if err := os.WriteFile(full, []byte(srt), 0o644); err != nil {
		t.Fatalf("write subtitle: %v", err)
	}
	subID := id.New()
	defInt := 0
	if isDefault {
		defInt = 1
	}
	if _, err := e.db.Exec(
		`INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
		subID, sourceID, relPath, mediaFileID, lang, title, format, defInt,
	); err != nil {
		t.Fatalf("insert subtitle_file: %v", err)
	}
	return subID
}

func (e *testEnv) writePNGThumb(t *testing.T, relUnderData string) string {
	t.Helper()
	full := filepath.Join(e.dataDir, filepath.FromSlash(relUnderData))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir thumb: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 120, B: 210, A: 255})
		}
	}
	f, err := os.Create(full)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return relUnderData
}

func formatDashedUpperUUID(raw string) string {
	return strings.ToUpper(raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:32])
}

func TestJellyfinAuthSystemAndCaseInsensitiveEmbyPrefix(t *testing.T) {
	env := setupTestEnv(t)

	// Unauthenticated protected endpoints must return 401.
	for _, protectedPath := range []string{
		"/System/Info",
		"/Users/Me",
		"/Users/" + env.admin.ID + "/Views",
		"/Items",
		"/Items/Counts",
	} {
		resp, _ := env.doJSON(t, http.MethodGet, protectedPath, nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET %s without auth: got %d, want 401", protectedPath, resp.StatusCode)
		}
	}

	// Public system endpoints + /emby prefix + case-insensitive matching.
	resp, body := env.doJSON(t, http.MethodGet, "/System/Info/Public", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /System/Info/Public status = %d, body = %s", resp.StatusCode, string(body))
	}
	var pubInfo struct {
		ServerName             string `json:"ServerName"`
		Version                string `json:"Version"`
		ProductName            string `json:"ProductName"`
		Id                     string `json:"Id"`
		StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
	}
	if err := json.Unmarshal(body, &pubInfo); err != nil {
		t.Fatalf("decode /System/Info/Public: %v", err)
	}
	if pubInfo.ServerName != "CourseFin" || pubInfo.Id == "" || !pubInfo.StartupWizardCompleted {
		t.Fatalf("unexpected PublicSystemInfo: %+v", pubInfo)
	}

	resp2, body2 := env.doJSON(t, http.MethodGet, "/emby/system/info/public", nil, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /emby/system/info/public status = %d", resp2.StatusCode)
	}
	var pubInfo2 map[string]any
	if err := json.Unmarshal(body2, &pubInfo2); err != nil {
		t.Fatalf("decode /emby/system/info/public: %v", err)
	}
	if pubInfo2["Id"] != pubInfo.Id {
		t.Fatalf("expected matching ServerId across /emby prefix, got %v vs %v", pubInfo2["Id"], pubInfo.Id)
	}

	resp, body = env.doJSON(t, http.MethodGet, "/System/Ping", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Jellyfin Server") {
		t.Fatalf("GET /System/Ping status = %d, body = %s", resp.StatusCode, string(body))
	}

	resp, body = env.doJSON(t, http.MethodGet, "/QuickConnect/Enabled", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /QuickConnect/Enabled status = %d", resp.StatusCode)
	}
	var qcEnabled bool
	if err := json.Unmarshal(body, &qcEnabled); err != nil || qcEnabled {
		t.Fatalf("expected QuickConnect false, got %s (err=%v)", string(body), err)
	}

	resp, body = env.doJSON(t, http.MethodGet, "/Users/Public", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /Users/Public status = %d", resp.StatusCode)
	}
	var pubUsers []struct {
		Id   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(body, &pubUsers); err != nil || len(pubUsers) != 1 || pubUsers[0].Name != "admin" {
		t.Fatalf("unexpected /Users/Public: %s (err=%v)", string(body), err)
	}

	resp, _ = env.doJSON(t, http.MethodGet, "/Branding/Configuration", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /Branding/Configuration status = %d", resp.StatusCode)
	}

	// AuthenticateByName
	token, userID, serverID := env.authenticate(t)
	if userID != env.admin.ID || serverID != pubInfo.Id {
		t.Fatalf("unexpected auth IDs: user=%s (want %s), server=%s (want %s)", userID, env.admin.ID, serverID, pubInfo.Id)
	}
	if _, ok := id.Normalize(userID); !ok || len(userID) != 32 {
		t.Fatalf("expected 32-char hex User.Id, got %q", userID)
	}

	// Wrong password must fail 401.
	resp, _ = env.doJSON(t, http.MethodPost, "/Users/AuthenticateByName", map[string]string{
		"Username": "admin",
		"Pw":       "wrongpass",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad password, got %d", resp.StatusCode)
	}

	// Verify all auth header & query schemes work across paths and case variants.
	authVariants := []struct {
		name    string
		path    string
		headers map[string]string
	}{
		{
			name:    "MediaBrowser header",
			path:    "/Users/Me",
			headers: map[string]string{"Authorization": `MediaBrowser Client="Infuse", Device="iPad", DeviceId="d1", Version="7.7", Token="` + token + `"`},
		},
		{
			name:    "Emby header + dashed uppercase userId",
			path:    "/emby/Users/" + formatDashedUpperUUID(userID),
			headers: map[string]string{"Authorization": `Emby Token="` + token + `"`},
		},
		{
			name:    "X-Emby-Token + zero UUID alias",
			path:    "/Users/00000000000000000000000000000000",
			headers: map[string]string{"X-Emby-Token": token},
		},
		{
			name:    "X-MediaBrowser-Token",
			path:    "/System/Info",
			headers: map[string]string{"X-MediaBrowser-Token": token},
		},
		{
			name: "api_key query param + /System/Endpoint",
			path: "/System/Endpoint?api_key=" + token,
		},
	}

	for _, tc := range authVariants {
		resp, body := env.doJSON(t, http.MethodGet, tc.path, nil, tc.headers)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s (%s) status = %d, body = %s", tc.name, tc.path, resp.StatusCode, string(body))
		}
	}

	// Verify DisplayPreferences preserves mixed-case ID across case-insensitive path matching.
	resp, body = env.doJSON(t, http.MethodPost, "/EMBY/displaypreferences/UserSettings_CamelCase?api_key="+token, map[string]any{
		"Id":                "UserSettings_CamelCase",
		"SortBy":            "DateCreated",
		"SortOrder":         "Descending",
		"RememberSorting":   true,
		"ShowSidebar":       true,
		"PrimaryImageWidth": 300,
	}, nil)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("POST DisplayPreferences status = %d, body = %s", resp.StatusCode, string(body))
	}

	resp, body = env.doJSON(t, http.MethodGet, "/DisplayPreferences/UserSettings_CamelCase?api_key="+token, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET DisplayPreferences status = %d, body = %s", resp.StatusCode, string(body))
	}
	var dp struct {
		Id        string `json:"Id"`
		SortBy    string `json:"SortBy"`
		SortOrder string `json:"SortOrder"`
	}
	if err := json.Unmarshal(body, &dp); err != nil {
		t.Fatalf("decode DisplayPreferences: %v", err)
	}
	if dp.Id != "UserSettings_CamelCase" || dp.SortBy != "DateCreated" || dp.SortOrder != "Descending" {
		t.Fatalf("unexpected DisplayPreferences: %+v", dp)
	}

	// Verify WebSocket /socket upgrades and handles KeepAlive cleanly.
	wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/socket?api_key=" + token
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsConn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer wsConn.CloseNow()

	if err := wsjson.Write(ctx, wsConn, map[string]any{"MessageType": "KeepAlive"}); err != nil {
		t.Fatalf("websocket write KeepAlive failed: %v", err)
	}
	var wsReply map[string]any
	if err := wsjson.Read(ctx, wsConn, &wsReply); err != nil {
		t.Fatalf("websocket read KeepAlive failed: %v", err)
	}
	if wsReply["MessageType"] != "KeepAlive" {
		t.Fatalf("expected KeepAlive reply, got %+v", wsReply)
	}
	if err := wsConn.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("websocket close failed: %v", err)
	}
}

func TestJellyfinCatalogMappingViewsSeriesSeasonsEpisodesTags(t *testing.T) {
	env := setupTestEnv(t)
	token, userID, _ := env.authenticate(t)
	ctx := context.Background()

	srcID := env.seedSource(t)
	thumb := env.writePNGThumb(t, "thumbs/lesson1.png")
	mf1 := env.seedMediaFile(t, srcID, "go-course/01-intro.mp4", "mp4", "h264", "aac", 90000, "", thumb)
	mf2 := env.seedMediaFile(t, srcID, "go-course/02-types.mp4", "mp4", "h264", "aac", 120000, "", thumb)
	mf3 := env.seedMediaFile(t, srcID, "rust-course/01-ownership.mp4", "mp4", "h264", "aac", 60000, "", "")

	course1, err := env.catSvc.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID:  env.libID,
		Title:      "Go 并发实战",
		Overview:   "深入理解 Go 并发模型",
		Instructor: "Gopher",
		Tags:       []string{"Go", "后端"},
	})
	if err != nil {
		t.Fatalf("CreateCourse 1 failed: %v", err)
	}
	ch1 := course1.Chapters[0]
	if _, err := env.catSvc.UpdateChapter(ctx, ch1.ID, "第一章 基础", ""); err != nil {
		t.Fatalf("UpdateChapter failed: %v", err)
	}
	ch2, err := env.catSvc.CreateChapter(ctx, course1.ID, "第二章 进阶")
	if err != nil {
		t.Fatalf("CreateChapter 2 failed: %v", err)
	}
	l1, err := env.catSvc.AddLesson(ctx, ch1.ID, mf1, "01 协程入门")
	if err != nil {
		t.Fatalf("AddLesson 1 failed: %v", err)
	}
	l2, err := env.catSvc.AddLesson(ctx, ch2.ID, mf2, "02 Channel 模式")
	if err != nil {
		t.Fatalf("AddLesson 2 failed: %v", err)
	}

	course2, err := env.catSvc.CreateCourse(ctx, catalog.CreateCourseInput{
		LibraryID: env.libID,
		Title:     "Rust 系统编程",
		Tags:      []string{"Rust"},
	})
	if err != nil {
		t.Fatalf("CreateCourse 2 failed: %v", err)
	}
	if _, err := env.catSvc.AddLesson(ctx, course2.Chapters[0].ID, mf3, "01 所有权"); err != nil {
		t.Fatalf("AddLesson 3 failed: %v", err)
	}

	headers := map[string]string{"X-Emby-Token": token}

	// 1. GET /Users/{uid}/Views -> CollectionFolder with CollectionType: "tvshows"
	resp, body := env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Views", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Views status = %d, body = %s", resp.StatusCode, string(body))
	}
	var viewsOut struct {
		Items []struct {
			Id             string `json:"Id"`
			Name           string `json:"Name"`
			Type           string `json:"Type"`
			CollectionType string `json:"CollectionType"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(body, &viewsOut); err != nil {
		t.Fatalf("decode Views: %v", err)
	}
	if viewsOut.TotalRecordCount != 1 || len(viewsOut.Items) != 1 {
		t.Fatalf("expected 1 view, got %+v", viewsOut)
	}
	if viewsOut.Items[0].Id != env.libID || viewsOut.Items[0].Type != "CollectionFolder" || viewsOut.Items[0].CollectionType != "tvshows" {
		t.Fatalf("unexpected CollectionFolder: %+v", viewsOut.Items[0])
	}

	// 2. GET /Users/{uid}/Items?ParentId={libId}&IncludeItemTypes=Series&Tags=Go
	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+formatDashedUpperUUID(userID)+"/Items?ParentId="+formatDashedUpperUUID(env.libID)+"&IncludeItemTypes=Series&Tags=Go", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Series by Tags status = %d, body = %s", resp.StatusCode, string(body))
	}
	var seriesOut struct {
		Items []struct {
			Id        string            `json:"Id"`
			Name      string            `json:"Name"`
			Type      string            `json:"Type"`
			Tags      []string          `json:"Tags"`
			Genres    []string          `json:"Genres"`
			ImageTags map[string]string `json:"ImageTags"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(body, &seriesOut); err != nil {
		t.Fatalf("decode Series: %v", err)
	}
	if seriesOut.TotalRecordCount != 1 || len(seriesOut.Items) != 1 || seriesOut.Items[0].Id != course1.ID {
		t.Fatalf("expected only course1 for Tags=Go, got %+v", seriesOut)
	}
	if len(seriesOut.Items[0].Tags) != 2 || len(seriesOut.Items[0].Genres) != 2 {
		t.Fatalf("expected both Tags and Genres populated with 2 entries, got tags=%v genres=%v", seriesOut.Items[0].Tags, seriesOut.Items[0].Genres)
	}
	if seriesOut.Items[0].ImageTags["Primary"] == "" {
		t.Fatalf("expected Primary ImageTag fallback from lesson thumbnail on course1")
	}

	// 3. GET /Shows/{courseId}/Seasons
	resp, body = env.doJSON(t, http.MethodGet, "/Shows/"+formatDashedUpperUUID(course1.ID)+"/Seasons", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Seasons status = %d, body = %s", resp.StatusCode, string(body))
	}
	var seasonsOut struct {
		Items []struct {
			Id          string `json:"Id"`
			Name        string `json:"Name"`
			Type        string `json:"Type"`
			SeriesId    string `json:"SeriesId"`
			IndexNumber int    `json:"IndexNumber"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(body, &seasonsOut); err != nil {
		t.Fatalf("decode Seasons: %v", err)
	}
	if seasonsOut.TotalRecordCount != 2 || len(seasonsOut.Items) != 2 {
		t.Fatalf("expected 2 seasons, got %+v", seasonsOut)
	}
	if seasonsOut.Items[0].IndexNumber != 1 || seasonsOut.Items[1].IndexNumber != 2 || seasonsOut.Items[0].Type != "Season" {
		t.Fatalf("unexpected seasons order/type: %+v", seasonsOut.Items)
	}

	// 4. GET /Shows/{courseId}/Episodes
	resp, body = env.doJSON(t, http.MethodGet, "/Shows/"+course1.ID+"/Episodes", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Episodes status = %d, body = %s", resp.StatusCode, string(body))
	}
	var epsOut struct {
		Items []struct {
			Id                string `json:"Id"`
			Name              string `json:"Name"`
			Type              string `json:"Type"`
			SeriesId          string `json:"SeriesId"`
			SeasonId          string `json:"SeasonId"`
			ParentIndexNumber int    `json:"ParentIndexNumber"`
			IndexNumber       int    `json:"IndexNumber"`
			RunTimeTicks      int64  `json:"RunTimeTicks"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(body, &epsOut); err != nil {
		t.Fatalf("decode Episodes: %v", err)
	}
	if epsOut.TotalRecordCount != 2 || len(epsOut.Items) != 2 {
		t.Fatalf("expected 2 episodes, got %+v", epsOut)
	}
	if epsOut.Items[0].Id != l1.ID || epsOut.Items[0].SeriesId != course1.ID || epsOut.Items[0].SeasonId != ch1.ID ||
		epsOut.Items[0].ParentIndexNumber != 1 || epsOut.Items[0].IndexNumber != 1 || epsOut.Items[0].RunTimeTicks != 90000*10000 {
		t.Fatalf("unexpected episode 1: %+v", epsOut.Items[0])
	}
	if epsOut.Items[1].Id != l2.ID || epsOut.Items[1].SeasonId != ch2.ID || epsOut.Items[1].ParentIndexNumber != 2 || epsOut.Items[1].IndexNumber != 1 {
		t.Fatalf("unexpected episode 2: %+v", epsOut.Items[1])
	}

	// 5. Auxiliary catalog endpoints: /Genres, /Tags, /Search/Hints, /Items/Counts, /DisplayPreferences/usersettings, /Intros, /Similar, /SpecialFeatures, /Items/Filters, /Items/Filters2, /Items/{id}/Images/Primary
	for _, endpoint := range []string{
		"/Genres",
		"/Tags",
		"/Search/Hints?SearchTerm=Go",
		"/Items/Counts",
		"/DisplayPreferences/usersettings",
		"/Users/" + userID + "/Items/" + l1.ID + "/Intros",
		"/Users/" + userID + "/Items/" + course1.ID + "/Similar",
		"/Users/" + userID + "/Items/" + course1.ID + "/SpecialFeatures",
		"/Items/Filters",
		"/Items/Filters2",
		"/Users/" + userID + "/Items/Latest",
	} {
		resp, body := env.doJSON(t, http.MethodGet, endpoint, nil, headers)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", endpoint, resp.StatusCode, string(body))
		}
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatalf("GET %s invalid JSON: %v (body=%s)", endpoint, err, string(body))
		}
	}

	// Verify /Items/Counts values
	_, countsBody := env.doJSON(t, http.MethodGet, "/Items/Counts", nil, headers)
	var counts struct {
		SeriesCount  int `json:"SeriesCount"`
		EpisodeCount int `json:"EpisodeCount"`
	}
	_ = json.Unmarshal(countsBody, &counts)
	if counts.SeriesCount != 2 || counts.EpisodeCount != 3 {
		t.Fatalf("unexpected /Items/Counts: %+v", counts)
	}

	// Verify /Search/Hints returns SearchHints array
	_, hintsBody := env.doJSON(t, http.MethodGet, "/Search/Hints?SearchTerm=并发", nil, headers)
	var hints struct {
		SearchHints []struct {
			Id   string `json:"Id"`
			Name string `json:"Name"`
			Type string `json:"Type"`
		} `json:"SearchHints"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	_ = json.Unmarshal(hintsBody, &hints)
	if hints.TotalRecordCount == 0 || len(hints.SearchHints) == 0 {
		t.Fatalf("expected SearchHints matches for 并发, got %s", string(hintsBody))
	}

	// Verify /Items/{courseId}/Images/Primary?maxWidth=120 returns resized JPEG and 304 on ETag
	resp, imgBody := env.doJSON(t, http.MethodGet, "/Items/"+course1.ID+"/Images/Primary?maxWidth=120", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/jpeg") || len(imgBody) == 0 {
		t.Fatalf("unexpected image response: status=%d ct=%s len=%d", resp.StatusCode, resp.Header.Get("Content-Type"), len(imgBody))
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		resp304, _ := env.doJSON(t, http.MethodGet, "/Items/"+course1.ID+"/Images/Primary?maxWidth=120", nil, map[string]string{"If-None-Match": etag})
		if resp304.StatusCode != http.StatusNotModified {
			t.Fatalf("expected 304 Not Modified on matching ETag, got %d", resp304.StatusCode)
		}
	}

	// Verify /Items queries: Genres filter, ParentId=courseId (Seasons), Recursive=true (Episodes), Ids, SortBy/Paging
	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items?ParentId="+env.libID+"&IncludeItemTypes=Series&Genres=%E5%90%8E%E7%AB%AF", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Series by Genres status = %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &seriesOut); err != nil || seriesOut.TotalRecordCount != 1 || seriesOut.Items[0].Id != course1.ID {
		t.Fatalf("expected course1 matching Genres=后端, got %s", string(body))
	}

	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items?ParentId="+course1.ID, nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Items?ParentId=courseId status = %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &seasonsOut); err != nil || seasonsOut.TotalRecordCount != 2 {
		t.Fatalf("expected 2 seasons under course1 ParentId, got %s", string(body))
	}

	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items?ParentId="+course1.ID+"&Recursive=true&StartIndex=1&Limit=1", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Items?ParentId=courseId&Recursive=true status = %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &epsOut); err != nil || epsOut.TotalRecordCount != 2 || len(epsOut.Items) != 1 || epsOut.Items[0].Id != l2.ID {
		t.Fatalf("expected total=2 paged len=1 (l2), got %s", string(body))
	}

	// Verify GET /Items/{lessonId} includes MediaSources with MediaSource.Id == media_file.id
	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items/"+l1.ID, nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /Items/{lessonId} status = %d", resp.StatusCode)
	}
	var singleEp struct {
		Id           string `json:"Id"`
		MediaSources []struct {
			Id string `json:"Id"`
		} `json:"MediaSources"`
	}
	if err := json.Unmarshal(body, &singleEp); err != nil || singleEp.Id != l1.ID || len(singleEp.MediaSources) != 1 || singleEp.MediaSources[0].Id != mf1 {
		t.Fatalf("expected single Episode with MediaSource.Id=%s, got %s", mf1, string(body))
	}
}

func TestDeviceProfileNegotiationDirectPlayVsRemux(t *testing.T) {
	env := setupTestEnv(t)
	token, userID, _ := env.authenticate(t)
	ctx := context.Background()

	srcID := env.seedSource(t)
	streamsJSON := `[
		{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"bit_rate":"2000000","disposition":{"default":1}},
		{"index":1,"codec_type":"audio","codec_name":"ac3","channels":6,"sample_rate":"48000","disposition":{"default":1},"tags":{"language":"eng"}},
		{"index":2,"codec_type":"subtitle","codec_name":"subrip","disposition":{"default":0},"tags":{"language":"eng","title":"Embedded EN"}}
	]`
	mfID := env.seedMediaFile(t, srcID, "remux-course/01-demo.mkv", "mkv", "h264", "ac3", 60000, streamsJSON, "")
	env.seedExternalSubtitle(t, srcID, mfID, "remux-course/01-demo.zh.srt", "zh", "中文", "srt", true)

	course, err := env.catSvc.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: env.libID, Title: "Remux Course"})
	if err != nil {
		t.Fatalf("CreateCourse failed: %v", err)
	}
	lesson, err := env.catSvc.AddLesson(ctx, course.Chapters[0].ID, mfID, "01 Demo")
	if err != nil {
		t.Fatalf("AddLesson failed: %v", err)
	}

	headers := map[string]string{"X-Emby-Token": token}

	// 1. Infuse-like request (empty DeviceProfile) -> DirectPlay
	resp, body := env.doJSON(t, http.MethodPost, "/Items/"+lesson.ID+"/PlaybackInfo", map[string]any{
		"UserId": userID,
	}, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PlaybackInfo (empty DeviceProfile) status = %d, body = %s", resp.StatusCode, string(body))
	}
	var pbInfuse jellyfin.PlaybackInfoResponse
	if err := json.Unmarshal(body, &pbInfuse); err != nil {
		t.Fatalf("decode PlaybackInfo: %v", err)
	}
	if len(pbInfuse.MediaSources) != 1 || pbInfuse.PlaySessionId == "" {
		t.Fatalf("unexpected MediaSources/PlaySessionId: %+v", pbInfuse)
	}
	msInfuse := pbInfuse.MediaSources[0]
	if msInfuse.Id != mfID {
		t.Fatalf("MediaSource.Id = %q, want media_file.id %q", msInfuse.Id, mfID)
	}
	if !msInfuse.SupportsDirectPlay || msInfuse.SupportsTranscoding {
		t.Fatalf("expected DirectPlay=true Transcoding=false for empty DeviceProfile, got %+v", msInfuse)
	}
	if !strings.HasPrefix(msInfuse.DirectStreamUrl, "/Videos/"+lesson.ID+"/stream.mkv?") || !strings.Contains(msInfuse.DirectStreamUrl, "MediaSourceId="+mfID) {
		t.Fatalf("unexpected DirectStreamUrl: %q", msInfuse.DirectStreamUrl)
	}

	// Verify DirectStreamUrl actually streams HTTP 206 Partial Content!
	rangeResp, rangeBody := env.doJSON(t, http.MethodGet, msInfuse.DirectStreamUrl, nil, map[string]string{"Range": "bytes=0-9"})
	if rangeResp.StatusCode != http.StatusPartialContent || len(rangeBody) != 10 {
		t.Fatalf("DirectStreamUrl range GET got status=%d len=%d", rangeResp.StatusCode, len(rangeBody))
	}

	// 2. Browser-like DeviceProfile (mp4+h264+aac only, TranscodingProfiles=hls) on .mkv (h264/ac3) -> Remux
	browserProfile := map[string]any{
		"UserId": userID,
		"DeviceProfile": map[string]any{
			"Name": "Browser",
			"DirectPlayProfiles": []map[string]any{
				{"Container": "mp4,m4v", "VideoCodec": "h264", "AudioCodec": "aac,mp3", "Type": "Video"},
			},
			"TranscodingProfiles": []map[string]any{
				{"Container": "ts", "Type": "Video", "VideoCodec": "h264", "AudioCodec": "aac", "Protocol": "hls", "Context": "Streaming"},
			},
			"SubtitleProfiles": []map[string]any{
				{"Format": "vtt", "Method": "External"},
			},
		},
	}
	resp, body = env.doJSON(t, http.MethodPost, "/Items/"+lesson.ID+"/PlaybackInfo", browserProfile, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PlaybackInfo (browser) status = %d, body = %s", resp.StatusCode, string(body))
	}
	var pbBrowser jellyfin.PlaybackInfoResponse
	if err := json.Unmarshal(body, &pbBrowser); err != nil {
		t.Fatalf("decode browser PlaybackInfo: %v", err)
	}
	if len(pbBrowser.MediaSources) != 1 {
		t.Fatalf("expected 1 media source, got %d", len(pbBrowser.MediaSources))
	}
	msBrowser := pbBrowser.MediaSources[0]
	if msBrowser.SupportsDirectPlay || !msBrowser.SupportsTranscoding {
		t.Fatalf("expected DirectPlay=false, Transcoding=true for browser on mkv/ac3, got %+v", msBrowser)
	}
	if !strings.HasPrefix(msBrowser.TranscodingUrl, "/Videos/"+lesson.ID+"/master.m3u8?") ||
		!strings.Contains(msBrowser.TranscodingUrl, "PlaySessionId="+pbBrowser.PlaySessionId) ||
		!strings.Contains(msBrowser.TranscodingUrl, "MediaSourceId="+mfID) ||
		!strings.Contains(msBrowser.TranscodingUrl, "api_key="+token) {
		t.Fatalf("unexpected TranscodingUrl: %q", msBrowser.TranscodingUrl)
	}

	// Verify MediaStreams has external subtitle (index 3) with DeliveryMethod=External and working Stream.vtt URL!
	var extSubURL string
	for _, st := range msBrowser.MediaStreams {
		if st.Type == "Subtitle" && st.IsExternal {
			if st.DeliveryMethod != "External" {
				t.Fatalf("expected external subtitle DeliveryMethod=External, got %q", st.DeliveryMethod)
			}
			if st.Index != 3 {
				t.Fatalf("expected external subtitle index=3 (after embedded 0,1,2), got %d", st.Index)
			}
			if !strings.Contains(st.DeliveryUrl, "/Videos/"+lesson.ID+"/"+mfID+"/Subtitles/3/Stream.vtt") {
				t.Fatalf("unexpected subtitle DeliveryUrl: %q", st.DeliveryUrl)
			}
			extSubURL = st.DeliveryUrl
		}
	}
	if extSubURL == "" {
		t.Fatalf("missing external subtitle in MediaStreams: %+v", msBrowser.MediaStreams)
	}

	// Fetch the DeliveryUrl directly and verify WEBVTT conversion works end-to-end!
	subResp, subBody := env.doJSON(t, http.MethodGet, extSubURL, nil, nil)
	if subResp.StatusCode != http.StatusOK || !strings.HasPrefix(string(subBody), "WEBVTT") || !strings.Contains(string(subBody), "00:00:01.500 --> 00:00:03.500") {
		t.Fatalf("unexpected subtitle fetch response: status=%d body=%q", subResp.StatusCode, string(subBody))
	}

	// Verify master.m3u8 and main.m3u8 resolve via TranscodingUrl!
	m3u8Resp, m3u8Body := env.doJSON(t, http.MethodGet, msBrowser.TranscodingUrl, nil, nil)
	if m3u8Resp.StatusCode != http.StatusOK || !strings.Contains(string(m3u8Body), "main.m3u8") {
		t.Fatalf("unexpected master.m3u8 response: status=%d body=%q", m3u8Resp.StatusCode, string(m3u8Body))
	}

	// 3. Direct unit check on EvaluatePlaybackInfo for matching DirectPlayProfiles, embedded subtitle delivery, and legacy codec rejection
	mp4Lesson := &catalog.LessonDetail{ID: lesson.ID, MediaFileID: mfID, Title: "MP4", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", DurationMs: 60000}
	mp4MF := &catalog.MediaFile{ID: mfID, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", DurationMs: 60000}
	mp4Streams := []media.StreamDescriptor{
		{Index: 0, Type: "video", Codec: "h264", Width: 1920, Height: 1080},
		{Index: 1, Type: "audio", Codec: "aac", Language: "zh", IsDefault: true},
		{Index: 2, Type: "subtitle", Codec: "subrip", Language: "en"},
	}
	mp4Resp := jellyfin.EvaluatePlaybackInfo(mp4Lesson, mp4MF, mp4Streams, jellyfin.PlaybackInfoRequest{
		DeviceProfile: &jellyfin.DeviceProfile{
			DirectPlayProfiles:  []jellyfin.DirectPlayProfile{{Container: "mp4,m4v", VideoCodec: "h264", AudioCodec: "aac", Type: "Video"}},
			TranscodingProfiles: []jellyfin.TranscodingProfile{{Container: "ts", VideoCodec: "h264", AudioCodec: "aac", Protocol: "hls", Type: "Video"}},
		},
	}, token)
	if !mp4Resp.MediaSources[0].SupportsDirectPlay || mp4Resp.MediaSources[0].SupportsTranscoding {
		t.Fatalf("expected DirectPlay=true on matching mp4/h264/aac profile, got %+v", mp4Resp.MediaSources[0])
	}
	if mp4Resp.MediaSources[0].MediaStreams[2].DeliveryMethod != "Embed" {
		t.Fatalf("expected embedded subtitle DeliveryMethod=Embed during DirectPlay, got %q", mp4Resp.MediaSources[0].MediaStreams[2].DeliveryMethod)
	}

	// Verify embedded text subtitle gets DeliveryMethod=External when transcoding
	var embeddedMethod string
	for _, st := range msBrowser.MediaStreams {
		if st.Type == "Subtitle" && !st.IsExternal {
			embeddedMethod = st.DeliveryMethod
		}
	}
	if embeddedMethod != "External" {
		t.Fatalf("expected embedded text subtitle DeliveryMethod=External during HLS remux, got %q", embeddedMethod)
	}

	// Verify GET /Items/{id}/PlaybackInfo & DELETE /Videos/ActiveEncodings work cleanly
	getPBResp, _ := env.doJSON(t, http.MethodGet, "/Items/"+lesson.ID+"/PlaybackInfo?UserId="+userID, nil, headers)
	if getPBResp.StatusCode != http.StatusOK {
		t.Fatalf("GET PlaybackInfo status = %d", getPBResp.StatusCode)
	}
	delEncResp, _ := env.doJSON(t, http.MethodDelete, "/Videos/ActiveEncodings?PlaySessionId="+pbBrowser.PlaySessionId, nil, headers)
	if delEncResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE ActiveEncodings status = %d", delEncResp.StatusCode)
	}

	legacyLesson := &catalog.LessonDetail{ID: lesson.ID, MediaFileID: mfID, Title: "Legacy", Container: "wmv", VideoCodec: "wmv3", AudioCodec: "wmav2", DurationMs: 60000}
	legacyMF := &catalog.MediaFile{ID: mfID, Container: "wmv", VideoCodec: "wmv3", AudioCodec: "wmav2", DurationMs: 60000}
	legacyResp := jellyfin.EvaluatePlaybackInfo(legacyLesson, legacyMF, nil, jellyfin.PlaybackInfoRequest{
		DeviceProfile: &jellyfin.DeviceProfile{
			DirectPlayProfiles:  []jellyfin.DirectPlayProfile{{Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Type: "Video"}},
			TranscodingProfiles: []jellyfin.TranscodingProfile{{Container: "ts", VideoCodec: "h264", AudioCodec: "aac", Protocol: "hls", Type: "Video"}},
		},
	}, token)
	if len(legacyResp.MediaSources) != 1 || legacyResp.MediaSources[0].SupportsDirectPlay || legacyResp.MediaSources[0].SupportsTranscoding {
		t.Fatalf("expected SupportsDirectPlay=false & SupportsTranscoding=false for wmv3, got %+v", legacyResp.MediaSources)
	}
}

func TestPlaybackProgressSyncAndResumeNextUp(t *testing.T) {
	env := setupTestEnv(t)
	token, userID, _ := env.authenticate(t)
	ctx := context.Background()

	srcID := env.seedSource(t)
	mf1 := env.seedMediaFile(t, srcID, "prog/01.mp4", "mp4", "h264", "aac", 100000, "", "")
	mf2 := env.seedMediaFile(t, srcID, "prog/02.mp4", "mp4", "h264", "aac", 100000, "", "")

	course, err := env.catSvc.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: env.libID, Title: "Progress Course"})
	if err != nil {
		t.Fatalf("CreateCourse failed: %v", err)
	}
	l1, err := env.catSvc.AddLesson(ctx, course.Chapters[0].ID, mf1, "Lesson 1")
	if err != nil {
		t.Fatalf("AddLesson 1 failed: %v", err)
	}
	l2, err := env.catSvc.AddLesson(ctx, course.Chapters[0].ID, mf2, "Lesson 2")
	if err != nil {
		t.Fatalf("AddLesson 2 failed: %v", err)
	}

	headers := map[string]string{"X-Emby-Token": token}

	// 1. Report Playing & Progress on Lesson 1 (30s = 300,000,000 ticks)
	resp, _ := env.doJSON(t, http.MethodPost, "/Sessions/Playing", map[string]any{
		"ItemId":        l1.ID,
		"PositionTicks": 0,
	}, headers)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /Sessions/Playing status = %d", resp.StatusCode)
	}

	posTicks := int64(30_000 * 10000)
	resp, _ = env.doJSON(t, http.MethodPost, "/Sessions/Playing/Progress", map[string]any{
		"ItemId":        l1.ID,
		"PositionTicks": posTicks,
	}, headers)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /Sessions/Playing/Progress status = %d", resp.StatusCode)
	}

	// 2. GET /Users/{uid}/Items/Resume returns Lesson 1
	resp, body := env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items/Resume", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /Resume status = %d, body = %s", resp.StatusCode, string(body))
	}
	var resumeOut struct {
		Items []struct {
			Id       string `json:"Id"`
			UserData struct {
				PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
				Played                bool  `json:"Played"`
			} `json:"UserData"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(body, &resumeOut); err != nil {
		t.Fatalf("decode Resume: %v", err)
	}
	if resumeOut.TotalRecordCount != 1 || len(resumeOut.Items) != 1 || resumeOut.Items[0].Id != l1.ID || resumeOut.Items[0].UserData.PlaybackPositionTicks != posTicks {
		t.Fatalf("unexpected Resume result: %+v", resumeOut)
	}

	// 3. POST /Users/{uid}/PlayedItems/{l1.ID} marks Lesson 1 played
	resp, body = env.doJSON(t, http.MethodPost, "/Users/"+userID+"/PlayedItems/"+l1.ID, nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST PlayedItems status = %d, body = %s", resp.StatusCode, string(body))
	}
	var ud struct {
		Played                bool  `json:"Played"`
		PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
	}
	if err := json.Unmarshal(body, &ud); err != nil || !ud.Played {
		t.Fatalf("expected Played=true, got %s (err=%v)", string(body), err)
	}

	// 4. GET /Shows/NextUp advances to Lesson 2!
	resp, body = env.doJSON(t, http.MethodGet, "/Shows/NextUp?UserId="+userID, nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /Shows/NextUp status = %d, body = %s", resp.StatusCode, string(body))
	}
	var nextUpOut struct {
		Items []struct {
			Id       string `json:"Id"`
			SeriesId string `json:"SeriesId"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(body, &nextUpOut); err != nil {
		t.Fatalf("decode NextUp: %v", err)
	}
	if nextUpOut.TotalRecordCount != 1 || len(nextUpOut.Items) != 1 || nextUpOut.Items[0].Id != l2.ID || nextUpOut.Items[0].SeriesId != course.ID {
		t.Fatalf("expected NextUp to advance to Lesson 2 (%s), got %+v", l2.ID, nextUpOut)
	}

	// 5. GET & POST UserData + DELETE PlayedItems
	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items/"+l1.ID+"/UserData", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET UserData status = %d", resp.StatusCode)
	}
	resp, _ = env.doJSON(t, http.MethodDelete, "/Users/"+userID+"/PlayedItems/"+l1.ID, nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE PlayedItems status = %d", resp.StatusCode)
	}
	resp, body = env.doJSON(t, http.MethodPost, "/Users/"+userID+"/Items/"+l2.ID+"/UserData", map[string]any{
		"PlaybackPositionTicks": int64(45_000 * 10000),
	}, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST UserData status = %d, body = %s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, &ud); err != nil || ud.PlaybackPositionTicks != int64(45_000*10000) {
		t.Fatalf("unexpected UserData update response: %s", string(body))
	}

	// 6. POST /Sessions/Playing/Stopped auto-completes at >= 90% and series-level PlayedItems marks whole course
	resp, _ = env.doJSON(t, http.MethodPost, "/Sessions/Playing/Stopped", map[string]any{
		"ItemId":        l2.ID,
		"PositionTicks": int64(95_000 * 10000),
	}, headers)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /Sessions/Playing/Stopped status = %d", resp.StatusCode)
	}
	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items/"+l2.ID+"/UserData", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET UserData after stop status = %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &ud); err != nil || !ud.Played {
		t.Fatalf("expected l2 auto-completed Played=true after >=90%% stop, got %s", string(body))
	}

	resp, body = env.doJSON(t, http.MethodPost, "/Users/"+userID+"/PlayedItems/"+course.ID, nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST PlayedItems on Series status = %d, body = %s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, &ud); err != nil || !ud.Played {
		t.Fatalf("expected Series Played=true, got %s", string(body))
	}

	// 7. Sessions list, Capabilities, and Logout revocation
	resp, _ = env.doJSON(t, http.MethodPost, "/Sessions/Capabilities/Full", map[string]any{}, headers)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST Capabilities/Full status = %d", resp.StatusCode)
	}
	resp, body = env.doJSON(t, http.MethodGet, "/Sessions", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /Sessions status = %d", resp.StatusCode)
	}
	var sessions []map[string]any
	if err := json.Unmarshal(body, &sessions); err != nil || len(sessions) == 0 {
		t.Fatalf("expected active sessions, got %s", string(body))
	}
	resp, _ = env.doJSON(t, http.MethodPost, "/Sessions/Logout", nil, headers)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /Sessions/Logout status = %d", resp.StatusCode)
	}
	resp, _ = env.doJSON(t, http.MethodGet, "/Users/Me", nil, headers)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after Logout, got %d", resp.StatusCode)
	}
}

func TestSortOrderDescendingStrictWeakOrderingAndStability(t *testing.T) {
	env := setupTestEnv(t)
	token, userID, _ := env.authenticate(t)
	ctx := context.Background()
	headers := map[string]string{"X-Emby-Token": token}

	srcID := env.seedSource(t)
	mf1 := env.seedMediaFile(t, srcID, "sort/01.mp4", "mp4", "h264", "aac", 60000, "", "")
	mf2 := env.seedMediaFile(t, srcID, "sort/02.mp4", "mp4", "h264", "aac", 60000, "", "")
	mf3 := env.seedMediaFile(t, srcID, "sort/03.mp4", "mp4", "h264", "aac", 60000, "", "")
	mf4 := env.seedMediaFile(t, srcID, "sort/04.mp4", "mp4", "h264", "aac", 60000, "", "")

	course, err := env.catSvc.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: env.libID, Title: "Alpha"})
	if err != nil {
		t.Fatalf("CreateCourse Alpha failed: %v", err)
	}
	l1, _ := env.catSvc.AddLesson(ctx, course.Chapters[0].ID, mf1, "L1")
	l2, _ := env.catSvc.AddLesson(ctx, course.Chapters[0].ID, mf2, "L2")
	l3, _ := env.catSvc.AddLesson(ctx, course.Chapters[0].ID, mf3, "L3")
	l4, _ := env.catSvc.AddLesson(ctx, course.Chapters[0].ID, mf4, "L4")

	// Set identical created_at across all 4 lessons to verify DateCreated descending stability on equal keys.
	sameTS := "2026-11-23T00:00:00Z"
	if _, err := env.db.Exec(`UPDATE lessons SET created_at = ? WHERE course_id = ?`, sameTS, course.ID); err != nil {
		t.Fatalf("update lesson created_at: %v", err)
	}

	// 1. All 4 lessons unplayed -> DatePlayed descending must preserve [L1, L2, L3, L4].
	resp, body := env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items?ParentId="+course.ID+"&Recursive=true&SortBy=DatePlayed&SortOrder=Descending", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Items DatePlayed desc status = %d", resp.StatusCode)
	}
	var res struct {
		Items []struct {
			Id   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode DatePlayed desc: %v", err)
	}
	gotIDs := []string{res.Items[0].Id, res.Items[1].Id, res.Items[2].Id, res.Items[3].Id}
	wantAllUnplayed := []string{l1.ID, l2.ID, l3.ID, l4.ID}
	for i := range wantAllUnplayed {
		if gotIDs[i] != wantAllUnplayed[i] {
			t.Fatalf("unplayed DatePlayed desc scrambled order: got %v, want %v", gotIDs, wantAllUnplayed)
		}
	}

	// 2. Mark L3 played -> L3 comes first, remaining equal unplayed items stay in stable order [L1, L2, L4].
	resp, _ = env.doJSON(t, http.MethodPost, "/Users/"+userID+"/PlayedItems/"+l3.ID, nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark L3 played status = %d", resp.StatusCode)
	}
	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items?ParentId="+course.ID+"&Recursive=true&SortBy=DatePlayed&SortOrder=Descending", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Items DatePlayed desc (mixed) status = %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode DatePlayed desc (mixed): %v", err)
	}
	gotMixed := []string{res.Items[0].Id, res.Items[1].Id, res.Items[2].Id, res.Items[3].Id}
	wantMixed := []string{l3.ID, l1.ID, l2.ID, l4.ID}
	for i := range wantMixed {
		if gotMixed[i] != wantMixed[i] {
			t.Fatalf("mixed DatePlayed desc order mismatch: got %v, want %v", gotMixed, wantMixed)
		}
	}

	// 3. Equal DateCreated across all 4 lessons -> DateCreated descending must preserve [L1, L2, L3, L4].
	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items?ParentId="+course.ID+"&Recursive=true&SortBy=DateCreated&SortOrder=Descending", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Items DateCreated desc status = %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode DateCreated desc: %v", err)
	}
	gotCreated := []string{res.Items[0].Id, res.Items[1].Id, res.Items[2].Id, res.Items[3].Id}
	for i := range wantAllUnplayed {
		if gotCreated[i] != wantAllUnplayed[i] {
			t.Fatalf("equal DateCreated desc scrambled order: got %v, want %v", gotCreated, wantAllUnplayed)
		}
	}

	// 4. Unequal keys reverse cleanly with SortBy=SortName&SortOrder=Descending.
	_, err = env.catSvc.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: env.libID, Title: "Gamma"})
	if err != nil {
		t.Fatalf("CreateCourse Gamma failed: %v", err)
	}
	_, err = env.catSvc.CreateCourse(ctx, catalog.CreateCourseInput{LibraryID: env.libID, Title: "Beta"})
	if err != nil {
		t.Fatalf("CreateCourse Beta failed: %v", err)
	}
	resp, body = env.doJSON(t, http.MethodGet, "/Users/"+userID+"/Items?ParentId="+env.libID+"&IncludeItemTypes=Series&SortBy=SortName&SortOrder=Descending", nil, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Series SortName desc status = %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode SortName desc: %v", err)
	}
	gotNames := []string{res.Items[0].Name, res.Items[1].Name, res.Items[2].Name}
	wantNames := []string{"Gamma", "Beta", "Alpha"}
	for i := range wantNames {
		if gotNames[i] != wantNames[i] {
			t.Fatalf("SortName desc order mismatch: got %v, want %v", gotNames, wantNames)
		}
	}
}
