package media_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"coursefin/internal/db"
	"coursefin/internal/id"
	"coursefin/internal/media"
	"coursefin/internal/scanner"
	"coursefin/internal/storage"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func openTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, dataDir
}

func seedSourceAndMedia(t *testing.T, conn *sql.DB, driver, rootPath, relPath, container, videoCodec, audioCodec string, durationMs int64, streamsJSON, thumbPath string) (sourceID, mediaID string) {
	t.Helper()
	sourceID = id.New()
	mediaID = id.New()
	now := time.Now().UTC().Format(time.RFC3339)

	if _, err := conn.Exec(
		`INSERT INTO storage_sources (id, name, driver, root_path, config_json, enabled, created_at, updated_at)
		 VALUES (?, 'Test Source', ?, ?, '{}', 1, ?, ?)`,
		sourceID, driver, rootPath, now, now,
	); err != nil {
		t.Fatalf("insert storage_source: %v", err)
	}

	if streamsJSON == "" {
		streamsJSON = "[]"
	}

	var fileSize int64
	if driver == "local" {
		if info, err := os.Stat(filepath.Join(rootPath, filepath.FromSlash(relPath))); err == nil {
			fileSize = info.Size()
		}
	}

	if _, err := conn.Exec(
		`INSERT INTO media_files (
			id, source_id, relative_path, file_name, file_size, mod_time, status,
			duration_ms, container, video_codec, audio_codec, width, height, bitrate,
			streams_json, thumbnail_path, probed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, 320, 240, 800000, ?, ?, ?, ?, ?)`,
		mediaID, sourceID, relPath, filepath.Base(relPath), fileSize, now,
		durationMs, container, videoCodec, audioCodec, streamsJSON, thumbPath, now, now, now,
	); err != nil {
		t.Fatalf("insert media_file: %v", err)
	}

	return sourceID, mediaID
}

func seedCourseHierarchy(t *testing.T, conn *sql.DB, mediaID, courseCover, lessonCover string) (libID, courseID, chapterID, lessonID string) {
	t.Helper()
	if err := conn.QueryRow(`SELECT id FROM libraries LIMIT 1`).Scan(&libID); err != nil {
		t.Fatalf("query default library: %v", err)
	}
	courseID = id.New()
	chapterID = id.New()
	lessonID = id.New()
	now := time.Now().UTC().Format(time.RFC3339)

	if _, err := conn.Exec(
		`INSERT INTO courses (id, library_id, title, sort_title, overview, instructor, cover_path, backdrop_path, created_at, updated_at)
		 VALUES (?, ?, 'Test Course', 'Test Course', '', '', ?, '', ?, ?)`,
		courseID, libID, courseCover, now, now,
	); err != nil {
		t.Fatalf("insert course: %v", err)
	}

	if _, err := conn.Exec(
		`INSERT INTO chapters (id, course_id, title, sort_order, overview, created_at, updated_at)
		 VALUES (?, ?, 'Chapter 1', 1, '', ?, ?)`,
		chapterID, courseID, now, now,
	); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}

	if _, err := conn.Exec(
		`INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, overview, sort_order, cover_path, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'Lesson 1', '', 1, ?, ?, ?)`,
		lessonID, courseID, chapterID, mediaID, lessonCover, now, now,
	); err != nil {
		t.Fatalf("insert lesson: %v", err)
	}

	return libID, courseID, chapterID, lessonID
}

type fakeRedirectProvider struct {
	targetURL string
}

func (f *fakeRedirectProvider) Walk(ctx context.Context, rootRelPath string) ([]storage.FileEntry, error) {
	return nil, nil
}
func (f *fakeRedirectProvider) Stat(ctx context.Context, relPath string) (storage.FileInfo, error) {
	return storage.FileInfo{RelPath: relPath, Name: filepath.Base(relPath), Size: 100}, nil
}
func (f *fakeRedirectProvider) Open(ctx context.Context, relPath string) (io.ReadSeekCloser, error) {
	return nil, os.ErrNotExist
}
func (f *fakeRedirectProvider) DirectURL(ctx context.Context, relPath string) (string, bool, error) {
	return f.targetURL, true, nil
}

func TestServeDirectPlaySupportsRangeAndHead(t *testing.T) {
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	payload := make([]byte, 2048)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	videoPath := filepath.Join(mediaRoot, "sample.mp4")
	if err := os.WriteFile(videoPath, payload, 0o644); err != nil {
		t.Fatalf("write sample.mp4: %v", err)
	}

	_, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "sample.mp4", "mp4", "h264", "aac", 10000, "", "")

	reg := storage.NewRegistry()
	svc := media.NewService(conn, dataDir, reg, "")
	defer svc.Close()

	// 1. HEAD request
	headReq := httptest.NewRequest(http.MethodHead, "/Videos/"+mediaID+"/stream.mp4", nil)
	headRec := httptest.NewRecorder()
	svc.ServeDirectPlay(headRec, headReq, mediaID)

	if headRec.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", headRec.Code)
	}
	if got := headRec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("HEAD Accept-Ranges = %q, want bytes", got)
	}
	if got := headRec.Header().Get("Content-Length"); got != "2048" {
		t.Errorf("HEAD Content-Length = %q, want 2048", got)
	}
	if got := headRec.Header().Get("ETag"); got == "" {
		t.Errorf("HEAD ETag should be non-empty")
	}
	if headRec.Body.Len() != 0 {
		t.Errorf("HEAD body length = %d, want 0", headRec.Body.Len())
	}

	// 2. GET with Range: bytes=100-199
	rangeReq := httptest.NewRequest(http.MethodGet, "/Videos/"+mediaID+"/stream.mp4", nil)
	rangeReq.Header.Set("Range", "bytes=100-199")
	rangeRec := httptest.NewRecorder()
	svc.ServeDirectPlay(rangeRec, rangeReq, mediaID)

	if rangeRec.Code != http.StatusPartialContent {
		t.Fatalf("GET Range status = %d, want 206", rangeRec.Code)
	}
	if got := rangeRec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Range Accept-Ranges = %q, want bytes", got)
	}
	if got := rangeRec.Header().Get("Content-Range"); got != "bytes 100-199/2048" {
		t.Errorf("Range Content-Range = %q, want bytes 100-199/2048", got)
	}
	if got := rangeRec.Header().Get("Content-Length"); got != "100" {
		t.Errorf("Range Content-Length = %q, want 100", got)
	}
	if !bytes.Equal(rangeRec.Body.Bytes(), payload[100:200]) {
		t.Fatalf("Range body bytes did not match payload[100:200]")
	}

	// 3. DirectURL 302 redirect hook
	reg.Register("mockredirect", func(rootPath, configJSON string) (storage.StorageProvider, error) {
		return &fakeRedirectProvider{targetURL: "https://cdn.example.com/direct/video.mp4"}, nil
	})
	_, redirMediaID := seedSourceAndMedia(t, conn, "mockredirect", "/remote", "video.mp4", "mp4", "h264", "aac", 5000, "", "")
	redirReq := httptest.NewRequest(http.MethodGet, "/Videos/"+redirMediaID+"/stream.mp4", nil)
	redirRec := httptest.NewRecorder()
	svc.ServeDirectPlay(redirRec, redirReq, redirMediaID)
	if redirRec.Code != http.StatusFound {
		t.Fatalf("DirectURL status = %d, want 302", redirRec.Code)
	}
	if got := redirRec.Header().Get("Location"); got != "https://cdn.example.com/direct/video.mp4" {
		t.Errorf("DirectURL Location = %q, want https://cdn.example.com/direct/video.mp4", got)
	}
}

