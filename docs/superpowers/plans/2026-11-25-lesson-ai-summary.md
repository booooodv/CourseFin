# Lesson AI Summary & Subtitle-Free ASR Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-lesson structured AI summaries (video outline + chronological timestamped content) with one-click Markdown note export, single-lesson and course-wide batch background generation, and an OpenAI-compatible ASR pipeline that persists playable AI subtitles when videos have no subtitles.

**Architecture:** Introduce a dedicated `internal/summary` background worker service (parallel to `internal/scanner` and `internal/updater`) wired into `server.Server` and `/api/v1` REST handlers, backed by two new SQLite tables (`media_transcripts` and `lesson_summaries`). When a lesson has neither external nor embedded text subtitles, FFmpeg extracts 16kHz mono MP3 chunks into `{dataDir}/cache/asr`, calls an OpenAI-compatible `/audio/transcriptions` client with format fallback (`verbose_json -> srt -> json`), saves merged WebVTT into `media_transcripts`, and exposes it via `internal/media` as a playable `AI 转写字幕` stream for both the Web Artplayer UI and Jellyfin clients; the Web `CourseDetailPage` right sidebar gains a `课程目录 / AI 总结` tab switcher with timestamp click-to-seek, live active-section highlight, manual transcript fallback, batch progress banner, and Markdown copy.

**Tech Stack:** Go 1.26 (`CGO_ENABLED=0`, stdlib `net/http`, `mime/multipart`, `os/exec`, `modernc.org/sqlite`, `chi/v5`), FFmpeg/FFprobe, React 19 + TypeScript + Tailwind CSS v4 semantic tokens + Artplayer + Vitest/Testing Library.

**Spec:** `docs/superpowers/specs/2026-11-25-lesson-ai-summary-design.md`

## Global Constraints

- Backend stays `CGO_ENABLED=0` pure Go with zero new third-party `go.mod` dependencies; frontend adds zero new `package.json` dependencies.
- Never write any file into user media storage directories (`/media` may be mounted `:ro`); temporary ASR audio chunks live exclusively under `{dataDir}/cache/asr/{jobID}` and are deleted on completion, failure, cancellation, or shutdown; AI subtitles live in SQLite `media_transcripts`.
- All new UI uses `web/src/index.css` semantic tokens (`bg-card`, `bg-subtle`, `text-ink-primary`, `bg-accent`, `bg-accent-soft`, `text-accent-ink`, `text-status-danger-ink`, etc.) — zero hardcoded `slate/indigo/rose/emerald/sky` classes.
- Backward-compatible API & Jellyfin contracts: existing `GET/PUT /api/v1/settings`, `GET /api/v1/courses/{id}`, `GET /api/v1/lessons/{id}/playback`, and Jellyfin `PlaybackInfo`/`Subtitles` only receive additive fields or appended streams after physical subtitles.
- Never forward main `ai_api_key` when an independent `ai_asr_base_url` is set and `ai_asr_api_key` is empty; never overwrite an existing saved `lesson_summaries` row if regeneration fails or is cancelled.
- Existing `ai_max_subtitle_chars` remains exclusively for course AI organize; single-lesson summarization uses its own budget (`24000` single-pass / `18000` map-reduce chunk runes).

## Review Focus

1. **YouTube/auto-caption rolling VTT duplicates and inline cue tags (`<00:01:02.000><c>text</c>`, ASS `{\pos(...)}`)**: Compaction strips cue markup tags and deduplicates consecutive identical caption lines before sending to LLM. *(Pinned in Task 4: `TestCueCompactionStripsTagsAndDedupesRollingCaptions`)*
2. **Out-of-bounds, negative, inverted, or unsorted LLM timestamps**: Validation clamps timestamps to `[0, durationSec]`, enforces `end_sec >= start_sec`, and stably sorts timeline sections chronologically (or clears all timestamps to `nil` when `has_timestamps == false`). *(Pinned in Task 2: `TestValidateAndNormalizeSummaryClampsAndSortsTimestamps`)*
3. **Failed or cancelled regeneration must preserve existing summary and remove ASR temp dir**: When a lesson already has a stored summary and regeneration fails/cancels, the existing `lesson_summaries` row remains intact and `{dataDir}/cache/asr/{jobID}` is deleted. *(Pinned in Task 4: `TestRegenerateFailurePreservesExistingSummaryAndCleansTempDir`)*
4. **ASR key isolation across hosts & stale async lesson switches on frontend**: Independent `ai_asr_base_url` with empty `ai_asr_api_key` sends no `Authorization` header; rapid lesson switching `A -> B` in `CourseDetailPage` never lets A's delayed summary response overwrite B's panel. *(Pinned in Task 5: `TestASRConfigDoesNotLeakMainKeyToDifferentHost` and Task 8: `ignores stale summary response after switching lessons`)*
5. **Batch fault isolation & single-lesson priority/cancellation**: A course batch encountering one lesson without audio or without subtitles when ASR is unconfigured records that lesson's error and continues processing remaining lessons; cancelling terminates active context and stops remaining items. *(Pinned in Task 4: `TestCourseBatchContinuesOnSingleLessonFailureAndSupportsCancel`)*

---

## File Structure

| Path | Action | Responsibility |
|---|---|---|
| `internal/db/migrations/003_lesson_ai_summary.sql` | Create | Schema for `media_transcripts` and `lesson_summaries` with `ON DELETE CASCADE` |
| `internal/db/db.go` | Modify | Ensure `{dataDir}/cache/asr` exists in `ensureDataDirs` |
| `internal/ai/asr.go` | Create | OpenAI-compatible `/audio/transcriptions` multipart client, format fallback (`verbose_json -> srt -> json`), silent WAV generator, VTT/SRT cue helpers |
| `internal/ai/summary.go` | Create | `OutlineItem`, `TimelineSection`, single-pass & Map-Reduce lesson summary methods on `OpenAICompatibleProvider`, JSON validation & timestamp normalization |
| `internal/media/streamer.go` | Modify | Resolve and expose `FFmpegBin()`, `FFprobeBin()`, `HasFFprobe()` |
| `internal/media/subtitle.go` | Modify | Append `media_transcripts` (`source='asr'`) as `AI 转写字幕` in `BuildStreamDescriptors` & `ServeSubtitle`; export `ExtractBestSubtitleVTT` and `ResolveLocalMediaInfo` |
| `internal/summary/cue_compact.go` | Create | WebVTT/SRT parsing, markup stripping, rolling-caption dedup, ~15s/200-rune compaction, chunk splitting, manual transcript timestamp detection |
| `internal/summary/audio.go` | Create | FFmpeg 16kHz mono 48kbps MP3 segment extraction to `{dataDir}/cache/asr/{jobID}`, ffprobe chunk duration calculation, temp cleanup |
| `internal/summary/service.go` | Create | Background worker loop (serial worker with single-lesson priority over course batch), job/batch status tracking, cancellation, persistence |
| `internal/api/handlers_ai.go` | Modify | Extend `settingsPayload`, `buildSettingsResponse`, `handleUpdateSettings` with ASR fields & `ResolveASRConfig` |
| `internal/api/handlers_summary.go` | Create | `handleTestASR`, single-lesson summary CRUD/generate/cancel, course batch status/generate/cancel |
| `internal/api/router.go` | Modify | Add `Summary *summary.Service` to `Deps`/`handler` and register summary & ASR test routes |
| `internal/server/server.go` | Modify | Instantiate `summary.Service`, inject into `api.Deps`, wait on `Close()` |
| `web/src/types/coursefin.ts` | Modify | Add ASR settings fields and lesson/batch summary DTO types |
| `web/src/api/client.ts` | Modify | Add `code?: string` on `ApiError` and add ASR test + lesson/course summary API methods |
| `web/src/utils/summaryMarkdown.ts` | Create | `formatTimestamp` and `formatLessonSummaryMarkdown` pure formatting utilities |
| `web/src/pages/SettingsPage.tsx` | Modify | Add ASR configuration section with fallback hints, clear-key toggle, and test button |
| `web/src/components/VideoPlayer.tsx` | Modify | Expose `VideoPlayerHandle.seekTo` via `forwardRef`; update subtitle menus in-place without tearing down Artplayer |
| `web/src/components/LessonSummaryPanel.tsx` | Create | AI summary tab UI (batch banner, outline, chronological timeline with seek & active highlight, copy Markdown, empty/ASR/manual-paste/progress/error states) |
| `web/src/pages/CourseDetailPage.tsx` | Modify | Right sidebar `课程目录 / AI 总结` tab switcher, summary badges, batch controls, `VideoPlayerHandle` wiring, live time tracking, non-disruptive AI subtitle refresh |

