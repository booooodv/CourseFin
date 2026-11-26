package summary_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"coursefin/internal/ai"
	"coursefin/internal/catalog"
	"coursefin/internal/db"
	"coursefin/internal/id"
	"coursefin/internal/media"
	"coursefin/internal/storage"
	"coursefin/internal/summary"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, payload any) *http.Response {
	b, _ := json.Marshal(payload)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(b)),
	}
}

func openTestEnv(t *testing.T) (*sql.DB, string, *catalog.Service, *media.Service) {
	t.Helper()
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	reg := storage.NewRegistry()
	catSvc := catalog.NewService(conn, dataDir)
	catSvc.SetRegistry(reg)

	mediaSvc := media.NewService(conn, dataDir, reg, "")
	t.Cleanup(func() { _ = mediaSvc.Close() })

	return conn, dataDir, catSvc, mediaSvc
}

func setAppSettings(t *testing.T, conn *sql.DB, kv map[string]string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	for k, v := range kv {
		if _, err := conn.Exec(
			`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			k, v, now,
		); err != nil {
			t.Fatalf("upsert app_settings %s: %v", k, err)
		}
	}
}

type lessonSeedOpts struct {
	RootDir      string
	RelPath      string
	DurationMs   int64
	StreamsJSON  string
	AudioCodec   string
	VideoCodec   string
	SubtitleRel  string
	SubtitleBody string
	SubtitleLang string
}

func seedTestCourse(t *testing.T, conn *sql.DB, courseTitle string, lessons []lessonSeedOpts) (courseID string, lessonIDs []string, mediaIDs []string) {
	t.Helper()
	var libID string
	if err := conn.QueryRow(`SELECT id FROM libraries LIMIT 1`).Scan(&libID); err != nil {
		t.Fatalf("query default library: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sourceID := id.New()
	rootDir := t.TempDir()
	if len(lessons) > 0 && lessons[0].RootDir != "" {
		rootDir = lessons[0].RootDir
	}

	if _, err := conn.Exec(
		`INSERT INTO storage_sources (id, name, driver, root_path, config_json, enabled, created_at, updated_at)
		 VALUES (?, 'Test Source', 'local', ?, '{}', 1, ?, ?)`,
		sourceID, rootDir, now, now,
	); err != nil {
		t.Fatalf("insert storage_source: %v", err)
	}

	courseID = id.New()
	chapterID := id.New()
	if _, err := conn.Exec(
		`INSERT INTO courses (id, library_id, title, sort_title, overview, instructor, cover_path, backdrop_path, source_id, source_root_path, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '', '', '', '', ?, ?, ?, ?)`,
		courseID, libID, courseTitle, courseTitle, sourceID, rootDir, now, now,
	); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO chapters (id, course_id, title, sort_order, overview, created_at, updated_at)
		 VALUES (?, ?, '第一章', 1, '', ?, ?)`,
		chapterID, courseID, now, now,
	); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}

	for i, opts := range lessons {
		mediaID := id.New()
		lessonID := id.New()
		relPath := opts.RelPath
		if relPath == "" {
			relPath = fmt.Sprintf("lesson_%02d.mp4", i+1)
		}
		absMedia := filepath.Join(rootDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(absMedia), 0o755); err != nil {
			t.Fatalf("mkdir media dir: %v", err)
		}
		if _, err := os.Stat(absMedia); os.IsNotExist(err) {
			if err := os.WriteFile(absMedia, []byte("fake-video-bytes"), 0o644); err != nil {
				t.Fatalf("write fake video: %v", err)
			}
		}

		fileSize := int64(1024)
		modTimeStr := now
		if st, statErr := os.Stat(absMedia); statErr == nil {
			fileSize = st.Size()
			modTimeStr = st.ModTime().UTC().Format(time.RFC3339Nano)
		}

		streamsJSON := opts.StreamsJSON
		if streamsJSON == "" {
			streamsJSON = `[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"audio","codec_name":"aac"}]`
		}
		videoCodec := opts.VideoCodec
		if videoCodec == "" {
			videoCodec = "h264"
		}
		audioCodec := opts.AudioCodec
		durationMs := opts.DurationMs
		if durationMs <= 0 {
			durationMs = 1500_000
		}

		if _, err := conn.Exec(
			`INSERT INTO media_files (
				id, source_id, relative_path, file_name, file_size, mod_time, status,
				duration_ms, container, video_codec, audio_codec, width, height, bitrate,
				streams_json, thumbnail_path, probed_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, 'active', ?, 'mp4', ?, ?, 1920, 1080, 1000000, ?, '', ?, ?, ?)`,
			mediaID, sourceID, relPath, filepath.Base(relPath), fileSize, modTimeStr,
			durationMs, videoCodec, audioCodec, streamsJSON, now, now, now,
		); err != nil {
			t.Fatalf("insert media_file: %v", err)
		}

		if opts.SubtitleRel != "" {
			subAbs := filepath.Join(rootDir, filepath.FromSlash(opts.SubtitleRel))
			if err := os.MkdirAll(filepath.Dir(subAbs), 0o755); err != nil {
				t.Fatalf("mkdir subtitle dir: %v", err)
			}
			if err := os.WriteFile(subAbs, []byte(opts.SubtitleBody), 0o644); err != nil {
				t.Fatalf("write subtitle: %v", err)
			}
			subLang := opts.SubtitleLang
			if subLang == "" {
				subLang = "zh"
			}
			subFmt := strings.TrimPrefix(filepath.Ext(opts.SubtitleRel), ".")
			if _, err := conn.Exec(
				`INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
				 VALUES (?, ?, ?, ?, ?, '字幕', ?, 1, 'active')`,
				id.New(), sourceID, opts.SubtitleRel, mediaID, subLang, subFmt,
			); err != nil {
				t.Fatalf("insert subtitle_file: %v", err)
			}
		}

		if _, err := conn.Exec(
			`INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, overview, sort_order, cover_path, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, '', ?, '', ?, ?)`,
			lessonID, courseID, chapterID, mediaID, fmt.Sprintf("第 %d 课", i+1), i+1, now, now,
		); err != nil {
			t.Fatalf("insert lesson: %v", err)
		}

		lessonIDs = append(lessonIDs, lessonID)
		mediaIDs = append(mediaIDs, mediaID)
	}
	return courseID, lessonIDs, mediaIDs
}

type fakeAudioExtractor struct {
	dataDir   string
	chunks    []summary.AudioChunk
	createDir bool
	err       error
	calls     int
	mu        sync.Mutex
}

func (f *fakeAudioExtractor) ExtractChunks(ctx context.Context, mediaPath string, jobID string) ([]summary.AudioChunk, func(), error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, func() {}, err
	}

	workDir := filepath.Join(f.dataDir, "cache", "asr", jobID)
	if f.createDir || len(f.chunks) > 0 {
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return nil, func() {}, err
		}
	}
	cleanup := func() {
		_ = os.RemoveAll(workDir)
	}

	if f.err != nil {
		return nil, cleanup, f.err
	}

	out := make([]summary.AudioChunk, len(f.chunks))
	for i, c := range f.chunks {
		chunkPath := filepath.Join(workDir, fmt.Sprintf("chunk_%03d.mp3", i))
		if err := os.WriteFile(chunkPath, []byte(fmt.Sprintf("fake-mp3-%d", i)), 0o644); err != nil {
			cleanup()
			return nil, func() {}, err
		}
		out[i] = summary.AudioChunk{
			Path:        chunkPath,
			OffsetSec:   c.OffsetSec,
			DurationSec: c.DurationSec,
		}
	}
	return out, cleanup, nil
}

func newTestService(t *testing.T, conn *sql.DB, dataDir string, cat *catalog.Service, med *media.Service, rt http.RoundTripper, optsMod func(*summary.Options)) (*summary.Service, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	opts := summary.Options{
		DB:           conn,
		DataDir:      dataDir,
		Catalog:      cat,
		Media:        med,
		LifecycleCtx: ctx,
		HTTPClient:   &http.Client{Transport: rt, Timeout: 10 * time.Second},
	}
	if optsMod != nil {
		optsMod(&opts)
	}
	svc := summary.NewService(opts)
	t.Cleanup(func() {
		cancel()
		svc.Wait()
	})
	return svc, cancel
}

func TestResolveEffectiveSettingsKeyIsolation(t *testing.T) {
	t.Run("reuses main base url and key when asr base url empty", func(t *testing.T) {
		snap := summary.ResolveEffectiveSettings(map[string]string{
			"ai_base_url":     "  https://api.openai.com/v1  ",
			"ai_api_key":      "  sk-main-secret ",
			"ai_model":        " gpt-4o-mini ",
			"ai_asr_model":    " whisper-1 ",
			"ai_asr_language": " zh ",
		})
		if !snap.AIConfigured || !snap.ASRConfigured {
			t.Fatalf("expected both AI and ASR configured, got %+v", snap)
		}
		if snap.ASR.BaseURL != "https://api.openai.com/v1" {
			t.Fatalf("ASR.BaseURL = %q", snap.ASR.BaseURL)
		}
		if snap.ASR.APIKey != "sk-main-secret" {
			t.Fatalf("ASR.APIKey = %q, want main key fallback", snap.ASR.APIKey)
		}
		if snap.ASR.Model != "whisper-1" || snap.ASR.Language != "zh" {
			t.Fatalf("unexpected ASR model/lang: %+v", snap.ASR)
		}
	})

	t.Run("does not leak main key to separate asr base url", func(t *testing.T) {
		snap := summary.ResolveEffectiveSettings(map[string]string{
			"ai_base_url":     "https://api.openai.com/v1",
			"ai_api_key":      "sk-main-secret",
			"ai_model":        "gpt-4o-mini",
			"ai_asr_base_url": "http://whisper:8000/v1",
			"ai_asr_api_key":  "   ",
			"ai_asr_model":    "faster-whisper-large-v3",
		})
		if !snap.ASRConfigured {
			t.Fatalf("expected ASR configured, got %+v", snap)
		}
		if snap.ASR.BaseURL != "http://whisper:8000/v1" {
			t.Fatalf("ASR.BaseURL = %q", snap.ASR.BaseURL)
		}
		if snap.ASR.APIKey != "" {
			t.Fatalf("ASR.APIKey = %q, MUST NOT forward main ai_api_key to custom ai_asr_base_url", snap.ASR.APIKey)
		}
	})

	t.Run("uses explicit asr api key when provided", func(t *testing.T) {
		snap := summary.ResolveEffectiveSettings(map[string]string{
			"ai_base_url":     "https://api.openai.com/v1",
			"ai_api_key":      "sk-main-secret",
			"ai_model":        "gpt-4o-mini",
			"ai_asr_base_url": "https://api.groq.com/openai/v1",
			"ai_asr_api_key":  "sk-asr-only",
			"ai_asr_model":    "whisper-large-v3-turbo",
		})
		if snap.ASR.APIKey != "sk-asr-only" {
			t.Fatalf("ASR.APIKey = %q, want sk-asr-only", snap.ASR.APIKey)
		}
	})

	t.Run("requires model and base url for configured flags", func(t *testing.T) {
		snap := summary.ResolveEffectiveSettings(map[string]string{
			"ai_base_url": "https://api.openai.com/v1",
			"ai_api_key":  "sk-main",
		})
		if snap.AIConfigured || snap.ASRConfigured {
			t.Fatalf("expected unconfigured without models, got %+v", snap)
		}
	})
}