func TestCanRemuxVideoCodecRejectsLegacyCodecs(t *testing.T) {
	conn, dataDir := openTestDB(t)
	svc := media.NewService(conn, dataDir, nil, "")
	defer svc.Close()

	for _, c := range []string{"h264", "H264", "hevc", "av1", "vp9"} {
		if !svc.CanRemuxVideoCodec(c) {
			t.Errorf("CanRemuxVideoCodec(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"wmv3", "rv40", "mpeg2video", "mpeg4", "vc1", ""} {
		if svc.CanRemuxVideoCodec(c) {
			t.Errorf("CanRemuxVideoCodec(%q) = true, want false", c)
		}
	}

	for _, c := range []string{"h264", "H264", "avc", "avc1"} {
		if !svc.CanWebRemuxVideoCodec(c) || !media.CanWebRemuxVideoCodec(c) {
			t.Errorf("CanWebRemuxVideoCodec(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"hevc", "h265", "av1", "vp9", "wmv3", ""} {
		if svc.CanWebRemuxVideoCodec(c) || media.CanWebRemuxVideoCodec(c) {
			t.Errorf("CanWebRemuxVideoCodec(%q) = true, want false", c)
		}
	}

	for _, c := range []string{"aac", "mp3"} {
		if svc.NeedsAudioTranscode(c) || media.NeedsAudioTranscode(c) {
			t.Errorf("NeedsAudioTranscode(%q) = true, want false", c)
		}
	}
	for _, c := range []string{"ac3", "eac3", "dts", "flac", "opus", "pcm_s16le", "pcm_f32le"} {
		if !svc.NeedsAudioTranscode(c) || !media.NeedsAudioTranscode(c) {
			t.Errorf("NeedsAudioTranscode(%q) = false, want true", c)
		}
	}
}

func resolveTestFFmpeg(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	for _, cand := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/usr/bin/ffmpeg"} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	t.Skip("ffmpeg not available")
	return ""
}

func TestVODHLSPlaylistAndLateSegmentSeek(t *testing.T) {
	ffmpegBin := resolveTestFFmpeg(t)
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	mkvPath := filepath.Join(mediaRoot, "lesson.mkv")
	cmd := exec.Command(
		ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=20:size=320x240:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=20",
		"-c:v", "libx264", "-g", "30",
		"-c:a", "aac",
		mkvPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test mkv failed: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber("", ffmpegBin)
	probeRes, err := prober.Probe(context.Background(), mkvPath)
	if err != nil {
		t.Fatalf("probe test mkv failed: %v", err)
	}
	if probeRes.DurationMs < 19000 || probeRes.DurationMs > 21000 {
		t.Fatalf("unexpected durationMs = %d", probeRes.DurationMs)
	}

	_, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "lesson.mkv", "mkv", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
	_, _, _, lessonID := seedCourseHierarchy(t, conn, mediaID, "", "")

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), ffmpegBin)
	defer svc.Close()

	sessionID := "playsess123"

	// 1. Master playlist propagates query params
	masterReq := httptest.NewRequest(http.MethodGet, "/Videos/"+lessonID+"/master.m3u8?PlaySessionId="+sessionID+"&api_key=tok123", nil)
	masterRec := httptest.NewRecorder()
	svc.ServeMasterPlaylist(masterRec, masterReq, lessonID, sessionID)
	if masterRec.Code != http.StatusOK {
		t.Fatalf("master.m3u8 status = %d, want 200 (body: %s)", masterRec.Code, masterRec.Body.String())
	}
	masterBody := masterRec.Body.String()
	if !strings.Contains(masterBody, "#EXTM3U") || !strings.Contains(masterBody, "main.m3u8?") {
		t.Fatalf("unexpected master.m3u8:\n%s", masterBody)
	}
	if !strings.Contains(masterBody, "PlaySessionId="+sessionID) || !strings.Contains(masterBody, "api_key=tok123") {
		t.Fatalf("master.m3u8 missing propagated query params:\n%s", masterBody)
	}

	// 2. Media playlist synthesizes full VOD #EXTM3U with 4 segments (0..3)
	mediaReq := httptest.NewRequest(http.MethodGet, "/Videos/"+lessonID+"/main.m3u8?PlaySessionId="+sessionID+"&api_key=tok123", nil)
	mediaRec := httptest.NewRecorder()
	svc.ServeMediaPlaylist(mediaRec, mediaReq, lessonID, sessionID)
	if mediaRec.Code != http.StatusOK {
		t.Fatalf("main.m3u8 status = %d, want 200", mediaRec.Code)
	}
	playlistBody := mediaRec.Body.String()
	for _, want := range []string{
		"#EXTM3U",
		"#EXT-X-PLAYLIST-TYPE:VOD",
		"#EXT-X-TARGETDURATION:6",
		"#EXT-X-ENDLIST",
		"hls1/main/0.ts?",
		"hls1/main/1.ts?",
		"hls1/main/2.ts?",
		"hls1/main/3.ts?",
		"PlaySessionId=" + sessionID,
		"api_key=tok123",
	} {
		if !strings.Contains(playlistBody, want) {
			t.Fatalf("main.m3u8 missing %q:\n%s", want, playlistBody)
		}
	}

	// 3. Seek directly to segment 2 (t=12s) WITHOUT requesting 0 or 1
	segReq := httptest.NewRequest(http.MethodGet, "/Videos/"+lessonID+"/hls1/main/2.ts?PlaySessionId="+sessionID+"&api_key=tok123", nil)
	segRec := httptest.NewRecorder()
	svc.ServeHLSSegment(segRec, segReq, lessonID, sessionID, 2)
	if segRec.Code != http.StatusOK {
		t.Fatalf("segment 2 status = %d, want 200 (body: %s)", segRec.Code, segRec.Body.String())
	}
	if ct := segRec.Header().Get("Content-Type"); !strings.Contains(ct, "video/mp2t") {
		t.Errorf("segment 2 Content-Type = %q, want video/mp2t", ct)
	}
	tsBytes := segRec.Body.Bytes()
	if len(tsBytes) < 188*2 || tsBytes[0] != 0x47 || tsBytes[188] != 0x47 {
		t.Fatalf("segment 2 does not look like valid MPEG-TS (len=%d, b0=0x%02x)", len(tsBytes), func() byte {
			if len(tsBytes) > 0 {
				return tsBytes[0]
			}
			return 0
		}())
	}

	sessDir := filepath.Join(dataDir, "cache", "remux", lessonID+"_"+sessionID)
	for _, skipped := range []string{"0.ts", "1.ts"} {
		if _, err := os.Stat(filepath.Join(sessDir, skipped)); !os.IsNotExist(err) {
			t.Fatalf("expected %s NOT to be generated on disk during late segment seek", skipped)
		}
	}

	// 4. StopRemuxSession cleans up session directory
	svc.StopRemuxSession(sessionID)
	if _, err := os.Stat(sessDir); !os.IsNotExist(err) {
		t.Fatalf("expected session dir %s to be deleted after StopRemuxSession", sessDir)
	}
}

func TestSRTToVTTConversion(t *testing.T) {
	srtInput := "\xEF\xBB\xBF1\r\n00:00:01,500 --> 00:00:04,200\r\n你好，课程开始了\r\nHello, welcome to the course!\r\n\r\n2\r\n00:00:05,000 --> 00:00:08,125\r\n第二句字幕, 包含逗号\r\n"
	got := media.ConvertSRTToVTT(srtInput)

	if !strings.HasPrefix(got, "WEBVTT\n\n") {
		t.Fatalf("expected WEBVTT header, got:\n%s", got)
	}
	for _, want := range []string{
		"00:00:01.500 --> 00:00:04.200",
		"你好，课程开始了",
		"Hello, welcome to the course!",
		"00:00:05.000 --> 00:00:08.125",
		"第二句字幕, 包含逗号",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("converted VTT missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "00:00:01,500") {
		t.Fatalf("converted VTT still contains comma timestamp:\n%s", got)
	}
}

func TestServeSubtitleSidecarAndEmbedded(t *testing.T) {
	ffmpegBin := resolveTestFFmpeg(t)
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	// Create sidecar .srt
	srtPath := filepath.Join(mediaRoot, "lesson.zh.srt")
	srtData := "1\n00:00:00,500 --> 00:00:02,500\n中文字幕测试\n"
	if err := os.WriteFile(srtPath, []byte(srtData), 0o644); err != nil {
		t.Fatalf("write sidecar srt: %v", err)
	}

	// Create .mkv with embedded subtitle stream at index 2
	embSrt := filepath.Join(mediaRoot, "emb.srt")
	if err := os.WriteFile(embSrt, []byte("1\n00:00:00,200 --> 00:00:01,800\nEmbedded cue\n"), 0o644); err != nil {
		t.Fatalf("write emb.srt: %v", err)
	}
	mkvPath := filepath.Join(mediaRoot, "lesson.mkv")
	cmd := exec.Command(
		ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=160x120:rate=15",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-i", embSrt,
		"-map", "0:v:0", "-map", "1:a:0", "-map", "2:s:0",
		"-c:v", "libx264", "-c:a", "aac", "-c:s", "srt",
		mkvPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate mkv with embedded sub: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber("", ffmpegBin)
	probeRes, err := prober.Probe(context.Background(), mkvPath)
	if err != nil {
		t.Fatalf("probe mkv: %v", err)
	}

	sourceID, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "lesson.mkv", "mkv", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
	if _, err := conn.Exec(
		`INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
		 VALUES (?, ?, 'lesson.zh.srt', ?, 'zh', '中文', 'srt', 1, 'active')`,
		id.New(), sourceID, mediaID,
	); err != nil {
		t.Fatalf("insert subtitle_file: %v", err)
	}

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), ffmpegBin)
	defer svc.Close()

	// Embedded subtitle is at index 2; external sidecar should be at index 3.
	embReq := httptest.NewRequest(http.MethodGet, "/Videos/"+mediaID+"/"+mediaID+"/Subtitles/2/Stream.vtt", nil)
	embRec := httptest.NewRecorder()
	svc.ServeSubtitle(embRec, embReq, mediaID, 2, "vtt")
	if embRec.Code != http.StatusOK {
		t.Fatalf("embedded subtitle status = %d, want 200 (body: %s)", embRec.Code, embRec.Body.String())
	}
	if !strings.Contains(embRec.Body.String(), "WEBVTT") || !strings.Contains(embRec.Body.String(), "Embedded cue") {
		t.Fatalf("unexpected embedded VTT:\n%s", embRec.Body.String())
	}

	extReq := httptest.NewRequest(http.MethodGet, "/Videos/"+mediaID+"/"+mediaID+"/Subtitles/3/Stream.vtt", nil)
	extRec := httptest.NewRecorder()
	svc.ServeSubtitle(extRec, extReq, mediaID, 3, "vtt")
	if extRec.Code != http.StatusOK {
		t.Fatalf("sidecar subtitle status = %d, want 200 (body: %s)", extRec.Code, extRec.Body.String())
	}
	if !strings.Contains(extRec.Body.String(), "WEBVTT") || !strings.Contains(extRec.Body.String(), "中文字幕测试") {
		t.Fatalf("unexpected sidecar VTT:\n%s", extRec.Body.String())
	}
}

func writeTestJPEG(t *testing.T, path string, w, h int, c color.RGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpeg: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
}

func TestServeImageFallbackAndResizeCache(t *testing.T) {
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	thumbName := "thumb_lesson1.jpg"
	writeTestJPEG(t, filepath.Join(dataDir, "thumbs", thumbName), 640, 360, color.RGBA{R: 30, G: 140, B: 220, A: 255})

	_, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "ep1.mp4", "mp4", "h264", "aac", 10000, "", thumbName)
	_, courseID, _, _ := seedCourseHierarchy(t, conn, mediaID, "", "")

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), "")
	defer svc.Close()

	// 1. Course without custom cover falls back to first lesson's thumbnail and resizes to maxWidth=200
	req1 := httptest.NewRequest(http.MethodGet, "/Items/"+courseID+"/Images/Primary?maxWidth=200", nil)
	rec1 := httptest.NewRecorder()
	svc.ServeImage(rec1, req1, courseID, "Primary", 200, 0)
	if rec1.Code != http.StatusOK {
		t.Fatalf("ServeImage status = %d, want 200 (body: %s)", rec1.Code, rec1.Body.String())
	}
	if ct := rec1.Header().Get("Content-Type"); !strings.Contains(ct, "image/jpeg") {
		t.Errorf("ServeImage Content-Type = %q, want image/jpeg", ct)
	}
	etag1 := rec1.Header().Get("ETag")
	if etag1 == "" {
		t.Fatalf("expected non-empty ETag")
	}

	decoded, err := jpeg.Decode(bytes.NewReader(rec1.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode resized jpeg: %v", err)
	}
	if decoded.Bounds().Dx() != 200 {
		t.Fatalf("resized width = %d, want 200", decoded.Bounds().Dx())
	}

	cacheEntries, err := os.ReadDir(filepath.Join(dataDir, "cache", "images"))
	if err != nil || len(cacheEntries) == 0 {
		t.Fatalf("expected cached image written to cache/images, err=%v count=%d", err, len(cacheEntries))
	}

	// 2. Conditional GET with matching If-None-Match returns 304 Not Modified
	req2 := httptest.NewRequest(http.MethodGet, "/Items/"+courseID+"/Images/Primary?maxWidth=200", nil)
	req2.Header.Set("If-None-Match", etag1)
	rec2 := httptest.NewRecorder()
	svc.ServeImage(rec2, req2, courseID, "Primary", 200, 0)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional ServeImage status = %d, want 304", rec2.Code)
	}

	// 3. Updating course cover busts cache and returns new ETag
	newCover := courseID + ".jpg"
	writeTestJPEG(t, filepath.Join(dataDir, "covers", newCover), 800, 400, color.RGBA{R: 220, G: 60, B: 40, A: 255})
	if _, err := conn.Exec(`UPDATE courses SET cover_path = ? WHERE id = ?`, newCover, courseID); err != nil {
		t.Fatalf("update course cover_path: %v", err)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/Items/"+courseID+"/Images/Primary?maxWidth=200", nil)
	req3.Header.Set("If-None-Match", etag1)
	rec3 := httptest.NewRecorder()
	svc.ServeImage(rec3, req3, courseID, "Primary", 200, 0)
	if rec3.Code != http.StatusOK {
		t.Fatalf("after cover update status = %d, want 200", rec3.Code)
	}
	if rec3.Header().Get("ETag") == etag1 {
		t.Fatalf("expected ETag to change after cover update")
	}
}

func TestVODHLSRemuxTranscodesAC3AudioAndSeekJumps(t *testing.T) {
	ffmpegBin := resolveTestFFmpeg(t)
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	mkvPath := filepath.Join(mediaRoot, "ac3_lesson.mkv")
	cmd := exec.Command(
		ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=20:size=320x240:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=20",
		"-c:v", "libx264", "-g", "30",
		"-c:a", "ac3", "-b:a", "192k",
		mkvPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate ac3 mkv failed: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber("", ffmpegBin)
	probeRes, err := prober.Probe(context.Background(), mkvPath)
	if err != nil {
		t.Fatalf("probe ac3 mkv failed: %v", err)
	}
	if probeRes.AudioCodec != "ac3" {
		t.Fatalf("expected ac3 audio codec, got %q", probeRes.AudioCodec)
	}

	_, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "ac3_lesson.mkv", "mkv", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
	_, _, _, lessonID := seedCourseHierarchy(t, conn, mediaID, "", "")

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), ffmpegBin)
	defer svc.Close()

	sessionID := "ac3sess"

	// Request segment 2 (forces -ss 12 + -c:a aac), then sequential segment 3, then backward seek to 0.
	for _, segIdx := range []int{2, 3, 0} {
		req := httptest.NewRequest(http.MethodGet, "/Videos/"+lessonID+"/hls1/main/seg.ts", nil)
		rec := httptest.NewRecorder()
		svc.ServeHLSSegment(rec, req, lessonID, sessionID, segIdx)
		if rec.Code != http.StatusOK {
			t.Fatalf("segment %d status = %d, want 200 (body: %s)", segIdx, rec.Code, rec.Body.String())
		}
		b := rec.Body.Bytes()
		if len(b) < 188 || b[0] != 0x47 {
			t.Fatalf("segment %d invalid MPEG-TS output (len=%d)", segIdx, len(b))
		}
	}

	// Verify Close() removes remux session cache directory.
	if err := svc.Close(); err != nil {
		t.Fatalf("svc.Close failed: %v", err)
	}
	sessDir := filepath.Join(dataDir, "cache", "remux", lessonID+"_"+sessionID)
	if _, err := os.Stat(sessDir); !os.IsNotExist(err) {
		t.Fatalf("expected Close() to clean %s", sessDir)
	}
}

