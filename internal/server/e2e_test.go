package server_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"coursefin/internal/catalog"
	"coursefin/internal/config"
	"coursefin/internal/jellyfin"
	"coursefin/internal/server"
)

func findBinary(t *testing.T, name string) string {
	t.Helper()
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	fallback := filepath.Join("/opt/homebrew/bin", name)
	if _, err := os.Stat(fallback); err == nil {
		dir := filepath.Dir(fallback)
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		return fallback
	}
	t.Skipf("%s binary not available for E2E test", name)
	return ""
}

func generateTestMediaDir(t *testing.T, ffmpegBin string) string {
	t.Helper()
	mediaDir := t.TempDir()
	courseDir := filepath.Join(mediaDir, "GoCourse")
	ch1Dir := filepath.Join(courseDir, "01_Basics")
	ch2Dir := filepath.Join(courseDir, "02_Advanced")
	for _, d := range []string{ch1Dir, ch2Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Generate valid cover.jpg
	img := image.NewRGBA(image.Rect(0, 0, 160, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, color.RGBA{R: 79, G: 70, B: 229, A: 255})
		}
	}
	coverPath := filepath.Join(courseDir, "cover.jpg")
	cf, err := os.Create(coverPath)
	if err != nil {
		t.Fatalf("create cover.jpg: %v", err)
	}
	if err := jpeg.Encode(cf, img, &jpeg.Options{Quality: 85}); err != nil {
		_ = cf.Close()
		t.Fatalf("encode cover.jpg: %v", err)
	}
	_ = cf.Close()

	// Generate 2s H.264/AAC MP4 video
	mp4Path := filepath.Join(ch1Dir, "01_intro.mp4")
	cmdMP4 := exec.Command(ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=1000:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "ultrafast",
		"-c:a", "aac", "-b:a", "96k",
		"-movflags", "+faststart",
		mp4Path,
	)
	if out, err := cmdMP4.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg generate mp4: %v\n%s", err, string(out))
	}

	// Write sidecar SRT subtitle for 01_intro.mp4
	srtContent := "1\n00:00:00,200 --> 00:00:01,500\nWelcome to GoCourse\n\n2\n00:00:01,600 --> 00:00:02,000\nLesson one complete\n"
	srtPath := filepath.Join(ch1Dir, "01_intro.en.srt")
	if err := os.WriteFile(srtPath, []byte(srtContent), 0o644); err != nil {
		t.Fatalf("write srt: %v", err)
	}

	// Generate 2s H.264/AC3 MKV video (triggers HLS remux for browsers)
	mkvPath := filepath.Join(ch2Dir, "02_remux.mkv")
	cmdMKV := exec.Command(ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "ultrafast",
		"-c:a", "ac3", "-b:a", "128k",
		mkvPath,
	)
	if out, err := cmdMKV.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg generate mkv: %v\n%s", err, string(out))
	}

	return mediaDir
}

func firstPlaylistLine(body string, substr string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, substr) {
			return trimmed
		}
	}
	return ""
}