func TestCueCompactionStripsTagsAndDedupesRollingCaptions(t *testing.T) {
	vtt := `WEBVTT

00:00:01.200 --> 00:00:03.000
<00:00:01.200><c>Hello</c> <i>world</i>

00:00:04.000 --> 00:00:06.000
{\pos(100,200)}中文

00:00:07.000 --> 00:00:08.000
今天讲 Go 并发

00:00:08.000 --> 00:00:09.000
今天讲 Go 并发

00:00:09.000 --> 00:00:10.000
今天讲 Go 并发

00:00:10.000 --> 00:00:11.000
今天讲 Go 并发
下一句：Channel 原理

01:05:03.500 --> 01:05:08.000
{\an8}超过一小时的内容
`
	cues := ai.ParseSubtitleCues(vtt)
	if len(cues) != 7 {
		t.Fatalf("parsed cues = %d, want 7", len(cues))
	}

	lines := summary.CompactCues(cues, 15.0, 200)
	if len(lines) != 2 {
		t.Fatalf("compacted lines = %d, want 2: %+v", len(lines), lines)
	}

	first := lines[0].Formatted
	if !strings.HasPrefix(first, "[00:01] ") {
		t.Fatalf("first line prefix = %q, want [00:01] ...", first)
	}
	for _, unwanted := range []string{"<c>", "</c>", "<i>", "</i>", "<00:00:01.200>", `\pos(100,200)`, `{`, `}`} {
		if strings.Contains(first, unwanted) {
			t.Fatalf("first line still contains tag %q: %q", unwanted, first)
		}
	}
	if !strings.Contains(first, "Hello world") || !strings.Contains(first, "中文") {
		t.Fatalf("first line missing cleaned text: %q", first)
	}
	if got := strings.Count(first, "今天讲 Go 并发"); got != 1 {
		t.Fatalf("duplicate rolling line count = %d, want 1 in %q", got, first)
	}
	if !strings.Contains(first, "下一句：Channel 原理") {
		t.Fatalf("missing following rolling line: %q", first)
	}

	second := lines[1].Formatted
	if second != "[1:05:03] 超过一小时的内容" {
		t.Fatalf("second line = %q, want %q", second, "[1:05:03] 超过一小时的内容")
	}

	// Verify chunk splitter splits > maxRunes on cue boundaries without losing any cues.
	var manyLines []summary.CompactedLine
	for i := 0; i < 6; i++ {
		txt := fmt.Sprintf("第%d段测试文本内容用于验证分片边界不丢失任何字幕条目", i+1)
		manyLines = append(manyLines, summary.CompactedLine{
			StartSec:  float64(i * 20),
			EndSec:    float64(i*20 + 15),
			Text:      txt,
			Formatted: fmt.Sprintf("[%02d:%02d] %s", (i*20)/60, (i*20)%60, txt),
		})
	}
	chunks := summary.SplitCompactedLines(manyLines, 80)
	if len(chunks) < 2 {
		t.Fatalf("expected >= 2 chunks, got %d", len(chunks))
	}
	for ci, ch := range chunks {
		if utf8.RuneCountInString(ch) > 80 {
			t.Fatalf("chunk %d exceeded maxRunes: %d", ci, utf8.RuneCountInString(ch))
		}
	}
	rejoined := strings.Join(chunks, "\n")
	for _, ln := range manyLines {
		if !strings.Contains(rejoined, ln.Formatted) {
			t.Fatalf("split lost cue %q", ln.Formatted)
		}
	}

	// Verify DetectManualTranscript for SRT and plain text (including "-->" without valid cues).
	_, plainText, hasTS := summary.DetectManualTranscript("第一行 A --> B\n第二行纯文本")
	if hasTS {
		t.Fatalf("expected plain text with '-->' and invalid cue timing to have hasTimestamps=false")
	}
	if !strings.Contains(plainText, "第一行 A --> B") {
		t.Fatalf("unexpected plainText: %q", plainText)
	}

	vttOut, _, hasSRTTS := summary.DetectManualTranscript("1\n00:00:01,000 --> 00:00:02,500\n你好 SRT\n")
	if !hasSRTTS || !strings.HasPrefix(vttOut, "WEBVTT") || !strings.Contains(vttOut, "你好 SRT") {
		t.Fatalf("unexpected SRT detection: hasTS=%v vtt=%q", hasSRTTS, vttOut)
	}
}

func TestSingleLessonFromExternalSubtitlePersistsSummary(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url": "https://mock-llm.local/v1",
		"ai_api_key":  "sk-llm-key",
		"ai_model":    "gpt-4o-mini",
	})

	srtBody := "1\n00:00:02,000 --> 00:00:06,000\n今天学习 Go 的 Goroutine 调度。\n\n2\n00:00:08,000 --> 00:00:15,000\n接着讲解 Channel 通信。\n"
	_, lessonIDs, _ := seedTestCourse(t, conn, "Go 并发课程", []lessonSeedOpts{
		{
			RelPath:      "01.mp4",
			DurationMs:   600_000,
			SubtitleRel:  "01.zh.srt",
			SubtitleBody: srtBody,
			SubtitleLang: "zh",
		},
	})
	lessonID := lessonIDs[0]

	var llmCalls int
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		llmCalls++
		if got := req.Header.Get("Authorization"); got != "Bearer sk-llm-key" {
			t.Errorf("Authorization = %q, want Bearer sk-llm-key", got)
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role": "assistant",
						"content": `{
							"overview": "本节介绍 Goroutine 与 Channel 并发模型。",
							"outline": [
								{"title": "Goroutine 调度", "summary": "理解 GMP 模型", "start_sec": 2},
								{"title": "Channel 通信", "summary": "掌握无缓冲与有缓冲通道", "start_sec": 8}
							],
							"timeline": [
								{"start_sec": 2, "end_sec": 6, "title": "Goroutine 入门", "summary": "讲解轻量级协程", "key_points": ["go 关键字"]},
								{"start_sec": 8, "end_sec": 15, "title": "Channel 基础", "summary": "讲解通道收发", "key_points": ["make(chan int)"]}
							]
						}`,
					},
				},
			},
		}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, nil)

	job, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{})
	if err != nil {
		t.Fatalf("StartLessonSummary failed: %v", err)
	}
	if !job.Running {
		t.Fatalf("expected queued/running job, got %+v", job)
	}

	// Idempotent re-trigger while running/queued returns current snapshot.
	job2, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{})
	if err != nil {
		t.Fatalf("second StartLessonSummary failed: %v", err)
	}
	if job2.LessonID != lessonID {
		t.Fatalf("unexpected second job snapshot: %+v", job2)
	}

	svc.Wait()

	if llmCalls != 1 {
		t.Fatalf("llmCalls = %d, want 1", llmCalls)
	}

	state, err := svc.GetLessonSummaryState(context.Background(), lessonID)
	if err != nil {
		t.Fatalf("GetLessonSummaryState failed: %v", err)
	}
	if state.Job == nil || state.Job.Stage != summary.StageCompleted || state.Job.Running {
		t.Fatalf("unexpected job state: %+v", state.Job)
	}
	if state.Summary == nil {
		t.Fatalf("expected persisted summary, got nil")
	}
	if state.Summary.SourceType != "external_subtitle" {
		t.Fatalf("SourceType = %q, want external_subtitle", state.Summary.SourceType)
	}
	if !state.Summary.HasTimestamps {
		t.Fatalf("expected HasTimestamps = true")
	}
	if len(state.Summary.Outline) != 2 || len(state.Summary.Timeline) != 2 {
		t.Fatalf("unexpected outline/timeline lengths: %d / %d", len(state.Summary.Outline), len(state.Summary.Timeline))
	}
	if !state.Capabilities.HasSubtitle || !state.Capabilities.AIConfigured {
		t.Fatalf("unexpected capabilities: %+v", state.Capabilities)
	}
}

func TestSingleLessonNoSubtitleRunsASRChunksSavesAISubtitleAndReusesCache(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":     "https://mock-llm.local/v1",
		"ai_api_key":      "sk-main-llm-only",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "http://local-whisper.internal/v1",
		"ai_asr_api_key":  "", // Must NOT receive sk-main-llm-only
		"ai_asr_model":    "whisper-large-v3",
		"ai_asr_language": "zh",
	})

	_, lessonIDs, mediaIDs := seedTestCourse(t, conn, "无字幕课程", []lessonSeedOpts{
		{
			RelPath:    "nosub.mp4",
			DurationMs: 1500_000,
		},
	})
	lessonID, mediaID := lessonIDs[0], mediaIDs[0]

	extractor := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks: []summary.AudioChunk{
			{OffsetSec: 0, DurationSec: 1200},
			{OffsetSec: 1200, DurationSec: 300},
		},
	}

	var mu sync.Mutex
	var asrCalls, llmCalls int
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()

		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			asrCalls++
			if auth := req.Header.Get("Authorization"); auth != "" {
				t.Errorf("ASR request leaked Authorization header %q to custom ai_asr_base_url", auth)
			}
			idx := asrCalls
			return jsonResponse(http.StatusOK, map[string]any{
				"language": "zh",
				"text":     fmt.Sprintf("片段%d语音内容", idx),
				"segments": []map[string]any{
					{"start": 0.0, "end": 5.0, "text": fmt.Sprintf("片段%d内容", idx)},
				},
			}), nil
		}

		if strings.HasSuffix(req.URL.Path, "/chat/completions") {
			llmCalls++
			if auth := req.Header.Get("Authorization"); auth != "Bearer sk-main-llm-only" {
				t.Errorf("LLM Authorization = %q, want Bearer sk-main-llm-only", auth)
			}
			return jsonResponse(http.StatusOK, map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role": "assistant",
							"content": `{
								"overview": "通过 ASR 转写生成的课程总结。",
								"outline": [{"title": "第一部分", "summary": "转写概览", "start_sec": 0}],
								"timeline": [
									{"start_sec": 0, "end_sec": 5, "title": "开篇", "summary": "片段1", "key_points": ["要点1"]},
									{"start_sec": 1200, "end_sec": 1205, "title": "后半部分", "summary": "片段2", "key_points": ["要点2"]}
								]
							}`,
						},
					},
				},
			}), nil
		}

		return jsonResponse(http.StatusNotFound, map[string]any{"error": "unexpected path"}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = extractor
	})

	// Run 1: No subtitle -> extracts audio -> 2 ASR chunks -> saves AI subtitle -> summarizes.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("StartLessonSummary run 1 failed: %v", err)
	}
	svc.Wait()

	if asrCalls != 2 || llmCalls != 1 {
		t.Fatalf("run 1 calls: asr=%d llm=%d, want 2/1", asrCalls, llmCalls)
	}

	var cachedSource, cachedVTT string
	if err := conn.QueryRow(`SELECT source, content_vtt FROM media_transcripts WHERE media_file_id = ?`, mediaID).Scan(&cachedSource, &cachedVTT); err != nil {
		t.Fatalf("query media_transcripts: %v", err)
	}
	if cachedSource != "asr" {
		t.Fatalf("cachedSource = %q, want asr", cachedSource)
	}
	cues := ai.ParseSubtitleCues(cachedVTT)
	if len(cues) != 2 {
		t.Fatalf("cached cues = %d, want 2: %q", len(cues), cachedVTT)
	}
	if cues[1].StartSec != 1200 || cues[1].EndSec != 1205 {
		t.Fatalf("second chunk cues not shifted to [1200..1205]: %+v", cues[1])
	}

	// Verify media.Service.ResolveStreams exposes "AI 转写字幕".
	streams, err := mediaSvc.ResolveStreams(context.Background(), mediaID)
	if err != nil {
		t.Fatalf("ResolveStreams failed: %v", err)
	}
	var foundAISub bool
	for _, st := range streams {
		if st.Type == "subtitle" && st.IsAIGenerated && st.Title == "AI 转写字幕" {
			foundAISub = true
		}
	}
	if !foundAISub {
		t.Fatalf("expected AI 转写字幕 in streams, got %+v", streams)
	}

	// Verify temp dir cleaned up.
	if entries, err := os.ReadDir(filepath.Join(dataDir, "cache", "asr")); err == nil && len(entries) != 0 {
		t.Fatalf("expected empty cache/asr dir, found %d entries", len(entries))
	}

	// Run 2: Force: true reuses media_transcripts with 0 additional ASR calls.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{Force: true}); err != nil {
		t.Fatalf("StartLessonSummary run 2 failed: %v", err)
	}
	svc.Wait()

	if asrCalls != 2 || llmCalls != 2 {
		t.Fatalf("run 2 calls: asr=%d llm=%d, want 2/2 (cache reused)", asrCalls, llmCalls)
	}

	// Run 3: ForceRetranscribe: true triggers ASR again.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{Force: true, ForceRetranscribe: true}); err != nil {
		t.Fatalf("StartLessonSummary run 3 failed: %v", err)
	}
	svc.Wait()

	if asrCalls != 4 || llmCalls != 3 {
		t.Fatalf("run 3 calls: asr=%d llm=%d, want 4/3", asrCalls, llmCalls)
	}
}