---

### Task 1: Database Migration `003_lesson_ai_summary.sql` & ASR Cache Dir

**Files:**
- Create: `internal/db/migrations/003_lesson_ai_summary.sql`
- Modify: `internal/db/db.go:55-69`
- Test: `internal/db/db_test.go`

**Interfaces:**
- Produces: SQLite tables `media_transcripts(media_file_id PK, source, language, model, content_vtt, plain_text, created_at, updated_at)` and `lesson_summaries(lesson_id PK, media_file_id, source_type, source_label, has_timestamps, model, overview, outline_json, timeline_json, created_at, updated_at)`; directory `{dataDir}/cache/asr`.

- [ ] **Step 1: Write failing migration & cascade tests in `internal/db/db_test.go`**

```go
func TestMigration003CreatesSummaryTablesAndCascades(t *testing.T) {
	dataDir := t.TempDir()
	conn, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	// Verify cache/asr directory exists
	info, err := os.Stat(filepath.Join(dataDir, "cache", "asr"))
	if err != nil || !info.IsDir() {
		t.Fatalf("expected cache/asr directory to exist: err=%v", err)
	}

	// Verify both tables exist
	for _, tbl := range []string{"media_transcripts", "lesson_summaries"} {
		var name string
		if err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil {
			t.Fatalf("expected table %q: %v", tbl, err)
		}
	}

	// Seed source -> media_file -> course -> chapter -> lesson, then insert transcript & summary.
	// Deleting lesson must cascade-delete lesson_summaries while keeping media_transcripts;
	// deleting media_file must cascade-delete media_transcripts.
	now := "2026-11-25T00:00:00Z"
	var libID string
	if err := conn.QueryRow(`SELECT id FROM libraries LIMIT 1`).Scan(&libID); err != nil {
		t.Fatalf("query default library: %v", err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	mustExec(`INSERT INTO storage_sources (id, name, driver, root_path, created_at, updated_at) VALUES ('s1','src','local','/tmp',?,?)`, now, now)
	mustExec(`INSERT INTO media_files (id, source_id, relative_path, file_name, created_at, updated_at) VALUES ('m1','s1','a.mp4','a.mp4',?,?)`, now, now)
	mustExec(`INSERT INTO courses (id, library_id, title, created_at, updated_at) VALUES ('c1',?,'Course',?,?)`, libID, now, now)
	mustExec(`INSERT INTO chapters (id, course_id, title, sort_order, created_at, updated_at) VALUES ('ch1','c1','Ch1',1,?,?)`, now, now)
	mustExec(`INSERT INTO lessons (id, course_id, chapter_id, media_file_id, title, sort_order, created_at, updated_at) VALUES ('l1','c1','ch1','m1','L1',1,?,?)`, now, now)
	mustExec(`INSERT INTO media_transcripts (media_file_id, source, language, model, content_vtt, plain_text, created_at, updated_at) VALUES ('m1','asr','zh','whisper-1','WEBVTT','',?,?)`, now, now)
	mustExec(`INSERT INTO lesson_summaries (lesson_id, media_file_id, source_type, source_label, has_timestamps, model, overview, outline_json, timeline_json, created_at, updated_at) VALUES ('l1','m1','asr','AI 语音转写',1,'gpt-4o-mini','ov','[]','[]',?,?)`, now, now)

	mustExec(`DELETE FROM lessons WHERE id = 'l1'`)
	var sumCount, trCount int
	_ = conn.QueryRow(`SELECT COUNT(*) FROM lesson_summaries WHERE lesson_id = 'l1'`).Scan(&sumCount)
	_ = conn.QueryRow(`SELECT COUNT(*) FROM media_transcripts WHERE media_file_id = 'm1'`).Scan(&trCount)
	if sumCount != 0 || trCount != 1 {
		t.Fatalf("after deleting lesson: summaries=%d want 0, transcripts=%d want 1", sumCount, trCount)
	}

	mustExec(`DELETE FROM media_files WHERE id = 'm1'`)
	_ = conn.QueryRow(`SELECT COUNT(*) FROM media_transcripts WHERE media_file_id = 'm1'`).Scan(&trCount)
	if trCount != 0 {
		t.Fatalf("after deleting media_file: transcripts=%d want 0", trCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db -run TestMigration003 -v`
Expected: FAIL (`expected cache/asr directory to exist` / missing tables).

- [ ] **Step 3: Create `internal/db/migrations/003_lesson_ai_summary.sql` and update `ensureDataDirs` in `internal/db/db.go`**

