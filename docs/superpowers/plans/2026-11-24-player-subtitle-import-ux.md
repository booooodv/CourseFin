# CourseFin 播放器、字幕、导入 AI 与进度交互优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver five cohesive UX and playback upgrades to CourseFin: AI organization inside the Import Course modal, an embedded Artplayer web player backed by three-tier FFmpeg H.264 HLS transcoding, reliable subtitle matching/encoding/auto-selection/manual binding, course-assignment visibility in Storage & Import, and circular SVG progress rings in the course syllabus.

**Architecture:** Extend the Go backend (`scanner`, `media`, `catalog`, `api`, `db`) with a lightweight SQLite migration (`002_subtitle_manual_bind.sql`), `golang.org/x/text` NFC/encoding helpers, on-demand `libx264` HLS transcoding behind `TranscodeVideo=1`, preview-stage AI organize endpoint, and course-assignment aggregations. Upgrade the React/TypeScript SPA (`web/`) with `artplayer` + `hls.js`, browser codec probing + single-retry transcode fallback, reusable AI diff preview inside `ImportCourseModal`, manual subtitle binding in `CourseEditorPage`, assignment badges/filters in `StoragePage`, and `LessonProgressIcon` SVG progress rings in `CourseDetailPage`.

**Tech Stack:** Go 1.26, `chi/v5`, SQLite (`modernc.org/sqlite`), `golang.org/x/text`, FFmpeg, React 19, TypeScript 5.8, Vite 6, Tailwind CSS v4, `artplayer`, `hls.js`, `lucide-react`, Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-11-24-player-subtitle-import-ux-design.md`

## Global Constraints

- **Dependencies:** Only add `artplayer` to `web/package.json` (syncing `web/pnpm-lock.yaml` for `--frozen-lockfile` Docker builds) and `golang.org/x/text` to `go.mod` (maintaining `CGO_ENABLED=0`).
- **Read-only media guarantee:** Never modify, rename, move, or delete user media/subtitle files on disk; keep all transcode caches under `{dataDir}/cache/remux` with existing 5-minute reaper and `DELETE /Videos/ActiveEncodings` cleanup.
- **Backward compatibility:** Preserve existing `CanRemuxVideoCodec`, `CanWebRemuxVideoCodec`, `NeedsAudioTranscode`, Jellyfin `PlaybackInfo` behavior, and existing REST fields (additive JSON fields only).
- **Theme tokens:** Use semantic theme tokens from `web/src/index.css` (`bg-card`, `bg-subtle`, `text-ink-primary`, `bg-accent`, `text-status-success-ink`, etc.) — never hardcode `slate/indigo/emerald/rose` palette classes.
- **Git hygiene:** Stage only files created/modified by each task; leave any pre-existing unrelated working-tree changes untouched.

## Review Focus

1. **DirectPlay runtime decode failure fallback:** A browser may report `canPlayType('probably')` for HEVC or receive an unplayable 10-bit/odd-profile stream and fire a `<video>` `error` before playback starts -> `VideoPlayer` + `CourseDetailPage` must automatically fall back once to `force_transcode=1` with a toast, never infinite-looping. (Pinned in Task 7 & Task 8)
2. **Manual subtitle unbind survives periodic/manual scans & restarts:** Unbinding a falsely auto-matched subtitle sets `media_file_id = NULL` with `manually_bound = 1`; neither `ScanSource` nor startup `RematchUnboundSubtitles` may re-attach it unless `reset_auto: true` is explicitly sent. (Pinned in Task 1 & Task 2)
3. **Non-UTF-8 subtitles (GB18030/GBK, UTF-16 LE/BE) and macOS NFD filenames:** Scanner matches NFD vs NFC paths accurately; both subtitle HTTP delivery (`/Subtitles/{i}/Stream.vtt`) and AI subtitle excerpts decode to clean UTF-8 without mojibake. (Pinned in Task 1 & Task 4)
4. **AI chapter/lesson reorganization in `ImportCourseModal`:** Reordering/regrouping lessons across chapters preserves per-lesson `duration_ms` lookup via `media_file_id` map, submits every `media_file_id` accurately, and "恢复初始识别" restores the original preview cleanly. (Pinned in Task 6)
5. **Single video assigned to multiple courses:** `StorageSource` and `IndexedDir` `assigned_count` must count distinct `media_file_id`s so `assigned_count <= media_count` and `unassigned_count >= 0`, while `MediaFile.courses` lists all unique courses. (Pinned in Task 5 & Task 9)

---

## File Structure Map

- **Database:** `internal/db/migrations/002_subtitle_manual_bind.sql` (new)
- **Subtitle Matching & Encoding:** `internal/scanner/subtitle_match.go`, `internal/scanner/scanner.go`, `internal/media/encoding.go` (new), `internal/media/subtitle.go`, `internal/server/server.go`
- **Playback & Transcoding:** `internal/media/streamer.go`, `internal/media/remux.go`, `internal/api/handlers_catalog.go`
- **Catalog, Storage Stats & Subtitle Binding:** `internal/catalog/service.go`, `internal/api/handlers_storage.go`, `internal/api/router.go`
- **Import Preview AI:** `internal/api/handlers_ai.go`, `internal/api/router.go`
- **Frontend Shared:** `web/src/types/coursefin.ts`, `web/src/api/client.ts`, `web/src/utils/mediaCapabilities.ts` (new)
- **Frontend Components & Pages:** `web/src/components/ImportCourseModal.tsx`, `web/src/components/VideoPlayer.tsx`, `web/src/components/LessonProgressIcon.tsx` (new), `web/src/components/SubtitleBindModal.tsx` (new), `web/src/pages/CourseDetailPage.tsx`, `web/src/pages/CourseEditorPage.tsx`, `web/src/pages/StoragePage.tsx`

---

### Task 1: Subtitle DB Migration, Enhanced Matcher, Startup Rematch & Encoding Decoder

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/db/migrations/002_subtitle_manual_bind.sql`
- Create: `internal/media/encoding.go`
- Modify: `internal/media/subtitle.go:195-260,344-415`
- Modify: `internal/scanner/subtitle_match.go`
- Modify: `internal/scanner/scanner.go:26-41,159-164,306-312,583-760`
- Modify: `internal/server/server.go:75-105`
- Test: `internal/db/db_test.go`, `internal/scanner/scanner_test.go`, `internal/media/media_test.go`

**Interfaces:**
- Consumes: `db.Open`, `scanner.Service`, `media.Service`
- Produces:
  - DB column `subtitle_files.manually_bound INTEGER NOT NULL DEFAULT 0`
  - `media.DecodeSubtitleToUTF8(raw []byte) string`
  - `scanner.ScanReport.MatchedSubtitles int` (JSON `matched_subtitles`)
  - `scanner.IsSubtitleDirName(name string) bool`
  - `scanner.MatchSubtitle(videoRelPath, subRelPath string) (SubtitleMatch, bool)` (NFC-normalized, supports subtitle subdirs, `.`/`_`/`-`/space/brackets separators, expanded aliases/modifiers, graceful custom-suffix fallback, numeric-suffix guard, quality-tag strip fallback)
  - `(*scanner.Service).RematchUnboundSubtitles(ctx context.Context) (int, error)`

- [ ] **Step 1: Add `golang.org/x/text` dependency and create migration `002_subtitle_manual_bind.sql`**