func TestSingleLessonManualTranscriptPlainTextAndSRT(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	_, lessonIDs, _ := seedTestCourse(t, conn, "手动文稿课程", []lessonSeedOpts{
		{RelPath: "manual.mp4", DurationMs: 300_000},
	})
	lessonID := lessonIDs[0]

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		var chatReq struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		_ = json.Unmarshal(raw, &chatReq)
		var input ai.LessonSummaryInput
		_ = json.Unmarshal([]byte(chatReq.Messages[1].Content), &input)

		if input.HasTimestamps {
			return jsonResponse(http.StatusOK, map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role": "assistant",
						"content": `{
							"overview": "SRT 手动文稿总结",
							"outline": [{"title": "模块一", "summary": "内容", "start_sec": 10}],
							"timeline": [{"start_sec": 10, "end_sec": 20, "title": "段落一", "summary": "讲解", "key_points": ["要点"]}]
						}`,
					},
				}},
			}), nil
		}

		// Plain text: return non-null timestamps intentionally to verify normalization clears them to nil.
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role": "assistant",
					"content": `{
						"overview": "纯文本手动文稿总结",
						"outline": [{"title": "模块一", "summary": "内容", "start_sec": 10}],
						"timeline": [{"start_sec": 10, "end_sec": 20, "title": "段落一", "summary": "讲解", "key_points": ["要点"]}]
					}`,
				},
			}},
		}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, nil)

	// Unconfigured AI -> ErrAINotConfigured.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{
		ManualTranscript: "手动内容",
	}); !errors.Is(err, summary.ErrAINotConfigured) {
		t.Fatalf("expected ErrAINotConfigured, got %v", err)
	}

	// Configure AI only (no ASR).
	setAppSettings(t, conn, map[string]string{
		"ai_base_url": "https://mock-llm.local/v1",
		"ai_api_key":  "sk-llm",
		"ai_model":    "gpt-4o-mini",
	})

	// Empty manual transcript without ASR configured -> ErrASRNotConfigured.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{}); !errors.Is(err, summary.ErrASRNotConfigured) {
		t.Fatalf("expected ErrASRNotConfigured, got %v", err)
	}

	// Plain-text manual transcript succeeds with has_timestamps == false and nil timestamps.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{
		ManualTranscript: "这是纯文本课程笔记。\n讲解了 Go 接口设计原则。",
	}); err != nil {
		t.Fatalf("plain manual transcript failed: %v", err)
	}
	svc.Wait()

	state, err := svc.GetLessonSummaryState(context.Background(), lessonID)
	if err != nil {
		t.Fatalf("GetLessonSummaryState failed: %v", err)
	}
	if state.Summary == nil || state.Summary.SourceType != "manual" || state.Summary.HasTimestamps {
		t.Fatalf("unexpected plain manual summary: %+v", state.Summary)
	}
	for _, sec := range state.Summary.Timeline {
		if sec.StartSec != nil || sec.EndSec != nil {
			t.Fatalf("expected nil timestamps for plain-text summary, got %+v", sec)
		}
	}
	for _, item := range state.Summary.Outline {
		if item.StartSec != nil {
			t.Fatalf("expected nil outline start_sec for plain-text summary, got %+v", item)
		}
	}

	// SRT manual transcript succeeds with has_timestamps == true and valid timestamps.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{
		ManualTranscript: "1\n00:00:10,000 --> 00:00:20,000\n接口与多态实战\n",
	}); err != nil {
		t.Fatalf("SRT manual transcript failed: %v", err)
	}
	svc.Wait()

	state2, err := svc.GetLessonSummaryState(context.Background(), lessonID)
	if err != nil {
		t.Fatalf("GetLessonSummaryState 2 failed: %v", err)
	}
	if state2.Summary == nil || state2.Summary.SourceType != "manual" || !state2.Summary.HasTimestamps {
		t.Fatalf("unexpected SRT manual summary: %+v", state2.Summary)
	}
	if state2.Summary.Timeline[0].StartSec == nil || *state2.Summary.Timeline[0].StartSec != 10 {
		t.Fatalf("expected start_sec == 10, got %+v", state2.Summary.Timeline[0])
	}

	// Timed cue containing only markup/tags strips to empty plain text and must be rejected before touching media_transcripts.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{
		ManualTranscript: "1\n00:00:01,000 --> 00:00:02,000\n{\\an8}<i>   </i>\n",
	}); err != nil {
		t.Fatalf("StartLessonSummary empty-stripped timed manual transcript failed: %v", err)
	}
	svc.Wait()

	state3, err := svc.GetLessonSummaryState(context.Background(), lessonID)
	if err != nil {
		t.Fatalf("GetLessonSummaryState 3 failed: %v", err)
	}
	if state3.Job == nil || state3.Job.Stage != summary.StageFailed || state3.Job.ErrorCode != "invalid_input" {
		t.Fatalf("expected invalid_input failure for empty-stripped timed manual transcript, got %+v", state3.Job)
	}
	var cachedPlain string
	if err := conn.QueryRow(`SELECT plain_text FROM media_transcripts WHERE media_file_id = (SELECT media_file_id FROM lessons WHERE id = ?)`, lessonID).Scan(&cachedPlain); err != nil {
		t.Fatalf("query media_transcripts failed: %v", err)
	}
	if !strings.Contains(cachedPlain, "接口与多态实战") {
		t.Fatalf("expected existing media_transcripts row preserved, got %q", cachedPlain)
	}
}

func TestLongTranscriptUsesMapReduce(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url": "https://mock-llm.local/v1",
		"ai_api_key":  "sk-llm",
		"ai_model":    "gpt-4o-mini",
	})

	// Build ~900-rune VTT subtitle across 18 cues separated by 20s so compaction keeps distinct cues.
	var vttBuf strings.Builder
	vttBuf.WriteString("WEBVTT\n\n")
	for i := 0; i < 18; i++ {
		start := i * 20
		end := start + 10
		vttBuf.WriteString(fmt.Sprintf("%02d:%02d:%02d.000 --> %02d:%02d:%02d.000\n", start/3600, (start%3600)/60, start%60, end/3600, (end%3600)/60, end%60))
		vttBuf.WriteString(fmt.Sprintf("第%02d节深入讲解分布式系统一致性协议与工程实践内容丰富共五十个字符\n\n", i+1))
	}

	_, lessonIDs, _ := seedTestCourse(t, conn, "超长课程", []lessonSeedOpts{
		{
			RelPath:      "long.mp4",
			DurationMs:   600_000,
			SubtitleRel:  "long.vtt",
			SubtitleBody: vttBuf.String(),
			SubtitleLang: "zh",
		},
	})
	lessonID := lessonIDs[0]

	var mu sync.Mutex
	var mapCalls, reduceCalls int
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		var chatReq struct {
			Messages []ai.ChatMessage `json:"messages"`
		}
		_ = json.Unmarshal(raw, &chatReq)
		userMsg := chatReq.Messages[1].Content

		mu.Lock()
		defer mu.Unlock()

		// Map calls carry chunk_index; Reduce call carries timeline array.
		if strings.Contains(userMsg, `"chunk_index"`) {
			mapCalls++
			var chunkIn ai.LessonChunkInput
			_ = json.Unmarshal([]byte(userMsg), &chunkIn)
			startSec := (chunkIn.ChunkIndex - 1) * 100
			endSec := startSec + 90
			return jsonResponse(http.StatusOK, map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role": "assistant",
						"content": fmt.Sprintf(`{
							"timeline": [{
								"start_sec": %d,
								"end_sec": %d,
								"title": "分块 %d 主题",
								"summary": "分块 %d 概括",
								"key_points": ["要点 %d"]
							}]
						}`, startSec, endSec, chunkIn.ChunkIndex, chunkIn.ChunkIndex, chunkIn.ChunkIndex),
					},
				}},
			}), nil
		}

		if strings.Contains(userMsg, `"timeline"`) {
			reduceCalls++
			return jsonResponse(http.StatusOK, map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role": "assistant",
						"content": `{
							"overview": "超长课程全局总结",
							"outline": [
								{"title": "全局模块一", "summary": "前段内容", "start_sec": 0},
								{"title": "全局模块二", "summary": "后段内容", "start_sec": 200}
							]
						}`,
					},
				}},
			}), nil
		}

		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "unexpected single-pass call"}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.SinglePassMaxRunes = 500
		o.MapChunkMaxRunes = 300
	})

	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("StartLessonSummary failed: %v", err)
	}
	svc.Wait()

	if mapCalls < 2 {
		t.Fatalf("mapCalls = %d, want >= 2", mapCalls)
	}
	if reduceCalls != 1 {
		t.Fatalf("reduceCalls = %d, want 1", reduceCalls)
	}

	state, err := svc.GetLessonSummaryState(context.Background(), lessonID)
	if err != nil {
		t.Fatalf("GetLessonSummaryState failed: %v", err)
	}
	if state.Summary == nil {
		t.Fatalf("expected summary, got nil")
	}
	if state.Summary.Overview != "超长课程全局总结" {
		t.Fatalf("Overview = %q", state.Summary.Overview)
	}
	if len(state.Summary.Timeline) != mapCalls {
		t.Fatalf("timeline sections = %d, want %d", len(state.Summary.Timeline), mapCalls)
	}
	if len(state.Summary.Outline) != 2 {
		t.Fatalf("outline items = %d, want 2", len(state.Summary.Outline))
	}
}

func TestRegenerateFailurePreservesExistingSummaryAndCleansTempDir(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":  "https://mock-llm.local/v1",
		"ai_api_key":   "sk-llm",
		"ai_model":     "gpt-4o-mini",
		"ai_asr_model": "whisper-1",
	})

	_, lessonIDs, mediaIDs := seedTestCourse(t, conn, "保留旧总结课程", []lessonSeedOpts{
		{RelPath: "keep.mp4", DurationMs: 300_000},
	})
	lessonID, mediaID := lessonIDs[0], mediaIDs[0]

	// Seed an existing saved summary.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(
		`INSERT INTO lesson_summaries (lesson_id, media_file_id, source_type, source_label, has_timestamps, model, overview, outline_json, timeline_json, created_at, updated_at)
		 VALUES (?, ?, 'external_subtitle', '外挂字幕 (zh)', 1, 'gpt-4o-mini', '旧的完整概览', '[{"title":"旧大纲","summary":"旧描述","start_sec":0}]', '[{"start_sec":0,"end_sec":60,"title":"旧分段","summary":"旧内容","key_points":["旧要点"]}]', ?, ?)`,
		lessonID, mediaID, now, now,
	); err != nil {
		t.Fatalf("seed existing summary: %v", err)
	}

	extractor := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks: []summary.AudioChunk{
			{OffsetSec: 0, DurationSec: 60},
		},
	}

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			return jsonResponse(http.StatusOK, map[string]any{
				"language": "zh",
				"segments": []map[string]any{{"start": 0.0, "end": 5.0, "text": "新的转写内容"}},
			}), nil
		}
		// LLM fails with HTTP 500.
		return jsonResponse(http.StatusInternalServerError, map[string]any{
			"error": map[string]any{"message": "upstream overloaded"},
		}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = extractor
	})

	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{
		Force:             true,
		ForceRetranscribe: true,
	}); err != nil {
		t.Fatalf("StartLessonSummary failed: %v", err)
	}
	svc.Wait()

	state, err := svc.GetLessonSummaryState(context.Background(), lessonID)
	if err != nil {
		t.Fatalf("GetLessonSummaryState failed: %v", err)
	}
	if state.Job == nil || state.Job.Stage != summary.StageFailed || state.Job.Running || state.Job.Error == "" {
		t.Fatalf("expected failed job status, got %+v", state.Job)
	}
	if state.Summary == nil || state.Summary.Overview != "旧的完整概览" || state.Summary.SourceType != "external_subtitle" {
		t.Fatalf("existing summary was corrupted or overwritten: %+v", state.Summary)
	}
	if len(state.Summary.Outline) != 1 || state.Summary.Outline[0].Title != "旧大纲" {
		t.Fatalf("existing outline changed: %+v", state.Summary.Outline)
	}

	// Verify temp dir under {dataDir}/cache/asr is completely cleaned up.
	entries, err := os.ReadDir(filepath.Join(dataDir, "cache", "asr"))
	if err != nil {
		t.Fatalf("readdir cache/asr: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 leftover job dirs in cache/asr, got %d", len(entries))
	}
}