Create `internal/db/migrations/003_lesson_ai_summary.sql` with exact schema from Spec §5.1 (`media_transcripts` and `lesson_summaries`). Add `filepath.Join(dataDir, "cache", "asr")` to `dirs` slice in `ensureDataDirs` (`internal/db/db.go:55-69`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/db -v`
Expected: PASS (all existing and new db tests pass).

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/003_lesson_ai_summary.sql internal/db/db.go internal/db/db_test.go
git commit -m "feat(db): add media_transcripts and lesson_summaries migration"
```

---

### Task 2: AI ASR Client (`internal/ai/asr.go`) & Lesson Summary Prompts/Validation (`internal/ai/summary.go`)

**Files:**
- Create: `internal/ai/asr.go`
- Create: `internal/ai/summary.go`
- Test: `internal/ai/asr_summary_test.go`

**Interfaces:**
- Consumes: `ai.Config`, `ai.ChatRequest`, `ai.ChatMessage`, `ai.OpenAICompatibleProvider.ChatCompletion`, `stripMarkdownJSONFence`, `formatUpstreamError` from `internal/ai/openai.go`.
- Produces:
  - `ai.ASRConfig { BaseURL, APIKey, Model, Language string }`
  - `ai.TranscriptCue { StartSec, EndSec float64; Text string }`
  - `ai.TranscribeResult { Language, Text string; Cues []TranscriptCue }`
  - `ai.NewOpenAICompatibleTranscriber(cfg ASRConfig, httpClient *http.Client) *OpenAICompatibleTranscriber`
  - `(*OpenAICompatibleTranscriber).Transcribe(ctx context.Context, fileName string, audio io.Reader) (*TranscribeResult, error)`
  - `(*OpenAICompatibleTranscriber).TestConnection(ctx context.Context) error`
  - `ai.GenerateSilentWAV(durationMs int) []byte`
  - `ai.ParseSubtitleCues(text string) []TranscriptCue` and `ai.FormatCuesAsWebVTT(cues []TranscriptCue) string`
  - `ai.OutlineItem { Title, Summary string; StartSec *int }`
  - `ai.TimelineSection { StartSec, EndSec *int; Title, Summary string; KeyPoints []string }`
  - `ai.LessonSummaryInput { CourseTitle, ChapterTitle, LessonTitle string; DurationSec int; HasTimestamps bool; Transcript string }`
  - `ai.LessonSummaryOutput { Overview string; Outline []OutlineItem; Timeline []TimelineSection }`
  - `ai.LessonChunkInput { CourseTitle, ChapterTitle, LessonTitle string; ChunkIndex, ChunkTotal, DurationSec int; HasTimestamps bool; Transcript string }`
  - `ai.LessonOutlineSynthInput { CourseTitle, ChapterTitle, LessonTitle string; DurationSec int; HasTimestamps bool; Timeline []TimelineSection }`
  - `(*OpenAICompatibleProvider).SummarizeLesson(ctx, LessonSummaryInput) (*LessonSummaryOutput, error)`
  - `(*OpenAICompatibleProvider).SummarizeLessonChunk(ctx, LessonChunkInput) ([]TimelineSection, error)`
  - `(*OpenAICompatibleProvider).SynthesizeLessonOutline(ctx, LessonOutlineSynthInput) (string, []OutlineItem, error)`
  - `ai.ValidateAndNormalizeSummary(out *LessonSummaryOutput, durationSec int, hasTimestamps bool) error`

- [ ] **Step 1: Write failing tests in `internal/ai/asr_summary_test.go`**

Cover all of the following test cases with `httptest.NewServer`:
- `TestTranscriberVerboseJSONAndFallback`:
  1. `verbose_json` returns `{"language":"zh","text":"你好世界","segments":[{"start":0.0,"end":3.5,"text":"你好"},{"start":3.5,"end":7.2,"text":"世界"}]}` → asserts `POST /v1/audio/transcriptions`, `Authorization: Bearer ...` (omitted when APIKey empty), multipart fields `model`, `file`, `response_format=verbose_json`, `language` (when set), and parsed `Language == "zh"` + 2 cues.
  2. Server rejects `verbose_json` with HTTP 400 and accepts `srt` → asserts automatic retry with `response_format=srt` and parsed SRT cues.
  3. Server rejects both `verbose_json` and `srt` with HTTP 422 and accepts `json` (`{"text":"plain transcript"}`) → asserts fallback returns `Text == "plain transcript"` and `len(Cues) == 0`.
- `TestTranscriberTestConnectionSendsSilentWAV`: asserts `TestConnection` sends a valid RIFF/WAVE header (`"RIFF"` + `"WAVE"`) with `response_format=json` and succeeds on HTTP 200 even with `{"text":""}`.
- `TestFormatAndParseWebVTTCuesRoundTrip`: round-trips cues ≥ 1 hour (`01:02:03.450 --> 01:02:08.000`) and multi-line text.
- `TestSummarizeLessonSinglePassAndMapReduce`:
  - `SummarizeLesson` parses JSON (including fenced JSON) into `Overview`, `Outline`, `Timeline`.
  - `SummarizeLessonChunk` parses `{"timeline":[...]}` for a chunk.
  - `SynthesizeLessonOutline` parses `{"overview":"...","outline":[...]}` from merged timeline sections.
- `TestValidateAndNormalizeSummaryClampsAndSortsTimestamps` *(Review Focus #2)*:
  - Given `durationSec = 300`, `hasTimestamps = true`, input with `start_sec: -5`, `end_sec: 999`, inverted `start_sec: 120, end_sec: 100`, and out-of-order sections `[200..250, 10..50]`, asserts: timestamps clamped to `[0, 300]`, inverted section normalized to `end_sec >= start_sec`, sections sorted by `start_sec` ascending, empty `key_points` trimmed/removed.
  - Given `hasTimestamps = false`, asserts all `start_sec` / `end_sec` on both `outline` and `timeline` are set to `nil` while preserving order.
  - Empty `outline` or empty `timeline` returns an error.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai -run 'TestTranscriber|TestFormatAndParseWebVTT|TestSummarizeLesson|TestValidateAndNormalizeSummary' -v`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement `internal/ai/asr.go` and `internal/ai/summary.go`**

- In `internal/ai/asr.go`:
  - Implement `normalizeTranscriptionsURL(raw string) string` (suffix `/audio/transcriptions`), default HTTP timeout `300 * time.Second`, multipart upload (buffer audio bytes into memory once up to `32 << 20` bytes so 400/422 fallback retries can replay the exact payload without reopening the reader).
  - Only attach `Authorization: Bearer <key>` when `strings.TrimSpace(cfg.APIKey) != ""`.
  - `GenerateSilentWAV(durationMs int) []byte`: standard 44-byte PCM WAV header (16kHz, mono, 16-bit LE) + zero bytes.
  - `ParseSubtitleCues(text string) []TranscriptCue` and `FormatCuesAsWebVTT(cues []TranscriptCue) string`: parse SRT/VTT cue blocks (`HH:MM:SS.mmm` / `MM:SS.mmm`, comma or dot ms), ignore header/sequence numbers/NOTE blocks.
- In `internal/ai/summary.go`:
  - Define exact prompt templates in Chinese/English hybrid instructions per Spec §5.3: output strictly valid JSON, Chinese prose preserving English technical identifiers/code/commands, `outline` (3–8 items) + chronological `timeline` (`start_sec`, `end_sec`, `title`, `summary`, `key_points`), `null` timestamps when `HasTimestamps == false`.
  - Use temperature `0.2` and accept custom timeout via `httpClient` (default `180 * time.Second` if `p.httpClient` has shorter default timeout? Keep `p.httpClient` as injected so tests work cleanly, and let callers pass a `180s` client for summary calls!).
  - Implement `ValidateAndNormalizeSummary`, `ValidateAndNormalizeTimelineChunk`, and `ValidateAndNormalizeOutline`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/asr.go internal/ai/summary.go internal/ai/asr_summary_test.go
git commit -m "feat(ai): add OpenAI-compatible ASR transcriber and lesson summary provider"
```

---

### Task 3: Media Layer AI Subtitle Exposure & Best-Subtitle Extraction

**Files:**
- Modify: `internal/media/streamer.go:25-64`
- Modify: `internal/media/subtitle.go:26-352`
- Test: `internal/media/media_test.go`

**Interfaces:**
- Consumes: `media_transcripts` table from Task 1.
- Produces:
  - `media.StreamDescriptor.IsAIGenerated bool`
  - `(*media.Service).FFmpegBin() string`, `(*media.Service).FFprobeBin() string`, `(*media.Service).HasFFprobe() bool`
  - `(*media.Service).BuildStreamDescriptors(...)` now appends `AI 转写字幕` when `media_transcripts` has `source='asr' AND content_vtt != ''`
  - `(*media.Service).ServeSubtitle(...)` serves `IsAIGenerated` tracks as VTT (or converts via FFmpeg if `srt`/`ass` requested)
  - `(*media.Service).ExtractBestSubtitleVTT(ctx context.Context, mediaFileID string) (vttText, sourceType, sourceLabel string, found bool, err error)`
  - `media.LocalMediaInfo { MediaFileID, AbsPath string; DurationMs int64; HasAudio bool }`
  - `(*media.Service).ResolveLocalMediaInfo(ctx context.Context, mediaFileID string) (*LocalMediaInfo, error)`

- [ ] **Step 1: Write failing tests in `internal/media/media_test.go`**

Add tests verifying:
1. `TestBuildAndServeAIGeneratedSubtitle`:
   - Insert a `media_file` + one external `subtitle_files` row + one `media_transcripts` row (`source='asr'`, `language='zh'`, `content_vtt="WEBVTT\n\n00:00:01.000 --> 00:00:04.000\n你好 AI 字幕\n"`).
   - `ResolveStreams` returns embedded streams, then external sidecar subtitle, then `AI 转写字幕` (`Index` strictly after sidecar, `IsExternal == true`, `IsAIGenerated == true`, `IsDefault == false`, `Language == "zh"`, `Format == "vtt"`).
   - `ServeSubtitle` with that index and `"vtt"` returns HTTP 200, `text/vtt; charset=utf-8`, body containing `"你好 AI 字幕"`.
   - A `media_transcripts` row with `source='manual'` is NOT exposed in `ResolveStreams`.
2. `TestExtractBestSubtitleVTTPriority`:
   - External sidecar subtitle beats embedded stream; among external subtitles, `is_default=1` beats non-default, and Chinese language (`zh`) beats English (`en`) when neither is default; GBK-encoded `.srt` is converted cleanly to UTF-8 WebVTT with `sourceType == "external_subtitle"`.
   - When only bitmap subtitle (`hdmv_pgs_subtitle`) exists in `streams_json`, `ExtractBestSubtitleVTT` returns `found == false, err == nil`.
3. `TestResolveLocalMediaInfoDetectsAudio`:
   - Video with audio stream in `streams_json` → `HasAudio == true`; video-only `streams_json` (`[{"index":0,"codec_type":"video","codec_name":"h264"}]`) → `HasAudio == false`; empty `streams_json` (`"[]"`) → `HasAudio == true` (let FFmpeg decide).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/media -run 'TestBuildAndServeAIGeneratedSubtitle|TestExtractBestSubtitleVTTPriority|TestResolveLocalMediaInfoDetectsAudio' -v`
Expected: FAIL.

- [ ] **Step 3: Implement changes in `internal/media/streamer.go` and `internal/media/subtitle.go`**

- In `internal/media/streamer.go`: resolve `ffprobeBin` via `resolveBinaryPath("", "ffprobe")` and `hasFFprobe`; expose `FFmpegBin()`, `FFprobeBin()`, `HasFFprobe()`.
- In `internal/media/subtitle.go`:
  - Add `IsAIGenerated bool` to `StreamDescriptor`.
  - In `BuildStreamDescriptors`: after reading active `subtitle_files`, query `media_transcripts` for `source = 'asr' AND content_vtt != ''`; if found, append synthetic stream descriptor (`Title: "AI 转写字幕"`, `Format: "vtt"`, `Codec: "vtt"`, `IsExternal: true`, `IsAIGenerated: true`, `IsDefault: false`).
  - In `ServeSubtitle`: branch on `target.IsAIGenerated` → `serveAIGeneratedSubtitle` reads `content_vtt` from SQLite and serves directly as VTT or converts with `convertSubtitleBytesWithFFmpeg` for other requested formats.
  - Extract reusable internal helpers `readExternalSubtitleAsVTT(ctx, sub)` and `extractEmbeddedSubtitleAsVTT(ctx, rec, sub)` shared by `ServeSubtitle` and `ExtractBestSubtitleVTT`.
  - Implement `ExtractBestSubtitleVTT` (priority: external sidecar [default → Chinese → first] → embedded non-bitmap [default → Chinese → first]; skip `IsAIGenerated` tracks so `summary.Service` handles cached transcripts explicitly in its own priority step).
  - Implement `ResolveLocalMediaInfo` using `resolveMediaRecord` + `storage.LocalPathResolver`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/media -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/media/streamer.go internal/media/subtitle.go internal/media/media_test.go
git commit -m "feat(media): expose AI transcripts as playable subtitles and export subtitle extractor"
```

---

### Task 4: Summary Service, Cue Compaction, Audio Chunking & Batch Queue (`internal/summary`)

**Files:**
- Create: `internal/summary/cue_compact.go`
- Create: `internal/summary/audio.go`
- Create: `internal/summary/service.go`
- Test: `internal/summary/service_test.go`

**Interfaces:**
- Consumes: `ai` (`Config`, `ASRConfig`, `OpenAICompatibleProvider`, `OpenAICompatibleTranscriber`, `ParseSubtitleCues`, `FormatCuesAsWebVTT`, `OutlineItem`, `TimelineSection`), `media.Service` (`ExtractBestSubtitleVTT`, `ResolveLocalMediaInfo`, `HasFFmpeg`, `FFmpegBin`, `FFprobeBin`), `catalog.Service` (`GetLesson`, `GetCourseDetail`), SQLite (`app_settings`, `media_transcripts`, `lesson_summaries`).
- Produces:
  - `summary.ConfigLoader` / `summary.AIConfigSnapshot` (`AI ai.Config`, `ASR ai.ASRConfig`, `AIConfigured bool`, `ASRConfigured bool`)
  - `summary.ResolveEffectiveSettings(settings map[string]string) AIConfigSnapshot`
  - `summary.LessonSummary` (JSON DTO matching DB row + parsed `Outline []ai.OutlineItem` & `Timeline []ai.TimelineSection`)
  - `summary.LessonCapabilities { HasSubtitle, HasCachedTranscript bool; TranscriptSource string; AIConfigured, ASRConfigured, HasFFmpeg bool }`
  - `summary.LessonJobStatus`, `summary.CourseBatchStatus`, `summary.BatchLessonError`, `summary.LessonSummaryState`, `summary.CourseSummariesStatus`
  - `summary.GenerateLessonOptions { Force, ForceRetranscribe bool; ManualTranscript string }`
  - `summary.AudioExtractor` interface (real FFmpeg implementation + injectable fake for unit tests)
  - `summary.NewService(opts Options) *Service`
  - Methods: `GetLessonSummaryState(ctx, lessonID)`, `StartLessonSummary(ctx, lessonID, opts)`, `CancelLessonSummary(lessonID)`, `DeleteLessonSummary(ctx, lessonID, deleteTranscript)`, `GetCourseSummaryStatus(ctx, courseID)`, `StartCourseBatch(ctx, courseID, mode)`, `CancelCourseBatch(courseID)`, `Wait()`
  - Sentinel errors: `ErrNotFound`, `ErrAINotConfigured`, `ErrASRNotConfigured`, `ErrBatchAlreadyRunning`, `ErrInvalidInput`

- [ ] **Step 1: Write failing tests in `internal/summary/service_test.go`**

Write comprehensive unit & integration tests (using real SQLite temp DB + `httptest.Server` mock LLM & ASR + injectable fake `AudioExtractor` for fast deterministic tests, plus a conditional real-FFmpeg audio-chunk smoke test when `ffmpeg` is on PATH):
1. `TestCueCompactionStripsTagsAndDedupesRollingCaptions` *(Review Focus #1)*:
   - Input VTT containing `<00:00:01.200><c>Hello</c>`, `<i>world</i>`, ASS `{\pos(100,200)}中文`, and duplicate consecutive rolling lines `"今天讲 Go 并发"` repeated across 3 cues → asserts tags stripped, consecutive duplicate lines collapsed, compact lines formatted as `[MM:SS] ...` (and `[H:MM:SS]` for ≥ 3600s), and chunk splitter splits > `maxRunes` on cue boundaries without losing any cues.
2. `TestSingleLessonFromExternalSubtitlePersistsSummary`:
   - Lesson with sidecar `.srt` → `StartLessonSummary` runs async, reaches `completed`, persists `lesson_summaries` (`source_type == "external_subtitle"`, `has_timestamps == true`), and `GetLessonSummaryState` returns the saved summary.
3. `TestSingleLessonNoSubtitleRunsASRChunksSavesAISubtitleAndReusesCache`:
   - Lesson with no subtitle → fake `AudioExtractor` produces 2 chunk files with durations `1200s` and `300s`; mock ASR returns cues `[0..5]` per chunk → asserts second chunk cues are shifted to `[1200..1205]`, `media_transcripts` row saved with `source == "asr"` and shifted WebVTT, `media.Service.ResolveStreams` now includes `"AI 转写字幕"`, and a second `StartLessonSummary(..., Force: true)` reuses `media_transcripts` with **0** additional ASR calls while `ForceRetranscribe: true` triggers ASR again.
4. `TestSingleLessonManualTranscriptPlainTextAndSRT`:
   - Without ASR configured: calling `StartLessonSummary` with empty `ManualTranscript` returns `ErrASRNotConfigured`.
   - Calling with plain-text `ManualTranscript` succeeds with `source_type == "manual"`, `has_timestamps == false`, and `timeline` timestamps all `nil`.
   - Calling with SRT `ManualTranscript` succeeds with `has_timestamps == true` and valid timestamps.
5. `TestLongTranscriptUsesMapReduce`:
   - Set `SinglePassMaxRunes: 500`, `MapChunkMaxRunes: 300` on `Options` (overridable for tests; defaults `24000` / `18000`). Feed ~900-rune transcript → asserts mock LLM receives chunk Map calls + final Outline Reduce call, and resulting summary has merged chronological timeline + global outline.
6. `TestRegenerateFailurePreservesExistingSummaryAndCleansTempDir` *(Review Focus #3)*:
   - Seed an existing saved summary; trigger regeneration with mock LLM returning HTTP 500 → asserts job ends in `failed`, old `lesson_summaries` row is completely unchanged, and `{dataDir}/cache/asr` contains no leftover job subdirectories.
7. `TestCourseBatchContinuesOnSingleLessonFailureAndSupportsCancel` *(Review Focus #5)*:
   - Course with 3 lessons: L1 has subtitle (succeeds), L2 has no audio / no subtitle without ASR (fails), L3 has subtitle (succeeds) → asserts batch completes with `total == 3, completed == 2, failed == 1`, `len(errors) == 1`, and L1 & L3 summaries saved; also test cancelling a slow batch stops remaining queued lessons and exits cleanly on `Wait()`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/summary -v`
Expected: FAIL (package does not exist).

- [ ] **Step 3: Implement `internal/summary/cue_compact.go`, `internal/summary/audio.go`, and `internal/summary/service.go`**

- `cue_compact.go`:
  - `CompactCues(cues []ai.TranscriptCue, windowSec float64, maxWindowRunes int) []CompactedLine` (defaults `15.0s`, `200 runes`): strip `<[^>]+>` and `\{\\[^}]*\}` tags, normalize whitespace, skip empty/duplicate consecutive lines, group into timestamped lines formatted as `[MM:SS] text` (or `[H:MM:SS] text` when `sec >= 3600`).
  - `SplitCompactedLines(lines []CompactedLine, maxChunkRunes int) []string` and `SplitPlainText(text string, maxChunkRunes int) []string`.
  - `DetectManualTranscript(raw string) (vtt string, plain string, hasTimestamps bool)` — checks for `-->` and valid parsed cues via `ai.ParseSubtitleCues`.
- `audio.go`:
  - `AudioChunk { Path string; OffsetSec float64; DurationSec float64 }`
  - `FFmpegAudioExtractor` creates `{dataDir}/cache/asr/{jobID}`, runs `ffmpeg -hide_banner -loglevel error -i <absPath> -vn -ac 1 -ar 16000 -b:a 48k -f segment -segment_time 1200 -reset_timestamps 1 <workDir>/chunk_%03d.mp3`, sorts chunks, probes each chunk duration via `ffprobe` (fallback `1200.0` if unavailable), returns chunks + cleanup func `os.RemoveAll(workDir)`.
- `service.go`:
  - Serial background worker goroutine started lazily or in `NewService` bound to `LifecycleCtx`, with a priority single-lesson queue (`singleQueue`) drained ahead of the course batch queue (`batchQueue`), protected by `sync.Mutex` + notification channel `notifyCh` (`chan struct{}` buffered 1).
  - Track per-lesson `LessonJobStatus` + `cancelFunc` and per-course `CourseBatchStatus` + `cancelFunc`.
  - Ensure `StartLessonSummary` validates `aiConfigured` (returns `ErrAINotConfigured`), and when `manualTranscript == ""` validates either `asrConfigured` (required when `force_retranscribe` is true) or `hasSubtitle || hasCachedTranscript || asrConfigured` (otherwise returns `ErrASRNotConfigured`) upfront so API returns immediate `400` with structured error code when unconfigured.
  - Ensure `StartCourseBatch` supports `mode: "missing_only"` (default) and `"all"`, skips already-summarized lessons in `missing_only` mode (counting them in `skipped`), checks `ErrAINotConfigured` upfront, and processes target lessons sequentially.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/summary -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/summary/
git commit -m "feat(summary): add background lesson summary and ASR transcription service"
```

---

### Task 5: REST API Endpoints & Server Wiring

**Files:**
- Modify: `internal/api/handlers_ai.go:29-269`
- Create: `internal/api/handlers_summary.go`
- Modify: `internal/api/router.go:35-171`
- Modify: `internal/server/server.go:36-189`
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: `summary.Service`, `summary.ResolveEffectiveSettings`, `ai.NewOpenAICompatibleTranscriber`.
- Produces:
  - Extended `GET/PUT /api/v1/settings` payload with `ai_asr_base_url`, `ai_asr_api_key`, `ai_asr_api_key_set`, `clear_ai_asr_api_key`, `ai_asr_model`, `ai_asr_language`, `ai_configured`, `ai_asr_configured`.
  - `POST /api/v1/settings/ai/asr/test`
  - `GET /api/v1/lessons/{id}/summary`
  - `POST /api/v1/lessons/{id}/summary/generate`
  - `POST /api/v1/lessons/{id}/summary/cancel`
  - `DELETE /api/v1/lessons/{id}/summary`
  - `GET /api/v1/courses/{id}/summaries/status`
  - `POST /api/v1/courses/{id}/summaries/generate`
  - `POST /api/v1/courses/{id}/summaries/cancel`
  - Structured error responses `{"error": "...", "code": "ai_not_configured" | "asr_not_configured"}`.

- [ ] **Step 1: Write failing API tests in `internal/api/api_test.go`**

Add tests covering:
1. `TestSettingsASRFieldsMaskingAndClear`: `PUT /api/v1/settings` saves `ai_asr_base_url`, `ai_asr_api_key`, `ai_asr_model`, `ai_asr_language`; `GET` returns `ai_asr_api_key: "****"`, `ai_asr_api_key_set: true`, `ai_asr_configured: true`; updating with `"****"` preserves stored key; `clear_ai_asr_api_key: true` clears it.
2. `TestASRConfigDoesNotLeakMainKeyToDifferentHost` *(Review Focus #4)*:
   - Save main `ai_base_url` + `ai_api_key: "sk-main-secret"`.
   - Call `POST /api/v1/settings/ai/asr/test` with independent `asr_base_url: asrSrv.URL + "/v1"`, empty `asr_api_key`, `asr_model: "whisper-1"` → asserts `asrSrv` receives **empty** `Authorization` header (does NOT leak `sk-main-secret`).
   - Call `POST /api/v1/settings/ai/asr/test` with empty `asr_base_url` (reusing main `ai_base_url: asrSrv.URL + "/v1"`) and empty `asr_api_key` → asserts `asrSrv` receives `Authorization: Bearer sk-main-secret`.
3. `TestLessonSummaryAPIEndToEndAndErrorCodes`:
   - Unauthenticated request returns `401`.
   - With AI unconfigured, `POST /api/v1/lessons/{id}/summary/generate` returns `400` with `code == "ai_not_configured"`.
   - With AI configured but no subtitle & no ASR, returns `400` with `code == "asr_not_configured"`.
   - With manual transcript posted to `POST /api/v1/lessons/{id}/summary/generate`, returns `202`; polling `GET /api/v1/lessons/{id}/summary` reaches completed summary; `DELETE /api/v1/lessons/{id}/summary` removes summary while retaining transcript unless `?delete_transcript=true`.
   - Course batch endpoints `POST /api/v1/courses/{id}/summaries/generate`, `GET /api/v1/courses/{id}/summaries/status`, and `POST /api/v1/courses/{id}/summaries/cancel` work end-to-end.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run 'TestSettingsASR|TestASRConfigDoesNotLeak|TestLessonSummaryAPI' -v`
Expected: FAIL (404 / missing fields).

- [ ] **Step 3: Implement API handlers, router registration, and `server.Server` wiring**

- Update `internal/api/handlers_ai.go`: add ASR fields + `ai_configured` + `ai_asr_configured` to `settingsPayload`, `buildSettingsResponse`, `handleUpdateSettings`.
- Create `internal/api/handlers_summary.go`: implement `handleTestASR`, `handleGetLessonSummary`, `handleGenerateLessonSummary`, `handleCancelLessonSummary`, `handleDeleteLessonSummary`, `handleGetCourseSummariesStatus`, `handleGenerateCourseSummaries`, `handleCancelCourseSummaries`, and `writeErrorWithCode(w, status, code, msg)`.
- Update `internal/api/router.go`: add `Summary *summary.Service` to `Deps` and `handler` (when nil in legacy tests, construct a default `summary.NewService` using `deps` or guard cleanly — constructing automatically in `NewHandler` when `deps.Summary == nil && deps.DB != nil && deps.Catalog != nil && deps.Media != nil` keeps all existing `setupAPITestEnv` callers working seamlessly!).
- Update `internal/server/server.go`: instantiate `summary.NewService(...)` with `LifecycleCtx: ctx`, pass into `api.Deps`, store on `Server`, and call `s.summary.Wait()` in `Server.Close()`.

- [ ] **Step 4: Run backend tests to verify everything passes**

Run: `go test ./internal/api ./internal/server -v`
Expected: PASS (all new and existing API + E2E tests pass).

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers_ai.go internal/api/handlers_summary.go internal/api/router.go internal/api/api_test.go internal/server/server.go
git commit -m "feat(api): add ASR test and lesson/course AI summary endpoints"
```

---

### Task 6: Frontend Types, API Client, Markdown Export Utility & Settings ASR UI

**Files:**
- Modify: `web/src/types/coursefin.ts:414-488`
- Modify: `web/src/api/client.ts:46-55,148-163,494-526`
- Create: `web/src/utils/summaryMarkdown.ts`
- Modify: `web/src/pages/SettingsPage.tsx:38-235,445-570`
- Test: `web/src/__tests__/SummaryMarkdownAndSettings.test.tsx`

**Interfaces:**
- Consumes: Task 5 REST API contract (`GET/PUT /api/v1/settings`, `POST /api/v1/settings/ai/asr/test`, lesson & course summary endpoints).
- Produces:
  - Types in `web/src/types/coursefin.ts`: `OutlineItem`, `TimelineSection`, `LessonSummary`, `LessonSummaryJob`, `LessonSummaryCapabilities`, `LessonSummaryStateResponse`, `CourseBatchLessonError`, `CourseBatchSummaryJob`, `CourseSummariesStatusResponse`, extended `SettingsPayload` & `UpdateSettingsInput`.
  - `ApiError.code?: string` in `web/src/api/client.ts`.
  - `api.testASR`, `api.getLessonSummary`, `api.generateLessonSummary`, `api.cancelLessonSummary`, `api.deleteLessonSummary`, `api.getCourseSummariesStatus`, `api.generateCourseSummaries`, `api.cancelCourseSummaries`.
  - `formatTimestamp(seconds: number): string` and `formatLessonSummaryMarkdown(params: { courseTitle: string; chapterTitle: string; lessonTitle: string; summary: LessonSummary }): string`.

- [ ] **Step 1: Write failing tests in `web/src/__tests__/SummaryMarkdownAndSettings.test.tsx`**

Test cases:
1. `formatTimestamp` formats `0 -> "00:00"`, `330 -> "05:30"`, `3725 -> "1:02:05"`, clamps negative to `"00:00"`.
2. `formatLessonSummaryMarkdown` produces exact Spec §5.7 Markdown:
   - With `has_timestamps: true`: includes `# lessonTitle`, `> 课程：... · 章节：...`, `## AI 总结视频大纲` with overview & numbered items with ``（`00:00`）``, and `## AI 总结视频内容（按时间顺序）` with `### [00:00 - 05:30] title`, summary, and `- key_point` bullets.
   - With `has_timestamps: false` (or `null` timestamps): omits timestamp brackets/parentheses cleanly.
3. `SettingsPage` ASR section:
   - Renders ASR Model, ASR Base URL, ASR API Key, ASR Language fields and 「测试语音转文字」 button.
   - Displays `"留空复用上方 AI API Key"` when ASR Base URL is empty, and `"留空表示该服务无需认证（不透传主 AI 密钥）"` when an independent ASR Base URL is entered.
   - Saving submits `ai_asr_base_url`, `ai_asr_model`, `ai_asr_language`, `ai_asr_api_key` / `clear_ai_asr_api_key`.
   - Clicking 「测试语音转文字」 posts to `/api/v1/settings/ai/asr/test` and shows success/error feedback.
   - Asserts zero legacy hardcoded Tailwind color classes (`LEGACY_CLASS_REGEX`).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- SummaryMarkdownAndSettings`
Expected: FAIL (module not found / missing inputs).

- [ ] **Step 3: Implement types, client methods, `summaryMarkdown.ts`, and `SettingsPage.tsx` ASR fields**

- Update `web/src/types/coursefin.ts` with all summary & ASR DTO interfaces.
- Update `web/src/api/client.ts`: parse optional `code` from JSON error body onto `ApiError`, add `testASR` and all lesson/course summary methods.
- Create `web/src/utils/summaryMarkdown.ts` implementing `formatTimestamp` and `formatLessonSummaryMarkdown`.
- Update `web/src/pages/SettingsPage.tsx`: add ASR form state synced from `applySettingsPayload`, render the ASR config block below the existing LLM block using existing `FIELD_INPUT_CLASS` and semantic tokens, wire `handleTestASR`, and include ASR fields in `handleSaveSettings`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && pnpm test -- SummaryMarkdownAndSettings EditorAndAI`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/types/coursefin.ts web/src/api/client.ts web/src/utils/summaryMarkdown.ts web/src/pages/SettingsPage.tsx web/src/__tests__/SummaryMarkdownAndSettings.test.tsx
git commit -m "feat(web): add summary API client, Markdown formatter, and ASR settings UI"
```

---

### Task 7: VideoPlayer `seekTo` Handle & In-Place Subtitle Refresh

**Files:**
- Modify: `web/src/components/VideoPlayer.tsx:192-680`
- Test: `web/src/__tests__/VideoPlayer.test.tsx`

**Interfaces:**
- Produces:
  - `export interface VideoPlayerHandle { seekTo: (seconds: number, autoPlay?: boolean) => void }`
  - `VideoPlayer` wrapped with `forwardRef<VideoPlayerHandle, VideoPlayerProps>`
  - Updating `playback.subtitles` for the same `lesson_id` / `play_session_id` updates Artplayer's `controls` & `setting` subtitle selectors in-place without calling `art.destroy()`.

- [ ] **Step 1: Write failing tests in `web/src/__tests__/VideoPlayer.test.tsx`**

Add tests:
1. `exposes seekTo via ref to jump video playback safely clamped to duration`: renders `<VideoPlayer ref={ref} ... />`, sets `video.duration = 200`, calls `ref.current.seekTo(75)` → asserts `video.currentTime === 75` and `video.play` called; calls `ref.current.seekTo(999, false)` → clamped to `200`.
2. `updates subtitle selectors in-place without destroying Artplayer when subtitles prop updates for the same lesson`: renders `VideoPlayer` with `subtitles: []` (1 `Artplayer` instance), rerenders with same `lesson_id`/`play_session_id` and newly added `AI 转写字幕` track → asserts `artMockInstances.length === 1`, `art.destroy` NOT called, and `art.controls.update` + `art.setting.update` updated with `"AI 转写字幕"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- VideoPlayer`
Expected: FAIL (ref not forwarded / Artplayer destroyed on subtitle update).

- [ ] **Step 3: Implement `forwardRef` handle and decoupled subtitle refresh in `web/src/components/VideoPlayer.tsx`**

- Store `textSubtitlesRef` alongside `textSubtitles`.
- Remove `textSubtitles` from the main Artplayer mount/destroy `useEffect` dependency array; keep subtitle control/setting registration inside a dedicated helper + secondary `useEffect([textSubtitles, playback.lesson_id, playback.play_session_id])` that updates `art.controls.update` / `art.setting.update` (and adds `coursefin-subtitle` / `coursefin-subtitle-setting` + `字幕字号` when transitioning from 0 subtitles to >0 subtitles via `art.controls.add` / `art.setting.add` fallback to `update` safely!).
- Implement `useImperativeHandle(ref, () => ({ seekTo(seconds, autoPlay = true) { ... } }))`: clamps `seconds` to `[0, duration]` (when finite duration > 0, else `Math.max(0, seconds)`), sets `video.currentTime`, updates `latestTimeRef.current`, and calls `void video.play().catch(() => {})` when `autoPlay` is true and `video.paused`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && pnpm test -- VideoPlayer`
Expected: PASS (all existing and new `VideoPlayer` & `CourseDetailPage` tests pass).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/VideoPlayer.tsx web/src/__tests__/VideoPlayer.test.tsx
git commit -m "feat(web): expose VideoPlayer seekTo handle and in-place subtitle refresh"
```

---

### Task 8: `LessonSummaryPanel` & `CourseDetailPage` Right-Sidebar Tabs

**Files:**
- Create: `web/src/components/LessonSummaryPanel.tsx`
- Modify: `web/src/pages/CourseDetailPage.tsx`
- Test: `web/src/__tests__/LessonSummary.test.tsx`

**Interfaces:**
- Consumes: Task 6 (`api.*Summary*`, `formatLessonSummaryMarkdown`, `formatTimestamp`, DTO types), Task 7 (`VideoPlayerHandle`), `copyTextToClipboard`, `useToast`, `Tooltip`.
- Produces:
  - `LessonSummaryPanel` component rendering all states from Spec §5.7 (batch banner, summary outline + chronological timeline, active-time highlight, copy Markdown, regenerate / re-transcribe, empty states for subtitle / ASR / unconfigured ASR manual paste / unconfigured AI, stage progress + cancel, non-destructive error banner).
  - `CourseDetailPage` right sidebar with `'syllabus' | 'summary'` tabs, `Sparkles` badge on summarized lessons in syllabus, batch summarize button & polling, `currentTimeSec` live state, `videoPlayerRef.current?.seekTo(sec)` callback, and non-disruptive subtitle refresh when ASR finishes on active lesson.

- [ ] **Step 1: Write failing tests in `web/src/__tests__/LessonSummary.test.tsx`**

Cover all key interactions & Review Focus #4:
1. `renders syllabus and AI summary tabs, displays outline & chronological timeline, highlights active section, seeks on timestamp click, and copies Markdown`:
   - Switches right sidebar to 「AI 总结」 tab → sees 「AI 总结视频大纲」 and 「AI 总结视频内容（按时间顺序）」.
   - Clicks a timeline timestamp button (`05:30 - 12:10`) → asserts `video.currentTime` jumps to `330`.
   - Advances `video.currentTime` to `400` and fires `timeupdate` → asserts the `[05:30 - 12:10]` section has `aria-current="true"` / active highlight attribute.
   - Clicks 「复制 Markdown」 → asserts clipboard receives formatted Markdown containing both `## AI 总结视频大纲` and `## AI 总结视频内容（按时间顺序）`.
2. `handles subtitle-less lesson when ASR is not configured via manual transcript paste`:
   - Renders empty state with `has_subtitle: false, asr_configured: false, ai_configured: true` → sees link to `/settings`, link to `/courses/c1/edit`, and textarea for pasting transcript.
   - Pastes text and clicks 「根据文稿生成总结」 → asserts `POST /api/v1/lessons/l1/summary/generate` called with `manual_transcript`.
3. `polls ASR transcription stage progress, supports cancel, and refreshes player subtitles on ASR completion`:
   - Clicking 「转写并生成总结」 starts polling `GET /api/v1/lessons/l1/summary`, renders `正在语音转写（分片 1/2）` and 「取消」 button; when job transitions to `completed` with `source_type: 'asr'`, displays summary and triggers playback subtitle refresh without destroying player.
4. `displays course batch progress banner, allows starting and cancelling batch generation, and shows summarized sparkles badge in syllabus`:
   - Syllabus shows `已生成 AI 总结` indicator for lesson IDs in `summary_lesson_ids`; clicking 「批量总结」 triggers `POST /api/v1/courses/c1/summaries/generate` and renders progress banner + 「取消」 button.
5. `ignores stale summary response after switching lessons` *(Review Focus #4)*:
   - Lesson L1 summary request stays pending while user clicks L2; L2 resolves first, then L1 resolves late → asserts L2's summary stays rendered and is NOT overwritten by L1's delayed payload.
6. Verifies zero legacy hardcoded color classes (`LEGACY_CLASS_REGEX`) in rendered DOM.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && pnpm test -- LessonSummary`
Expected: FAIL.

- [ ] **Step 3: Implement `web/src/components/LessonSummaryPanel.tsx` and integrate into `web/src/pages/CourseDetailPage.tsx`**

- Build `LessonSummaryPanel.tsx` using semantic tokens (`bg-card`, `bg-subtle`, `bg-accent-soft`, `text-accent-ink`, `border-subtle`, `text-ink-primary`, `text-ink-secondary`, `text-ink-muted`, `bg-status-danger-soft`, `text-status-danger-ink`), with `requestIdRef` / cancellation token so delayed responses never leak across lesson switches, and visibility-aware 1.5s polling while `job?.running` is true.
- Update `CourseDetailPage.tsx`:
  - Keep existing `"收起目录"` / `"展开目录"` header button label so existing UI expectations & tests remain intact.
  - Upgrade `<aside>` header to a clean two-tab switcher (`课程目录` and `AI 总结`), defaulting to `课程目录`.
  - Track `activeCurrentTimeSec` inside `handleTimeProgress`, hold `videoPlayerRef = useRef<VideoPlayerHandle>(null)`, fetch `/api/v1/courses/{id}/summaries/status` safely (`.catch(() => null)` so partial mocks never break course loading), poll while `batchJob?.running`, and refresh `playback.subtitles` in-place via `api.getLessonPlayback` (updating only `playback.subtitles` when `lesson_id` matches) whenever active lesson finishes an ASR summary.

- [ ] **Step 4: Run all frontend tests to verify they pass**

Run: `cd web && pnpm test`
Expected: PASS (all existing and new Vitest suites pass).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/LessonSummaryPanel.tsx web/src/pages/CourseDetailPage.tsx web/src/__tests__/LessonSummary.test.tsx
git commit -m "feat(web): add AI summary right-sidebar tab, timeline seek, and batch controls"
```

---

### Task 9: Full-Stack Verification, Embedded SPA Build & Cleanliness Check

**Files:**
- Modify: `internal/server/webdist/` (only if repo tracks built SPA assets — check `git status` / `Makefile` before touching)

- [ ] **Step 1: Run complete Go test suite with race detector**

Run: `go test -race ./...`
Expected: PASS across every package (`internal/ai`, `internal/api`, `internal/auth`, `internal/catalog`, `internal/config`, `internal/db`, `internal/id`, `internal/jellyfin`, `internal/media`, `internal/scanner`, `internal/server`, `internal/storage`, `internal/summary`, `internal/updater`).

- [ ] **Step 2: Run full frontend test suite and production TypeScript + Vite build**

Run: `cd web && pnpm test && pnpm build`
Expected: All Vitest tests PASS, `tsc -b && vite build` exits 0 with zero TypeScript errors.

- [ ] **Step 3: Sync embedded web assets if tracked and verify Go binary build**

Check `Makefile` and run the standard build command (`make build` or `go build ./cmd/coursefin`), verifying `CGO_ENABLED=0 go build ./cmd/coursefin` succeeds cleanly and no temporary artifacts remain untracked.

- [ ] **Step 4: Final git status & diff audit**

Run: `git status -sb && git diff --stat`
Expected: Clean working tree (or only intended files committed), zero temporary files in `data/cache/asr`.