func TestRemuxConcurrencyUnderRace(t *testing.T) {
	ffmpegBin := resolveTestFFmpeg(t)
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	mkvPath := filepath.Join(mediaRoot, "conc.mkv")
	cmd := exec.Command(
		ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "testsrc=duration=14:size=320x240:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=14",
		"-c:v", "libx264", "-g", "30",
		"-c:a", "aac",
		mkvPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate conc mkv failed: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber("", ffmpegBin)
	probeRes, err := prober.Probe(context.Background(), mkvPath)
	if err != nil {
		t.Fatalf("probe conc mkv failed: %v", err)
	}

	_, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "conc.mkv", "mkv", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
	_, _, _, lessonID := seedCourseHierarchy(t, conn, mediaID, "", "")

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), ffmpegBin)
	defer svc.Close()

	var wg sync.WaitGroup
	errs := make(chan string, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/Videos/"+lessonID+"/hls1/main/1.ts", nil)
			rec := httptest.NewRecorder()
			svc.ServeHLSSegment(rec, req, lessonID, "concsess", 1)
			if rec.Code != http.StatusOK {
				errs <- rec.Body.String()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("concurrent ServeHLSSegment failed: %s", e)
	}
}

func TestDirectPlayAndSubtitleAndImageEdgeCases(t *testing.T) {
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	payload := []byte("0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(filepath.Join(mediaRoot, "clip.mp4"), payload, 0o644); err != nil {
		t.Fatalf("write clip.mp4: %v", err)
	}

	streamsJSON := `[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle"}]`
	_, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "clip.mp4", "mp4", "h264", "", 10000, streamsJSON, "thumb1.png")
	libID, courseID, chapterID, lessonID := seedCourseHierarchy(t, conn, mediaID, "", "")

	// Write PNG thumbnail & library cover & course backdrop
	pngImg := image.NewRGBA(image.Rect(0, 0, 300, 150))
	if err := os.MkdirAll(filepath.Join(dataDir, "thumbs"), 0o755); err != nil {
		t.Fatalf("mkdir thumbs: %v", err)
	}
	tf, err := os.Create(filepath.Join(dataDir, "thumbs", "thumb1.png"))
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	if err := png.Encode(tf, pngImg); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	tf.Close()

	libCover := libID + ".jpg"
	writeTestJPEG(t, filepath.Join(dataDir, "covers", libCover), 400, 400, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	if _, err := conn.Exec(`UPDATE libraries SET cover_path = ? WHERE id = ?`, libCover, libID); err != nil {
		t.Fatalf("update library cover: %v", err)
	}
	bdCover := courseID + "_bd.jpg"
	writeTestJPEG(t, filepath.Join(dataDir, "covers", bdCover), 500, 250, color.RGBA{R: 50, G: 60, B: 70, A: 255})
	if _, err := conn.Exec(`UPDATE courses SET backdrop_path = ? WHERE id = ?`, bdCover, courseID); err != nil {
		t.Fatalf("update course backdrop: %v", err)
	}

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), "")
	defer svc.Close()

	// Dashed UUID lookup via lessonID fallback on ServeDirectPlay + HEAD with Range
	dashedLessonID := lessonID[:8] + "-" + lessonID[8:12] + "-" + lessonID[12:16] + "-" + lessonID[16:20] + "-" + lessonID[20:]
	req := httptest.NewRequest(http.MethodHead, "/Videos/"+dashedLessonID+"/stream.mp4", nil)
	req.Header.Set("Range", "bytes=0-7")
	rec := httptest.NewRecorder()
	svc.ServeDirectPlay(rec, req, dashedLessonID)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("lesson fallback HEAD Range status = %d, want 206", rec.Code)
	}

	// Missing media ID returns 404
	missingRec := httptest.NewRecorder()
	svc.ServeDirectPlay(missingRec, httptest.NewRequest(http.MethodGet, "/Videos/missing/stream.mp4", nil), id.New())
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing media status = %d, want 404", missingRec.Code)
	}

	// Bitmap subtitle returns 415
	bmpRec := httptest.NewRecorder()
	svc.ServeSubtitle(bmpRec, httptest.NewRequest(http.MethodGet, "/sub.vtt", nil), mediaID, 1, "vtt")
	if bmpRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("bitmap subtitle status = %d, want 415", bmpRec.Code)
	}

	// Library, Chapter fallback, Lesson thumbnail fallback, Course Backdrop
	for _, tc := range []struct {
		name     string
		id       string
		imgType  string
		maxW     int
		wantCode int
	}{
		{"library_primary", libID, "Primary", 100, http.StatusOK},
		{"chapter_fallback", chapterID, "Primary", 100, http.StatusOK},
		{"lesson_thumb", lessonID, "Primary", 150, http.StatusOK},
		{"course_backdrop", courseID, "Backdrop", 0, http.StatusOK},
		{"unknown_item", id.New(), "Primary", 0, http.StatusNotFound},
	} {
		imReq := httptest.NewRequest(http.MethodGet, "/Items/"+tc.id+"/Images/"+tc.imgType, nil)
		imRec := httptest.NewRecorder()
		svc.ServeImage(imRec, imReq, tc.id, tc.imgType, tc.maxW, 0)
		if imRec.Code != tc.wantCode {
			t.Errorf("%s status = %d, want %d (body: %s)", tc.name, imRec.Code, tc.wantCode, imRec.Body.String())
		}
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for file %s", path)
}