func TestCourseBatchContinuesOnSingleLessonFailureAndSupportsCancel(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url": "https://mock-llm.local/v1",
		"ai_api_key":  "sk-llm",
		"ai_model":    "gpt-4o-mini",
	})

	courseID, lessonIDs, _ := seedTestCourse(t, conn, "批量测试课程", []lessonSeedOpts{
		{
			RelPath:      "l1.mp4",
			SubtitleRel:  "l1.srt",
			SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\n第一课字幕\n",
		},
		{
			RelPath:     "l2.mp4",
			StreamsJSON: `[{"index":0,"codec_type":"video","codec_name":"h264"}]`, // no transcript -> skipped_no_transcript
		},
		{
			RelPath:      "l3.mp4",
			SubtitleRel:  "l3.srt",
			SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\nFAIL_L3_SUBTITLE\n",
		},
		{
			RelPath:      "l4.mp4",
			SubtitleRel:  "l4.srt",
			SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\n第四课字幕\n",
		},
	})

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		if strings.Contains(string(raw), "FAIL_L3_SUBTITLE") {
			return jsonResponse(http.StatusInternalServerError, map[string]any{
				"error": map[string]any{"message": "llm 500 on l3"},
			}), nil
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role": "assistant",
					"content": `{
						"overview": "批量总结概览",
						"outline": [{"title": "模块", "summary": "概括", "start_sec": 1}],
						"timeline": [{"start_sec": 1, "end_sec": 5, "title": "分段", "summary": "内容", "key_points": ["要点"]}]
					}`,
				},
			}},
		}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, nil)

	batch, err := svc.StartCourseBatch(context.Background(), courseID, "missing_only")
	if err != nil {
		t.Fatalf("StartCourseBatch failed: %v", err)
	}
	if !batch.Running || batch.Total != 4 || batch.SkippedNoTranscript != 1 || batch.Skipped != 1 {
		t.Fatalf("unexpected initial batch: %+v", batch)
	}

	// Overlapping batch on same course returns ErrBatchAlreadyRunning.
	if _, err := svc.StartCourseBatch(context.Background(), courseID, "missing_only"); !errors.Is(err, summary.ErrBatchAlreadyRunning) {
		t.Fatalf("expected ErrBatchAlreadyRunning, got %v", err)
	}

	svc.Wait()

	cStatus, err := svc.GetCourseSummaryStatus(context.Background(), courseID)
	if err != nil {
		t.Fatalf("GetCourseSummaryStatus failed: %v", err)
	}
	if cStatus.BatchJob == nil || cStatus.BatchJob.Running {
		t.Fatalf("expected completed batch job, got %+v", cStatus.BatchJob)
	}
	bj := cStatus.BatchJob
	if bj.Total != 4 || bj.Completed != 2 || bj.Failed != 1 || bj.Skipped != 1 || bj.SkippedNoTranscript != 1 {
		t.Fatalf("unexpected batch counts: %+v", bj)
	}
	if len(bj.Errors) != 1 || bj.Errors[0].LessonID != lessonIDs[2] {
		t.Fatalf("unexpected batch errors: %+v", bj.Errors)
	}
	if len(cStatus.SummaryLessonIDs) != 2 {
		t.Fatalf("SummaryLessonIDs = %+v, want L1 and L4", cStatus.SummaryLessonIDs)
	}

	// missing_only again skips already summarized L1 & L3 and attempts only L2.
	// Now test cancellation of a slow batch on a fresh course.
	course2ID, slowLessons, _ := seedTestCourse(t, conn, "慢速取消课程", []lessonSeedOpts{
		{RelPath: "s1.mp4", SubtitleRel: "s1.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:02,000\n第一课\n"},
		{RelPath: "s2.mp4", SubtitleRel: "s2.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:02,000\n第二课\n"},
		{RelPath: "s3.mp4", SubtitleRel: "s3.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:02,000\n第三课\n"},
	})

	startedS1 := make(chan struct{}, 1)
	var slowCalls int
	var slowMu sync.Mutex
	slowRT := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		slowMu.Lock()
		slowCalls++
		slowMu.Unlock()

		select {
		case startedS1 <- struct{}{}:
		default:
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})

	svc2, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, slowRT, nil)
	b2, err := svc2.StartCourseBatch(context.Background(), course2ID, "all")
	if err != nil {
		t.Fatalf("StartCourseBatch slow failed: %v", err)
	}
	if !b2.Running {
		t.Fatalf("expected slow batch running")
	}

	select {
	case <-startedS1:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for slow batch L1 to enter LLM call")
	}

	cancelledBatch, err := svc2.CancelCourseBatch(course2ID)
	if err != nil {
		t.Fatalf("CancelCourseBatch failed: %v", err)
	}
	_ = cancelledBatch

	svc2.Wait()

	cStatus2, err := svc2.GetCourseSummaryStatus(context.Background(), course2ID)
	if err != nil {
		t.Fatalf("GetCourseSummaryStatus slow failed: %v", err)
	}
	if cStatus2.BatchJob == nil || cStatus2.BatchJob.Running {
		t.Fatalf("expected cancelled batch not running, got %+v", cStatus2.BatchJob)
	}
	if cStatus2.BatchJob.CurrentStage != summary.StageCancelled {
		t.Fatalf("CurrentStage = %q, want cancelled", cStatus2.BatchJob.CurrentStage)
	}
	if slowCalls != 1 {
		t.Fatalf("expected remaining queued lessons stopped after cancel, got slowCalls=%d", slowCalls)
	}
	if len(cStatus2.SummaryLessonIDs) != 0 {
		t.Fatalf("expected 0 saved summaries on cancelled batch, got %+v", cStatus2.SummaryLessonIDs)
	}
	// Ensure s2 and s3 were never run.
	st2, err := svc2.GetLessonSummaryState(context.Background(), slowLessons[1])
	if err != nil {
		t.Fatalf("GetLessonSummaryState s2 failed: %v", err)
	}
	if st2.Job != nil {
		t.Fatalf("expected queued s2 never started after cancel, got %+v", st2.Job)
	}
}

func TestFFmpegAudioExtractorSmoke(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH, skipping real FFmpeg smoke test")
	}
	ffprobePath, _ := exec.LookPath("ffprobe")

	srcDir := t.TempDir()
	sampleVideo := filepath.Join(srcDir, "sample.mp4")
	cmd := exec.Command(
		ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=160x120:d=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "mpeg4", "-c:a", "aac", "-shortest",
		sampleVideo,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg lavfi sample generation unsupported in environment: %v (%s)", err, string(out))
	}

	dataDir := t.TempDir()
	extractor := summary.NewFFmpegAudioExtractor(dataDir, ffmpegPath, ffprobePath, 1200)
	chunks, cleanup, err := extractor.ExtractChunks(context.Background(), sampleVideo, "smoke-job")
	if err != nil {
		t.Fatalf("ExtractChunks failed: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("expected non-nil cleanup func")
	}
	defer cleanup()

	if len(chunks) != 1 {
		t.Fatalf("expected 1 audio chunk, got %d", len(chunks))
	}
	if chunks[0].OffsetSec != 0 || chunks[0].DurationSec <= 0 {
		t.Fatalf("unexpected chunk timing: %+v", chunks[0])
	}
	info, err := os.Stat(chunks[0].Path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected non-empty chunk file %s: err=%v", chunks[0].Path, err)
	}

	cleanup()
	if _, err := os.Stat(filepath.Join(dataDir, "cache", "asr", "smoke-job")); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup to remove job workdir, stat err=%v", err)
	}
}