Run: `go get golang.org/x/text`
Create `internal/db/migrations/002_subtitle_manual_bind.sql`:
```sql
ALTER TABLE subtitle_files ADD COLUMN manually_bound INTEGER NOT NULL DEFAULT 0;
```

- [ ] **Step 2: Write failing tests for migration, enhanced subtitle matching, startup rematch, manual-unbind preservation, and GBK/UTF-16 decoding**

In `internal/db/db_test.go`, add `TestMigration002AddsManuallyBoundColumn` asserting `PRAGMA table_info(subtitle_files)` includes `manually_bound` defaulting to `0`.

In `internal/scanner/scanner_test.go`:
- Update `TestMatchSubtitlesByBasenameAndLanguage`: replace `"course/sub/01_intro.srt"` in the ignored list with `"course/chapter2/01_intro.srt"` (non-subtitle directory must still be ignored).
- Add `TestMatchSubtitlesRelaxedPatternsAndDirs` covering:
  - Same-dir separators & real-world suffixes: `01_intro_简体中文.srt` (`zh`/`简体中文`), `01_intro-中英双语.srt` (`zh`/`中英双语`), `01_intro [English].vtt` (`en`/`English`), `01_intro.en-orig.vtt` (`en`), `01_intro.ai-zh.srt` (`zh`), `01_intro (机翻校对).srt` (matched with `und` and non-empty title `机翻校对`).
  - Subtitle subdirs: `course/Subs/01_intro.zh.srt`, `course/Subtitles/01_intro.srt`, `course/字幕/01_intro.srt` match `course/01_intro.mp4`.
  - Quality tag stripping fallback: `course/01_intro [1080p].mp4` matches `course/01_intro.srt`.
  - Unicode NFD vs NFC: decomposed NFD video filename matches composed NFC subtitle filename.
  - Guard against false positives: `course/01.mp4` does NOT match `course/01_2.srt` or `course/01-02.srt`; `course/chapter2/01_intro.srt` does NOT match `course/chapter1/01_intro.mp4`.
- Add `TestScanAndRematchRespectManualUnbindAndSiblingSubtitleDir`:
  - Seeds sibling `Course/Videos/01_setup.mp4` + `Course/Subtitles/01_setup.zh.srt`, verifies `ScanSource` sets `MatchedSubtitles == 1` and links `media_file_id`.
  - Manually updates a subtitle row to `media_file_id = NULL, manually_bound = 1`, runs both `ScanSource` and `RematchUnboundSubtitles`, and asserts `media_file_id` remains `NULL` and `manually_bound` remains `1`.
  - Seeds an unbound row with `media_file_id = NULL, manually_bound = 0` matching an existing `media_files` row, calls `RematchUnboundSubtitles`, and asserts it returns `matched >= 1` and updates `media_file_id`.