func TestEndToEndUserJourney(t *testing.T) {
	ffmpegBin := findBinary(t, "ffmpeg")
	_ = findBinary(t, "ffprobe")

	mediaDir := generateTestMediaDir(t, ffmpegBin)

	cfg := config.Config{
		Port:     "0",
		DataDir:  t.TempDir(),
		LogLevel: "error",
		BaseURL:  "http://127.0.0.1:8096",
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

	doReq := func(method, reqPath string, body any, headers map[string]string) (*http.Response, []byte) {
		t.Helper()
		var r io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			r = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, ts.URL+reqPath, r)
		if err != nil {
			t.Fatalf("new request %s %s: %v", method, reqPath, err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, reqPath, err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body %s %s: %v", method, reqPath, err)
		}
		return resp, raw
	}

	// 1. Setup admin -> login
	resp, raw := doReq(http.MethodPost, "/api/v1/setup", map[string]string{
		"username":    "admin",
		"password":    "secret123",
		"server_name": "CourseFin E2E",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/v1/setup = %d: %s", resp.StatusCode, raw)
	}

	resp, raw = doReq(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username":  "admin",
		"password":  "secret123",
		"device_id": "e2e-web-1",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/auth/login = %d: %s", resp.StatusCode, raw)
	}
	var loginRes struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &loginRes); err != nil || loginRes.Token == "" {
		t.Fatalf("unexpected login response: %s", raw)
	}
	apiHeaders := map[string]string{"Authorization": "Bearer " + loginRes.Token}

	// 2. Add local source -> synchronous scan -> preview -> commit import
	resp, raw = doReq(http.MethodPost, "/api/v1/sources", map[string]string{
		"name":      "Course Media",
		"driver":    "local",
		"root_path": mediaDir,
	}, apiHeaders)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/v1/sources = %d: %s", resp.StatusCode, raw)
	}
	var src catalog.StorageSource
	if err := json.Unmarshal(raw, &src); err != nil || src.ID == "" {
		t.Fatalf("decode source: %v (%s)", err, raw)
	}

	resp, raw = doReq(http.MethodPost, "/api/v1/sources/"+src.ID+"/scan", nil, apiHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/sources/%s/scan = %d: %s", src.ID, resp.StatusCode, raw)
	}
	var scanRep struct {
		TotalVideos    int `json:"total_videos"`
		TotalSubtitles int `json:"total_subtitles"`
	}
	if err := json.Unmarshal(raw, &scanRep); err != nil {
		t.Fatalf("decode scan report: %v", err)
	}
	if scanRep.TotalVideos < 2 || scanRep.TotalSubtitles < 1 {
		t.Fatalf("unexpected scan counts: %+v (%s)", scanRep, raw)
	}

	resp, raw = doReq(http.MethodPost, "/api/v1/import/preview", map[string]string{
		"source_id":    src.ID,
		"dir_rel_path": "GoCourse",
	}, apiHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/import/preview = %d: %s", resp.StatusCode, raw)
	}
	var preview catalog.ImportPreview
	if err := json.Unmarshal(raw, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.CourseTitle != "GoCourse" || preview.CoverRelPath == "" || len(preview.Chapters) != 2 {
		t.Fatalf("unexpected import preview: %+v", preview)
	}

	commitChapters := make([]catalog.CommitImportChapter, 0, len(preview.Chapters))
	for _, pch := range preview.Chapters {
		lessons := make([]catalog.CommitImportLesson, 0, len(pch.Lessons))
		for _, pl := range pch.Lessons {
			lessons = append(lessons, catalog.CommitImportLesson{
				MediaFileID: pl.MediaFileID,
				Title:       pl.Title,
			})
		}
		commitChapters = append(commitChapters, catalog.CommitImportChapter{
			Title:   pch.Title,
			Lessons: lessons,
		})
	}

	resp, raw = doReq(http.MethodPost, "/api/v1/import/commit", catalog.CommitImportInput{
		SourceID:     src.ID,
		DirRelPath:   "GoCourse",
		Title:        preview.CourseTitle,
		CoverRelPath: preview.CoverRelPath,
		Chapters:     commitChapters,
	}, apiHeaders)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/v1/import/commit = %d: %s", resp.StatusCode, raw)
	}
	var importedCourse catalog.CourseDetail
	if err := json.Unmarshal(raw, &importedCourse); err != nil || importedCourse.ID == "" {
		t.Fatalf("decode imported course: %v (%s)", err, raw)
	}

	// 3. Connect as Jellyfin client
	resp, raw = doReq(http.MethodPost, "/Users/AuthenticateByName", map[string]string{
		"Username": "admin",
		"Pw":       "secret123",
	}, map[string]string{
		"Authorization": `MediaBrowser Client="Infuse", Device="MacBook", DeviceId="e2e-jf-1", Version="1.0"`,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /Users/AuthenticateByName = %d: %s", resp.StatusCode, raw)
	}
	var jfAuth struct {
		AccessToken string `json:"AccessToken"`
		User        struct {
			Id string `json:"Id"`
		} `json:"User"`
	}
	if err := json.Unmarshal(raw, &jfAuth); err != nil || jfAuth.AccessToken == "" || jfAuth.User.Id == "" {
		t.Fatalf("unexpected Jellyfin auth response: %s", raw)
	}
	jfHeaders := map[string]string{"X-Emby-Token": jfAuth.AccessToken}

	// Views
	resp, raw = doReq(http.MethodGet, "/Users/"+jfAuth.User.Id+"/Views", nil, jfHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Views = %d: %s", resp.StatusCode, raw)
	}
	var views jellyfin.QueryResult[jellyfin.BaseItemDto]
	if err := json.Unmarshal(raw, &views); err != nil || len(views.Items) == 0 {
		t.Fatalf("expected library views, got %s", raw)
	}
	libID := views.Items[0].Id

	// Items
	resp, raw = doReq(http.MethodGet, "/Users/"+jfAuth.User.Id+"/Items?ParentId="+libID+"&IncludeItemTypes=Series&Recursive=true", nil, jfHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Items = %d: %s", resp.StatusCode, raw)
	}
	var seriesRes jellyfin.QueryResult[jellyfin.BaseItemDto]
	if err := json.Unmarshal(raw, &seriesRes); err != nil || len(seriesRes.Items) != 1 || seriesRes.Items[0].Id != importedCourse.ID {
		t.Fatalf("expected imported course in Items, got %s", raw)
	}

	// Seasons
	resp, raw = doReq(http.MethodGet, "/Shows/"+importedCourse.ID+"/Seasons?UserId="+jfAuth.User.Id, nil, jfHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Seasons = %d: %s", resp.StatusCode, raw)
	}
	var seasonsRes jellyfin.QueryResult[jellyfin.BaseItemDto]
	if err := json.Unmarshal(raw, &seasonsRes); err != nil || len(seasonsRes.Items) != 2 {
		t.Fatalf("expected 2 seasons, got %s", raw)
	}

	// Episodes
	resp, raw = doReq(http.MethodGet, "/Shows/"+importedCourse.ID+"/Episodes?UserId="+jfAuth.User.Id+"&Fields=MediaSources", nil, jfHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Episodes = %d: %s", resp.StatusCode, raw)
	}
	var epsRes jellyfin.QueryResult[jellyfin.BaseItemDto]
	if err := json.Unmarshal(raw, &epsRes); err != nil || len(epsRes.Items) != 2 {
		t.Fatalf("expected 2 episodes, got %s", raw)
	}

	var mp4Ep, mkvEp jellyfin.BaseItemDto
	for _, ep := range epsRes.Items {
		if strings.Contains(ep.Name, "intro") || (len(ep.MediaSources) > 0 && ep.MediaSources[0].Container == "mp4") {
			mp4Ep = ep
		} else {
			mkvEp = ep
		}
	}
	if mp4Ep.Id == "" || mkvEp.Id == "" {
		t.Fatalf("failed to resolve mp4 and mkv episodes: %+v", epsRes.Items)
	}

	// Primary Image
	resp, raw = doReq(http.MethodGet, "/Items/"+importedCourse.ID+"/Images/Primary", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") || len(raw) == 0 {
		t.Fatalf("GET /Items/%s/Images/Primary = %d ct=%q len=%d", importedCourse.ID, resp.StatusCode, resp.Header.Get("Content-Type"), len(raw))
	}

	// 4a. DirectPlay .mp4 with Range header (206 Partial Content)
	resp, raw = doReq(http.MethodGet, "/Videos/"+mp4Ep.Id+"/stream.mp4?Static=true&MediaSourceId="+mp4Ep.MediaSources[0].Id, nil, map[string]string{
		"X-Emby-Token": jfAuth.AccessToken,
		"Range":        "bytes=0-1023",
	})
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("DirectPlay Range status = %d, want 206 (%s)", resp.StatusCode, string(raw))
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes 0-1023/") || len(raw) != 1024 {
		t.Fatalf("unexpected Range response: Content-Range=%q len=%d", resp.Header.Get("Content-Range"), len(raw))
	}

	// 4b. HLS VOD remux for .mkv (master.m3u8 -> main.m3u8 -> 0.ts)
	resp, raw = doReq(http.MethodPost, "/Items/"+mkvEp.Id+"/PlaybackInfo", jellyfin.PlaybackInfoRequest{
		UserId: jfAuth.User.Id,
		DeviceProfile: &jellyfin.DeviceProfile{
			DirectPlayProfiles:  []jellyfin.DirectPlayProfile{{Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Type: "Video"}},
			TranscodingProfiles: []jellyfin.TranscodingProfile{{Container: "ts", VideoCodec: "h264", AudioCodec: "aac", Protocol: "hls", Type: "Video"}},
		},
	}, jfHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST PlaybackInfo mkv = %d: %s", resp.StatusCode, raw)
	}
	var pbMKV jellyfin.PlaybackInfoResponse
	if err := json.Unmarshal(raw, &pbMKV); err != nil || len(pbMKV.MediaSources) == 0 {
		t.Fatalf("decode PlaybackInfo mkv: %v (%s)", err, raw)
	}
	msMKV := pbMKV.MediaSources[0]
	if !msMKV.SupportsTranscoding || msMKV.TranscodingUrl == "" {
		t.Fatalf("expected HLS transcoding URL for mkv/ac3, got %+v", msMKV)
	}

	resp, masterBody := doReq(http.MethodGet, msMKV.TranscodingUrl, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET master.m3u8 = %d: %s", resp.StatusCode, masterBody)
	}
	mainLine := firstPlaylistLine(string(masterBody), "main.m3u8")
	if mainLine == "" {
		t.Fatalf("missing main.m3u8 in master playlist:\n%s", masterBody)
	}

	resp, mainBody := doReq(http.MethodGet, "/Videos/"+mkvEp.Id+"/"+mainLine, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(mainBody), "#EXT-X-ENDLIST") {
		t.Fatalf("GET main.m3u8 = %d: %s", resp.StatusCode, mainBody)
	}
	seg0Line := firstPlaylistLine(string(mainBody), ".ts")
	if seg0Line == "" {
		t.Fatalf("missing .ts segment in main.m3u8:\n%s", mainBody)
	}

	resp, segBytes := doReq(http.MethodGet, "/Videos/"+mkvEp.Id+"/"+seg0Line, nil, nil)
	if resp.StatusCode != http.StatusOK || len(segBytes) < 188 || segBytes[0] != 0x47 {
		t.Fatalf("GET HLS segment %s = %d len=%d firstByte=%#x", seg0Line, resp.StatusCode, len(segBytes), func() byte {
			if len(segBytes) > 0 {
				return segBytes[0]
			}
			return 0
		}())
	}

	// 4c. Fetch .vtt converted subtitle for 01_intro.mp4
	resp, raw = doReq(http.MethodPost, "/Items/"+mp4Ep.Id+"/PlaybackInfo", jellyfin.PlaybackInfoRequest{
		UserId: jfAuth.User.Id,
	}, jfHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST PlaybackInfo mp4 = %d: %s", resp.StatusCode, raw)
	}
	var pbMP4 jellyfin.PlaybackInfoResponse
	if err := json.Unmarshal(raw, &pbMP4); err != nil || len(pbMP4.MediaSources) == 0 {
		t.Fatalf("decode PlaybackInfo mp4: %v (%s)", err, raw)
	}
	var subURL string
	for _, st := range pbMP4.MediaSources[0].MediaStreams {
		if st.Type == "Subtitle" && st.DeliveryUrl != "" {
			subURL = st.DeliveryUrl
			break
		}
	}
	if subURL == "" {
		t.Fatalf("missing subtitle DeliveryUrl in %+v", pbMP4.MediaSources[0].MediaStreams)
	}
	resp, subBody := doReq(http.MethodGet, subURL, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(subBody), "WEBVTT") || !strings.Contains(string(subBody), "Welcome to GoCourse") {
		t.Fatalf("GET subtitle %s = %d: %s", subURL, resp.StatusCode, subBody)
	}

	// 5. Report progress via Jellyfin -> verify CourseFin /api/v1/home shows identical position_ticks
	const targetTicks int64 = 15000000
	resp, raw = doReq(http.MethodPost, "/Sessions/Playing/Progress", jellyfin.PlaybackProgressInfo{
		ItemId:        mp4Ep.Id,
		MediaSourceId: mp4Ep.MediaSources[0].Id,
		PlaySessionId: pbMP4.PlaySessionId,
		PositionTicks: targetTicks,
	}, jfHeaders)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /Sessions/Playing/Progress = %d: %s", resp.StatusCode, raw)
	}

	resp, raw = doReq(http.MethodGet, "/api/v1/home", nil, apiHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/home = %d: %s", resp.StatusCode, raw)
	}
	var homeRes struct {
		Resume []catalog.LessonWithContext `json:"resume"`
	}
	if err := json.Unmarshal(raw, &homeRes); err != nil {
		t.Fatalf("decode /api/v1/home: %v", err)
	}
	foundResume := false
	for _, rItem := range homeRes.Resume {
		if rItem.ID == mp4Ep.Id {
			foundResume = true
			if rItem.PositionTicks != targetTicks {
				t.Fatalf("resume position_ticks = %d, want %d", rItem.PositionTicks, targetTicks)
			}
		}
	}
	if !foundResume {
		t.Fatalf("expected lesson %s in /api/v1/home resume list, got %+v", mp4Ep.Id, homeRes.Resume)
	}

	// 6. GET /web/ serves the real built React SPA
	resp, webHTML := doReq(http.MethodGet, "/web/", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /web/ = %d", resp.StatusCode)
	}
	htmlStr := string(webHTML)
	if !strings.Contains(htmlStr, `<div id="root"`) || !strings.Contains(htmlStr, `/web/assets/`) {
		t.Fatalf("expected built React SPA HTML from /web/, got:\n%s", htmlStr)
	}
}