func TestSingleLessonPriorityOverBatchAndIdempotencyAndCancelAndDelete(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url": "https://mock-llm.local/v1",
		"ai_api_key":  "sk-llm",
		"ai_model":    "gpt-4o-mini",
	})

	courseAID, lessonsA, _ := seedTestCourse(t, conn, "批量队列课程A", []lessonSeedOpts{
		{RelPath: "a1.mp4", SubtitleRel: "a1.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\nLESSON_A1\n"},
		{RelPath: "a2.mp4", SubtitleRel: "a2.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\nLESSON_A2\n"},
		{RelPath: "a3.mp4", SubtitleRel: "a3.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\nLESSON_A3\n"},
	})
	_, lessonsB, _ := seedTestCourse(t, conn, "插队单课课程B", []lessonSeedOpts{
		{RelPath: "b1.mp4", SubtitleRel: "b1.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\nLESSON_B1\n"},
	})

	a1Entered := make(chan struct{})
	releaseA1 := make(chan struct{})
	b1Entered := make(chan struct{})
	releaseB1 := make(chan struct{})
	var mu sync.Mutex
	var execOrder []string

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		bodyStr := string(raw)

		var tag string
		switch {
		case strings.Contains(bodyStr, "LESSON_A1"):
			tag = "A1"
			close(a1Entered)
			select {
			case <-releaseA1:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		case strings.Contains(bodyStr, "LESSON_A2"):
			tag = "A2"
		case strings.Contains(bodyStr, "LESSON_A3"):
			tag = "A3"
		case strings.Contains(bodyStr, "LESSON_B1"):
			tag = "B1"
			close(b1Entered)
			select {
			case <-releaseB1:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		mu.Lock()
		execOrder = append(execOrder, tag)
		mu.Unlock()

		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role": "assistant",
					"content": `{
						"overview": "总结",
						"outline": [{"title": "大纲", "summary": "描述", "start_sec": 1}],
						"timeline": [{"start_sec": 1, "end_sec": 5, "title": "分段", "summary": "内容", "key_points": ["要点"]}]
					}`,
				},
			}},
		}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, nil)

	// Wait() returns immediately when nothing is running.
	svc.Wait()

	if _, err := svc.StartCourseBatch(context.Background(), courseAID, "missing_only"); err != nil {
		t.Fatalf("StartCourseBatch failed: %v", err)
	}

	select {
	case <-a1Entered:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for A1 to start")
	}

	// Enqueue single lesson B1 while batch is running A1; also verify idempotent re-trigger of B1 and A1.
	bJob1, err := svc.StartLessonSummary(context.Background(), lessonsB[0], summary.GenerateLessonOptions{})
	if err != nil {
		t.Fatalf("StartLessonSummary B1 failed: %v", err)
	}
	if bJob1.Stage != summary.StageQueued || !bJob1.Running {
		t.Fatalf("expected B1 queued and running, got %+v", bJob1)
	}
	bJob2, err := svc.StartLessonSummary(context.Background(), lessonsB[0], summary.GenerateLessonOptions{Force: true})
	if err != nil {
		t.Fatalf("idempotent StartLessonSummary B1 failed: %v", err)
	}
	if !bJob2.Running || bJob2.Stage != summary.StageQueued {
		t.Fatalf("expected idempotent queued status for B1, got %+v", bJob2)
	}

	// Idempotent re-trigger of currently running batch lesson A1 also returns live running status.
	a1Dup, err := svc.StartLessonSummary(context.Background(), lessonsA[0], summary.GenerateLessonOptions{})
	if err != nil {
		t.Fatalf("idempotent StartLessonSummary A1 failed: %v", err)
	}
	if !a1Dup.Running {
		t.Fatalf("expected A1 running snapshot, got %+v", a1Dup)
	}

	close(releaseA1)

	select {
	case <-b1Entered:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for priority job B1 to preempt batch")
	}

	preemptStatus, err := svc.GetCourseSummaryStatus(context.Background(), courseAID)
	if err != nil {
		t.Fatalf("GetCourseSummaryStatus during preemption failed: %v", err)
	}
	if preemptStatus.BatchJob == nil || !preemptStatus.BatchJob.Running {
		t.Fatalf("expected running batch job during preemption, got %+v", preemptStatus.BatchJob)
	}
	if preemptStatus.BatchJob.CurrentStage != summary.StageQueued ||
		preemptStatus.BatchJob.CurrentLessonID != "" ||
		preemptStatus.BatchJob.CurrentLessonTitle != "" ||
		preemptStatus.BatchJob.ChunkIndex != 0 ||
		preemptStatus.BatchJob.ChunkTotal != 0 {
		t.Fatalf("expected stale batch fields cleared during priority preemption, got %+v", preemptStatus.BatchJob)
	}

	close(releaseB1)
	svc.Wait()

	wantOrder := []string{"A1", "B1", "A2", "A3"}
	if fmt.Sprint(execOrder) != fmt.Sprint(wantOrder) {
		t.Fatalf("execution order = %v, want %v (single-lesson priority over batch)", execOrder, wantOrder)
	}

	// missing_only on Course A now skips all 3 lessons immediately with Running == false.
	noopBatch, err := svc.StartCourseBatch(context.Background(), courseAID, "missing_only")
	if err != nil {
		t.Fatalf("StartCourseBatch missing_only noop failed: %v", err)
	}
	if noopBatch.Running || noopBatch.Total != 3 || noopBatch.Skipped != 3 || noopBatch.CurrentStage != summary.StageCompleted {
		t.Fatalf("unexpected noop batch: %+v", noopBatch)
	}

	// Delete summary without and with deleteTranscript.
	if err := svc.DeleteLessonSummary(context.Background(), lessonsB[0], true); err != nil {
		t.Fatalf("DeleteLessonSummary failed: %v", err)
	}
	stB, err := svc.GetLessonSummaryState(context.Background(), lessonsB[0])
	if err != nil {
		t.Fatalf("GetLessonSummaryState B1 after delete failed: %v", err)
	}
	if stB.Summary != nil {
		t.Fatalf("expected nil summary after delete, got %+v", stB.Summary)
	}

	// Test CancelLessonSummary while active in flight.
	blockEntered := make(chan struct{})
	blockRT := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(blockEntered)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	svcCancel, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, blockRT, nil)
	if _, err := svcCancel.StartLessonSummary(context.Background(), lessonsB[0], summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("StartLessonSummary for cancel failed: %v", err)
	}
	select {
	case <-blockEntered:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for job to enter LLM call")
	}
	if _, err := svcCancel.CancelLessonSummary(lessonsB[0]); err != nil {
		t.Fatalf("CancelLessonSummary failed: %v", err)
	}
	svcCancel.Wait()

	cancelledState, err := svcCancel.GetLessonSummaryState(context.Background(), lessonsB[0])
	if err != nil {
		t.Fatalf("GetLessonSummaryState after cancel failed: %v", err)
	}
	if cancelledState.Job == nil || cancelledState.Job.Running || cancelledState.Job.Stage != summary.StageCancelled {
		t.Fatalf("expected cancelled job status, got %+v", cancelledState.Job)
	}
}

func TestCourseBatchMissingOnlyCancelDuringSkipCheck(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url": "https://mock-llm.local/v1",
		"ai_api_key":  "sk-llm",
		"ai_model":    "gpt-4o-mini",
	})

	// Exercise CancelCourseBatch racing against executeBatchItem's missing_only SQLite skip check
	// immediately after prioritized single-lesson jobs finish summarizing L1 and L2.
	for iter := 0; iter < 15; iter++ {
		courseID, lessonIDs, _ := seedTestCourse(t, conn, fmt.Sprintf("竞态取消测试-%d", iter), []lessonSeedOpts{
			{RelPath: "l1.mp4", SubtitleRel: "l1.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:02,000\nL1\n"},
			{RelPath: "l2.mp4", SubtitleRel: "l2.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:02,000\nL2\n"},
		})

		l1Entered := make(chan struct{})
		releaseL1 := make(chan struct{})
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(req.Body)
			if strings.Contains(string(raw), "L1") {
				select {
				case <-l1Entered:
				default:
					close(l1Entered)
				}
				select {
				case <-releaseL1:
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}
			return jsonResponse(http.StatusOK, map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role": "assistant",
						"content": `{
							"overview": "概览",
							"outline": [{"title": "模块", "summary": "描述", "start_sec": 1}],
							"timeline": [{"start_sec": 1, "end_sec": 2, "title": "分段", "summary": "内容", "key_points": ["要点"]}]
						}`,
					},
				}},
			}), nil
		})

		svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, nil)

		if _, err := svc.StartLessonSummary(context.Background(), lessonIDs[0], summary.GenerateLessonOptions{}); err != nil {
			t.Fatalf("iter %d StartLessonSummary L1 failed: %v", iter, err)
		}
		if _, err := svc.StartLessonSummary(context.Background(), lessonIDs[1], summary.GenerateLessonOptions{}); err != nil {
			t.Fatalf("iter %d StartLessonSummary L2 failed: %v", iter, err)
		}
		<-l1Entered

		// Enqueue missing_only batch while L1 and L2 are not yet persisted in lesson_summaries.
		if _, err := svc.StartCourseBatch(context.Background(), courseID, "missing_only"); err != nil {
			t.Fatalf("iter %d StartCourseBatch failed: %v", iter, err)
		}

		// Release L1/L2 and race CancelCourseBatch right as the worker enters batch skip checks.
		var cancelRes *summary.CourseBatchStatus
		var cancelErr error
		var cancelWG sync.WaitGroup
		cancelWG.Add(1)
		go func() {
			defer cancelWG.Done()
			if iter%3 != 0 {
				time.Sleep(time.Duration(iter%5) * 50 * time.Microsecond)
			}
			cancelRes, cancelErr = svc.CancelCourseBatch(courseID)
		}()
		close(releaseL1)
		cancelWG.Wait()
		if cancelErr != nil {
			t.Fatalf("iter %d CancelCourseBatch failed: %v", iter, cancelErr)
		}
		svc.Wait()

		st, err := svc.GetCourseSummaryStatus(context.Background(), courseID)
		if err != nil {
			t.Fatalf("iter %d GetCourseSummaryStatus failed: %v", iter, err)
		}
		if st.BatchJob == nil || st.BatchJob.Running {
			t.Fatalf("iter %d expected finished batch, got %+v", iter, st.BatchJob)
		}
		// Whenever CancelCourseBatch cancelled a running batch (returning StageCancelled),
		// a concurrent missing_only skip check must never overwrite CurrentStage to StageCompleted.
		if cancelRes.CurrentStage == summary.StageCancelled && st.BatchJob.CurrentStage != summary.StageCancelled {
			t.Fatalf("iter %d CurrentStage = %q after CancelCourseBatch returned %q, want %q",
				iter, st.BatchJob.CurrentStage, cancelRes.CurrentStage, summary.StageCancelled)
		}
	}
}