func TestVODHLSForwardBackwardForwardSeek(t *testing.T) {
	ffmpegBin := resolveTestFFmpeg(t)
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()
	ctrlDir := t.TempDir()

	mkvPath := filepath.Join(mediaRoot, "long.mkv")
	genCmd := exec.Command(
		ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "color=c=blue:size=160x120:rate=15:duration=60",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=60",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "15",
		"-c:a", "aac",
		mkvPath,
	)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate 60s mkv failed: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber("", ffmpegBin)
	probeRes, err := prober.Probe(context.Background(), mkvPath)
	if err != nil {
		t.Fatalf("probe 60s mkv failed: %v", err)
	}

	_, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "long.mkv", "mkv", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
	_, _, _, lessonID := seedCourseHierarchy(t, conn, mediaID, "", "")

	logFile := filepath.Join(ctrlDir, "invocations.log")
	holdSeg0 := filepath.Join(ctrlDir, "hold_seg0")
	if err := os.WriteFile(holdSeg0, []byte("1"), 0o644); err != nil {
		t.Fatalf("write hold_seg0: %v", err)
	}

	// Wrapper runs real ffmpeg for segment 9 and segment 5, while holding segment 0's process
	// open after generating only 0.ts..1.ts so stale 9.ts remains on disk during the backward run.
	wrapScript := fmt.Sprintf(`#!/bin/sh
START_NUM=""
PREV=""
for arg in "$@"; do
  if [ "$PREV" = "-start_number" ]; then
    START_NUM="$arg"
  fi
  PREV="$arg"
done
echo "start_number=$START_NUM" >> %q
if [ "$START_NUM" = "0" ] && [ -f %q ]; then
  rm -f %q
  ARGS=""
  OUT=""
  for arg in "$@"; do
    OUT="$arg"
  done
  # Run real ffmpeg capped at 7s to emit 0.ts + 1.ts + live.m3u8, then keep process alive.
  set -- "$@"
  %q -hide_banner -loglevel error -ss 0 -i %q -t 7 -map 0:v:0 -map '0:a:0?' -c:v copy -c:a copy -f hls -hls_time 6 -start_number 0 -hls_segment_filename "$(dirname "$OUT")/%%d.ts" -hls_playlist_type event -hls_flags temp_file -output_ts_offset 0 -hls_segment_options mpegts_copyts=1 -y "$OUT"
  exec sleep 10
fi
exec %q "$@"
`, logFile, holdSeg0, holdSeg0, ffmpegBin, mkvPath, ffmpegBin)

	wrapBin := filepath.Join(ctrlDir, "ffmpeg_wrap.sh")
	if err := os.WriteFile(wrapBin, []byte(wrapScript), 0o755); err != nil {
		t.Fatalf("write wrap script: %v", err)
	}

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), wrapBin)
	defer svc.Close()

	sessionID := "seekseq"
	sessDir := filepath.Join(dataDir, "cache", "remux", lessonID+"_"+sessionID)

	// 1. Seek forward to segment 9 -> produces 9.ts on disk.
	rec9 := httptest.NewRecorder()
	svc.ServeHLSSegment(rec9, httptest.NewRequest(http.MethodGet, "/9.ts", nil), lessonID, sessionID, 9)
	if rec9.Code != http.StatusOK || rec9.Body.Len() < 188 || rec9.Body.Bytes()[0] != 0x47 {
		t.Fatalf("segment 9 failed: status=%d len=%d body=%s", rec9.Code, rec9.Body.Len(), rec9.Body.String())
	}

	// 2. Seek backward to segment 0 -> active run writes 0.ts, 1.ts, live.m3u8, while stale 9.ts stays in sessDir.
	rec0 := httptest.NewRecorder()
	svc.ServeHLSSegment(rec0, httptest.NewRequest(http.MethodGet, "/0.ts", nil), lessonID, sessionID, 0)
	if rec0.Code != http.StatusOK || rec0.Body.Len() < 188 || rec0.Body.Bytes()[0] != 0x47 {
		t.Fatalf("segment 0 failed: status=%d len=%d body=%s", rec0.Code, rec0.Body.Len(), rec0.Body.String())
	}

	// 3. Seek forward to segment 5 (5 > 1+3, between 0 and stale 9.ts).
	// Must NOT mistake stale 9.ts for active progress; must restart FFmpeg at -ss 30 (start_number=5).
	start := time.Now()
	rec5 := httptest.NewRecorder()
	svc.ServeHLSSegment(rec5, httptest.NewRequest(http.MethodGet, "/5.ts", nil), lessonID, sessionID, 5)
	elapsed := time.Since(start)
	if rec5.Code != http.StatusOK || rec5.Body.Len() < 188 || rec5.Body.Bytes()[0] != 0x47 {
		t.Fatalf("segment 5 failed: status=%d len=%d body=%s", rec5.Code, rec5.Body.Len(), rec5.Body.String())
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("segment 5 took too long (%v), likely stalled behind t=0 run", elapsed)
	}

	if _, err := os.Stat(filepath.Join(sessDir, "4.ts")); !os.IsNotExist(err) {
		t.Fatalf("expected 4.ts NOT to exist when restarting at segment 5")
	}

	logBytes, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read invocations.log: %v", err)
	}
	logStr := string(logBytes)
	for _, want := range []string{"start_number=9", "start_number=0", "start_number=5"} {
		if !strings.Contains(logStr, want) {
			t.Fatalf("expected %q in invocations.log, got:\n%s", want, logStr)
		}
	}
}

