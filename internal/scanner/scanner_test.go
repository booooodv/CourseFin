package scanner_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"coursefin/internal/db"
	"coursefin/internal/id"
	"coursefin/internal/scanner"
	"coursefin/internal/storage"
)

func TestMatchSubtitlesByBasenameAndLanguage(t *testing.T) {
	candidates := []string{
		"course/01_intro.srt",
		"course/01_intro.zh.srt",
		"course/01_intro.en.vtt",
		"course/01_intro.zh-cn.ass",
		"course/01_intro.chs.ssa",
		"course/01_intro.ja.srt",
		"course/02_other.srt",
		"course/sub/01_intro.srt",
		"course/01_intro.txt",
	}

	matches := scanner.MatchSubtitlesForVideo("course/01_intro.mp4", candidates)

	got := make(map[string]scanner.SubtitleMatch, len(matches))
	for _, m := range matches {
		got[m.RelPath] = m
	}

	want := map[string]struct {
		lang   string
		format string
	}{
		"course/01_intro.srt":       {lang: "und", format: "srt"},
		"course/01_intro.zh.srt":    {lang: "zh", format: "srt"},
		"course/01_intro.en.vtt":    {lang: "en", format: "vtt"},
		"course/01_intro.zh-cn.ass": {lang: "zh", format: "ass"},
		"course/01_intro.chs.ssa":   {lang: "zh", format: "ssa"},
		"course/01_intro.ja.srt":    {lang: "ja", format: "srt"},
	}

	if len(got) != len(want) {
		t.Fatalf("matched %d subtitles (%+v), want %d", len(got), got, len(want))
	}

	for rel, exp := range want {
		m, ok := got[rel]
		if !ok {
			t.Fatalf("expected subtitle %q to match", rel)
		}
		if m.Language != exp.lang {
			t.Errorf("%q language = %q, want %q", rel, m.Language, exp.lang)
		}
		if m.Format != exp.format {
			t.Errorf("%q format = %q, want %q", rel, m.Format, exp.format)
		}
	}

	for _, ignored := range []string{"course/02_other.srt", "course/sub/01_intro.srt", "course/01_intro.txt"} {
		if _, ok := got[ignored]; ok {
			t.Fatalf("expected %q to be ignored", ignored)
		}
	}
}

type fakeProber struct {
	probeCalls atomic.Int32
	thumbCalls atomic.Int32
}

func (f *fakeProber) Probe(ctx context.Context, absPath string) (*scanner.ProbeResult, error) {
	f.probeCalls.Add(1)
	return &scanner.ProbeResult{
		DurationMs:  120000,
		Container:   "mp4",
		VideoCodec:  "h264",
		AudioCodec:  "aac",
		Width:       1920,
		Height:      1080,
		Bitrate:     1500000,
		StreamsJSON: `[{"index":0,"codec_type":"video","codec_name":"h264"}]`,
	}, nil
}

func (f *fakeProber) ExtractThumbnail(ctx context.Context, absPath string, seekSec float64, outPath string) error {
	f.thumbCalls.Add(1)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}, 0o644)
}