func TestASRChunkCheckpointResumeAndAtomicCleanup(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":     "https://mock-llm.local/v1",
		"ai_api_key":      "sk-llm",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "http://asr.local/v1",
		"ai_asr_model":    "whisper-1",
	})

	courseID, lessonIDs, mediaIDs := seedTestCourse(t, conn, "分片断点续转课程", []lessonSeedOpts{
		{RelPath: "long_resume.mp4", DurationMs: 3600_000},
	})
	lessonID, mediaID := lessonIDs[0], mediaIDs[0]

	extractor := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks: []summary.AudioChunk{
			{OffsetSec: 0, DurationSec: 1200},
			{OffsetSec: 1200, DurationSec: 1200},
			{OffsetSec: 2400, DurationSec: 1200},
		},
	}

	var mu sync.Mutex
	var asrCalls, llmCalls int
	failSecondChunk := true
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			asrCalls++
			raw, _ := io.ReadAll(req.Body)
			bodyStr := string(raw)
			switch {
			case strings.Contains(bodyStr, "fake-mp3-0"):
				return jsonResponse(http.StatusOK, map[string]any{
					"language": "zh",
					"segments": []map[string]any{{"start": 1.0, "end": 4.0, "text": "第一片内容"}},
				}), nil
			case strings.Contains(bodyStr, "fake-mp3-1"):
				if failSecondChunk {
					failSecondChunk = false
					return jsonResponse(http.StatusInternalServerError, map[string]any{
						"error": map[string]any{"message": "asr chunk 2 transient error"},
					}), nil
				}
				return jsonResponse(http.StatusOK, map[string]any{
					"language": "zh",
					"segments": []map[string]any{{"start": 2.0, "end": 5.0, "text": "第二片内容"}},
				}), nil
			default:
				return jsonResponse(http.StatusOK, map[string]any{
					"language": "zh",
					"segments": []map[string]any{{"start": 3.0, "end": 6.0, "text": "第三片内容"}},
				}), nil
			}
		}
		if strings.HasSuffix(req.URL.Path, "/chat/completions") {
			llmCalls++
			return jsonResponse(http.StatusOK, map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role": "assistant",
						"content": `{
							"overview": "断点续转后的完整总结",
							"outline": [{"title": "全节", "summary": "概览", "start_sec": 1}],
							"timeline": [{"start_sec": 1, "end_sec": 2406, "title": "完整分段", "summary": "内容", "key_points": ["要点"]}]
						}`,
					},
				}},
			}), nil
		}
		return jsonResponse(http.StatusNotFound, nil), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = extractor
	})

	// Run 1: chunk 0 succeeds, chunk 1 fails -> 1 checkpoint saved, 0 LLM calls.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("StartLessonSummary run 1 failed: %v", err)
	}
	svc.Wait()

	if asrCalls != 2 || llmCalls != 0 {
		t.Fatalf("after run 1: asrCalls=%d want 2, llmCalls=%d want 0", asrCalls, llmCalls)
	}

	var savedChunkCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM media_transcript_chunks WHERE media_file_id = ?`, mediaID).Scan(&savedChunkCount); err != nil {
		t.Fatalf("query checkpoints: %v", err)
	}
	if savedChunkCount != 1 {
		t.Fatalf("savedChunkCount = %d, want 1", savedChunkCount)
	}

	st1, err := svc.GetLessonSummaryState(context.Background(), lessonID)
	if err != nil {
		t.Fatalf("GetLessonSummaryState failed: %v", err)
	}
	if st1.Capabilities.PartialTranscriptChunks != 1 || st1.Capabilities.PartialTranscriptTotal != 3 {
		t.Fatalf("unexpected partial capabilities: %+v", st1.Capabilities)
	}

	cs1, err := svc.GetCourseSummaryStatus(context.Background(), courseID)
	if err != nil {
		t.Fatalf("GetCourseSummaryStatus failed: %v", err)
	}
	if cs1.Counts.PartialLessons != 1 || cs1.Counts.MissingTranscript != 1 || cs1.Counts.TranscriptReady != 0 {
		t.Fatalf("unexpected course counts during partial checkpoint: %+v", cs1.Counts)
	}
	if len(cs1.Lessons) != 1 || cs1.Lessons[0].TranscriptStatus != "partial" || cs1.Lessons[0].TranscribedChunks != 1 || cs1.Lessons[0].TotalChunks != 3 {
		t.Fatalf("unexpected lesson pipeline status: %+v", cs1.Lessons)
	}

	// Run 2: skips chunk 0, only calls ASR for chunk 1 and chunk 2 -> total ASR calls = 4.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("StartLessonSummary run 2 failed: %v", err)
	}
	svc.Wait()

	if asrCalls != 4 || llmCalls != 1 {
		t.Fatalf("after run 2: asrCalls=%d want 4 (resumed), llmCalls=%d want 1", asrCalls, llmCalls)
	}

	if err := conn.QueryRow(`SELECT COUNT(*) FROM media_transcript_chunks WHERE media_file_id = ?`, mediaID).Scan(&savedChunkCount); err != nil {
		t.Fatalf("query checkpoints after completion: %v", err)
	}
	if savedChunkCount != 0 {
		t.Fatalf("expected checkpoints cleaned up after finalization, got %d", savedChunkCount)
	}

	var vtt string
	if err := conn.QueryRow(`SELECT content_vtt FROM media_transcripts WHERE media_file_id = ?`, mediaID).Scan(&vtt); err != nil {
		t.Fatalf("query finalized transcript: %v", err)
	}
	cues := ai.ParseSubtitleCues(vtt)
	if len(cues) != 3 || cues[0].Text != "第一片内容" || cues[1].StartSec != 1202 || cues[2].StartSec != 2403 {
		t.Fatalf("unexpected merged cues after resume: %+v (vtt=%q)", cues, vtt)
	}
}

func TestASRSilentChunkPersistedAndAllEmptyCleansUp(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":     "https://mock-llm.local/v1",
		"ai_api_key":      "sk-llm",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "http://asr.local/v1",
		"ai_asr_model":    "whisper-1",
	})

	_, lessonIDs, mediaIDs := seedTestCourse(t, conn, "静音分片测试", []lessonSeedOpts{
		{RelPath: "silent_then_speech.mp4", DurationMs: 2400_000},
		{RelPath: "all_silent.mp4", DurationMs: 1200_000},
	})

	// Case 1: chunk 0 is silent (empty text/cues), chunk 1 fails once then succeeds on retry.
	ext1 := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks: []summary.AudioChunk{
			{OffsetSec: 0, DurationSec: 1200},
			{OffsetSec: 1200, DurationSec: 1200},
		},
	}
	var mu sync.Mutex
	var asrCalls1 int
	failOnce := true
	rt1 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			asrCalls1++
			raw, _ := io.ReadAll(req.Body)
			if strings.Contains(string(raw), "fake-mp3-0") {
				return jsonResponse(http.StatusOK, map[string]any{"language": "zh", "text": "", "segments": []any{}}), nil
			}
			if failOnce {
				failOnce = false
				return jsonResponse(http.StatusBadGateway, map[string]any{"error": map[string]any{"message": "temp err"}}), nil
			}
			return jsonResponse(http.StatusOK, map[string]any{
				"language": "zh",
				"segments": []map[string]any{{"start": 5.0, "end": 10.0, "text": "第二片人声"}},
			}), nil
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"overview":"总结","outline":[{"title":"段落","summary":"描述","start_sec":1205}],"timeline":[{"start_sec":1205,"end_sec":1210,"title":"人声段","summary":"内容","key_points":["要点"]}]}`,
				},
			}},
		}), nil
	})
	svc1, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt1, func(o *summary.Options) {
		o.AudioExtractor = ext1
	})
	if _, err := svc1.StartLessonSummary(context.Background(), lessonIDs[0], summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	svc1.Wait()
	if asrCalls1 != 2 {
		t.Fatalf("asrCalls1 after fail = %d, want 2", asrCalls1)
	}
	var cpCues string
	if err := conn.QueryRow(`SELECT cues_json FROM media_transcript_chunks WHERE media_file_id = ? AND chunk_index = 0`, mediaIDs[0]).Scan(&cpCues); err != nil {
		t.Fatalf("expected silent chunk 0 checkpoint persisted: %v", err)
	}
	if _, err := svc1.StartLessonSummary(context.Background(), lessonIDs[0], summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	svc1.Wait()
	if asrCalls1 != 3 {
		t.Fatalf("expected silent chunk 0 skipped on retry (total asrCalls1 = %d, want 3)", asrCalls1)
	}

	// Case 2: all chunks return empty -> fails with empty_transcript AND cleans up checkpoints.
	ext2 := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks:  []summary.AudioChunk{{OffsetSec: 0, DurationSec: 1200}},
	}
	rt2 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"language": "zh", "text": "", "segments": []any{}}), nil
	})
	svc2, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt2, func(o *summary.Options) {
		o.AudioExtractor = ext2
	})
	if _, err := svc2.StartLessonSummary(context.Background(), lessonIDs[1], summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("all-empty start failed: %v", err)
	}
	svc2.Wait()
	st2, _ := svc2.GetLessonSummaryState(context.Background(), lessonIDs[1])
	if st2.Job == nil || st2.Job.Stage != summary.StageFailed || st2.Job.ErrorCode != "empty_transcript" {
		t.Fatalf("expected empty_transcript failure, got %+v", st2.Job)
	}
	var leftover int
	_ = conn.QueryRow(`SELECT COUNT(*) FROM media_transcript_chunks WHERE media_file_id = ?`, mediaIDs[1]).Scan(&leftover)
	if leftover != 0 {
		t.Fatalf("expected 0 checkpoint rows after empty_transcript, got %d", leftover)
	}
}

func TestASRCheckpointInvalidatedOnDiskFileReplacementAndForceRetranscribe(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":     "https://mock-llm.local/v1",
		"ai_api_key":      "sk-llm",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "http://asr.local/v1",
		"ai_asr_model":    "whisper-1",
	})

	_, lessonIDs, mediaIDs := seedTestCourse(t, conn, "文件替换失效断点", []lessonSeedOpts{
		{RelPath: "replaceable.mp4", DurationMs: 2400_000},
	})
	lessonID, mediaID := lessonIDs[0], mediaIDs[0]

	var rootPath string
	if err := conn.QueryRow(`SELECT ss.root_path FROM media_files mf JOIN storage_sources ss ON ss.id = mf.source_id WHERE mf.id = ?`, mediaID).Scan(&rootPath); err != nil {
		t.Fatalf("query root_path: %v", err)
	}
	absMedia := filepath.Join(rootPath, "replaceable.mp4")

	ext := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks: []summary.AudioChunk{
			{OffsetSec: 0, DurationSec: 1200},
			{OffsetSec: 1200, DurationSec: 1200},
		},
	}

	var mu sync.Mutex
	var asrCalls int
	failSecond := true
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			asrCalls++
			raw, _ := io.ReadAll(req.Body)
			if strings.Contains(string(raw), "fake-mp3-1") && failSecond {
				failSecond = false
				return jsonResponse(http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": "fail chunk 1"}}), nil
			}
			return jsonResponse(http.StatusOK, map[string]any{
				"language": "zh",
				"segments": []map[string]any{{"start": 0.0, "end": 5.0, "text": fmt.Sprintf("语音-%d", asrCalls)}},
			}), nil
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"overview":"总结","outline":[{"title":"一","summary":"描述","start_sec":0}],"timeline":[{"start_sec":0,"end_sec":1205,"title":"段","summary":"内容","key_points":["点"]}]}`,
				},
			}},
		}), nil
	})
	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = ext
	})

	// Run 1: saves chunk 0 checkpoint, fails chunk 1.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	svc.Wait()
	if asrCalls != 2 {
		t.Fatalf("asrCalls after run 1 = %d, want 2", asrCalls)
	}

	// Replace video file on disk WITHOUT rescanning DB -> os.Stat fingerprint changes immediately!
	if err := os.WriteFile(absMedia, []byte("brand-new-replaced-video-content-longer"), 0o644); err != nil {
		t.Fatalf("overwrite media file: %v", err)
	}

	// Run 2: stale chunk 0 checkpoint must be invalidated, so both chunks 0 and 1 run again (asrCalls += 2 -> 4).
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	svc.Wait()
	if asrCalls != 4 {
		t.Fatalf("expected stale checkpoint invalidated on un-rescanned file replacement (asrCalls=%d, want 4)", asrCalls)
	}

	// Seed a leftover checkpoint then test ForceRetranscribe clears it and re-transcribes all chunks.
	if _, err := svc.StartLessonSummary(context.Background(), lessonID, summary.GenerateLessonOptions{Force: true, ForceRetranscribe: true}); err != nil {
		t.Fatalf("run 3 ForceRetranscribe failed: %v", err)
	}
	svc.Wait()
	if asrCalls != 6 {
		t.Fatalf("expected ForceRetranscribe to transcribe all 2 chunks (asrCalls=%d, want 6)", asrCalls)
	}
}

func TestCourseSummaryStatusPipelineCountsAndStatuses(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	courseID, lessonIDs, mediaIDs := seedTestCourse(t, conn, "状态统计课程", []lessonSeedOpts{
		{RelPath: "l1_ext.mp4", SubtitleRel: "l1_ext.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:02,000\n外挂\n"},
		{RelPath: "l2_emb.mp4", StreamsJSON: `[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"audio","codec_name":"aac"},{"index":2,"codec_type":"subtitle","codec_name":"subrip"}]`},
		{RelPath: "l3_asr.mp4"},
		{RelPath: "l4_manual.mp4"},
		{RelPath: "l5_partial.mp4"},
		{RelPath: "l6_none.mp4", StreamsJSON: `[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle"}]`},
	})

	now := time.Now().UTC().Format(time.RFC3339Nano)
	// l3 has ASR transcript
	if _, err := conn.Exec(
		`INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at)
		 VALUES (?, 'asr', 'zh', 'whisper-1', 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nhi\n', 'hi', ?, ?)`,
		mediaIDs[2], now, now,
	); err != nil {
		t.Fatalf("insert l3 asr transcript: %v", err)
	}
	// l4 has manual transcript
	if _, err := conn.Exec(
		`INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at)
		 VALUES (?, 'manual', 'und', '', '', '手动内容', ?, ?)`,
		mediaIDs[3], now, now,
	); err != nil {
		t.Fatalf("insert l4 manual transcript: %v", err)
	}
	// l5 has 2 of 4 partial chunks
	for idx := 0; idx < 2; idx++ {
		if _, err := conn.Exec(
			`INSERT INTO media_transcript_chunks (media_file_id, chunk_index, chunk_total, segment_sec, media_fingerprint, offset_sec, duration_sec, language, model, cues_json, created_at, updated_at)
			 VALUES (?, ?, 4, 1200, '', ?, 1200, 'zh', 'whisper-1', '[]', ?, ?)`,
			mediaIDs[4], idx, float64(idx*1200), now, now,
		); err != nil {
			t.Fatalf("insert l5 chunk %d: %v", idx, err)
		}
	}
	// l1 and l3 have summaries
	for _, lid := range []string{lessonIDs[0], lessonIDs[2]} {
		if _, err := conn.Exec(
			`INSERT INTO lesson_summaries (lesson_id, media_file_id, source_type, source_label, has_timestamps, model, overview, outline_json, timeline_json, created_at, updated_at)
			 VALUES (?, '', 'asr', 'AI 语音转写', 1, 'gpt-4o-mini', 'ov', '[]', '[]', ?, ?)`,
			lid, now, now,
		); err != nil {
			t.Fatalf("insert summary for %s: %v", lid, err)
		}
	}

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, nil), nil
	}), nil)

	st, err := svc.GetCourseSummaryStatus(context.Background(), courseID)
	if err != nil {
		t.Fatalf("GetCourseSummaryStatus failed: %v", err)
	}
	if len(st.SummaryLessonIDs) != 2 {
		t.Fatalf("SummaryLessonIDs = %v, want 2", st.SummaryLessonIDs)
	}
	if len(st.TranscriptLessonIDs) != 4 {
		t.Fatalf("TranscriptLessonIDs = %v, want 4 (l1..l4)", st.TranscriptLessonIDs)
	}
	wantCounts := summary.CoursePipelineCounts{
		TotalLessons:       6,
		SubtitleLessons:    2,
		TranscribedLessons: 2,
		TranscriptReady:    4,
		PartialLessons:     1,
		MissingTranscript:  2,
		SummarizedLessons:  2,
		ReadyToSummarize:   2,
	}
	if st.Counts != wantCounts {
		t.Fatalf("Counts = %+v, want %+v", st.Counts, wantCounts)
	}
	wantStatuses := []string{"external_subtitle", "embedded_subtitle", "asr", "manual", "partial", "none"}
	if len(st.Lessons) != 6 {
		t.Fatalf("Lessons len = %d, want 6", len(st.Lessons))
	}
	for i, want := range wantStatuses {
		if st.Lessons[i].TranscriptStatus != want {
			t.Fatalf("lesson[%d] TranscriptStatus = %q, want %q", i, st.Lessons[i].TranscriptStatus, want)
		}
	}
	if st.Lessons[2].AISubtitleUpdatedAt != now {
		t.Fatalf("lesson[2] AISubtitleUpdatedAt = %q, want %q", st.Lessons[2].AISubtitleUpdatedAt, now)
	}
	if st.Lessons[4].TranscribedChunks != 2 || st.Lessons[4].TotalChunks != 4 {
		t.Fatalf("lesson[4] partial counts = %d/%d, want 2/4", st.Lessons[4].TranscribedChunks, st.Lessons[4].TotalChunks)
	}
}