func TestVODHLSConcurrentSeekReturns409(t *testing.T) {
	ffmpegBin := resolveTestFFmpeg(t)
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()
	ctrlDir := t.TempDir()

	mkvPath := filepath.Join(mediaRoot, "concseek.mkv")
	genCmd := exec.Command(
		ffmpegBin,
		"-y",
		"-f", "lavfi", "-i", "color=c=red:size=160x120:rate=15:duration=50",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=50",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "15",
		"-c:a", "aac",
		mkvPath,
	)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate 50s mkv failed: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber("", ffmpegBin)
	probeRes, err := prober.Probe(context.Background(), mkvPath)
	if err != nil {
		t.Fatalf("probe 50s mkv failed: %v", err)
	}

	_, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "concseek.mkv", "mkv", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
	_, _, _, lessonID := seedCourseHierarchy(t, conn, mediaID, "", "")

	holdSeg1 := filepath.Join(ctrlDir, "hold_seg1")
	inflightSeg1 := filepath.Join(ctrlDir, "seg1_inflight")
	if err := os.WriteFile(holdSeg1, []byte("1"), 0o644); err != nil {
		t.Fatalf("write hold_seg1: %v", err)
	}

	wrapScript := fmt.Sprintf(`#!/bin/sh
START_NUM=""
PREV=""
for arg in "$@"; do
  if [ "$PREV" = "-start_number" ]; then
    START_NUM="$arg"
  fi
  PREV="$arg"
done
if [ "$START_NUM" = "1" ] && [ -f %q ]; then
  rm -f %q
  touch %q
  exec sleep 10
fi
exec %q "$@"
`, holdSeg1, holdSeg1, inflightSeg1, ffmpegBin)

	wrapBin := filepath.Join(ctrlDir, "ffmpeg_wrap.sh")
	if err := os.WriteFile(wrapBin, []byte(wrapScript), 0o755); err != nil {
		t.Fatalf("write wrap script: %v", err)
	}

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), wrapBin)
	defer svc.Close()

	sessionID := "conc409"
	rec1 := httptest.NewRecorder()
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		svc.ServeHLSSegment(rec1, httptest.NewRequest(http.MethodGet, "/1.ts", nil), lessonID, sessionID, 1)
	}()

	waitForFile(t, inflightSeg1, 5*time.Second)
	// Give the waiter goroutine a moment to settle into <-done before cancelling.
	time.Sleep(30 * time.Millisecond)

	rec2 := httptest.NewRecorder()
	svc.ServeHLSSegment(rec2, httptest.NewRequest(http.MethodGet, "/7.ts", nil), lessonID, sessionID, 7)
	if rec2.Code != http.StatusOK || rec2.Body.Len() < 188 || rec2.Body.Bytes()[0] != 0x47 {
		t.Fatalf("seek segment 7 failed: status=%d len=%d body=%s", rec2.Code, rec2.Body.Len(), rec2.Body.String())
	}

	<-doneCh
	if rec1.Code != http.StatusConflict {
		t.Fatalf("superseded segment 1 status = %d, want 409 Conflict (body: %s)", rec1.Code, rec1.Body.String())
	}
}

func TestDecodeSubtitleToUTF8AndServeNonUTF8SRT(t *testing.T) {
	sample := "1\n00:00:00,500 --> 00:00:02,000\n你好，课程字幕测试！\n"

	// UTF-8 with BOM
	utf8BOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte(sample)...)
	if got := media.DecodeSubtitleToUTF8(utf8BOM); got != sample {
		t.Fatalf("UTF-8 BOM decode mismatch: got %q", got)
	}

	// UTF-16 LE with BOM
	leBytes, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(sample))
	if err != nil {
		t.Fatalf("encode UTF-16LE: %v", err)
	}
	if got := media.DecodeSubtitleToUTF8(leBytes); got != sample {
		t.Fatalf("UTF-16LE decode mismatch: got %q", got)
	}

	// UTF-16 BE with BOM
	beBytes, err := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(sample))
	if err != nil {
		t.Fatalf("encode UTF-16BE: %v", err)
	}
	if got := media.DecodeSubtitleToUTF8(beBytes); got != sample {
		t.Fatalf("UTF-16BE decode mismatch: got %q", got)
	}

	// GB18030 / GBK encoded Chinese SRT
	gbkBytes, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(sample))
	if err != nil {
		t.Fatalf("encode GB18030: %v", err)
	}
	if got := media.DecodeSubtitleToUTF8(gbkBytes); got != sample {
		t.Fatalf("GB18030 decode mismatch: got %q", got)
	}

	// Serve GBK external SRT as VTT via Service.ServeSubtitle
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(mediaRoot, "lesson.mp4"), []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write mp4: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaRoot, "lesson.zh.srt"), gbkBytes, 0o644); err != nil {
		t.Fatalf("write gbk srt: %v", err)
	}
	sourceID, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "lesson.mp4", "mp4", "h264", "aac", 5000, `[{"index":0,"codec_type":"video","codec_name":"h264"}]`, "")
	if _, err := conn.Exec(
		`INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
		 VALUES (?, ?, 'lesson.zh.srt', ?, 'zh', '中文', 'srt', 1, 'active')`,
		id.New(), sourceID, mediaID,
	); err != nil {
		t.Fatalf("insert subtitle: %v", err)
	}

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), "")
	defer svc.Close()

	rec := httptest.NewRecorder()
	svc.ServeSubtitle(rec, httptest.NewRequest(http.MethodGet, "/sub.vtt", nil), mediaID, 1, "vtt")
	if rec.Code != http.StatusOK {
		t.Fatalf("ServeSubtitle status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "WEBVTT") || !strings.Contains(body, "你好，课程字幕测试！") {
		t.Fatalf("expected valid UTF-8 WebVTT output, got %q", body)
	}
}

func findTestBinary(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		cand := filepath.Join(dir, name)
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}
	return ""
}

func TestHasFFmpegDetection(t *testing.T) {
	conn, dataDir := openTestDB(t)
	missingSvc := media.NewService(conn, dataDir, nil, "/nonexistent/coursefin-ffmpeg-bin")
	defer missingSvc.Close()
	if missingSvc.HasFFmpeg() {
		t.Fatalf("expected HasFFmpeg() == false for nonexistent explicit binary path")
	}

	if ffmpegBin := findTestBinary("ffmpeg"); ffmpegBin != "" {
		okSvc := media.NewService(conn, dataDir, nil, ffmpegBin)
		defer okSvc.Close()
		if !okSvc.HasFFmpeg() {
			t.Fatalf("expected HasFFmpeg() == true for %s", ffmpegBin)
		}
	}
}