func TestIncrementalScanProbesNewSkipsUnchangedMarksMissingAndRestores(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mediaRoot := t.TempDir()

	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer conn.Close()

	// Seed a storage source.
	sourceID := id.New()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(
		`INSERT INTO storage_sources (id, name, driver, root_path, config_json, enabled, created_at, updated_at)
		 VALUES (?, 'Test Source', 'local', ?, '{}', 1, ?, ?)`,
		sourceID, mediaRoot, now, now,
	); err != nil {
		t.Fatalf("insert storage_sources failed: %v", err)
	}

	// Create 2 videos and 1 sidecar subtitle.
	v1Path := filepath.Join(mediaRoot, "01_intro.mp4")
	v2Path := filepath.Join(mediaRoot, "ch1", "02_setup.mkv")
	s1Path := filepath.Join(mediaRoot, "01_intro.zh.srt")

	if err := os.MkdirAll(filepath.Dir(v2Path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	v1Bytes := []byte("video-1-payload")
	if err := os.WriteFile(v1Path, v1Bytes, 0o644); err != nil {
		t.Fatalf("write v1 failed: %v", err)
	}
	if err := os.WriteFile(v2Path, []byte("video-2-payload"), 0o644); err != nil {
		t.Fatalf("write v2 failed: %v", err)
	}
	if err := os.WriteFile(s1Path, []byte("1\n00:00:00,000 --> 00:00:01,000\n你好\n"), 0o644); err != nil {
		t.Fatalf("write s1 failed: %v", err)
	}

	v1Info, err := os.Stat(v1Path)
	if err != nil {
		t.Fatalf("stat v1 failed: %v", err)
	}
	v1ModTime := v1Info.ModTime()

	prober := &fakeProber{}
	reg := storage.NewRegistry()
	svc := scanner.NewService(conn, dataDir, reg, prober)

	// 1. Initial scan.
	rep1, err := svc.ScanSource(ctx, sourceID)
	if err != nil {
		t.Fatalf("initial ScanSource failed: %v", err)
	}
	if prober.probeCalls.Load() != 2 {
		t.Fatalf("probeCalls = %d, want 2", prober.probeCalls.Load())
	}
	if prober.thumbCalls.Load() != 2 {
		t.Fatalf("thumbCalls = %d, want 2", prober.thumbCalls.Load())
	}
	if rep1.AddedVideos != 2 || rep1.ProbedVideos != 2 {
		t.Fatalf("unexpected initial ScanReport: %+v", rep1)
	}

	var v1ID, thumbPath, status string
	if err := conn.QueryRow(
		`SELECT id, thumbnail_path, status FROM media_files WHERE source_id = ? AND relative_path = '01_intro.mp4'`,
		sourceID,
	).Scan(&v1ID, &thumbPath, &status); err != nil {
		t.Fatalf("query v1 failed: %v", err)
	}
	if status != "active" || thumbPath == "" {
		t.Fatalf("unexpected v1 row: status=%q thumbPath=%q", status, thumbPath)
	}

	// Verify subtitle linked to v1.
	var subMediaID sql.NullString
	var subLang string
	if err := conn.QueryRow(
		`SELECT media_file_id, language FROM subtitle_files WHERE source_id = ? AND relative_path = '01_intro.zh.srt'`,
		sourceID,
	).Scan(&subMediaID, &subLang); err != nil {
		t.Fatalf("query subtitle failed: %v", err)
	}
	if !subMediaID.Valid || subMediaID.String != v1ID || subLang != "zh" {
		t.Fatalf("unexpected subtitle link: media_file_id=%v, lang=%q, want %q/zh", subMediaID, subLang, v1ID)
	}

	// Verify storage_sources.last_scanned_at was updated.
	var lastScanned sql.NullString
	if err := conn.QueryRow(`SELECT last_scanned_at FROM storage_sources WHERE id = ?`, sourceID).Scan(&lastScanned); err != nil {
		t.Fatalf("query last_scanned_at failed: %v", err)
	}
	if !lastScanned.Valid || lastScanned.String == "" {
		t.Fatalf("expected last_scanned_at to be populated")
	}

	// Create a course/chapter/lesson referencing v1ID to ensure missing files never break lessons.
	var libID string
	if err := conn.QueryRow(`SELECT id FROM libraries LIMIT 1`).Scan(&libID); err != nil {
		t.Fatalf("query default library failed: %v", err)
	}
	courseID, chapID, lessonID := id.New(), id.New(), id.New()
	if _, err := conn.Exec(`INSERT INTO courses (id, library_id, title, created_at, updated_at) VALUES (?, ?, 'Course', ?, ?)`, courseID, libID, now, now); err != nil {
		t.Fatalf("insert course failed: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO chapters (id, course_id, title, sort_order, created_at, updated_at) VALUES (?, ?, 'Ch1', 1, ?, ?)`, chapID, courseID, now, now); err != nil {
		t.Fatalf("insert chapter failed: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, 'L1', 1, ?, ?)`, lessonID, courseID, chapID, v1ID, now, now); err != nil {
		t.Fatalf("insert lesson failed: %v", err)
	}

	// 2. Second scan with unchanged files must call Probe 0 times.
	prober.probeCalls.Store(0)
	prober.thumbCalls.Store(0)
	rep2, err := svc.ScanSource(ctx, sourceID)
	if err != nil {
		t.Fatalf("second ScanSource failed: %v", err)
	}
	if prober.probeCalls.Load() != 0 {
		t.Fatalf("expected 0 probe calls on unchanged scan, got %d", prober.probeCalls.Load())
	}
	if rep2.SkippedVideos != 2 {
		t.Fatalf("SkippedVideos = %d, want 2", rep2.SkippedVideos)
	}

	// 3. Removing 1 video marks status='missing' without deleting media_files or lessons row.
	if err := os.Remove(v1Path); err != nil {
		t.Fatalf("remove v1 failed: %v", err)
	}
	rep3, err := svc.ScanSource(ctx, sourceID)
	if err != nil {
		t.Fatalf("third ScanSource failed: %v", err)
	}
	if rep3.MissingVideos != 1 {
		t.Fatalf("MissingVideos = %d, want 1", rep3.MissingVideos)
	}
	if err := conn.QueryRow(`SELECT status FROM media_files WHERE id = ?`, v1ID).Scan(&status); err != nil {
		t.Fatalf("expected v1 row to still exist: %v", err)
	}
	if status != "missing" {
		t.Fatalf("status = %q, want missing", status)
	}
	var lessonCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons WHERE id = ?`, lessonID).Scan(&lessonCount); err != nil || lessonCount != 1 {
		t.Fatalf("expected lesson row to remain intact: count=%d err=%v", lessonCount, err)
	}

	// 4. Restoring the video flips status back to 'active' (and preserves ID).
	if err := os.WriteFile(v1Path, v1Bytes, 0o644); err != nil {
		t.Fatalf("restore v1 failed: %v", err)
	}
	if err := os.Chtimes(v1Path, v1ModTime, v1ModTime); err != nil {
		t.Fatalf("chtimes v1 failed: %v", err)
	}
	rep4, err := svc.ScanSource(ctx, sourceID)
	if err != nil {
		t.Fatalf("fourth ScanSource failed: %v", err)
	}
	if rep4.RestoredVideos != 1 {
		t.Fatalf("RestoredVideos = %d, want 1", rep4.RestoredVideos)
	}
	if err := conn.QueryRow(`SELECT status FROM media_files WHERE id = ?`, v1ID).Scan(&status); err != nil {
		t.Fatalf("query restored v1 failed: %v", err)
	}
	if status != "active" {
		t.Fatalf("restored status = %q, want active", status)
	}
	if st := svc.Status(sourceID); st.Running || st.LastReport == nil || st.LastReport.RestoredVideos != 1 {
		t.Fatalf("unexpected ScanState after scan: %+v", st)
	}

	// 5. Modifying v1 size triggers re-probe while keeping the same media_files.id.
	prober.probeCalls.Store(0)
	if err := os.WriteFile(v1Path, []byte("video-1-payload-modified"), 0o644); err != nil {
		t.Fatalf("modify v1 failed: %v", err)
	}
	rep5, err := svc.ScanSource(ctx, sourceID)
	if err != nil {
		t.Fatalf("fifth ScanSource failed: %v", err)
	}
	if rep5.UpdatedVideos != 1 || prober.probeCalls.Load() != 1 {
		t.Fatalf("expected UpdatedVideos=1 and probeCalls=1, got rep=%+v calls=%d", rep5, prober.probeCalls.Load())
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM media_files WHERE id = ?`, v1ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected media_files id %q to be preserved after re-probe", v1ID)
	}
}

type blockingProber struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingProber) Probe(ctx context.Context, absPath string) (*scanner.ProbeResult, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &scanner.ProbeResult{DurationMs: 1000, Container: "mp4", VideoCodec: "h264", StreamsJSON: "[]"}, nil
}

func (b *blockingProber) ExtractThumbnail(ctx context.Context, absPath string, seekSec float64, outPath string) error {
	return os.WriteFile(outPath, []byte{0xff, 0xd8, 0xff, 0xd9}, 0o644)
}

func TestScanPerSourceLockAndScanAllEnabled(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	enabledRoot := t.TempDir()
	disabledRoot := t.TempDir()

	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer conn.Close()

	if err := os.WriteFile(filepath.Join(enabledRoot, "a.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatalf("write enabled video failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(disabledRoot, "b.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatalf("write disabled video failed: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	srcEnabled := id.New()
	srcDisabled := id.New()
	if _, err := conn.Exec(
		`INSERT INTO storage_sources (id, name, driver, root_path, config_json, enabled, created_at, updated_at)
		 VALUES (?, 'Enabled', 'local', ?, '{}', 1, ?, ?), (?, 'Disabled', 'local', ?, '{}', 0, ?, ?)`,
		srcEnabled, enabledRoot, now, now, srcDisabled, disabledRoot, now, now,
	); err != nil {
		t.Fatalf("insert storage_sources failed: %v", err)
	}

	bp := &blockingProber{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	svc := scanner.NewService(conn, dataDir, storage.NewRegistry(), bp)

	done := make(chan error, 1)
	go func() {
		_, err := svc.ScanSource(ctx, srcEnabled)
		done <- err
	}()

	<-bp.started
	if !svc.Status(srcEnabled).Running {
		t.Fatalf("expected Status.Running == true during active scan")
	}
	if _, err := svc.ScanSource(ctx, srcEnabled); !errors.Is(err, scanner.ErrScanInProgress) {
		t.Fatalf("expected ErrScanInProgress on overlapping scan, got %v", err)
	}

	close(bp.release)
	if err := <-done; err != nil {
		t.Fatalf("concurrent ScanSource failed: %v", err)
	}

	// Verify ScanAllEnabled skips disabled source.
	svc.ScanAllEnabled(ctx)
	var disabledCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM media_files WHERE source_id = ?`, srcDisabled).Scan(&disabledCount); err != nil {
		t.Fatalf("query disabled media count failed: %v", err)
	}
	if disabledCount != 0 {
		t.Fatalf("expected disabled source not to be scanned, got %d files", disabledCount)
	}
}

func TestFFmpegProberIntegration(t *testing.T) {
	ffmpegBin := findBinary("ffmpeg")
	ffprobeBin := findBinary("ffprobe")
	if ffmpegBin == "" || ffprobeBin == "" {
		t.Skip("ffmpeg/ffprobe not available")
	}

	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "sample.mp4")
	thumbPath := filepath.Join(tmpDir, "thumb.jpg")

	// Generate a 2s silent H.264/AAC MP4.
	cmd := exec.Command(
		ffmpegBin,
		"-f", "lavfi", "-i", "testsrc=duration=2:size=640x360:rate=25",
		"-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=44100",
		"-t", "2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-y", videoPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg generate sample failed: %v\n%s", err, string(out))
	}

	prober := scanner.NewFFmpegProber(ffprobeBin, ffmpegBin)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := prober.Probe(ctx, videoPath)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if res.DurationMs <= 1500 {
		t.Fatalf("DurationMs = %d, want > 1500", res.DurationMs)
	}
	if res.VideoCodec != "h264" {
		t.Fatalf("VideoCodec = %q, want h264", res.VideoCodec)
	}
	if res.AudioCodec != "aac" {
		t.Fatalf("AudioCodec = %q, want aac", res.AudioCodec)
	}
	if res.Width != 640 || res.Height != 360 {
		t.Fatalf("resolution = %dx%d, want 640x360", res.Width, res.Height)
	}
	if res.Container != "mp4" {
		t.Fatalf("Container = %q, want mp4", res.Container)
	}

	var streams []map[string]any
	if err := json.Unmarshal([]byte(res.StreamsJSON), &streams); err != nil || len(streams) < 2 {
		t.Fatalf("expected valid streams JSON array with >=2 streams, got err=%v raw=%s", err, res.StreamsJSON)
	}

	if err := prober.ExtractThumbnail(ctx, videoPath, 0, thumbPath); err != nil {
		t.Fatalf("ExtractThumbnail failed: %v", err)
	}
	thumbBytes, err := os.ReadFile(thumbPath)
	if err != nil {
		t.Fatalf("read thumbnail failed: %v", err)
	}
	if len(thumbBytes) < 100 || thumbBytes[0] != 0xff || thumbBytes[1] != 0xd8 {
		t.Fatalf("expected valid non-empty JPEG thumbnail, got %d bytes", len(thumbBytes))
	}
}

func findBinary(name string) string {
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