func seedMultiChapterCourse(t *testing.T, conn *sql.DB, chapters []struct {
	Title   string
	Lessons []lessonSeedOpts
}) (courseID string, chapterIDs []string, lessonIDs [][]string, mediaIDs [][]string) {
	t.Helper()
	var libID string
	if err := conn.QueryRow(`SELECT id FROM libraries LIMIT 1`).Scan(&libID); err != nil {
		t.Fatalf("query default library: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sourceID := id.New()
	rootDir := t.TempDir()
	if _, err := conn.Exec(
		`INSERT INTO storage_sources (id, name, driver, root_path, config_json, enabled, created_at, updated_at)
		 VALUES (?, 'MultiSrc', 'local', ?, '{}', 1, ?, ?)`,
		sourceID, rootDir, now, now,
	); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	courseID = id.New()
	if _, err := conn.Exec(
		`INSERT INTO courses (id, library_id, title, sort_title, overview, instructor, cover_path, backdrop_path, source_id, source_root_path, created_at, updated_at)
		 VALUES (?, ?, '多章节测试课程', '多章节测试课程', '', '', '', '', ?, ?, ?, ?)`,
		courseID, libID, sourceID, rootDir, now, now,
	); err != nil {
		t.Fatalf("insert course: %v", err)
	}

	for cIdx, ch := range chapters {
		chID := id.New()
		chapterIDs = append(chapterIDs, chID)
		if _, err := conn.Exec(
			`INSERT INTO chapters (id, course_id, title, sort_order, overview, created_at, updated_at)
			 VALUES (?, ?, ?, ?, '', ?, ?)`,
			chID, courseID, ch.Title, cIdx+1, now, now,
		); err != nil {
			t.Fatalf("insert chapter: %v", err)
		}
		var chLessons, chMedia []string
		for lIdx, opts := range ch.Lessons {
			mediaID := id.New()
			lessonID := id.New()
			relPath := opts.RelPath
			if relPath == "" {
				relPath = fmt.Sprintf("ch%d_l%d.mp4", cIdx+1, lIdx+1)
			}
			absMedia := filepath.Join(rootDir, filepath.FromSlash(relPath))
			_ = os.MkdirAll(filepath.Dir(absMedia), 0o755)
			if err := os.WriteFile(absMedia, []byte("video-"+relPath), 0o644); err != nil {
				t.Fatalf("write video: %v", err)
			}
			st, _ := os.Stat(absMedia)
			streamsJSON := opts.StreamsJSON
			if streamsJSON == "" {
				streamsJSON = `[{"index":0,"codec_type":"video","codec_name":"h264"},{"index":1,"codec_type":"audio","codec_name":"aac"}]`
			}
			durMs := opts.DurationMs
			if durMs <= 0 {
				durMs = 1200_000
			}
			if _, err := conn.Exec(
				`INSERT INTO media_files (id, source_id, relative_path, file_name, file_size, mod_time, status, duration_ms, container, video_codec, audio_codec, width, height, bitrate, streams_json, thumbnail_path, probed_at, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, 'active', ?, 'mp4', 'h264', 'aac', 1920, 1080, 1000000, ?, '', ?, ?, ?)`,
				mediaID, sourceID, relPath, filepath.Base(relPath), st.Size(), st.ModTime().UTC().Format(time.RFC3339Nano), durMs, streamsJSON, now, now, now,
			); err != nil {
				t.Fatalf("insert media_file: %v", err)
			}
			if opts.SubtitleRel != "" {
				subAbs := filepath.Join(rootDir, filepath.FromSlash(opts.SubtitleRel))
				_ = os.MkdirAll(filepath.Dir(subAbs), 0o755)
				if err := os.WriteFile(subAbs, []byte(opts.SubtitleBody), 0o644); err != nil {
					t.Fatalf("write sub: %v", err)
				}
				if _, err := conn.Exec(
					`INSERT INTO subtitle_files (id, source_id, relative_path, media_file_id, language, title, format, is_default, status)
					 VALUES (?, ?, ?, ?, 'zh', '字幕', 'srt', 1, 'active')`,
					id.New(), sourceID, opts.SubtitleRel, mediaID,
				); err != nil {
					t.Fatalf("insert subtitle_file: %v", err)
				}
			}
			if _, err := conn.Exec(
				`INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, overview, sort_order, cover_path, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, '', ?, '', ?, ?)`,
				lessonID, courseID, chID, mediaID, fmt.Sprintf("第%d-%d课", cIdx+1, lIdx+1), lIdx+1, now, now,
			); err != nil {
				t.Fatalf("insert lesson: %v", err)
			}
			chLessons = append(chLessons, lessonID)
			chMedia = append(chMedia, mediaID)
		}
		lessonIDs = append(lessonIDs, chLessons)
		mediaIDs = append(mediaIDs, chMedia)
	}
	return courseID, chapterIDs, lessonIDs, mediaIDs
}

func TestDecoupledBatchTranscribeAndBatchSummarizeConcurrent(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":     "https://mock-llm.local/v1",
		"ai_api_key":      "sk-llm",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "http://asr.local/v1",
		"ai_asr_model":    "whisper-1",
	})

	courseID, chapterIDs, lessonIDs, _ := seedMultiChapterCourse(t, conn, []struct {
		Title   string
		Lessons []lessonSeedOpts
	}{
		{
			Title: "第一章-有字幕",
			Lessons: []lessonSeedOpts{
				{RelPath: "c1_l1.mp4", SubtitleRel: "c1_l1.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\nC1L1\n"},
				{RelPath: "c1_l2.mp4"},
			},
		},
		{
			Title: "第二章-无字幕",
			Lessons: []lessonSeedOpts{
				{RelPath: "c2_l1.mp4"},
				{RelPath: "c2_l2.mp4", SubtitleRel: "c2_l2.srt", SubtitleBody: "1\n00:00:01,000 --> 00:00:05,000\nC2L2\n"},
			},
		},
	})

	ext := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks:  []summary.AudioChunk{{OffsetSec: 0, DurationSec: 600}},
	}

	asrEntered := make(chan struct{}, 4)
	llmEntered := make(chan struct{}, 4)
	releaseBoth := make(chan struct{})
	var mu sync.Mutex
	var asrCalls, llmCalls int

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			mu.Lock()
			asrCalls++
			mu.Unlock()
			select {
			case asrEntered <- struct{}{}:
			default:
			}
			<-releaseBoth
			return jsonResponse(http.StatusOK, map[string]any{
				"language": "zh",
				"segments": []map[string]any{{"start": 0.0, "end": 5.0, "text": "转写内容"}},
			}), nil
		}
		mu.Lock()
		llmCalls++
		mu.Unlock()
		select {
		case llmEntered <- struct{}{}:
		default:
		}
		<-releaseBoth
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"overview":"总结","outline":[{"title":"一","summary":"描述","start_sec":1}],"timeline":[{"start_sec":1,"end_sec":5,"title":"段","summary":"内容","key_points":["点"]}]}`,
				},
			}},
		}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = ext
	})

	// Launch chapter 2 batch transcribe AND whole-course batch summarize concurrently!
	trBatch, err := svc.StartCourseTranscribeBatch(context.Background(), courseID, summary.CourseBatchOptions{
		Mode:      "missing_only",
		ChapterID: chapterIDs[1],
	})
	if err != nil {
		t.Fatalf("StartCourseTranscribeBatch failed: %v", err)
	}
	if trBatch.Kind != summary.BatchKindTranscribe || trBatch.ChapterID != chapterIDs[1] || trBatch.Total != 2 || trBatch.SkippedAlreadyDone != 1 {
		t.Fatalf("unexpected initial transcribe batch: %+v", trBatch)
	}

	sumBatch, err := svc.StartCourseSummaryBatch(context.Background(), courseID, summary.CourseBatchOptions{
		Mode: "missing_only",
	})
	if err != nil {
		t.Fatalf("StartCourseSummaryBatch failed: %v", err)
	}
	if sumBatch.Kind != summary.BatchKindSummarize || sumBatch.Total != 4 || sumBatch.SkippedNoTranscript != 2 {
		t.Fatalf("unexpected initial summary batch: %+v", sumBatch)
	}

	// Prove both workers are in-flight simultaneously (no mutual blocking).
	select {
	case <-asrEntered:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for transcribeWorker")
	}
	select {
	case <-llmEntered:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for summaryWorker while transcribeWorker is blocked")
	}

	close(releaseBoth)
	svc.Wait()

	if asrCalls != 1 || llmCalls != 2 {
		t.Fatalf("calls: asr=%d want 1 (ch2 c2_l1 only), llm=%d want 2 (c1_l1 & c2_l2 only)", asrCalls, llmCalls)
	}

	st, err := svc.GetCourseSummaryStatus(context.Background(), courseID)
	if err != nil {
		t.Fatalf("GetCourseSummaryStatus failed: %v", err)
	}
	if st.TranscribeBatchJob == nil || st.TranscribeBatchJob.Running || st.TranscribeBatchJob.Completed != 1 || st.TranscribeBatchJob.SkippedAlreadyDone != 1 {
		t.Fatalf("unexpected TranscribeBatchJob: %+v", st.TranscribeBatchJob)
	}
	if st.SummaryBatchJob == nil || st.SummaryBatchJob.Running || st.SummaryBatchJob.Completed != 2 || st.SummaryBatchJob.SkippedNoTranscript != 2 {
		t.Fatalf("unexpected SummaryBatchJob: %+v", st.SummaryBatchJob)
	}
	// Ch1's c1_l2 was NOT transcribed because transcribe batch was scoped to Ch2.
	for _, lp := range st.Lessons {
		if lp.LessonID == lessonIDs[0][1] && lp.TranscriptReady {
			t.Fatalf("expected ch1 l2 to remain untranscribed after ch2-scoped batch")
		}
		if lp.LessonID == lessonIDs[1][0] && (!lp.TranscriptReady || lp.HasSummary) {
			t.Fatalf("expected ch2 l1 transcribed without summary, got %+v", lp)
		}
	}
}

func TestBatchTranscribeWorksWithoutAIConfigured(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_asr_base_url": "http://asr.local/v1",
		"ai_asr_model":    "whisper-1",
		// Intentionally NO ai_base_url / ai_model
	})

	courseID, lessonIDs, mediaIDs := seedTestCourse(t, conn, "纯ASR课程", []lessonSeedOpts{
		{RelPath: "l1.mp4"},
		{RelPath: "l2.mp4"},
	})
	ext := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks:  []summary.AudioChunk{{OffsetSec: 0, DurationSec: 300}},
	}
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{
			"language": "zh",
			"segments": []map[string]any{{"start": 0.0, "end": 4.0, "text": "纯转写文本"}},
		}), nil
	})
	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = ext
	})

	// Single-lesson transcribe-only works without AI configured
	job, err := svc.StartLessonTranscribe(context.Background(), lessonIDs[0], summary.TranscribeLessonOptions{})
	if err != nil {
		t.Fatalf("StartLessonTranscribe without AI failed: %v", err)
	}
	if job.Action != summary.ActionTranscribe {
		t.Fatalf("Action = %q, want transcribe", job.Action)
	}
	svc.Wait()

	// Course batch transcribe skips l1 (already done) and transcribes l2 without AI configured
	batch, err := svc.StartCourseTranscribeBatch(context.Background(), courseID, summary.CourseBatchOptions{Mode: "missing_only"})
	if err != nil {
		t.Fatalf("StartCourseTranscribeBatch without AI failed: %v", err)
	}
	if batch.SkippedAlreadyDone != 1 || !batch.Running {
		t.Fatalf("unexpected transcribe batch: %+v", batch)
	}
	svc.Wait()

	for _, mid := range mediaIDs {
		var cnt int
		_ = conn.QueryRow(`SELECT COUNT(*) FROM media_transcripts WHERE media_file_id = ?`, mid).Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("expected transcript persisted for media %s", mid)
		}
	}
}

func TestCrossWorkerCollisionWaitsWithoutDeadlock(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":     "https://mock-llm.local/v1",
		"ai_api_key":      "sk-llm",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "http://asr.local/v1",
		"ai_asr_model":    "whisper-1",
	})

	courseID, _, mediaIDs := seedTestCourse(t, conn, "跨Worker碰撞课程", []lessonSeedOpts{
		{RelPath: "collide.mp4"},
	})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := conn.Exec(
		`INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at)
		 VALUES (?, 'asr', 'zh', 'whisper-1', 'WEBVTT\n\n00:00:00.000 --> 00:00:05.000\n旧文稿\n', '旧文稿', ?, ?)`,
		mediaIDs[0], now, now,
	); err != nil {
		t.Fatalf("seed cached transcript: %v", err)
	}

	ext := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks:  []summary.AudioChunk{{OffsetSec: 0, DurationSec: 300}},
	}
	asrEntered := make(chan struct{})
	releaseASR := make(chan struct{})
	var mu sync.Mutex
	var events []string

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			mu.Lock()
			events = append(events, "asr_start")
			mu.Unlock()
			close(asrEntered)
			<-releaseASR
			mu.Lock()
			events = append(events, "asr_end")
			mu.Unlock()
			return jsonResponse(http.StatusOK, map[string]any{
				"language": "zh",
				"segments": []map[string]any{{"start": 0.0, "end": 5.0, "text": "最新转写文稿"}},
			}), nil
		}
		raw, _ := io.ReadAll(req.Body)
		mu.Lock()
		events = append(events, "llm:"+fmt.Sprintf("%v", strings.Contains(string(raw), "最新转写文稿")))
		mu.Unlock()
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"overview":"总结","outline":[{"title":"一","summary":"描述","start_sec":0}],"timeline":[{"start_sec":0,"end_sec":5,"title":"段","summary":"内容","key_points":["点"]}]}`,
				},
			}},
		}), nil
	})
	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = ext
	})

	if _, err := svc.StartCourseTranscribeBatch(context.Background(), courseID, summary.CourseBatchOptions{Mode: "all"}); err != nil {
		t.Fatalf("StartCourseTranscribeBatch failed: %v", err)
	}
	<-asrEntered

	// Start batch summary targeting the same lesson while transcribeWorker holds it.
	if _, err := svc.StartCourseSummaryBatch(context.Background(), courseID, summary.CourseBatchOptions{Mode: "all"}); err != nil {
		t.Fatalf("StartCourseSummaryBatch failed: %v", err)
	}

	// Give summaryWorker a moment to hit the collision gate and verify it did NOT enter LLM prematurely.
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	earlyEvents := append([]string(nil), events...)
	mu.Unlock()
	if len(earlyEvents) != 1 || earlyEvents[0] != "asr_start" {
		t.Fatalf("summaryWorker violated collision gate while ASR held lesson: events=%v", earlyEvents)
	}

	close(releaseASR)
	svc.Wait()

	want := []string{"asr_start", "asr_end", "llm:true"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v (summary must wait for ASR and see updated transcript)", events, want)
	}
}