func TestServeMasterPlaylistPropagatesTranscodeVideoAndTranscodesMPEG4(t *testing.T) {
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	ffmpegBin := findTestBinary("ffmpeg")
	ffprobeBin := findTestBinary("ffprobe")

	// Seed a wmv3 record first to test playlist propagation & 415 vs 200 contract
	wmvPath := filepath.Join(mediaRoot, "legacy.wmv")
	if err := os.WriteFile(wmvPath, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write wmv: %v", err)
	}
	_, wmvMF := seedSourceAndMedia(t, conn, "local", mediaRoot, "legacy.wmv", "wmv", "wmv3", "wmav2", 12000, "", "")
	_, _, _, wmvLesson := seedCourseHierarchy(t, conn, wmvMF, "", "")

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), ffmpegBin)
	defer svc.Close()

	// Without TranscodeVideo=1, wmv3 must still return 415 (preserving Jellyfin contract)
	rec415 := httptest.NewRecorder()
	svc.ServeMasterPlaylist(rec415, httptest.NewRequest(http.MethodGet, "/Videos/"+wmvLesson+"/master.m3u8?PlaySessionId=s1", nil), wmvLesson, "s1")
	if rec415.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("wmv3 master.m3u8 without TranscodeVideo = %d, want 415", rec415.Code)
	}

	if ffmpegBin == "" {
		return
	}

	// With TranscodeVideo=1, master.m3u8 and main.m3u8 succeed and propagate TranscodeVideo=1
	recMaster := httptest.NewRecorder()
	svc.ServeMasterPlaylist(recMaster, httptest.NewRequest(http.MethodGet, "/Videos/"+wmvLesson+"/master.m3u8?PlaySessionId=s1&TranscodeVideo=1&api_key=tok", nil), wmvLesson, "s1")
	if recMaster.Code != http.StatusOK || !strings.Contains(recMaster.Body.String(), "TranscodeVideo=1") {
		t.Fatalf("master.m3u8 with TranscodeVideo=1: status=%d body=%s", recMaster.Code, recMaster.Body.String())
	}

	recMedia := httptest.NewRecorder()
	svc.ServeMediaPlaylist(recMedia, httptest.NewRequest(http.MethodGet, "/Videos/"+wmvLesson+"/main.m3u8?PlaySessionId=s1&TranscodeVideo=1&api_key=tok", nil), wmvLesson, "s1")
	if recMedia.Code != http.StatusOK || !strings.Contains(recMedia.Body.String(), "TranscodeVideo=1") {
		t.Fatalf("main.m3u8 with TranscodeVideo=1: status=%d body=%s", recMedia.Code, recMedia.Body.String())
	}

	// Generate a real MPEG-4 (.avi) video and transcode segment 0.ts -> verify h264 + yuv420p
	aviPath := filepath.Join(mediaRoot, "sample_mpeg4.avi")
	genCmd := exec.Command(
		ffmpegBin, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=blue:size=160x120:rate=15:duration=8",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=8",
		"-c:v", "mpeg4", "-c:a", "mp3",
		aviPath,
	)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate mpeg4 avi: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber(ffprobeBin, ffmpegBin)
	probeRes, err := prober.Probe(context.Background(), aviPath)
	if err != nil {
		t.Fatalf("probe mpeg4 avi: %v", err)
	}
	_, aviMF := seedSourceAndMedia(t, conn, "local", mediaRoot, "sample_mpeg4.avi", "avi", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
	_, _, _, aviLesson := seedCourseHierarchy(t, conn, aviMF, "", "")

	segRec := httptest.NewRecorder()
	svc.ServeHLSSegment(segRec, httptest.NewRequest(http.MethodGet, "/Videos/"+aviLesson+"/hls1/main/0.ts?PlaySessionId=tc1&TranscodeVideo=1", nil), aviLesson, "tc1", 0)
	if segRec.Code != http.StatusOK || segRec.Body.Len() < 188 || segRec.Body.Bytes()[0] != 0x47 {
		t.Fatalf("transcode segment 0.ts failed: status=%d len=%d body=%s", segRec.Code, segRec.Body.Len(), segRec.Body.String())
	}

	if ffprobeBin != "" {
		outTS := filepath.Join(t.TempDir(), "out0.ts")
		if err := os.WriteFile(outTS, segRec.Body.Bytes(), 0o644); err != nil {
			t.Fatalf("write out0.ts: %v", err)
		}
		pCmd := exec.Command(ffprobeBin, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name,pix_fmt", "-of", "csv=p=0", outTS)
		pOut, err := pCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ffprobe out0.ts: %v (%s)", err, string(pOut))
		}
		firstLine := strings.TrimSpace(strings.Split(strings.TrimSpace(string(pOut)), "\n")[0])
		if firstLine != "h264,yuv420p" {
			t.Fatalf("transcoded segment codec/pix_fmt = %q (raw %q), want h264,yuv420p", firstLine, string(pOut))
		}
	}
}

func TestTranscodeOddDimensionVideoSucceeds(t *testing.T) {
	ffmpegBin := findTestBinary("ffmpeg")
	ffprobeBin := findTestBinary("ffprobe")
	if ffmpegBin == "" {
		t.Skip("ffmpeg not available")
	}

	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	// Odd resolution 319x179 (common in window screen recordings)
	oddPath := filepath.Join(mediaRoot, "odd_screen.avi")
	genCmd := exec.Command(
		ffmpegBin, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=green:size=319x179:rate=15:duration=6",
		"-c:v", "mpeg4",
		oddPath,
	)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate odd dimension video: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber(ffprobeBin, ffmpegBin)
	probeRes, err := prober.Probe(context.Background(), oddPath)
	if err != nil {
		t.Fatalf("probe odd video: %v", err)
	}
	_, mfID := seedSourceAndMedia(t, conn, "local", mediaRoot, "odd_screen.avi", "avi", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
	_, _, _, lessonID := seedCourseHierarchy(t, conn, mfID, "", "")

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), ffmpegBin)
	defer svc.Close()

	segRec := httptest.NewRecorder()
	svc.ServeHLSSegment(segRec, httptest.NewRequest(http.MethodGet, "/Videos/"+lessonID+"/hls1/main/0.ts?PlaySessionId=odd1&TranscodeVideo=1", nil), lessonID, "odd1", 0)
	if segRec.Code != http.StatusOK || segRec.Body.Len() < 188 || segRec.Body.Bytes()[0] != 0x47 {
		t.Fatalf("odd-dimension transcode failed: status=%d len=%d body=%s", segRec.Code, segRec.Body.Len(), segRec.Body.String())
	}

	if ffprobeBin != "" {
		outTS := filepath.Join(t.TempDir(), "odd0.ts")
		if err := os.WriteFile(outTS, segRec.Body.Bytes(), 0o644); err != nil {
			t.Fatalf("write odd0.ts: %v", err)
		}
		pCmd := exec.Command(ffprobeBin, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name,pix_fmt,width,height", "-of", "csv=p=0", outTS)
		pOut, err := pCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ffprobe odd0.ts: %v (%s)", err, string(pOut))
		}
		firstLine := strings.TrimSpace(strings.Split(strings.TrimSpace(string(pOut)), "\n")[0])
		if firstLine != "h264,320,180,yuv420p" && firstLine != "h264,yuv420p,320,180" {
			t.Fatalf("odd transcoded stream = %q, want h264 + yuv420p + 320x180", firstLine)
		}
	}
}