In `internal/media/media_test.go`, add `TestDecodeSubtitleToUTF8AndServeNonUTF8SRT`:
- Tests `media.DecodeSubtitleToUTF8` on UTF-8 with BOM, UTF-16 LE with BOM, UTF-16 BE with BOM, and GB18030/GBK encoded Chinese SRT bytes.
- Serves a GBK-encoded external `.srt` via `svc.ServeSubtitle(..., "vtt")` and asserts HTTP 200, `WEBVTT` header, and valid UTF-8 Chinese cue text.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/db ./internal/scanner ./internal/media -run "TestMigration002|TestMatchSubtitles|TestScanAndRematch|TestDecodeSubtitleToUTF8"`
Expected: FAIL (missing `DecodeSubtitleToUTF8`, `RematchUnboundSubtitles`, `MatchedSubtitles`, relaxed matcher assertions fail).

- [ ] **Step 4: Implement `internal/media/encoding.go` and wire into `internal/media/subtitle.go`**

- Implement `DecodeSubtitleToUTF8(raw []byte) string` in `internal/media/encoding.go`:
  1. Strip UTF-8 BOM (`0xEF, 0xBB, 0xBF`) if present.
  2. Detect UTF-16 LE (`0xFF, 0xFE`) or UTF-16 BE (`0xFE, 0xFF`) BOM and decode via `golang.org/x/text/encoding/unicode`.
  3. If `utf8.Valid(raw)`, return `string(raw)`.
  4. Otherwise decode via `simplifiedchinese.GB18030.NewDecoder().Bytes(raw)` (fallback to `string(raw)` on error).
- In `internal/media/subtitle.go`:
  - Call `decoded := DecodeSubtitleToUTF8(rawBytes)` in `serveExternalSubtitle`.
  - When `targetFormat == "vtt"` and `srcFmt == "vtt"`, ensure `WEBVTT` header is present (prepend `"WEBVTT\n\n"` if missing after trimming whitespace).
  - Pass `[]byte(decoded)` to `ConvertSRTToVTT` and `convertSubtitleBytesWithFFmpeg`.

- [ ] **Step 5: Implement enhanced matching in `internal/scanner/subtitle_match.go`, update `internal/scanner/scanner.go`, and wire startup rematch in `internal/server/server.go`**

- In `internal/scanner/subtitle_match.go`:
  - Add `IsSubtitleDirName(name string) bool` matching case-insensitive: `subs`, `subtitles`, `subtitle`, `sub`, `字幕`, `中文字幕`, `英文字幕`, `srt`, `vtt`, `ass`.
  - Normalize video & subtitle paths with `norm.NFC.String` before comparisons.
  - In `MatchSubtitle(videoRelPath, subRelPath string)`: allow same dir OR subtitle located in an immediate recognized subtitle child dir of video's dir (`path.Dir(subDir) == videoDir && IsSubtitleDirName(path.Base(subDir))`).
  - Expand `langAliases` and `subtitleModifiers` per Spec Section 5.3.1.
  - Support separators `.`, `_`, `-`, `' '`, `[`, `(`, `【`, `（` after `videoStem`.
  - Parse tokens across delimiters to extract best `language`, `title`, and `isDefault`; if suffix is non-empty and not purely numeric (numeric guard prevents `01.mp4` matching `01_2.srt` / `01-02.srt`), match with `language="und"` and cleaned suffix as `title` instead of rejecting.
  - Add helper `StripVideoQualityTags(stem string) string` stripping trailing resolution/codec tags (`[1080p]`, `(720p)`, `.1080p`, `_4K`, `[HEVC]`, `[x264]`, `[x265]`, etc.) used in a third-pass fallback when exact/prefix passes don't match.
- In `internal/scanner/scanner.go`:
  - Add `MatchedSubtitles int` (`json:"matched_subtitles"`) to `ScanReport`.
  - Extend `existingSubtitleRecord` with `manuallyBound bool` and `mediaFileID sql.NullString`.
  - In `syncSubtitlesTx`:
    - Skip overwriting `media_file_id`, `language`, `title`, `is_default` for rows where `ex.manuallyBound` is true (only refresh `status = 'active'`).
    - Match candidates from: (1) same directory, (2) parent directory if subtitle dir is a recognized subtitle dir, (3) sibling directories under the same parent if subtitle dir is a recognized subtitle dir (only bind when unique candidate matches).
    - Also try quality-stripped stem fallback pass when unambiguous.
    - Count matched subtitles into `matchedCount` returned to populate `rep.MatchedSubtitles`.
  - Implement `(*Service).RematchUnboundSubtitles(ctx context.Context) (int, error)` querying active `subtitle_files WHERE media_file_id IS NULL AND manually_bound = 0`, matching against active `media_files` of each `source_id` using the same matcher, and updating matched rows in a transaction.
- In `internal/server/server.go`: call `_, _ = scanSvc.RematchUnboundSubtitles(context.Background())` immediately after `scanSvc` is created in `server.New`.

- [ ] **Step 6: Run tests to verify Task 1 passes**

Run: `go test -race -count=1 ./internal/db ./internal/scanner ./internal/media ./internal/server`
Expected: PASS

- [ ] **Step 7: Commit Task 1**

```bash
git add go.mod go.sum internal/db/migrations/002_subtitle_manual_bind.sql internal/db/db_test.go internal/media/encoding.go internal/media/subtitle.go internal/media/media_test.go internal/scanner/subtitle_match.go internal/scanner/scanner.go internal/scanner/scanner_test.go internal/server/server.go
git commit -m "feat(subtitles): add relaxed matcher, encoding decoder, startup rematch, and manual_bind migration"
```

---

### Task 2: Manual Subtitle Listing & Binding API + Lesson `source_id`

**Files:**
- Modify: `internal/catalog/service.go:133-159,663-695,1288-1321` (plus new methods)
- Modify: `internal/api/handlers_storage.go`
- Modify: `internal/api/router.go:106-117`
- Test: `internal/catalog/catalog_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: `subtitle_files.manually_bound` from Task 1
- Produces:
  - `catalog.LessonDetail.SourceID string` (JSON `source_id,omitempty`)
  - `catalog.IndexedSubtitle` struct (`ID`, `SourceID`, `RelativePath`, `FileName`, `MediaFileID`, `Language`, `Title`, `Format`, `IsDefault`, `ManuallyBound`, `Status`)
  - `catalog.BindSubtitleInput` struct (`MediaFileID *string`, `Title *string`, `Language *string`, `IsDefault *bool`, `ResetAuto bool`)
  - `(*catalog.Service).ListSourceSubtitles(ctx, sourceID, prefix, search string, unmatchedOnly *bool) ([]IndexedSubtitle, error)`
  - `(*catalog.Service).BindSubtitle(ctx, subtitleID string, input BindSubtitleInput) (*IndexedSubtitle, error)`
  - REST endpoints:
    - `GET /api/v1/sources/{id}/subtitles?unmatched=true|false&prefix=...&search=...` -> `{"subtitles": [...]}`
    - `PUT /api/v1/subtitles/{id}/bind` -> returns updated `IndexedSubtitle`

- [ ] **Step 1: Write failing tests for listing subtitles, binding, unbinding (keeping `manually_bound = 1`), `reset_auto`, and `LessonDetail.SourceID`**

In `internal/catalog/catalog_test.go`, add `TestListAndBindSubtitles`:
- Seeds source, media file, unmatched subtitle, and course/lesson; verifies `GetCourseDetail` and `GetLesson` populate `LessonDetail.SourceID == sourceID`.
- Calls `ListSourceSubtitles(..., &true)` -> returns the unmatched subtitle.
- Calls `BindSubtitle` with `MediaFileID = &mfID, Title = &"中文字幕", Language = &"zh"` -> returns `ManuallyBound == true`, `MediaFileID == mfID`, and `GetLesson` includes the subtitle in `lesson.Subtitles`.
- Calls `BindSubtitle` with `MediaFileID = &empty` (unbind) -> returns `MediaFileID == ""` AND `ManuallyBound == true` (locked detached).
- Calls `BindSubtitle` with `ResetAuto: true` -> returns `MediaFileID == ""` AND `ManuallyBound == false`.

In `internal/api/api_test.go`, add `TestSourceSubtitlesAndManualBindEndpoints` testing `GET /api/v1/sources/{id}/subtitles?unmatched=true`, `PUT /api/v1/subtitles/{id}/bind`, 404 for unknown subtitle/media file, and verifying bound subtitle appears in `GET /api/v1/lessons/{id}/playback`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/catalog ./internal/api -run "TestListAndBindSubtitles|TestSourceSubtitlesAndManualBind"`
Expected: FAIL (undefined methods/routes).

- [ ] **Step 3: Implement `LessonDetail.SourceID`, `ListSourceSubtitles`, and `BindSubtitle` in `internal/catalog/service.go`**

- Select `mf.source_id` in `GetCourseDetail`, `GetLesson`, and `lessonWithContextSelect` (in `internal/catalog/progress.go`) to populate `LessonDetail.SourceID`.
- Implement `ListSourceSubtitles` querying `subtitle_files WHERE source_id = ? AND status = 'active'`, applying `unmatchedOnly` (`media_file_id IS NULL` vs `IS NOT NULL`), `prefix`, and `search`, sorted via `NaturalCompare(a.RelativePath, b.RelativePath)`.
- Implement `BindSubtitle`:
  - Look up existing `subtitle_files` row (return `ErrNotFound` if missing).
  - If `input.ResetAuto`: set `media_file_id = NULL, manually_bound = 0`.
  - Else if `input.MediaFileID != nil`:
    - Trimmed empty string -> set `media_file_id = NULL, manually_bound = 1`.
    - Non-empty ID -> normalize, verify `media_files` row exists (else `ErrNotFound`), set `media_file_id = normalized, manually_bound = 1`.
  - Apply optional `Title`, `Language`, `IsDefault` overrides when provided.
  - Return the updated `IndexedSubtitle`.

- [ ] **Step 4: Register HTTP handlers in `internal/api/handlers_storage.go` and `internal/api/router.go`**

- Add `handleListSourceSubtitles` and `handleBindSubtitle` in `internal/api/handlers_storage.go`.
- Register `pr.Get("/sources/{id}/subtitles", h.handleListSourceSubtitles)` and `pr.Put("/subtitles/{id}/bind", h.handleBindSubtitle)` in `internal/api/router.go`.

- [ ] **Step 5: Run tests to verify Task 2 passes**

Run: `go test -race -count=1 ./internal/catalog ./internal/api`
Expected: PASS

- [ ] **Step 6: Commit Task 2**

```bash
git add internal/catalog/service.go internal/catalog/progress.go internal/catalog/catalog_test.go internal/api/handlers_storage.go internal/api/router.go internal/api/api_test.go
git commit -m "feat(subtitles): add source subtitle listing, manual bind/unbind API, and lesson source_id"
```

---

### Task 3: Three-Tier Playback Negotiation & On-Demand FFmpeg H.264 HLS Transcoding

**Files:**
- Modify: `internal/media/streamer.go:25-56,100-140`
- Modify: `internal/media/remux.go:135-335,374-565,650-675`
- Modify: `internal/api/handlers_catalog.go:538-678`
- Test: `internal/media/media_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: `media.Service`, `catalog.Service`
- Produces:
  - `(*media.Service).HasFFmpeg() bool`
  - Query param `TranscodeVideo=1` propagated through `/Videos/{id}/master.m3u8` -> `main.m3u8` -> `hls1/main/{idx}.ts`
  - `GET /api/v1/lessons/{id}/playback?codecs=h264,hevc,vp9,av1&force_transcode=1`
  - Response adds `"transcoding": bool` alongside existing fields (`mode`: `"direct" | "hls" | "unsupported"`, `stream_url`, etc.)

- [ ] **Step 1: Write failing tests for `HasFFmpeg`, `TranscodeVideo=1` HLS transcoding, and 4-tier playback negotiation**

In `internal/media/media_test.go`:
- Add `TestHasFFmpegDetection` testing valid ffmpeg (when installed) vs nonexistent explicit path (`media.NewService(conn, dataDir, nil, "/nonexistent/ffmpeg-bin")` -> `HasFFmpeg() == false`).
- Add `TestServeMasterPlaylistPropagatesTranscodeVideoAndTranscodesHEVC`:
  - Verifies `GET /Videos/{id}/master.m3u8?PlaySessionId=s1&TranscodeVideo=1` on a `wmv3`/`hevc` record returns `200` (not `415`) and includes `TranscodeVideo=1` in `main.m3u8` and `.ts` URLs; without `TranscodeVideo=1`, `wmv3` still returns `415` (preserving Jellyfin contract).
  - When `ffmpeg` is available in PATH, generates a short test HEVC or MPEG4 video with `ffmpeg`, requests segment `0.ts` with `TranscodeVideo=1`, asserts HTTP 200, non-empty MPEG-TS body, and probes output segment video codec == `h264` with `pix_fmt == yuv420p`.

In `internal/api/api_test.go`:
- Update/expand playback negotiation test (`TestLessonPlaybackThreeTierNegotiation`):
  - `mp4 + hevc + aac` with `?codecs=h264,hevc,vp9,av1` -> `mode: "direct"`, `transcoding: false`.
  - `mp4 + hevc + aac` with `?codecs=h264,vp9,av1` (or omitted `codecs`) when FFmpeg available -> `mode: "hls"`, `transcoding: true`, `stream_url` contains `TranscodeVideo=1`.
  - `mp4 + hevc + aac` with `pix_fmt: yuv444p` in `streams_json` even when `?codecs=h264,hevc` -> falls back to `mode: "hls"`, `transcoding: true`.
  - `mp4 + h264 + aac` with `?force_transcode=1` -> `mode: "hls"`, `transcoding: true`, `stream_url` contains `TranscodeVideo=1`.
  - `mkv + h264 + opus` -> `mode: "hls"`, `transcoding: false`, `stream_url` does NOT contain `TranscodeVideo=1`.
  - `mkv + hevc + aac`, `mkv + vp9 + opus`, `wmv + wmv3 + wmav2` when FFmpeg available -> `mode: "hls"`, `transcoding: true`, `stream_url` contains `TranscodeVideo=1`.
  - Non-direct video when `media.NewService` has nonexistent `ffmpegBin` (`!HasFFmpeg()`) -> `mode: "unsupported"`, `stream_url: ""`, `transcoding: false`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/media ./internal/api -run "TestHasFFmpeg|TestServeMasterPlaylistPropagatesTranscodeVideo|TestLessonPlayback"`
Expected: FAIL

- [ ] **Step 3: Implement `HasFFmpeg()` and `TranscodeVideo=1` pipeline in `internal/media/streamer.go` and `internal/media/remux.go`**

- In `internal/media/streamer.go`:
  - Add `hasFFmpeg bool` field computed in `NewService` by checking `ffmpegBin` via `os.Stat` (for paths containing separator) or `exec.LookPath`.
  - Add `func (s *Service) HasFFmpeg() bool`.
- In `internal/media/remux.go`:
  - Add helper `isTranscodeVideoRequested(r *http.Request) bool` checking `queryParamCI(r, "TranscodeVideo")` for `"1"` or `"true"`.
  - In `propagateHLSQuery`, preserve `TranscodeVideo=1` when requested.
  - In `ServeMasterPlaylist`, `ServeMediaPlaylist`, `ServeHLSSegment`: skip the `!s.CanRemuxVideoCodec(rec.VideoCodec)` 415 guard when `isTranscodeVideoRequested(r)` is true (if `!s.HasFFmpeg()`, return `503 Service Unavailable`).
  - Pass `transcodeVideo bool` into `ensureSegmentReady` -> `launchFFmpegRemuxLocked`.
  - In `launchFFmpegRemuxLocked`:
    - When `transcodeVideo` is true: replace `"-c:v", "copy"` with `"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", SegmentDurationSec), "-sc_threshold", "0"`.
    - Keep `-ss <startSec>` before `-i`, `-output_ts_offset <startSec>`, audio copy vs AAC transcode, and temp file/reaper lifecycle unchanged.

- [ ] **Step 4: Implement 4-tier negotiation in `internal/api/handlers_catalog.go`**

- Update `handleLessonPlayback`:
  - Parse `?codecs=` into normalized set (default `{"h264": true, "vp9": true, "av1": true}` when omitted; normalize `avc`/`avc1`->`h264`, `h265`/`hvc1`/`hev1`->`hevc`).
  - Parse `?force_transcode=` (`"1"` or `"true"`).
  - Helper `hasBrowserSafePixFmt(streamsJSON string) bool`: inspects first non-attached-pic video stream's `pix_fmt`; returns `false` for `yuv444p`, `yuv444p10le`, `yuv422p10le`, `gbrp` (returns `true` if absent or `yuv420p`/`yuvj420p`/`nv12`/`yuv420p10le` when not hevc-unsafe; specifically reject `yuv444*` and `10le`/`12le` for direct play to guarantee clean browser decoding).
  - Note: fetch `streams_json` from `media_files` (or expose `StreamsJSON` on `catalog.MediaFile` with `json:"-"`).
  - Apply 4-tier decision from Spec Section 5.2 and include `"transcoding": transcoding` in JSON response.

- [ ] **Step 5: Run tests to verify Task 3 passes**

Run: `go test -race -count=1 ./internal/media ./internal/api ./internal/jellyfin`
Expected: PASS

- [ ] **Step 6: Commit Task 3**

```bash
git add internal/catalog/service.go internal/media/streamer.go internal/media/remux.go internal/media/media_test.go internal/api/handlers_catalog.go internal/api/api_test.go
git commit -m "feat(playback): add browser codec negotiation and on-demand FFmpeg H.264 HLS transcoding"
```

---

### Task 4: Import Preview AI Organize Endpoint

**Files:**
- Modify: `internal/api/handlers_ai.go:269-477`
- Modify: `internal/api/router.go:152-158`
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: `media.DecodeSubtitleToUTF8` (Task 1), `ai.OpenAICompatibleProvider.OrganizeCourse`
- Produces:
  - `POST /api/v1/import/ai/organize`
  - Request JSON: `{ source_id, dir_rel_path, title, overview, chapters: [{ title, lessons: [{ media_file_id, title }] }] }`
  - Response JSON (`ImportOrganizeProposal`): `{ title, overview, tags: string[], chapters: [{ title, lessons: [{ media_file_id, title }] }] }`

- [ ] **Step 1: Write failing tests for `POST /api/v1/import/ai/organize` and GBK subtitle excerpt decoding**

In `internal/api/api_test.go`, add `TestImportAIOrganizeEndpoint`:
- Seeds a storage source, 2 media files (`mf1`, `mf2`), and a GBK-encoded sidecar subtitle linked to `mf1`.
- Configures mock OpenAI HTTP server inspecting the outbound prompt to assert `mf1`'s Chinese subtitle excerpt arrived as valid UTF-8 (no mojibake) and `lesson_id` equals `mf1`/`mf2`.
- Mock server returns reorganized chapters grouping `mf1` and `mf2` with cleaned title, overview, and tags.
- Asserts `POST /api/v1/import/ai/organize` returns `200` with `ImportOrganizeProposal` containing `chapters[].lessons[].media_file_id` matching `mf1` & `mf2`, without mutating any DB course rows.
- Asserts `400` when AI config is missing, `400` when `chapters` has no lessons or contains a `media_file_id` not belonging to `source_id`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestImportAIOrganizeEndpoint`
Expected: FAIL (404 route not found).

- [ ] **Step 3: Refactor shared media/subtitle loader and implement `handleImportAIOrganize` in `internal/api/handlers_ai.go` + register route**

- Update `readSubtitleExcerpt` to decode raw bytes using `media.DecodeSubtitleToUTF8(raw)` before stripping SRT/VTT cue numbers and timestamps.
- Extract `loadMediaAndSubtitlesByIDs(ctx context.Context, mfIDs []string, includeSubtitles bool) (map[string]lessonMediaSubtitleMeta, error)` and make `loadLessonMediaAndSubtitles` delegate to it.
- Implement `handleImportAIOrganize`:
  - Validate AI config (`BaseURL != ""` && `Model != ""`), source existence, non-empty unique `media_file_id` list, and verify every `media_file_id` exists in `media_files` under `source_id`.
  - Build `ai.CourseOrganizeInput` with `LessonID: l.MediaFileID`, attach filenames, relative paths, and budgeted subtitle excerpts.
  - Call `provider.OrganizeCourse(ctx, input)` and map `ProposalLesson.LessonID` -> `ImportProposalLesson.MediaFileID` in `ImportOrganizeProposal`.
- Register `pr.Post("/import/ai/organize", h.handleImportAIOrganize)` in `internal/api/router.go`.

- [ ] **Step 4: Run tests to verify Task 4 passes**

Run: `go test -race -count=1 ./internal/api ./internal/ai`
Expected: PASS

- [ ] **Step 5: Commit Task 4**

```bash
git add internal/api/handlers_ai.go internal/api/router.go internal/api/api_test.go
git commit -m "feat(ai): add import preview AI organize endpoint and UTF-8 subtitle excerpt decoding"
```

---

### Task 5: Storage Course-Assignment Counts & Filters

**Files:**
- Modify: `internal/catalog/service.go:186-227,1408-1465,1530-1655`
- Modify: `internal/api/handlers_storage.go:113-130`
- Test: `internal/catalog/catalog_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: `lessons`, `courses`, `media_files`, `storage_sources` tables
- Produces:
  - `catalog.CourseRef` (`ID`, `Title`)
  - `catalog.StorageSource` adds `AssignedCount int` (`assigned_count`), `UnassignedCount int` (`unassigned_count`)
  - `catalog.IndexedDir` adds `AssignedCount int` (`assigned_count`), `UnassignedCount int` (`unassigned_count`), `Courses []CourseRef` (`courses`)
  - `catalog.MediaFile` adds `Assigned bool` (`assigned`), `Courses []CourseRef` (`courses`)
  - `(*catalog.Service).ListMediaFilesWithAssigned(ctx, sourceID, dirPrefix, search, status, assignedFilter string) ([]MediaFile, error)` (keeping `ListMediaFiles` calling it with `""`)
  - `GET /api/v1/sources/{id}/files?assigned=true|false`

- [ ] **Step 1: Write failing tests for assigned/unassigned counts, multi-course deduplication, and `?assigned=` filtering**

In `internal/catalog/catalog_test.go`, add `TestStorageAssignmentCountsAndMultiCourseDedup`:
- Seeds source with 3 active files (`DirA/01.mp4`, `DirA/02.mp4`, `DirB/03.mp4`).
- Adds `DirA/01.mp4` to **both** Course 1 and Course 2 (2 lessons referencing same `media_file_id`).
- Asserts `ListSources` / `GetSource`: `MediaCount == 3`, `AssignedCount == 1` (deduplicated!), `UnassignedCount == 2`.
- Asserts `ListIndexedDirs(sourceID, "")`:
  - `DirA`: `FileCount == 2`, `AssignedCount == 1`, `UnassignedCount == 1`, `Courses` has 2 unique `CourseRef`s sorted by title.
  - `DirB`: `FileCount == 1`, `AssignedCount == 0`, `UnassignedCount == 1`, `Courses` is empty `[]`.
- Asserts `ListMediaFilesWithAssigned`:
  - `""`: returns all 3; `DirA/01.mp4` has `Assigned == true` and 2 unique courses; others have `Assigned == false` and `Courses == []`.
  - `"true"`: returns only `DirA/01.mp4`.
  - `"false"`: returns `DirA/02.mp4` and `DirB/03.mp4`.

In `internal/api/api_test.go`, add `TestStorageSourcesDirsAndFilesAssignmentAPI` verifying JSON fields `assigned_count`, `unassigned_count`, `assigned`, `courses` and `?assigned=false` query filtering on `/api/v1/sources/{id}/files`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/catalog ./internal/api -run "TestStorageAssignment|TestStorageSourcesDirsAndFilesAssignment"`
Expected: FAIL

- [ ] **Step 3: Implement assignment stats & filtering in `internal/catalog/service.go` and `internal/api/handlers_storage.go`**

- Add `CourseRef` struct and additive fields on `StorageSource`, `IndexedDir`, and `MediaFile`.
- Update `ListSources` & `GetSource` SQL to compute `COUNT(DISTINCT CASE WHEN EXISTS (SELECT 1 FROM lessons l WHERE l.media_file_id = mf.id) THEN mf.id END) AS assigned_count` and derive `UnassignedCount = max(0, MediaCount - AssignedCount)`.
- Add shared helper `loadCourseRefsByMediaFileIDs(ctx context.Context, mfIDs []string) (map[string][]CourseRef, error)` executing a single `SELECT DISTINCT l.media_file_id, c.id, c.title FROM lessons l JOIN courses c ON c.id = l.course_id WHERE l.media_file_id IN (...) ORDER BY c.title ASC, c.id ASC`.
- Update `ListIndexedDirs` to load `(id, relative_path)` for active files in `sourceID`, batch-load their `CourseRef`s once, and aggregate per-directory `AssignedCount`, `UnassignedCount`, and deduplicated `Courses` (always initialized to non-nil `[]CourseRef{}`).
- Implement `ListMediaFilesWithAssigned` (with `ListMediaFiles` delegating `assignedFilter = ""`), populating `Assigned` and `Courses` (non-nil `[]CourseRef{}`), and filtering when `assignedFilter` is `"true"`/`"1"` or `"false"`/`"0"`.
- Update `handleListFiles` in `internal/api/handlers_storage.go` to pass `q.Get("assigned")`.

- [ ] **Step 4: Run tests to verify Task 5 passes**

Run: `go test -race -count=1 ./internal/catalog ./internal/api`
Expected: PASS

- [ ] **Step 5: Commit Task 5**

```bash
git add internal/catalog/service.go internal/catalog/catalog_test.go internal/api/handlers_storage.go internal/api/api_test.go
git commit -m "feat(storage): expose course assignment counts, linked courses, and assigned file filter"
```

---

### Task 6: Frontend Types, API Client, Codec Probe Utility & Import Modal AI Organization

**Files:**
- Modify: `web/src/types/coursefin.ts`
- Modify: `web/src/api/client.ts`
- Create: `web/src/utils/mediaCapabilities.ts`
- Modify: `web/src/components/ImportCourseModal.tsx`
- Create: `web/src/__tests__/ImportAI.test.tsx`

**Interfaces:**
- Consumes: `POST /api/v1/import/ai/organize`, `GET /api/v1/sources/{id}/subtitles`, `PUT /api/v1/subtitles/{id}/bind`, updated playback/storage responses (Tasks 2–5)
- Produces:
  - TS types: `CourseRef`, `IndexedSubtitle`, `ImportAIOrganizeInput`, `ImportOrganizeProposal`, updated `LessonDetail`, `LessonPlaybackInfo`, `StorageSource`, `ScanReport`, `IndexedDir`, `MediaFile`
  - `api.getLessonPlayback(lessonId, options?: { codecs?: string[]; forceTranscode?: boolean })`
  - `api.listFiles(sourceId, params: { prefix?: string; search?: string; status?: string; assigned?: 'true' | 'false' })`
  - `api.aiOrganizeImport(input: ImportAIOrganizeInput): Promise<ImportOrganizeProposal>`
  - `api.listSourceSubtitles(sourceId, params?: { unmatched?: boolean; prefix?: string; search?: string })`
  - `api.bindSubtitle(subtitleId, input: { media_file_id?: string | null; title?: string; language?: string; is_default?: boolean; reset_auto?: boolean })`
  - `detectBrowserVideoCodecs(): string[]` in `web/src/utils/mediaCapabilities.ts`

- [ ] **Step 1: Write failing tests for `detectBrowserVideoCodecs` and `ImportCourseModal` AI organize flow**

Create `web/src/__tests__/ImportAI.test.tsx`:
- Tests `detectBrowserVideoCodecs()` with mocked `HTMLVideoElement.prototype.canPlayType`:
  - Returns `['h264', 'hevc', 'vp9', 'av1']` when `hvc1` returns `'probably'`.
  - Omits `'hevc'` when `hvc1`/`hev1` only return `'maybe'` or `''` (conservative HEVC detection).
- Tests `ImportCourseModal` AI workflow:
  - Renders `AI 智能整理` button after preview loads.
  - Clicking triggers `POST /api/v1/import/ai/organize` with current draft chapters/lessons.
  - Shows side-by-side comparison of Title, Overview, Tags, and Chapters/Lessons with checkboxes.
  - Unchecking "应用建议简介" and clicking "应用到导入表单" updates title, tags, and reorganized chapters/lessons while keeping original overview intact; does NOT call `/api/v1/import/commit`.
  - Verifies durations for reordered/regrouped lessons are still displayed accurately via `media_file_id` lookup.
  - Clicking "恢复初始识别" restores the initial preview title, tags, and chapters.
  - Clicking "AI 智能整理" when backend returns 400 (AI not configured) displays error message with link to `/settings` and retry button.
  - Verifies no legacy palette classes (`slate/indigo/rose/emerald`).

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --dir web test src/__tests__/ImportAI.test.tsx`
Expected: FAIL

- [ ] **Step 3: Update `web/src/types/coursefin.ts`, `web/src/api/client.ts`, and create `web/src/utils/mediaCapabilities.ts`**

- Add all new/updated interfaces in `web/src/types/coursefin.ts` (making new backend fields optional or defaulted safely so existing test fixtures continue compiling cleanly).
- Update `api.getLessonPlayback`, `api.listFiles`, and add `api.aiOrganizeImport`, `api.listSourceSubtitles`, `api.bindSubtitle` in `web/src/api/client.ts`.
- Create `web/src/utils/mediaCapabilities.ts` implementing `detectBrowserVideoCodecs(): string[]`:
  - Checks `h264`, `hevc` (`hvc1.1.6.L93.B0` / `hev1.1.6.L93.B0` requiring `'probably'`), `vp9`, `av1`.
  - Falls back to `['h264', 'vp9', 'av1']` in non-browser/jsdom environments where `canPlayType` is unavailable or returns empty for all codecs.

- [ ] **Step 4: Integrate AI organization preview & apply flow into `web/src/components/ImportCourseModal.tsx`**

- Build `previewLessonMap = useMemo(() => new Map(...), [preview])` keyed by `media_file_id` so lesson durations render accurately after AI reorders/regroups chapters.
- Add `AI 智能整理` button (`Sparkles` icon) in header/section toolbar and `恢复初始识别` button when `aiApplied` is true.
- When AI preview is open:
  - Render an overlay/nested dialog (`aria-labelledby="import-ai-modal-title"`) matching `AIOrganizeModal`'s exact semantic styling, 4 comparison cards (Title, Overview, Tags, Chapters & Lessons) with checkboxes, error card with `/settings` link + "重新生成", and "应用到导入表单" button.
  - Pressing `Escape` closes only the AI preview overlay first, never the outer `ImportCourseModal`.
  - Applying updates `title`, `overview`, `tags`, `chapters` in local form state, sets `aiApplied = true`, closes the AI preview, and shows a success toast.

- [ ] **Step 5: Run tests to verify Task 6 passes**

Run: `pnpm --dir web test src/__tests__/ImportAI.test.tsx src/__tests__/EditorAndAI.test.tsx`
Expected: PASS

- [ ] **Step 6: Commit Task 6**

```bash
git add web/src/types/coursefin.ts web/src/api/client.ts web/src/utils/mediaCapabilities.ts web/src/components/ImportCourseModal.tsx web/src/__tests__/ImportAI.test.tsx
git commit -m "feat(web): add AI organization preview to ImportCourseModal and browser codec probe"
```

---

### Task 7: Artplayer Embedded Web Video Player, Auto-Subtitles & Transcode Fallback

**Files:**
- Modify: `web/package.json`, `web/pnpm-lock.yaml`
- Modify: `web/src/components/VideoPlayer.tsx`
- Modify: `web/src/__tests__/VideoPlayer.test.tsx`

**Interfaces:**
- Consumes: `artplayer`, `hls.js`, `LessonPlaybackInfo` (including `transcoding?: boolean`), `api.reportProgress`, `api.stopActiveEncoding`
- Produces:
  - `SUBTITLE_PREF_STORAGE_KEY = 'coursefin_subtitle_pref'`
  - `pickInitialSubtitle(subtitles: PlaybackSubtitleItem[], savedPref: string | null): PlaybackSubtitleItem | null`
  - `VideoPlayerProps`: adds `onTimeProgress?: (lessonId: string, currentSec: number, durationSec: number) => void` and `onRequestTranscodeFallback?: () => void`

- [ ] **Step 1: Install `artplayer` in `web/`**

Run: `pnpm --dir web add artplayer` (verify `web/package.json` and `web/pnpm-lock.yaml` updated cleanly).

- [ ] **Step 2: Write/update failing tests in `web/src/__tests__/VideoPlayer.test.tsx`**

Update/add tests covering:
- `pickInitialSubtitle` unit tests:
  - Respects `savedPref === 'off'` -> returns `null`.
  - Matches `savedPref === 'en'` -> picks English track over default Chinese.
  - No `savedPref` -> picks `is_default: true` track; if none marked default, picks Chinese (`zh` / `中文`) track; if no Chinese, picks first text track.
- `VideoPlayer` mounts `Artplayer` (mocked cleanly in jsdom with a real `<video>` inside the mount container so DOM/video events work authentically):
  - Initializes with `lang: 'zh-cn'`, `fullscreenWeb: true`, `pip: true`, `miniProgressBar: true`, `screenshot: true`, `subtitleOffset: true`, and preferred subtitle preloaded.
  - Switching subtitle via selector/control updates active subtitle and saves preference to `localStorage['coursefin_subtitle_pref']`; turning off saves `'off'`.
  - Changing playback speed persists to `localStorage['coursefin_playback_rate']`.
  - Resumes from `position_ticks` on unplayed lesson, reports progress on pause/ended, flushes via `keepalive` on unmount without double-flushing, and emits `onTimeProgress` on `timeupdate`.
  - Initializes `hls.js` inside `customType.m3u8` (passing `{ startPosition }` for unplayed resume), destroys `hls` and calls `DELETE /Videos/ActiveEncodings` on unmount without clearing token on 401.
  - Displays subtle `兼容转码播放` badge when `playback.transcoding === true`.
  - Fires `onRequestTranscodeFallback` once when `playback.mode === 'direct'` encounters a `<video>` `error` before playback starts, and does NOT fire if already `hls` or already started playing.
  - Keeps `unsupported` native Jellyfin recommendation banner (Infuse/VidHub/SenPlayer) with zero hardcoded legacy palette classes.

- [ ] **Step 3: Run tests to verify they fail**

Run: `pnpm --dir web test src/__tests__/VideoPlayer.test.tsx`
Expected: FAIL

- [ ] **Step 4: Rewrite `web/src/components/VideoPlayer.tsx` wrapping `Artplayer` + `hls.js`**

- Export `SUBTITLE_PREF_STORAGE_KEY`, `PLAYBACK_RATE_STORAGE_KEY`, `PLAYBACK_RATES`, and `pickInitialSubtitle`.
- Mount `Artplayer` inside a dedicated container `div` in a `useEffect` keyed on `[playback.lesson_id, playback.play_session_id, resolvedStreamUrl, playback.mode]`:
  - Read computed `--accent` CSS variable for `theme` (fallback `#1E88E5`).
  - Configure `url: resolvedStreamUrl`, `type: playback.mode === 'hls' ? 'm3u8' : ''`, `customType.m3u8` using `hls.js` + Safari native HLS fallback.
  - Configure `subtitle` with initial selected track (via `pickInitialSubtitle`), `type: 'vtt'`, `encoding: 'utf-8'`, `escape: false`, clean readable style (`fontSize`, `marginBottom`, subtle background pill).
  - Register Artplayer `settings` items for:
    1. **字幕轨道** (`关闭` + all `textSubtitles` with `formatSubtitleLabel`), switching via `art.subtitle.switch(withApiKey(sub.url), { name, type: 'vtt' })` + `art.subtitle.show = true` (or `art.subtitle.show = false` when off) and saving `coursefin_subtitle_pref`.
    2. **字幕字号** (`小 16px` / `中 20px` / `大 24px`).
  - Register a convenient bottom control bar subtitle toggle/selector right inside Artplayer (`controls` option) so subtitle switching is one click away in normal, web-fullscreen, and browser-fullscreen modes!
  - Wire video/Artplayer events:
    - `ready` / `loadedmetadata`: apply saved `playbackRate`, apply resume `currentTime` for direct play (`!played && position_ticks > 0`).
    - `play` / `playing`: mark `hasPlayedRef.current = true`, start 10s interval progress sync.
    - `timeupdate` / `seeked`: update `latestTimeRef.current` and invoke `onTimeProgress` throttled to >= 1% progress delta (or 1s).
    - `pause`: report current progress ticks.
    - `ended`: report final ticks and call `onEnded`.
    - `ratechange`: sync & persist `playbackRate`.
    - `error`: if `playback.mode === 'direct' && !hasPlayedRef.current && !fallbackTriggeredRef.current && onRequestTranscodeFallbackRef.current`, trigger fallback once; otherwise show inline error banner.
  - Clean up `hls.destroy()`, `art.destroy(false)`, and `api.stopActiveEncoding(playback.play_session_id)` on unmount/lesson change, plus keepalive progress flush matching existing dedup rules.
  - Keep a clean compact footer status bar below the player (or integrated badges) showing `兼容转码播放` badge when `playback.transcoding` is true, quick subtitle indicator, and keyboard shortcut hints.
  - Keep `mode === 'unsupported'` recommendation card unchanged.

- [ ] **Step 5: Run tests to verify Task 7 passes**

Run: `pnpm --dir web test src/__tests__/VideoPlayer.test.tsx`
Expected: PASS

- [ ] **Step 6: Commit Task 7**

```bash
git add web/package.json web/pnpm-lock.yaml web/src/components/VideoPlayer.tsx web/src/__tests__/VideoPlayer.test.tsx
git commit -m "feat(player): embed Artplayer with hls.js, auto-subtitle memory, and transcode fallback"
```

---

### Task 8: Course Detail Circular Progress Rings + Editor Manual Subtitle Binding

**Files:**
- Create: `web/src/components/LessonProgressIcon.tsx`
- Modify: `web/src/pages/CourseDetailPage.tsx`
- Create: `web/src/components/SubtitleBindModal.tsx`
- Modify: `web/src/pages/CourseEditorPage.tsx`
- Test: `web/src/__tests__/VideoPlayer.test.tsx`, `web/src/__tests__/EditorAndAI.test.tsx`

**Interfaces:**
- Consumes: `VideoPlayer` (`onTimeProgress`, `onRequestTranscodeFallback`), `detectBrowserVideoCodecs`, `api.listSourceSubtitles`, `api.bindSubtitle`, `LessonDetail.source_id`
- Produces:
  - `LessonProgressIcon({ played, progressRatio, isActive })`
  - `SubtitleBindModal({ isOpen, lesson, fallbackSourceId, onClose, onChanged })`

- [ ] **Step 1: Write failing tests for `LessonProgressIcon`, `CourseDetailPage` live ring + transcode fallback, and `SubtitleBindModal` in `CourseEditorPage`**

In `web/src/__tests__/VideoPlayer.test.tsx`:
- Add `LessonProgressIcon` tests verifying all 4 visual states:
  - `played: true` -> `aria-label="已学完"`, `data-progress-percent="100"`.
  - `isActive: true, played: false, progressRatio: 0.65` -> `aria-label="正在播放 · 已观看 65%"`, `data-progress-percent="65"`, renders SVG progress circle + inner play polygon.
  - `isActive: false, played: false, progressRatio: 0.42` -> `aria-label="已观看 42%"`, `data-progress-percent="42"`, renders SVG progress circle without play polygon.
  - `isActive: false, played: false, progressRatio: 0` -> `aria-label="未播放"`, `data-progress-percent="0"`.
- Add `CourseDetailPage` test verifying:
  - Sends `?codecs=...` on `GET /api/v1/lessons/l1/playback`.
  - Renders `LessonProgressIcon` reflecting `position_ticks / (duration_ms * 10000)` for partially watched lessons and updates live when video `timeupdate` advances.
  - On direct-play video error, automatically retries once with `force_transcode=1` (and does NOT retry a second time if that also fails).

In `web/src/__tests__/EditorAndAI.test.tsx`:
- Add `TestCourseEditorManualSubtitleBinding`:
  - Clicks "关联字幕" on a lesson row -> fetches `GET /api/v1/sources/{sourceId}/subtitles` -> displays bound & unmatched subtitles -> clicking "绑定" sends `PUT /api/v1/subtitles/{subId}/bind` with `{ media_file_id }` -> refreshes course detail and displays newly bound subtitle chip; clicking "解绑" sends `{ media_file_id: null }`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm --dir web test src/__tests__/VideoPlayer.test.tsx src/__tests__/EditorAndAI.test.tsx`
Expected: FAIL

- [ ] **Step 3: Implement `LessonProgressIcon.tsx` and wire into `CourseDetailPage.tsx`**

- Create `web/src/components/LessonProgressIcon.tsx` implementing the `18px × 18px` (`viewBox="0 0 20 20"`) SVG progress ring per Spec Section 5.5 (`r=7.5`, `C=2*Math.PI*7.5`, `-90deg` rotation, `clamp(ratio, 0.04, 0.99)` when `ratio > 0`, inner play triangle when `isActive && !played`, green `CheckCircle2` when `played`, wrapped in `<Tooltip>`).
- Update `web/src/pages/CourseDetailPage.tsx`:
  - Pass `detectBrowserVideoCodecs()` to `api.getLessonPlayback(activeLessonId, { codecs, forceTranscode })`.
  - Track `forceTranscodeLessonId` state (reset on lesson switch) and pass `onRequestTranscodeFallback` to `VideoPlayer` to trigger a single fallback fetch + `toast.info(...)`.
  - Track `activeLiveProgress: { lessonId: string; ratio: number } | null` updated from `VideoPlayer.onTimeProgress` and cleared on lesson switch / `handleProgressSaved` / `handleTogglePlayed`.
  - Replace sidebar status icon block with `<LessonProgressIcon played={lesson.played} progressRatio={ratio} isActive={isActive} />`.

- [ ] **Step 4: Implement `SubtitleBindModal.tsx` and wire into `CourseEditorPage.tsx`**

- Create `web/src/components/SubtitleBindModal.tsx`:
  - Uses `lesson.source_id || fallbackSourceId` to load `api.listSourceSubtitles(sourceId, { unmatched: filterMode === 'unmatched', search })`.
  - Top section lists currently bound subtitles for `lesson` with "解绑" button (`api.bindSubtitle(sub.id, { media_file_id: null })`).
  - Bottom section has filter (`仅看未匹配` / `全部字幕`) + search box + list with "绑定到本节" button (`api.bindSubtitle(sub.id, { media_file_id: lesson.media_file_id })`).
  - Calls `onChanged()` after bind/unbind so `CourseEditorPage` reloads course subtitles immediately.
- In `web/src/pages/CourseEditorPage.tsx`:
  - Add a subtle `Subtitles` icon button (`aria-label={`管理课时字幕 ${lesson.title}`}`, tooltip `"查看或手动关联外挂字幕"`) on `SortableLessonRow` when `lesson.source_id || course.source_id` is available, opening `SubtitleBindModal`.

- [ ] **Step 5: Run tests to verify Task 8 passes**

Run: `pnpm --dir web test src/__tests__/VideoPlayer.test.tsx src/__tests__/EditorAndAI.test.tsx`
Expected: PASS

- [ ] **Step 6: Commit Task 8**

```bash
git add web/src/components/LessonProgressIcon.tsx web/src/components/SubtitleBindModal.tsx web/src/pages/CourseDetailPage.tsx web/src/pages/CourseEditorPage.tsx web/src/__tests__/VideoPlayer.test.tsx web/src/__tests__/EditorAndAI.test.tsx
git commit -m "feat(web): add syllabus circular progress rings, transcode fallback wiring, and manual subtitle binding"
```

---

### Task 9: Storage Page Course-Assignment Indicators, Scan Match Stats & Full Build Verification

**Files:**
- Modify: `web/src/pages/StoragePage.tsx`
- Create: `web/src/__tests__/StorageAssignment.test.tsx`
- Rebuild embedded assets: `internal/server/webdist/*` (via `make sync-web`)

**Interfaces:**
- Consumes: `StorageSource.{assigned_count, unassigned_count}`, `ScanReport.matched_subtitles`, `IndexedDir.{assigned_count, unassigned_count, courses}`, `MediaFile.{assigned, courses}`, `api.listFiles(..., { assigned })`
- Produces: Complete Storage & Import UI indicators + verified Go & Web test suites + compiled binary

- [ ] **Step 1: Write failing tests in `web/src/__tests__/StorageAssignment.test.tsx`**

Test `StoragePage`:
- Source card displays `未入课 2 个` when `unassigned_count === 2` and `全部已入课` when `media_count > 0 && unassigned_count === 0`.
- Scan report displays `字幕：5（已匹配 4）` when `matched_subtitles` is present.
- Subdirectory rows show:
  - Fully assigned dir: `已全部入课` badge + clickable link to `/courses/{id}` + secondary-styled `一键导入为课程` button.
  - Partially assigned dir: `部分已入课` badge + `未入课 1 个`.
  - Unassigned dir: `未入课` badge.
- File list displays `全部 | 未入课 | 已入课` segmented filter; clicking `未入课` requests `/api/v1/sources/{id}/files?...&assigned=false`.
- File rows display `已入课` + course title link (with `+1` indicator when belonging to 2+ courses) or `未入课` badge.
- Completing an import via `ImportCourseModal` refreshes sources and current directory browse data automatically.
- Zero hardcoded legacy palette classes (`slate/indigo/rose/emerald`).

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --dir web test src/__tests__/StorageAssignment.test.tsx`
Expected: FAIL

- [ ] **Step 3: Update `web/src/pages/StoragePage.tsx`**

- Add `fileAssignedFilter` state (`'all' | 'unassigned' | 'assigned'`) and pass `assigned: filter === 'assigned' ? 'true' : filter === 'unassigned' ? 'false' : undefined` to `api.listFiles` (and keep synced in `browseStateRef`).
- Update Storage Source cards to display `未入课 N 个` / `全部已入课` badge alongside `已索引 N 个视频`.
- Update Scan Report bar to show `字幕：{total_subtitles}` plus `（已匹配 {matched_subtitles}）` when `matched_subtitles !== undefined`.
- Update Subdirectory rows with `未入课 {dir.unassigned_count} 个`, status pills (`已全部入课` / `部分已入课` / `未入课`), clickable `<Link to="/courses/{id}">` course chips (up to 2 + `+N` Tooltip), and secondary button styling when `dir.unassigned_count === 0 && dir.file_count > 0`.
- Update Media File list header with `全部 | 未入课 | 已入课` filter tabs, and update each file row with `已入课 · {course.title}` link badge(s) or `未入课` badge.
- In `ImportCourseModal`'s `onImported` callback, call `api.listSources()` and `loadBrowseData(...)` so counts and badges refresh immediately.

- [ ] **Step 4: Run StoragePage tests and full Vitest suite**

Run: `pnpm --dir web test`
Expected: All Vitest test files PASS.

- [ ] **Step 5: Sync embedded frontend assets, build binary, and run complete test suite**

Run:
1. `make sync-web`
2. `go test -race -count=1 ./...`
3. `pnpm --dir web test`
4. `make build`
Expected: All Go tests pass with `-race`, all Vitest tests pass, `tsc --noEmit && vite build` succeeds, and `coursefin` binary builds cleanly with `CGO_ENABLED=0`.

- [ ] **Step 6: Commit Task 9**

```bash
git add web/src/pages/StoragePage.tsx web/src/__tests__/StorageAssignment.test.tsx internal/server/webdist/
git commit -m "feat(storage): display course assignment status, unassigned counts, and sync embedded web assets"
```