func TestLessonJobConflictOppositeActionsAndSingleHandoff(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":     "https://mock-llm.local/v1",
		"ai_api_key":      "sk-llm",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "http://asr.local/v1",
		"ai_asr_model":    "whisper-1",
	})

	_, lessonIDs, mediaIDs := seedTestCourse(t, conn, "冲突与交接课程", []lessonSeedOpts{
		{RelPath: "l1_only_asr.mp4"},
		{RelPath: "l2_handoff.mp4"},
		{RelPath: "l3_cancel_llm.mp4"},
	})
	ext := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks:  []summary.AudioChunk{{OffsetSec: 0, DurationSec: 300}},
	}

	asrEntered := make(chan struct{}, 4)
	releaseASR := make(chan struct{})
	llmEntered := make(chan struct{}, 4)
	releaseLLM := make(chan struct{})

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			select {
			case asrEntered <- struct{}{}:
			default:
			}
			select {
			case <-releaseASR:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			return jsonResponse(http.StatusOK, map[string]any{
				"language": "zh",
				"segments": []map[string]any{{"start": 0.0, "end": 5.0, "text": "转写结果"}},
			}), nil
		}
		select {
		case llmEntered <- struct{}{}:
		default:
		}
		select {
		case <-releaseLLM:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"overview":"交接总结","outline":[{"title":"一","summary":"描述","start_sec":0}],"timeline":[{"start_sec":0,"end_sec":5,"title":"段","summary":"内容","key_points":["点"]}]}`,
				},
			}},
		}), nil
	})

	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = ext
	})

	// 1. While L1 is running transcribe-only, StartLessonSummary returns ErrLessonJobConflict.
	if _, err := svc.StartLessonTranscribe(context.Background(), lessonIDs[0], summary.TranscribeLessonOptions{}); err != nil {
		t.Fatalf("StartLessonTranscribe L1 failed: %v", err)
	}
	<-asrEntered
	if _, err := svc.StartLessonSummary(context.Background(), lessonIDs[0], summary.GenerateLessonOptions{}); !errors.Is(err, summary.ErrLessonJobConflict) {
		t.Fatalf("expected ErrLessonJobConflict when summarizing transcribing lesson, got %v", err)
	}
	close(releaseASR)
	svc.Wait()

	// 2. Handoff: L2 starts ActionTranscribeAndSummarize, transitions from ASR -> LLM seamlessly.
	releaseASR = make(chan struct{})
	close(releaseASR) // allow ASR immediately, block in LLM
	j2, err := svc.StartLessonSummary(context.Background(), lessonIDs[1], summary.GenerateLessonOptions{})
	if err != nil {
		t.Fatalf("StartLessonSummary L2 failed: %v", err)
	}
	if j2.Action != summary.ActionTranscribeAndSummarize {
		t.Fatalf("Action = %q, want transcribe_and_summarize", j2.Action)
	}
	<-llmEntered
	// While L2 is in LLM phase of handoff, StartLessonTranscribe returns idempotent running snapshot (same job) or if ActionSummarize returns conflict;
	// for L2 (ActionTranscribeAndSummarize), StartLessonTranscribe is idempotent:
	dup, err := svc.StartLessonTranscribe(context.Background(), lessonIDs[1], summary.TranscribeLessonOptions{})
	if err != nil || !dup.Running {
		t.Fatalf("expected idempotent snapshot for L2 transcribe_and_summarize, got job=%+v err=%v", dup, err)
	}
	close(releaseLLM)
	svc.Wait()

	st2, _ := svc.GetLessonSummaryState(context.Background(), lessonIDs[1])
	if st2.Summary == nil || st2.Summary.Overview != "交接总结" {
		t.Fatalf("expected handoff summary saved for L2, got %+v", st2.Summary)
	}

	// 3. Cancel L3 during LLM phase after ASR finishes -> transcript is kept, summary is nil, job is cancelled.
	releaseLLM = make(chan struct{})
	if _, err := svc.StartLessonSummary(context.Background(), lessonIDs[2], summary.GenerateLessonOptions{}); err != nil {
		t.Fatalf("StartLessonSummary L3 failed: %v", err)
	}
	<-llmEntered
	if _, err := svc.CancelLessonSummary(lessonIDs[2]); err != nil {
		t.Fatalf("CancelLessonSummary L3 failed: %v", err)
	}
	svc.Wait()

	st3, _ := svc.GetLessonSummaryState(context.Background(), lessonIDs[2])
	if st3.Job == nil || st3.Job.Stage != summary.StageCancelled || st3.Summary != nil {
		t.Fatalf("expected cancelled job and nil summary for L3, got job=%+v summary=%+v", st3.Job, st3.Summary)
	}
	var trCount int
	_ = conn.QueryRow(`SELECT COUNT(*) FROM media_transcripts WHERE media_file_id = ?`, mediaIDs[2]).Scan(&trCount)
	if trCount != 1 {
		t.Fatalf("expected ASR transcript retained when cancelled during LLM phase, got %d", trCount)
	}
}

func TestForceRetranscribeHandoffPrefersASRWhenPhysicalSubtitleExists(t *testing.T) {
	conn, dataDir, catSvc, mediaSvc := openTestEnv(t)
	setAppSettings(t, conn, map[string]string{
		"ai_base_url":     "https://mock-llm.local/v1",
		"ai_api_key":      "sk-llm",
		"ai_model":        "gpt-4o-mini",
		"ai_asr_base_url": "http://asr.local/v1",
		"ai_asr_model":    "whisper-1",
	})

	_, lessonIDs, _ := seedTestCourse(t, conn, "强转覆盖外挂字幕", []lessonSeedOpts{
		{
			RelPath:      "sub_and_asr.mp4",
			SubtitleRel:  "sub_and_asr.srt",
			SubtitleBody: "1\n00:00:00,000 --> 00:00:03,000\nPHYSICAL_SUBTITLE_TEXT\n",
		},
	})
	ext := &fakeAudioExtractor{
		dataDir: dataDir,
		chunks:  []summary.AudioChunk{{OffsetSec: 0, DurationSec: 60}},
	}
	var seenASRTextInLLM bool
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/audio/transcriptions") {
			return jsonResponse(http.StatusOK, map[string]any{
				"language": "zh",
				"segments": []map[string]any{{"start": 0.0, "end": 5.0, "text": "FRESH_ASR_TRANSCRIPT_TEXT"}},
			}), nil
		}
		raw, _ := io.ReadAll(req.Body)
		if strings.Contains(string(raw), "FRESH_ASR_TRANSCRIPT_TEXT") {
			seenASRTextInLLM = true
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"overview":"基于新ASR的总结","outline":[{"title":"一","summary":"描述","start_sec":0}],"timeline":[{"start_sec":0,"end_sec":5,"title":"段","summary":"内容","key_points":["点"]}]}`,
				},
			}},
		}), nil
	})
	svc, _ := newTestService(t, conn, dataDir, catSvc, mediaSvc, rt, func(o *summary.Options) {
		o.AudioExtractor = ext
	})

	if _, err := svc.StartLessonSummary(context.Background(), lessonIDs[0], summary.GenerateLessonOptions{
		Force:             true,
		ForceRetranscribe: true,
	}); err != nil {
		t.Fatalf("StartLessonSummary ForceRetranscribe failed: %v", err)
	}
	svc.Wait()

	if !seenASRTextInLLM {
		t.Fatalf("expected ForceRetranscribe handoff to feed fresh ASR transcript to LLM")
	}
	st, err := svc.GetLessonSummaryState(context.Background(), lessonIDs[0])
	if err != nil {
		t.Fatalf("GetLessonSummaryState failed: %v", err)
	}
	if st.Summary == nil || st.Summary.SourceType != "asr" {
		t.Fatalf("expected Summary.SourceType = asr, got %+v", st.Summary)
	}
}