func TestBuildAndServeAIGeneratedSubtitle(t *testing.T) {
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(mediaRoot, "lesson.mp4"), []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write lesson.mp4: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaRoot, "lesson.en.srt"), []byte("1\n00:00:00,500 --> 00:00:02,000\nSidecar EN\n"), 0o644); err != nil {
		t.Fatalf("write sidecar srt: %v", err)
	}

	streamsJSON := `[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"audio","codec_name":"aac"},{"index":2,"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng"}}]`
	sourceID, mediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "lesson.mp4", "mp4", "h264", "aac", 12000, streamsJSON, "")

	if _, err := conn.Exec(
		`INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
		 VALUES (?, ?, 'lesson.en.srt', ?, 'en', 'English', 'srt', 1, 'active')`,
		id.New(), sourceID, mediaID,
	); err != nil {
		t.Fatalf("insert subtitle_files: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	vttPayload := "WEBVTT\n\n00:00:01.000 --> 00:00:04.000\n你好 AI 字幕\n"
	if _, err := conn.Exec(
		`INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at)
		 VALUES (?, 'asr', 'zh', 'whisper-1', ?, '你好 AI 字幕', ?, ?)`,
		mediaID, vttPayload, now, now,
	); err != nil {
		t.Fatalf("insert media_transcripts asr: %v", err)
	}

	ffmpegBin := findTestBinary("ffmpeg")
	svc := media.NewService(conn, dataDir, storage.NewRegistry(), ffmpegBin)
	defer svc.Close()

	streams, err := svc.ResolveStreams(context.Background(), mediaID)
	if err != nil {
		t.Fatalf("ResolveStreams failed: %v", err)
	}
	// Expected: 3 embedded (0, 1, 2) + 1 sidecar (3) + 1 AI generated (4)
	if len(streams) != 5 {
		t.Fatalf("ResolveStreams len = %d, want 5: %+v", len(streams), streams)
	}

	sidecar := streams[3]
	if sidecar.Index != 3 || !sidecar.IsExternal || sidecar.IsAIGenerated {
		t.Fatalf("unexpected sidecar descriptor: %+v", sidecar)
	}

	aiTrack := streams[4]
	if aiTrack.Index != 4 {
		t.Errorf("aiTrack.Index = %d, want 4 (strictly after sidecar)", aiTrack.Index)
	}
	if !aiTrack.IsExternal || !aiTrack.IsAIGenerated || aiTrack.IsDefault {
		t.Errorf("unexpected aiTrack flags: IsExternal=%v IsAIGenerated=%v IsDefault=%v", aiTrack.IsExternal, aiTrack.IsAIGenerated, aiTrack.IsDefault)
	}
	if aiTrack.Language != "zh" || aiTrack.Title != "AI 转写字幕" || aiTrack.Format != "vtt" || aiTrack.Codec != "vtt" || aiTrack.Type != "subtitle" {
		t.Errorf("unexpected aiTrack metadata: %+v", aiTrack)
	}

	// Serve AI subtitle as VTT
	vttRec := httptest.NewRecorder()
	svc.ServeSubtitle(vttRec, httptest.NewRequest(http.MethodGet, "/sub.vtt", nil), mediaID, aiTrack.Index, "vtt")
	if vttRec.Code != http.StatusOK {
		t.Fatalf("ServeSubtitle(vtt) status = %d, want 200 (body: %s)", vttRec.Code, vttRec.Body.String())
	}
	if ct := vttRec.Header().Get("Content-Type"); ct != "text/vtt; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/vtt; charset=utf-8", ct)
	}
	if !strings.Contains(vttRec.Body.String(), "你好 AI 字幕") {
		t.Errorf("VTT body missing AI cue: %q", vttRec.Body.String())
	}

	// Serve AI subtitle converted to SRT when ffmpeg is available
	if ffmpegBin != "" {
		srtRec := httptest.NewRecorder()
		svc.ServeSubtitle(srtRec, httptest.NewRequest(http.MethodGet, "/sub.srt", nil), mediaID, aiTrack.Index, "srt")
		if srtRec.Code != http.StatusOK {
			t.Fatalf("ServeSubtitle(srt) status = %d, want 200 (body: %s)", srtRec.Code, srtRec.Body.String())
		}
		if ct := srtRec.Header().Get("Content-Type"); ct != "application/x-subrip; charset=utf-8" {
			t.Errorf("SRT Content-Type = %q, want application/x-subrip; charset=utf-8", ct)
		}
		if !strings.Contains(srtRec.Body.String(), "00:00:01,000 --> 00:00:04,000") || !strings.Contains(srtRec.Body.String(), "你好 AI 字幕") {
			t.Errorf("SRT body unexpected: %q", srtRec.Body.String())
		}
	}

	// Unsupported target format on AI subtitle returns 415
	badFmtRec := httptest.NewRecorder()
	svc.ServeSubtitle(badFmtRec, httptest.NewRequest(http.MethodGet, "/sub.pgs", nil), mediaID, aiTrack.Index, "pgs")
	if badFmtRec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("ServeSubtitle(pgs) status = %d, want 415", badFmtRec.Code)
	}

	// Empty language in media_transcripts defaults to "und"
	if _, err := conn.Exec(`UPDATE media_transcripts SET language = '' WHERE media_file_id = ?`, mediaID); err != nil {
		t.Fatalf("update language empty: %v", err)
	}
	streamsDefaultLang, err := svc.ResolveStreams(context.Background(), mediaID)
	if err != nil {
		t.Fatalf("ResolveStreams empty lang failed: %v", err)
	}
	if got := streamsDefaultLang[len(streamsDefaultLang)-1].Language; got != "und" {
		t.Errorf("empty language fallback = %q, want und", got)
	}

	// Manual transcript must NOT be exposed in ResolveStreams
	_, manualMediaID := seedSourceAndMedia(t, conn, "local", mediaRoot, "lesson.mp4", "mp4", "h264", "aac", 12000, streamsJSON, "")
	if _, err := conn.Exec(
		`INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at)
		 VALUES (?, 'manual', 'zh', '', ?, '手动字幕', ?, ?)`,
		manualMediaID, vttPayload, now, now,
	); err != nil {
		t.Fatalf("insert manual media_transcripts: %v", err)
	}
	manualStreams, err := svc.ResolveStreams(context.Background(), manualMediaID)
	if err != nil {
		t.Fatalf("ResolveStreams manual failed: %v", err)
	}
	for _, st := range manualStreams {
		if st.IsAIGenerated {
			t.Fatalf("manual transcript must not be exposed as AI stream: %+v", st)
		}
	}
	missingSubRec := httptest.NewRecorder()
	svc.ServeSubtitle(missingSubRec, httptest.NewRequest(http.MethodGet, "/sub.vtt", nil), manualMediaID, 99, "vtt")
	if missingSubRec.Code != http.StatusNotFound {
		t.Errorf("non-existent subtitle index status = %d, want 404", missingSubRec.Code)
	}
}

func TestExtractBestSubtitleVTTPriority(t *testing.T) {
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(mediaRoot, "video.mp4"), []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write video.mp4: %v", err)
	}

	// Prepare GBK-encoded Chinese SRT and UTF-8 English SRT
	zhSRT := "1\n00:00:01,000 --> 00:00:03,000\n你好，世界中文课程\n"
	gbkBytes, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(zhSRT))
	if err != nil {
		t.Fatalf("encode GB18030: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaRoot, "sub.zh.srt"), gbkBytes, 0o644); err != nil {
		t.Fatalf("write sub.zh.srt: %v", err)
	}
	enSRT := "1\n00:00:01,000 --> 00:00:03,000\nHello English Subtitle\n"
	if err := os.WriteFile(filepath.Join(mediaRoot, "sub.en.srt"), []byte(enSRT), 0o644); err != nil {
		t.Fatalf("write sub.en.srt: %v", err)
	}

	// 1. Neither external is default -> Chinese external beats English external and embedded stream
	streamsWithEmbedded := `[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"subtitle","codec_name":"subrip","disposition":{"default":1},"tags":{"language":"eng"}}]`
	sourceID, mediaID1 := seedSourceAndMedia(t, conn, "local", mediaRoot, "video.mp4", "mp4", "h264", "", 10000, streamsWithEmbedded, "")
	if _, err := conn.Exec(
		`INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
		 VALUES (?, ?, 'sub.en.srt', ?, 'en', 'English', 'srt', 0, 'active'),
		        (?, ?, 'sub.zh.srt', ?, 'zh', '中文', 'srt', 0, 'active')`,
		id.New(), sourceID, mediaID1,
		id.New(), sourceID, mediaID1,
	); err != nil {
		t.Fatalf("insert external subtitles: %v", err)
	}

	ffmpegBin := findTestBinary("ffmpeg")
	svc := media.NewService(conn, dataDir, storage.NewRegistry(), ffmpegBin)
	defer svc.Close()

	vtt, srcType, srcLabel, found, err := svc.ExtractBestSubtitleVTT(ctx, mediaID1)
	if err != nil {
		t.Fatalf("ExtractBestSubtitleVTT(mediaID1) err = %v", err)
	}
	if !found || srcType != "external_subtitle" {
		t.Fatalf("got found=%v srcType=%q, want true, external_subtitle", found, srcType)
	}
	if !strings.Contains(srcLabel, "外挂字幕") || !strings.Contains(srcLabel, "zh") {
		t.Errorf("unexpected srcLabel = %q", srcLabel)
	}
	if !strings.HasPrefix(vtt, "WEBVTT") || !strings.Contains(vtt, "你好，世界中文课程") {
		t.Fatalf("expected UTF-8 WebVTT from GBK Chinese SRT, got:\n%s", vtt)
	}

	// 2. When English external is explicitly is_default=1, it beats non-default Chinese external
	sourceID2, mediaID2 := seedSourceAndMedia(t, conn, "local", mediaRoot, "video.mp4", "mp4", "h264", "", 10000, streamsWithEmbedded, "")
	if _, err := conn.Exec(
		`INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
		 VALUES (?, ?, 'sub.en.srt', ?, 'en', 'English', 'srt', 1, 'active'),
		        (?, ?, 'sub.zh.srt', ?, 'zh', '中文', 'srt', 0, 'active')`,
		id.New(), sourceID2, mediaID2,
		id.New(), sourceID2, mediaID2,
	); err != nil {
		t.Fatalf("insert external subtitles case 2: %v", err)
	}
	vtt2, srcType2, _, found2, err := svc.ExtractBestSubtitleVTT(ctx, mediaID2)
	if err != nil {
		t.Fatalf("ExtractBestSubtitleVTT(mediaID2) err = %v", err)
	}
	if !found2 || srcType2 != "external_subtitle" || !strings.Contains(vtt2, "Hello English Subtitle") {
		t.Fatalf("expected default EN external subtitle, got found=%v srcType=%q vtt=%q", found2, srcType2, vtt2)
	}

	// 3. Only bitmap subtitle + AI transcript -> ExtractBestSubtitleVTT skips both and returns found=false, err=nil
	bitmapStreams := `[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"zh"}}]`
	_, mediaIDBitmap := seedSourceAndMedia(t, conn, "local", mediaRoot, "video.mp4", "mp4", "h264", "", 10000, bitmapStreams, "")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(
		`INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at)
		 VALUES (?, 'asr', 'zh', 'whisper-1', 'WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nAI 字幕\n', 'AI 字幕', ?, ?)`,
		mediaIDBitmap, now, now,
	); err != nil {
		t.Fatalf("insert ai transcript on bitmap media: %v", err)
	}
	vttBmp, srcBmp, labelBmp, foundBmp, err := svc.ExtractBestSubtitleVTT(ctx, mediaIDBitmap)
	if err != nil {
		t.Fatalf("ExtractBestSubtitleVTT(bitmap) err = %v, want nil", err)
	}
	if foundBmp || vttBmp != "" || srcBmp != "" || labelBmp != "" {
		t.Fatalf("expected found=false for bitmap+AI-only media, got found=%v src=%q label=%q vtt=%q", foundBmp, srcBmp, labelBmp, vttBmp)
	}

	// 4. Embedded text subtitle extraction when no external subtitle exists
	if ffmpegBin != "" {
		embEn := filepath.Join(mediaRoot, "emb_en.srt")
		embZh := filepath.Join(mediaRoot, "emb_zh.srt")
		if err := os.WriteFile(embEn, []byte("1\n00:00:00,100 --> 00:00:01,500\nEmbedded EN\n"), 0o644); err != nil {
			t.Fatalf("write emb_en: %v", err)
		}
		if err := os.WriteFile(embZh, []byte("1\n00:00:00,100 --> 00:00:01,500\n内嵌中文字幕\n"), 0o644); err != nil {
			t.Fatalf("write emb_zh: %v", err)
		}
		mkvPath := filepath.Join(mediaRoot, "multi_emb.mkv")
		genCmd := exec.Command(
			ffmpegBin, "-y",
			"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=15",
			"-i", embEn,
			"-i", embZh,
			"-map", "0:v:0", "-map", "1:s:0", "-map", "2:s:0",
			"-metadata:s:s:0", "language=eng",
			"-metadata:s:s:1", "language=chi",
			"-disposition:s:0", "0",
			"-disposition:s:1", "0",
			"-c:v", "libx264", "-c:s", "srt",
			mkvPath,
		)
		if out, err := genCmd.CombinedOutput(); err != nil {
			t.Fatalf("generate multi_emb.mkv: %v\n%s", err, string(out))
		}
		prober := scanner.NewFFmpegProber("", ffmpegBin)
		probeRes, err := prober.Probe(ctx, mkvPath)
		if err != nil {
			t.Fatalf("probe multi_emb.mkv: %v", err)
		}
		_, mediaIDEmb := seedSourceAndMedia(t, conn, "local", mediaRoot, "multi_emb.mkv", "mkv", probeRes.VideoCodec, probeRes.AudioCodec, probeRes.DurationMs, probeRes.StreamsJSON, "")
		vttEmb, srcTypeEmb, srcLabelEmb, foundEmb, err := svc.ExtractBestSubtitleVTT(ctx, mediaIDEmb)
		if err != nil {
			t.Fatalf("ExtractBestSubtitleVTT(embedded) err = %v", err)
		}
		if !foundEmb || srcTypeEmb != "embedded_subtitle" || !strings.Contains(srcLabelEmb, "内嵌字幕") {
			t.Fatalf("unexpected embedded result: found=%v srcType=%q srcLabel=%q", foundEmb, srcTypeEmb, srcLabelEmb)
		}
		if !strings.HasPrefix(vttEmb, "WEBVTT") || !strings.Contains(vttEmb, "内嵌中文字幕") {
			t.Fatalf("expected Chinese embedded subtitle to beat English, got:\n%s", vttEmb)
		}
	}
}

func TestResolveLocalMediaInfoDetectsAudio(t *testing.T) {
	conn, dataDir := openTestDB(t)
	mediaRoot := t.TempDir()
	ctx := context.Background()

	videoPath := filepath.Join(mediaRoot, "clip.mp4")
	if err := os.WriteFile(videoPath, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write clip.mp4: %v", err)
	}
	wantAbsPath, err := filepath.EvalSymlinks(videoPath)
	if err != nil {
		wantAbsPath = videoPath
	}

	_, withAudioID := seedSourceAndMedia(t, conn, "local", mediaRoot, "clip.mp4", "mp4", "h264", "aac", 45000,
		`[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"audio","codec_name":"aac"}]`, "")
	_, videoOnlyID := seedSourceAndMedia(t, conn, "local", mediaRoot, "clip.mp4", "mp4", "h264", "", 45000,
		`[{"index":0,"codec_type":"video","codec_name":"h264"}]`, "")
	_, emptyStreamsID := seedSourceAndMedia(t, conn, "local", mediaRoot, "clip.mp4", "mp4", "h264", "", 45000,
		`[]`, "")

	svc := media.NewService(conn, dataDir, storage.NewRegistry(), "/nonexistent/coursefin-ffmpeg-bin")
	defer svc.Close()

	if svc.FFmpegBin() != "/nonexistent/coursefin-ffmpeg-bin" {
		t.Errorf("FFmpegBin() = %q, want /nonexistent/coursefin-ffmpeg-bin", svc.FFmpegBin())
	}
	if ffprobeBin := findTestBinary("ffprobe"); ffprobeBin != "" {
		if !svc.HasFFprobe() || svc.FFprobeBin() == "" {
			t.Errorf("expected HasFFprobe() == true and non-empty FFprobeBin(), got %v %q", svc.HasFFprobe(), svc.FFprobeBin())
		}
	}

	infoAudio, err := svc.ResolveLocalMediaInfo(ctx, withAudioID)
	if err != nil {
		t.Fatalf("ResolveLocalMediaInfo(withAudio) err = %v", err)
	}
	if !infoAudio.HasAudio || infoAudio.MediaFileID != withAudioID || infoAudio.DurationMs != 45000 || infoAudio.AbsPath != wantAbsPath {
		t.Fatalf("unexpected infoAudio: %+v (want AbsPath=%q)", infoAudio, wantAbsPath)
	}

	infoVideoOnly, err := svc.ResolveLocalMediaInfo(ctx, videoOnlyID)
	if err != nil {
		t.Fatalf("ResolveLocalMediaInfo(videoOnly) err = %v", err)
	}
	if infoVideoOnly.HasAudio {
		t.Errorf("expected HasAudio == false for video-only streams_json, got true")
	}

	infoEmpty, err := svc.ResolveLocalMediaInfo(ctx, emptyStreamsID)
	if err != nil {
		t.Fatalf("ResolveLocalMediaInfo(empty) err = %v", err)
	}
	if !infoEmpty.HasAudio {
		t.Errorf("expected HasAudio == true for empty streams_json (let FFmpeg decide), got false")
	}
}
