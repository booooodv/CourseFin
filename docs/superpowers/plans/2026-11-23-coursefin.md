# CourseFin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build CourseFin, a single-container Docker self-hosted course management platform that decouples local video indexing from custom course organization, exposes a high-fidelity Jellyfin-compatible API (DirectPlay + on-demand seekable VOD HLS remux), serves an embedded Web management/player UI, and supports OpenAI-compatible AI course metadata organization.

**Architecture:** A single Go binary embeds the compiled React/TypeScript SPA (`embed.FS`), uses SQLite (WAL mode) under `/data`, wraps `ffmpeg`/`ffprobe` for probing, thumbnails, subtitle extraction, and on-demand `-ss` HLS remuxing, and cleanly separates pluggable `StorageProvider` (`LocalFS` in v1) and `AIProvider` (`OpenAICompatibleProvider` in v1) interfaces. HTTP routing serves Jellyfin API at root `/` (plus `/emby` alias and case-insensitive matching), CourseFin management REST API at `/api/v1`, and the Web SPA at `/web` (redirecting browser `/` to `/web`).

**Tech Stack:** Go 1.24, `github.com/go-chi/chi/v5`, `modernc.org/sqlite` (pure-Go SQLite, CGO-free static builds), `golang.org/x/crypto/bcrypt`, `github.com/coder/websocket`, `golang.org/x/image` (cover resizing), React 19 + TypeScript + Vite + Tailwind CSS + `hls.js` + `@dnd-kit/core` + Vitest, Docker multi-stage build (`debian:bookworm-slim` + `ffmpeg`).

**Spec:** `docs/superpowers/specs/2026-11-23-coursefin-design.md`

## Global Constraints

- Single Docker container (`amd64`/`arm64`), single writable `/data` volume, read-only media mounts (`/media/...:ro`), default port `8096` (`COURSEFIN_PORT`).
- Never modify, move, rename, or delete any file on media storage volumes.
- All entity IDs (Library, Course, Chapter, Lesson, Tag, User, StorageSource, MediaFile, SubtitleFile) are persistent 32-character lowercase hex UUIDs (no hyphens).
- Single-admin user account (no multi-user CRUD UI), but full multi-device session tokens compatible with Jellyfin auth (`Authorization: MediaBrowser/Emby ... Token="..."`, `X-Emby-Token`, `X-MediaBrowser-Token`, `?api_key=`).
- Root-path / origin-only hosting (`http://ip:8096` or dedicated subdomain); no runtime subpath hosting.
- Playback progress is unified across Jellyfin clients and CourseFin Web in Jellyfin ticks (`1 tick = 100ns = ms * 10000`); `>= 90%` auto-marks `played = true` and resets `position_ticks = 0`.
- Re-scanning storage and AI metadata organization must never silently overwrite user-edited titles, covers, tags, or ordering.
- DirectPlay supports full HTTP Range (`HEAD`, `206 Partial Content`, `Accept-Ranges: bytes`, `Content-Length`, `ETag`); Remux uses `-c:v copy` (no video re-encoding) with on-demand `-ss` VOD HLS seeking.
- Web UI is Chinese-first, served under `/web`, talking to `/api/v1`.

## Review Focus

1. **Path traversal or symlink escape on local storage:** A request or corrupted relative path containing `..` or a symlink escaping `root_path` must be rejected (`ErrPathOutsideRoot`) and never leak host files. *(Pinned in Task 2)*
2. **Temporary NAS unmount during scan:** If media files disappear temporarily during a re-scan, `media_files.status` flips to `"missing"` without deleting curated courses, chapters, lessons, or playback progress, and recovers to `"active"` when files return. *(Pinned in Task 2 & Task 3)*
3. **Natural ordering & deep nesting on directory import + idempotent incremental sync:** Mixed Chinese/English numeric filenames (`第2讲`/`第10讲`, `1.2`/`1.10`) sort in human order, nested folders (>2 levels) group into top-level chapters with subfolder prefix, and clicking incremental sync never duplicates existing lessons. *(Pinned in Task 3)*
4. **Late-timestamp seek/resume on remuxed HLS & unsupported browser video codec:** Requesting segment N of a long `.mkv` starts FFmpeg at `-ss` for that segment rather than remuxing from `t=0`; unsupported video codecs (e.g., `wmv3`, `rv40`) report non-browser-playable status so the Web player shows a clear native-player guidance banner. *(Pinned in Task 4 & Task 9)*
5. **Jellyfin client header/path quirks & malformed/incomplete AI JSON:** Requests using `/emby/...`, mixed case (`/users/public`), `Emby Token=`, or `?api_key=` authenticate and route identically to canonical Jellyfin paths; AI responses wrapped in markdown fences are unwrapped, while responses omitting or inventing any `lesson_id` are rejected before preview. *(Pinned in Task 5 & Task 6)*

---

## File Structure

- `cmd/coursefin/main.go` — Process entrypoint, env config loading, DB/service wiring, graceful shutdown.
- `internal/config/config.go` — Environment variable parsing (`COURSEFIN_PORT`, `COURSEFIN_DATA_DIR`, `COURSEFIN_ADMIN_USER`, `COURSEFIN_ADMIN_PASS`, `COURSEFIN_LOG_LEVEL`, `COURSEFIN_BASE_URL`).
- `internal/id/id.go` — `NewID()` (32-char lowercase hex UUID) and `NormalizeID(raw string) (string, bool)`.
- `internal/db/db.go` & `internal/db/migrations/001_init.sql` — SQLite connection (WAL, foreign keys, busy timeout) and embedded schema migrations.
- `internal/auth/service.go` — Single-admin setup, login, bcrypt password verification, multi-device session store, Jellyfin auth header parser.
- `internal/storage/provider.go` & `internal/storage/local.go` — Pluggable `StorageProvider` interface and hardened `LocalFS` driver.
- `internal/scanner/prober.go`, `subtitle_match.go`, `scanner.go` — `ffprobe` metadata parser, `ffmpeg` thumbnail extractor, sidecar subtitle matcher, incremental directory scanner.
- `internal/catalog/natsort.go`, `service.go`, `importer.go`, `progress.go` — Natural sorting, Libraries/Courses/Chapters/Lessons/Tags CRUD, one-click directory import & incremental sync, unified progress/Resume/NextUp calculations.
- `internal/media/streamer.go`, `remux.go`, `subtitle.go`, `image.go` — HTTP Range DirectPlay (with `DirectURL` 302 hook), on-demand seekable VOD HLS remux manager, sidecar/embedded subtitle delivery + SRT→VTT converter, resized image cache.
- `internal/ai/provider.go` & `internal/ai/openai.go` — `AIProvider` interface, proposal validator, OpenAI-compatible `/chat/completions` client.
- `internal/jellyfin/dto.go`, `profile.go`, `handlers_system.go`, `handlers_items.go`, `handlers_media.go`, `router.go` — Complete Jellyfin 10.8/10.9+ REST/WebSocket compatibility layer and `DeviceProfile` playback negotiation.
- `internal/api/handlers_auth.go`, `handlers_storage.go`, `handlers_catalog.go`, `handlers_ai.go`, `router.go` — CourseFin Web management REST API (`/api/v1/*`).
- `internal/server/server.go` & `internal/server/embed.go` — Top-level HTTP mux combining `/api/v1`, `/web` (embedded SPA), `/` browser redirect, and Jellyfin root + `/emby`.
- `web/` — React + TypeScript + Vite + Tailwind CSS SPA (Setup/Login, Course Home, Course Detail & Web Player, Course Editor + AI Modal, Storage & Directory Importer, Settings).
- `Dockerfile`, `docker-compose.yml`, `.gitignore` — Multi-stage container build and ready-to-run compose stack.

---

### Task 1: Project Foundation, SQLite Schema & Single-Admin Auth

**Files:**
- Create: `.gitignore`, `go.mod`, `internal/config/config.go`, `internal/id/id.go`, `internal/db/db.go`, `internal/db/migrations/001_init.sql`, `internal/auth/service.go`
- Test: `internal/db/db_test.go`, `internal/auth/service_test.go`

**Interfaces:**
- Consumes: none (foundation task)
- Produces:
  - `config.Load() config.Config` (`Port`, `DataDir`, `AdminUser`, `AdminPass`, `LogLevel`, `BaseURL`)
  - `id.New() string` (32-char lowercase hex), `id.Normalize(s string) (string, bool)` (accepts dashed or undashed UUID)
  - `db.Open(dataDir string) (*sql.DB, error)` (runs `001_init.sql`, enables WAL & `PRAGMA foreign_keys = ON`, seeds persistent `server_id` and default library `"我的课程"`)
  - `auth.NewService(db *sql.DB) *auth.Service`
  - `auth.Service`: `NeedsSetup(ctx) (bool, error)`, `SetupAdmin(ctx, username, password) (*User, error)`, `EnsureEnvAdmin(ctx, username, password) error`, `Login(ctx, username, password, device DeviceInfo) (*Session, *User, error)`, `Authenticate(ctx, token) (*Session, *User, error)`, `ChangePassword(ctx, oldPass, newPass) error`, `ListSessions(ctx) ([]Session, error)`, `RevokeSession(ctx, token) error`, `GetAdminUser(ctx) (*User, error)`
  - `auth.ParseAuthHeader(r *http.Request) (token string, device auth.DeviceInfo)`

- [ ] **Step 1: Initialize Git repository, `.gitignore`, and `go.mod`**

Create `.gitignore` (ignoring `/data`, `web/node_modules`, `web/dist`, `*.db`, `*.db-wal`, `*.db-shm`, binary artifacts) and `go.mod` with module `coursefin` (Go 1.24) adding `github.com/go-chi/chi/v5`, `modernc.org/sqlite`, `golang.org/x/crypto`, `golang.org/x/image`, `github.com/coder/websocket`, `github.com/google/uuid`.

- [ ] **Step 2: Write failing tests for DB migrations, ID normalization, and Auth**

Create `internal/db/db_test.go` and `internal/auth/service_test.go` with tests:
- `TestOpenCreatesSchemaAndSeedsDefaults`: opens SQLite in `t.TempDir()`, verifies WAL mode, foreign keys enabled, all 11 tables exist (`storage_sources`, `media_files`, `subtitle_files`, `libraries`, `courses`, `chapters`, `lessons`, `tags`, `course_tags`, `playback_progress`, `users`, `sessions`, `app_settings`), `server_id` is a valid 32-char hex string persisted across re-open, and default library `"我的课程"` exists.
- `TestSetupLoginMultiDeviceAndRevoke`: verifies `NeedsSetup` starts `true`, `SetupAdmin` succeeds once and rejects second call with `ErrAlreadySetup`, `Login` from two different `DeviceId`s creates two concurrent sessions while re-login from same `DeviceId` replaces only that device's token, `Authenticate` succeeds for both tokens, `RevokeSession` invalidates only the targeted token, and `ChangePassword` updates hash cleanly.
- `TestParseAuthHeaderVariants`: tests all 4 Jellyfin token mechanisms: `Authorization: MediaBrowser Client="Infuse", Device="iPad", DeviceId="dev1", Version="7.7", Token="tok1"`, `Authorization: Emby Token="tok2"`, `X-Emby-Token: tok3`, `X-MediaBrowser-Token: tok4`, and `?api_key=tok5`.

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./internal/db/... ./internal/auth/...`
Expected: FAIL (packages/functions not yet implemented).

- [ ] **Step 4: Implement `config`, `id`, `db` (`001_init.sql`), and `auth`**

- `internal/config/config.go`: `Load()` with defaults `Port="8096"`, `DataDir="/data"`, `LogLevel="info"`.
- `internal/id/id.go`: `New()` strips hyphens from UUIDv4 to 32-char lowercase hex; `Normalize()` accepts 32-char hex or 36-char dashed UUID and returns canonical 32-char lowercase hex.
- `internal/db/migrations/001_init.sql`: exact schema from Spec Section 6 (`storage_sources`, `media_files`, `subtitle_files`, `libraries`, `courses`, `chapters`, `lessons`, `tags`, `course_tags`, `playback_progress`, `users`, `sessions`, `app_settings`) with indexes on `(source_id, relative_path)`, `(course_id, sort_order)`, `(chapter_id, sort_order)`, `playback_progress(last_played_at)`.
- `internal/db/db.go`: creates `dataDir` + `covers/`, `thumbs/`, `cache/images/`, `cache/remux/`; opens SQLite via `modernc.org/sqlite` with `_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)`; runs embedded migrations tracked in `schema_migrations`; seeds `server_id`, `server_name="CourseFin"`, and default library `"我的课程"` if `libraries` is empty.
- `internal/auth/service.go`: bcrypt password hashing, crypto/rand 32-byte hex session tokens, case-insensitive header parsing for `MediaBrowser` / `Emby` quoted key-value fields plus `X-Emby-Token`, `X-MediaBrowser-Token`, and `api_key` query fallback.

- [ ] **Step 5: Run tests to verify pass**

Run: `go test -v ./internal/config/... ./internal/id/... ./internal/db/... ./internal/auth/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add .gitignore go.mod go.sum internal/config internal/id internal/db internal/auth
git commit -m "feat: add core config, sqlite schema, and single-admin auth"
```

---

### Task 2: Pluggable Storage (`LocalFS`) & Incremental Media Scanner

**Files:**
- Create: `internal/storage/provider.go`, `internal/storage/local.go`, `internal/scanner/prober.go`, `internal/scanner/subtitle_match.go`, `internal/scanner/scanner.go`
- Test: `internal/storage/local_test.go`, `internal/scanner/scanner_test.go`

**Interfaces:**
- Consumes: `db.Open`, `id.New`
- Produces:
  - `storage.StorageProvider` interface: `Walk(ctx context.Context, rootRelPath string) ([]storage.FileEntry, error)`, `Stat(ctx context.Context, relPath string) (storage.FileInfo, error)`, `Open(ctx context.Context, relPath string) (io.ReadSeekCloser, error)`, `DirectURL(ctx context.Context, relPath string) (string, bool, error)`
  - `storage.NewLocalFS(rootPath string) (*storage.LocalFS, error)`, `storage.ErrPathOutsideRoot`
  - `storage.Registry`: `ProviderFor(driver, rootPath, configJSON string) (storage.StorageProvider, error)`
  - `scanner.Prober` interface: `Probe(ctx context.Context, absPath string) (*scanner.ProbeResult, error)`, `ExtractThumbnail(ctx context.Context, absPath string, seekSec float64, outPath string) error`
  - `scanner.NewFFmpegProber(ffprobeBin, ffmpegBin string) *scanner.FFmpegProber`
  - `scanner.NewService(db *sql.DB, dataDir string, registry *storage.Registry, prober scanner.Prober) *scanner.Service`
  - `scanner.Service`: `ScanSource(ctx context.Context, sourceID string) (*scanner.ScanReport, error)`, `ScanAllEnabled(ctx context.Context)`, `StartPeriodicLoop(ctx context.Context, intervalFn func() time.Duration)`, `Status(sourceID string) scanner.ScanState`

- [ ] **Step 1: Write failing tests for `LocalFS` path security, subtitle matching, and incremental scan**

Create `internal/storage/local_test.go`:
- `TestLocalFSWalkStatOpenAndDirectURL`: verifies recursive listing with clean forward-slash relative paths, `Stat`, `Open` seeking, and `DirectURL` returning `("", false, nil)`.
- `TestLocalFSRejectsTraversalAndSymlinkEscape` *(Review Focus #1)*: attempts `../secret.txt`, `sub/../../secret.txt`, absolute `/etc/passwd`, and a symlink inside root pointing to a file outside root; asserts every call returns `errors.Is(err, storage.ErrPathOutsideRoot)`.

Create `internal/scanner/scanner_test.go`:
- `TestMatchSubtitlesByBasenameAndLanguage`: verifies `01_intro.mp4` matches `01_intro.srt` (`und`), `01_intro.zh.srt` (`zh`), `01_intro.en.vtt` (`en`), and ignores `02_other.srt`.
- `TestIncrementalScanProbesNewSkipsUnchangedMarksMissingAndRestores` *(Review Focus #2)*: uses a fake `Prober` counting invocations;
  1. Initial scan indexes 2 videos + 1 sidecar `.srt`, calls `Probe` & `ExtractThumbnail` twice, links subtitle to video.
  2. Second scan with unchanged files calls `Probe` 0 times.
  3. Removing 1 video marks its `media_files.status = "missing"` without deleting the row (nor any referencing `lessons` row).
  4. Restoring the video flips `status` back to `"active"`.
- `TestFFmpegProberIntegration` (skipped only if `ffprobe`/`ffmpeg` unavailable): generates a 2s silent MP4 via `ffmpeg -f lavfi`, runs real `FFmpegProber.Probe` and `ExtractThumbnail`, asserts `DurationMs > 1500`, `VideoCodec == "h264"`, and non-empty thumbnail JPEG created.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/storage/... ./internal/scanner/...`
Expected: FAIL

- [ ] **Step 3: Implement `LocalFS`, `FFmpegProber`, subtitle matcher, and `scanner.Service`**

- `internal/storage/local.go`: resolves `rootPath` via `filepath.EvalSymlinks`; every operation cleans `relPath`, joins with root, verifies both lexical path and `EvalSymlinks` target stay strictly inside `rootPath` (returning `ErrPathOutsideRoot` otherwise).
- `internal/scanner/prober.go`: executes `ffprobe -v quiet -print_format json -show_format -show_streams` and parses duration, bitrate, container, primary video/audio codecs, width/height, and full streams JSON; `ExtractThumbnail` runs `ffmpeg -ss <seek> -i <in> -frames:v 1 -vf scale='min(640,iw)':-2 -q:v 4 -y <out>` where `seek = clamp(duration*0.10, 5, 60)` (or `0` if video `< 5s`).
- `internal/scanner/subtitle_match.go`: supports `.srt`, `.vtt`, `.ass`, `.ssa`; strips video extension and matches `stem + "." + ext` or `stem + "." + lang + "." + ext` (normalizing `zh`, `chs`, `cht`, `zh-cn`, `en`, `ja`, etc.).
- `internal/scanner/scanner.go`: supports video extensions `.mp4 .mkv .webm .mov .m4v .avi .flv .ts .wmv .rmvb`; concurrent bounded probe worker pool (4 workers); atomic per-source lock preventing overlapping scans; updates `storage_sources.last_scanned_at`.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v ./internal/storage/... ./internal/scanner/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/storage internal/scanner
git commit -m "feat: add pluggable LocalFS storage provider and incremental media scanner"
```

---

### Task 3: Course Catalog, Natural-Sort Importer & Unified Progress Service

**Files:**
- Create: `internal/catalog/natsort.go`, `internal/catalog/service.go`, `internal/catalog/importer.go`, `internal/catalog/progress.go`
- Test: `internal/catalog/catalog_test.go`

**Interfaces:**
- Consumes: `db.Open`, `id.New`, `storage.Registry`
- Produces:
  - `catalog.NaturalCompare(a, b string) int`
  - `catalog.NewService(db *sql.DB, dataDir string) *catalog.Service`
  - `catalog.Service`:
    - Libraries: `ListLibraries(ctx)`, `CreateLibrary(ctx, name)`, `UpdateLibrary(ctx, id, name)`, `DeleteLibrary(ctx, id)`
    - Courses: `ListCourses(ctx, CourseFilter)`, `GetCourseDetail(ctx, courseID)`, `CreateCourse(ctx, CreateCourseInput)`, `UpdateCourse(ctx, courseID, UpdateCourseInput)`, `DeleteCourse(ctx, courseID)`, `SetCourseTags(ctx, courseID, tagNames []string)`, `ListTags(ctx)`
    - Chapters & Lessons: `CreateChapter(ctx, courseID, title)`, `UpdateChapter(ctx, chapterID, title, overview)`, `DeleteChapter(ctx, chapterID)`, `ReorderChapters(ctx, courseID, chapterIDs []string)`, `AddLesson(ctx, chapterID, mediaFileID, title)`, `UpdateLesson(ctx, lessonID, title, overview, chapterID)`, `DeleteLesson(ctx, lessonID)`, `ReorderLessons(ctx, courseID, placements []LessonPlacement)`
    - Storage Browser: `ListSources(ctx)`, `CreateSource(ctx, name, driver, rootPath)`, `DeleteSource(ctx, id)`, `ListIndexedDirs(ctx, sourceID, prefix)`, `ListMediaFiles(ctx, sourceID, dirPrefix, search, status)`
    - Importer: `PreviewDirectoryImport(ctx, sourceID, dirRelPath) (*ImportPreview, error)`, `CommitDirectoryImport(ctx, CommitImportInput) (*CourseDetail, error)`, `CheckNewFiles(ctx, courseID) ([]MediaFile, error)`, `AppendNewFiles(ctx, courseID, mediaFileIDs []string) error`
    - Progress: `ReportProgress(ctx, lessonID string, positionTicks int64) (*Progress, error)`, `SetPlayed(ctx, lessonID string, played bool) (*Progress, error)`, `GetResumeLessons(ctx, limit int) ([]LessonWithContext, error)`, `GetNextUpLessons(ctx, limit int) ([]LessonWithContext, error)`, `GetLatestCourses(ctx, libraryID string, limit int) ([]CourseSummary, error)`

- [ ] **Step 1: Write failing tests for natural sort, decoupled CRUD, directory import, incremental sync, and progress**

Create `internal/catalog/catalog_test.go`:
- `TestNaturalCompareMixedChineseAndDecimal` *(Review Focus #3)*: asserts `第1讲` < `第2讲` < `第10讲`, `第一章` < `第二章` < `第十章`, `1.2 基础` < `1.10 进阶`, `01_intro` < `02_setup` < `10_end`.
- `TestCourseDecouplingAndMultiCourseReuse`: adds same `media_file` into two different courses, reorders chapters/lessons, deletes one course, verifies the other course and underlying `media_files` row remain untouched.
- `TestDirectoryImportPreviewDeepNestingAndIdempotentSync` *(Review Focus #3 & #2)*: seeds indexed files with root videos, 2 chapter folders, a 3rd-level nested folder `02_Core/Deep/01_sub.mp4`, and a `cover.jpg`;
  - Verifies `PreviewDirectoryImport` places root videos in Chapter 1, chapter subfolders in natural order, flattens `02_Core/Deep/01_sub.mp4` into chapter `02_Core` with `"Deep / 01_sub"` proposed title, and detects `cover.jpg`.
  - Commits import, user renames a lesson manually, adds a new indexed file under `02_Core`, verifies `CheckNewFiles` returns only the 1 new file, `AppendNewFiles` appends it without touching the user-renamed lesson, and a second `CheckNewFiles` returns empty.
- `TestProgressAutoCompleteAt90PercentAndNextUp`: reports progress at 50% (appears in `GetResumeLessons`), then at 91% (flips `played = true`, resets `position_ticks = 0`, disappears from Resume, causes Lesson 2 of same course to appear in `GetNextUpLessons`, and updates Course `UnplayedItemCount` & `PlayedPercentage`).

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/catalog/...`
Expected: FAIL

- [ ] **Step 3: Implement `natsort.go`, `service.go`, `importer.go`, and `progress.go`**

- `natsort.go`: tokenizes runs of ASCII digits and Chinese numerals (`零一二三四五六七八九十百千`) into comparable integer tokens alongside lowercase text tokens.
- `service.go`: enforces every new course gets a default chapter `"正片"` (`sort_order = 1`) if created empty; `ReorderLessons` accepts `(lesson_id, chapter_id, sort_order)` in a single transaction so drag-and-drop across chapters is atomic.
- `importer.go`: copies detected local cover image (`cover.*`/`poster.*`/`folder.*`/`thumb.*`) into `/data/covers/{courseID}.{ext}` when committing (read-only on media volume); sets `source_id` & `source_root_path` for future incremental sync.
- `progress.go`: stores `position_ticks` (`ms * 10000`); when `duration_ms > 0` and `float64(positionTicks) / float64(durationMs*10000) >= 0.90`, marks `played = true`, increments `play_count`, and resets `position_ticks = 0`.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v ./internal/catalog/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/catalog
git commit -m "feat: add course catalog, natural-sort directory importer, and progress sync"
```

---

### Task 4: Media Streaming Engine (DirectPlay, Seekable VOD HLS Remux, Subtitles & Image Cache)

**Files:**
- Create: `internal/media/streamer.go`, `internal/media/remux.go`, `internal/media/subtitle.go`, `internal/media/image.go`
- Test: `internal/media/media_test.go`

**Interfaces:**
- Consumes: `storage.Registry`, `catalog.Service` (or DB queries for lesson/media_file/subtitle/cover resolution)
- Produces:
  - `media.NewService(db *sql.DB, dataDir string, registry *storage.Registry, ffmpegBin string) *media.Service`
  - `media.Service`:
    - `ServeDirectPlay(w http.ResponseWriter, r *http.Request, mediaFileID string)` (checks `DirectURL` for 302 redirect; else `http.ServeContent` with `Accept-Ranges: bytes`, `ETag`, `Content-Type`)
    - `CanRemuxVideoCodec(codec string) bool` (`h264`, `hevc`, `av1`, `vp9`; returns `false` for `wmv3`, `rv40`, `mpeg2video`, etc.)
    - `NeedsAudioTranscode(codec string) bool` (`false` for `aac`, `mp3`, `opus`; `true` for `ac3`, `eac3`, `dts`, `flac`, `pcm_*`)
    - `ServeMasterPlaylist(w, r, lessonID, sessionID)` & `ServeMediaPlaylist(w, r, lessonID, sessionID)` (synthetic fixed-6s VOD `#EXTM3U` with `#EXT-X-ENDLIST` covering full `duration_ms`)
    - `ServeHLSSegment(w, r, lessonID, sessionID string, segmentIndex int)` (starts/reuses FFmpeg at `-ss <segmentIndex*6>` with `-c:v copy` and `-c:a copy|aac`, waits for target `.ts` segment, serves it)
    - `StopRemuxSession(playSessionID string)` & `Close()` (kills active FFmpeg children + cleans `/data/cache/remux`)
    - `ServeSubtitle(w, r, mediaFileID string, streamIndex int, targetFormat string)` (serves sidecar or extracts embedded subtitle via `ffmpeg`, converts `srt` → `vtt` on the fly)
    - `ServeImage(w, r, itemID, imageType string, maxWidth, maxHeight int)` (resolves Library/Course/Chapter/Lesson image with Lesson→thumbnail & Course→first-lesson fallback; resizes & caches under `/data/cache/images`)

- [ ] **Step 1: Write failing tests for Range streaming, VOD HLS late-segment seeking, SRT→VTT conversion, and image caching**

Create `internal/media/media_test.go`:
- `TestServeDirectPlaySupportsRangeAndHead`: creates a temp MP4 file, sends `HEAD` and `GET` with `Range: bytes=100-199`, asserts `206 Partial Content`, `Content-Range: bytes 100-199/...`, `Content-Length: 100`, exact byte slice, and `Accept-Ranges: bytes`.
- `TestVODHLSPlaylistAndLateSegmentSeek` *(Review Focus #4)*: generates a 20-second H.264/AAC `.mkv` via `ffmpeg -f lavfi`, requests `main.m3u8` (asserts `#EXT-X-PLAYLIST-TYPE:VOD`, `#EXT-X-ENDLIST`, 4 segment entries `0.ts`..`3.ts`), then directly requests segment `2` (`t=12s`) *without* requesting `0` or `1`, asserts `200 OK`, valid MPEG-TS sync byte `0x47`, and that segments `0.ts` and `1.ts` were NOT generated on disk! Calls `StopRemuxSession` and asserts session directory is cleaned up.
- `TestCanRemuxVideoCodecRejectsLegacyCodecs` *(Review Focus #4)*: asserts `CanRemuxVideoCodec("h264") == true`, `CanRemuxVideoCodec("wmv3") == false`, `CanRemuxVideoCodec("rv40") == false`.
- `TestSRTToVTTConversion`: converts multi-cue Chinese/English `.srt` with comma timestamps (`00:00:01,500 --> 00:00:04,200`) to valid `WEBVTT` with dot timestamps (`00:00:01.500 --> 00:00:04.200`).
- `TestServeImageFallbackAndResizeCache`: verifies Course with no custom cover falls back to first lesson's thumbnail, resizes when `maxWidth=200`, writes cached JPEG to `/data/cache/images`, and returns `304 Not Modified` on matching `If-None-Match` ETag.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/media/...`
Expected: FAIL

- [ ] **Step 3: Implement `streamer.go`, `remux.go`, `subtitle.go`, and `image.go`**

- `remux.go`:
  - Fixed segment duration `6` seconds (`SegmentDurationSec = 6`).
  - Synthesizes full VOD `.m3u8` deterministically from `media_files.duration_ms` so `hls.js` and Jellyfin clients know exact duration and allow instant scrubbing anywhere.
  - Each `remuxSession` tracks active `exec.Cmd` + `startSegment` + `lastAccess` under a mutex. When `segmentIndex` is requested:
    1. If `{sessionDir}/{segmentIndex}.ts` already finalized on disk, serve immediately.
    2. If an active FFmpeg process is currently producing segments within `[currentProd, currentProd+3]` ahead of `segmentIndex`, wait briefly for `{segmentIndex}.ts` to appear & stabilize (next segment started or process exited).
    3. Otherwise (seek jump!), kill previous FFmpeg for this session and launch `ffmpeg -ss <startSec> -i <path> -map 0:v:0 -map 0:a:0? -c:v copy -c:a <copy|aac> -f hls -hls_time 6 -hls_start_number <segmentIndex> -hls_segment_filename <dir>/%d.ts -hls_playlist_type event -output_ts_offset <startSec> <dir>/live.m3u8`.
  - Background reaper cleans sessions idle `> 5m`.
- `subtitle.go`: supports external sidecar subtitles (indexed after embedded streams using Jellyfin's stream index numbering) and embedded text subtitles (`ffmpeg -i <path> -map 0:<idx> -f webvtt pipe:1`).
- `image.go`: uses Go stdlib `image/jpeg`, `image/png` + `golang.org/x/image/draw` (`BiLinear`/`CatmullRom`) + `golang.org/x/image/webp` decode.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v ./internal/media/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/media
git commit -m "feat: add DirectPlay streamer, seekable VOD HLS remuxer, subtitles, and image cache"
```

---

### Task 5: Jellyfin API Compatibility Layer

**Files:**
- Create: `internal/jellyfin/dto.go`, `internal/jellyfin/profile.go`, `internal/jellyfin/handlers_system.go`, `internal/jellyfin/handlers_items.go`, `internal/jellyfin/handlers_media.go`, `internal/jellyfin/router.go`
- Test: `internal/jellyfin/jellyfin_test.go`

**Interfaces:**
- Consumes: `auth.Service`, `catalog.Service`, `media.Service`, `db` (`server_id`, `server_name`)
- Produces:
  - `jellyfin.NewHandler(db *sql.DB, authSvc *auth.Service, catSvc *catalog.Service, mediaSvc *media.Service) http.Handler`
  - `jellyfin.EvaluatePlaybackInfo(lesson, mediaFile, subtitles, req PlaybackInfoRequest, token string) PlaybackInfoResponse`

- [ ] **Step 1: Write failing contract tests covering Infuse/VidHub + official Jellyfin client flows**

Create `internal/jellyfin/jellyfin_test.go`:
- `TestJellyfinAuthSystemAndCaseInsensitiveEmbyPrefix` *(Review Focus #5)*:
  - `GET /System/Info/Public`, `GET /emby/system/info/public`, `GET /System/Ping`, `GET /QuickConnect/Enabled` (`false`), `GET /Users/Public` return expected Jellyfin PascalCase JSON.
  - `POST /Users/AuthenticateByName` with `{"Username":"admin","Pw":"secret"}` returns `AccessToken`, `ServerId`, `User.Id` (32-char hex), `SessionInfo`.
  - Verifies authenticated calls succeed with `Authorization: MediaBrowser Token="..."`, `Authorization: Emby Token="..."`, `X-Emby-Token`, and `?api_key=...`, and `GET /socket` accepts WebSocket upgrade and stays open cleanly.
- `TestJellyfinCatalogMappingViewsSeriesSeasonsEpisodesTags`:
  - Seeds 1 library, 2 courses (with tags `["Go", "后端"]`), chapters, lessons.
  - `GET /Users/{uid}/Views` returns `CollectionFolder` with `CollectionType: "tvshows"`.
  - `GET /Users/{uid}/Items?ParentId={libId}&IncludeItemTypes=Series&Tags=Go` returns matching `Series` with both `Tags` and `Genres` populated.
  - `GET /Shows/{courseId}/Seasons` returns `Season` items in `sort_order`; `GET /Shows/{courseId}/Episodes` returns `Episode` items with `SeriesId`, `SeasonId`, `ParentIndexNumber`, `IndexNumber`, `RunTimeTicks`.
  - `GET /Genres`, `GET /Tags`, `GET /Search/Hints`, `GET /Items/Counts`, `GET /DisplayPreferences/usersettings`, `GET /Users/{uid}/Items/{id}/Intros` all return `200 OK` valid Jellyfin JSON.
- `TestDeviceProfileNegotiationDirectPlayVsRemux`:
  - Infuse-like request (empty `DeviceProfile` or `DirectPlayProfiles` including `mkv`/`h264`) → returns `SupportsDirectPlay: true`, `DirectStreamUrl` pointing to `/Videos/{id}/stream.mkv?...`.
  - Browser-like `DeviceProfile` (`DirectPlayProfiles` only `mp4`+`h264`+`aac`, `TranscodingProfiles` `hls`) on an `.mkv` (h264/ac3) → returns `SupportsDirectPlay: false`, `SupportsTranscoding: true`, `TranscodingUrl` pointing to `/Videos/{id}/master.m3u8?...` and external subtitles in `MediaStreams` with `DeliveryMethod: "External"` and `DeliveryUrl`.
- `TestPlaybackProgressSyncAndResumeNextUp`:
  - `POST /Sessions/Playing/Progress` with `PositionTicks` updates DB; `GET /Users/{uid}/Items/Resume` returns the lesson; `POST /Users/{uid}/PlayedItems/{id}` marks played; `GET /Shows/NextUp` advances to next lesson.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/jellyfin/...`
Expected: FAIL

- [ ] **Step 3: Implement Jellyfin DTOs, `DeviceProfile` negotiator, handlers, and router**

- `router.go`: wraps chi router with middleware that strips optional `/emby` prefix and lowercases route path segments (preserving query params and ID casing) so all Jellyfin path-case variants resolve identically without duplicating route tables.
- `dto.go`: exact Jellyfin 10.8/10.9+ PascalCase JSON fields (`Id`, `Name`, `ServerId`, `Type`, `CollectionType`, `MediaType`, `SeriesId`, `SeriesName`, `SeasonId`, `SeasonName`, `IndexNumber`, `ParentIndexNumber`, `RunTimeTicks`, `Tags`, `Genres`, `ImageTags`, `BackdropImageTags`, `UserData`, `MediaSources`, `MediaStreams`, `PlaySessionId`).
- `handlers_system.go`: implements `/System/*`, `/Branding/Configuration`, `/QuickConnect/Enabled`, `/DisplayPreferences/{id}` (persisted in `app_settings` keyed by `display_pref:{id}`), `/Items/Counts`, `/Users/*`, `/Sessions/*`, and `/socket` WebSocket handler via `github.com/coder/websocket` (reads/ignores client keepalives, responds cleanly to `KeepAlive` messages).
- `handlers_items.go` & `handlers_media.go`: wires every catalog, image, playback, stream, HLS, subtitle, and progress endpoint to `catalog.Service` and `media.Service`.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v ./internal/jellyfin/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/jellyfin
git commit -m "feat: add Jellyfin API compatibility layer with DeviceProfile negotiation"
```

---

### Task 6: Pluggable AI Metadata Organization (`OpenAICompatibleProvider`)

**Files:**
- Create: `internal/ai/provider.go`, `internal/ai/openai.go`
- Test: `internal/ai/openai_test.go`

**Interfaces:**
- Consumes: `db` (`app_settings` for AI config)
- Produces:
  - `ai.Config` (`BaseURL`, `APIKey`, `Model`, `MaxSubtitleChars`)
  - `ai.Provider` interface: `TestConnection(ctx) error`, `OrganizeCourse(ctx, CourseOrganizeInput) (*CourseOrganizeProposal, error)`
  - `ai.NewOpenAICompatibleProvider(cfg ai.Config, httpClient *http.Client) *ai.OpenAICompatibleProvider`
  - `ai.ValidateProposal(input CourseOrganizeInput, prop *CourseOrganizeProposal) error`

- [ ] **Step 1: Write failing tests for OpenAI-compatible client, fence stripping, and strict `lesson_id` validation**

Create `internal/ai/openai_test.go`:
- `TestOrganizeCourseParsesValidJSONAndMarkdownFence` *(Review Focus #5)*: spins up `httptest.Server` mimicking `/chat/completions`, verifies Bearer auth header, model & messages payload, parses both raw JSON and ```` ```json ... ``` ```` fenced responses into `CourseOrganizeProposal` (`Title`, `Overview`, `Tags`, `Chapters[].Lessons[]`).
- `TestValidateProposalRejectsMissingOrUnknownLessonID` *(Review Focus #5)*: passes a proposal missing one existing `lesson_id`, another with a fabricated `lesson_id`, and another duplicating a `lesson_id`; asserts `ValidateProposal` returns descriptive errors for all three.
- `TestTestConnectionReportsUpstreamErrors`: verifies `200` passes and `401`/`500` returns clear error with status code.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/ai/...`
Expected: FAIL

- [ ] **Step 3: Implement `provider.go` and `openai.go`**

- `openai.go`: normalizes `BaseURL` (trims trailing `/`, appends `/chat/completions` if not already ending with it); sends system prompt instructing strict JSON output matching `CourseOrganizeProposal` preserving every `lesson_id` verbatim; extracts JSON cleanly even if wrapped in code fences; truncates optional subtitle excerpts per lesson to respect `MaxSubtitleChars` (default `4000` total, `200` per lesson).
- `ValidateProposal`: ensures every input `lesson_id` appears exactly once across proposed chapters, no unknown IDs exist, `title` is non-empty, and `chapters` is non-empty.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v ./internal/ai/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ai
git commit -m "feat: add OpenAI-compatible AI provider and proposal validator"
```

---

### Task 7: CourseFin Management REST API (`/api/v1`) & Unified Server Assembly

**Files:**
- Create: `internal/api/handlers_auth.go`, `internal/api/handlers_storage.go`, `internal/api/handlers_catalog.go`, `internal/api/handlers_ai.go`, `internal/api/router.go`, `internal/server/embed.go`, `internal/server/server.go`, `cmd/coursefin/main.go`
- Create: `internal/server/webdist/index.html` (minimal placeholder so `go test` passes before frontend build)
- Test: `internal/api/api_test.go`, `internal/server/server_test.go`

**Interfaces:**
- Consumes: `config.Load`, `db.Open`, `auth.Service`, `storage.Registry`, `scanner.Service`, `catalog.Service`, `media.Service`, `ai.NewOpenAICompatibleProvider`, `jellyfin.NewHandler`
- Produces:
  - `/api/v1` REST API:
    - Auth/Setup: `GET /api/v1/setup/status`, `POST /api/v1/setup`, `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`, `POST /api/v1/auth/password`, `GET /api/v1/sessions`, `DELETE /api/v1/sessions/{token}`
    - Storage & Scan: `GET|POST /api/v1/sources`, `DELETE /api/v1/sources/{id}`, `POST /api/v1/sources/{id}/scan`, `GET /api/v1/sources/{id}/scan/status`, `GET /api/v1/sources/{id}/dirs`, `GET /api/v1/sources/{id}/files`, `POST /api/v1/import/preview`, `POST /api/v1/import/commit`
    - Catalog & Playback: `GET|POST /api/v1/libraries`, `PUT|DELETE /api/v1/libraries/{id}`, `GET|POST /api/v1/courses`, `GET|PUT|DELETE /api/v1/courses/{id}`, `POST /api/v1/courses/{id}/cover` (multipart upload or `{lesson_id}` frame pick), `POST /api/v1/courses/{id}/backdrop`, `GET /api/v1/courses/{id}/new-files`, `POST /api/v1/courses/{id}/new-files`, `POST /api/v1/courses/{id}/chapters`, `PUT|DELETE /api/v1/chapters/{id}`, `PUT /api/v1/courses/{id}/chapters/reorder`, `POST /api/v1/chapters/{id}/lessons`, `PUT|DELETE /api/v1/lessons/{id}`, `PUT /api/v1/courses/{id}/lessons/reorder`, `GET /api/v1/lessons/{id}/playback` (returns `mode: "direct"|"hls"|"unsupported"`, `stream_url`, `subtitles`, `progress`, `video_codec`, `audio_codec`), `POST /api/v1/lessons/{id}/progress`, `POST /api/v1/lessons/{id}/played`, `GET /api/v1/tags`, `GET /api/v1/home` (resume + libraries + courses)
    - Settings & AI: `GET|PUT /api/v1/settings` (masks `api_key` on GET, preserves existing `api_key` on PUT if blank/masked), `POST /api/v1/settings/ai/test`, `POST /api/v1/courses/{id}/ai/organize` (returns preview proposal only, zero DB writes), `POST /api/v1/courses/{id}/ai/apply` (applies user-confirmed fields/chapters/lessons in a transaction)
  - `server.New(cfg config.Config) (*server.Server, error)` with `Handler() http.Handler` and `Close() error`

- [ ] **Step 1: Write failing API and routing integration tests**

Create `internal/api/api_test.go` and `internal/server/server_test.go`:
- `TestAPISetupLoginAndCourseLifecycle`: full HTTP flow through `/api/v1/setup` → `/api/v1/auth/login` → create source → scan → `/api/v1/import/preview` → `/api/v1/import/commit` → update course/tags → reorder lessons → report progress → verify `/api/v1/home` reflects resume & progress.
- `TestAIOrganizeDoesNotMutateUntilApply`: calls `/api/v1/courses/{id}/ai/organize` against mocked OpenAI server, asserts DB course title/chapters are UNCHANGED; then calls `/api/v1/courses/{id}/ai/apply` with selective toggles (`apply_title: true, apply_chapters: false`), asserts only title updated.
- `TestLessonPlaybackEndpointReportsDirectHLSAndUnsupported`: asserts `.mp4` (h264/aac) → `mode: "direct"`, `.mkv` (h264/ac3) → `mode: "hls"`, `.wmv` (wmv3/wmav2) → `mode: "unsupported"`.
- `TestServerRoutingNoCollisionBetweenWebAPIAndJellyfin`: verifies `GET /` with `Accept: text/html` redirects `302` to `/web/`, `GET /web/` serves SPA HTML, `GET /api/v1/setup/status` hits CourseFin API, `GET /System/Info/Public` and `GET /emby/System/Info/Public` hit Jellyfin handler, and `GET /health` returns `200 OK`.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/api/... ./internal/server/...`
Expected: FAIL

- [ ] **Step 3: Implement `/api/v1` handlers, `server`, `embed.go`, and `cmd/coursefin/main.go`**

- `/api/v1` auth middleware validates `Authorization: Bearer <token>` (or `X-Emby-Token`) via `auth.Service.Authenticate`.
- `POST /api/v1/courses/{id}/ai/organize` reads optional sidecar subtitle excerpts from `StorageProvider.Open` (capped per config) and calls `ai.OpenAICompatibleProvider.OrganizeCourse` + `ai.ValidateProposal`.
- `internal/server/server.go`: mounts `/health`, `/api/v1/*`, `/web/*` (serving embedded SPA static assets with fallback to `index.html` for client-side routes), browser `GET /` → `302 /web/`, and routes all other paths to `jellyfin.NewHandler`.
- `cmd/coursefin/main.go`: loads config, ensures env admin if configured, starts HTTP server on `:{COURSEFIN_PORT}`, listens for `SIGINT`/`SIGTERM`, gracefully shuts down HTTP + remux FFmpeg processes + SQLite.

- [ ] **Step 4: Run all backend tests to verify pass**

Run: `go test -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd internal/api internal/server
git commit -m "feat: add CourseFin /api/v1 endpoints, unified server router, and entrypoint"
```

---

### Task 8: Web SPA Foundation, Setup/Login & Course Library Home

**Files:**
- Create: `web/package.json`, `web/tsconfig.json`, `web/vite.config.ts`, `web/index.html`, `web/src/main.tsx`, `web/src/index.css`, `web/src/App.tsx`, `web/src/types/coursefin.ts`, `web/src/api/client.ts`, `web/src/components/Layout.tsx`, `web/src/pages/SetupLoginPage.tsx`, `web/src/pages/HomePage.tsx`
- Test: `web/src/__tests__/HomePage.test.tsx`

**Interfaces:**
- Consumes: `/api/v1/setup/status`, `/api/v1/setup`, `/api/v1/auth/login`, `/api/v1/home`, `/api/v1/libraries`, `/api/v1/tags`, `/Items/{id}/Images/Primary`
- Produces:
  - Typed `api` client in `web/src/api/client.ts` (persists session token in `localStorage`, attaches `Authorization: Bearer <token>`, redirects to `/web/login` on `401`)
  - Shared `Layout` navigation shell (`课程库`, `存储与导入`, `设置`) and responsive Chinese-first UI theme
  - `SetupLoginPage` (auto-detects `needs_setup` and switches between first-run admin creation wizard and login form)
  - `HomePage` (Continue Watching rail, library tabs + "全部" + create library button, tag filter chips, instant search box, course card grid with cover, tags, progress bar, quick link to play/edit)

- [ ] **Step 1: Scaffold `web/` with Vite + React + TypeScript + Tailwind CSS + Vitest**

Configure `vite.config.ts` with `base: '/web/'`, dev proxy for `/api`, `/Items`, `/Videos`, `/System` → `http://localhost:8096`, and Vitest + jsdom + `@testing-library/react`.

- [ ] **Step 2: Write failing frontend tests for Setup/Login and Course Home filtering**

Create `web/src/__tests__/HomePage.test.tsx`:
- `renders continue watching rail and filters courses by library, tag, and search query`
- `shows first-run setup form when setup status reports needs_setup=true`

- [ ] **Step 3: Run tests to verify failure**

Run: `pnpm --dir web test -- --run`
Expected: FAIL

- [ ] **Step 4: Implement `api/client.ts`, `types/coursefin.ts`, `Layout.tsx`, `SetupLoginPage.tsx`, `HomePage.tsx`, and `App.tsx`**

Build clean, modern dark/light-balanced Chinese UI using Tailwind CSS with crisp course poster cards, progress indicators, and clear empty states guiding first-time users to "存储与导入".

- [ ] **Step 5: Run tests and production build to verify pass**

Run: `pnpm --dir web test -- --run && pnpm --dir web build`
Expected: PASS and `web/dist/index.html` generated with `/web/` asset prefix.

- [ ] **Step 6: Commit**

```bash
git add web
git commit -m "feat(web): scaffold SPA foundation, setup/login, and course library home"
```

---

### Task 9: Web Course Detail & Built-in Video Player (HTML5 + HLS.js + Progress Sync)

**Files:**
- Create: `web/src/components/VideoPlayer.tsx`, `web/src/pages/CourseDetailPage.tsx`
- Modify: `web/src/App.tsx` (wire `/courses/:courseId` route)
- Test: `web/src/__tests__/VideoPlayer.test.tsx`

**Interfaces:**
- Consumes: `GET /api/v1/courses/{id}`, `GET /api/v1/lessons/{id}/playback`, `POST /api/v1/lessons/{id}/progress`, `POST /api/v1/lessons/{id}/played`, `/Videos/{id}/stream`, `/Videos/{id}/master.m3u8`, `/Videos/{id}/{sourceId}/Subtitles/{idx}/Stream.vtt`
- Produces:
  - `VideoPlayer` component supporting:
    - DirectPlay `<video src=...>` and HLS remux via `hls.js` (with native HLS fallback on Safari)
    - Resume at saved `position_ticks` on metadata loaded
    - Playback rate switcher (`0.75x`, `1.0x`, `1.25x`, `1.5x`, `1.75x`, `2.0x`, persisted in `localStorage`)
    - External/embedded WebVTT subtitle `<track>` switcher + off option
    - Keyboard shortcuts (`Space` play/pause, `ArrowLeft`/`ArrowRight` ±10s, `F` fullscreen)
    - Progress sync every 10s while playing, on pause, and on `ended` (auto-advancing to next lesson)
    - Unsupported-codec guidance banner when `mode === "unsupported"` recommending Infuse / VidHub / SenPlayer *(Review Focus #4)*

- [ ] **Step 1: Write failing tests for `VideoPlayer`**

Create `web/src/__tests__/VideoPlayer.test.tsx`:
- `shows native Jellyfin player recommendation banner when playback mode is unsupported` *(Review Focus #4)*
- `changes playbackRate when user selects 1.5x speed and renders subtitle selector options`
- `reports playback progress ticks to API on pause and ended`

- [ ] **Step 2: Run tests to verify failure**

Run: `pnpm --dir web test -- --run src/__tests__/VideoPlayer.test.tsx`
Expected: FAIL

- [ ] **Step 3: Implement `VideoPlayer.tsx` and `CourseDetailPage.tsx`**

`CourseDetailPage` displays course header (cover, title, tags, overview, progress, quick "编辑课程" link), main `VideoPlayer`, and collapsible chapter/lesson playlist sidebar highlighting the current lesson and played checkmarks (plus manual "标记已看/未看" toggle).

- [ ] **Step 4: Run tests and build to verify pass**

Run: `pnpm --dir web test -- --run && pnpm --dir web build`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/components/VideoPlayer.tsx web/src/pages/CourseDetailPage.tsx web/src/App.tsx web/src/__tests__/VideoPlayer.test.tsx
git commit -m "feat(web): add course detail page and built-in HTML5/HLS video player"
```

---

### Task 10: Course Editor, Storage/Importer, AI Organize Modal & Settings Pages

**Files:**
- Create: `web/src/components/ImportCourseModal.tsx`, `web/src/components/AIOrganizeModal.tsx`, `web/src/pages/CourseEditorPage.tsx`, `web/src/pages/StoragePage.tsx`, `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/App.tsx` (wire `/courses/:courseId/edit`, `/storage`, `/settings`)
- Test: `web/src/__tests__/EditorAndAI.test.tsx`

**Interfaces:**
- Consumes: All remaining `/api/v1/*` endpoints (sources, scan, dirs/files, import preview/commit, course/chapter/lesson CRUD + reorder, covers, new-files sync, AI organize/apply, settings & sessions)
- Produces:
  - `CourseEditorPage`: metadata form (title, overview, instructor, library, tags chip input, cover upload or "从课时帧选取封面"), chapter & lesson drag-and-drop organizer (`@dnd-kit`), inline rename, "从存储添加视频" modal, "检查新文件" incremental sync button, subtitle viewer, and "AI 智能整理" trigger.
  - `AIOrganizeModal`: calls `/api/v1/courses/{id}/ai/organize`, shows side-by-side preview of suggested title/overview/tags/chapters/lesson titles with independent checkboxes so user confirms before `/apply` is ever called.
  - `StoragePage`: manage local directory paths, trigger scan with live status polling, view missing files badge, browse indexed folders and launch `ImportCourseModal` ("一键导入为课程" with editable chapter/lesson preview).
  - `SettingsPage`: Jellyfin connection guide card (copyable server URL, username, Infuse/VidHub/SenPlayer setup hints), AI Provider config form (Base URL, API Key, Model, "测试连接"), password change form, and active device sessions table with revoke buttons.

- [ ] **Step 1: Write failing tests for AI Organize preview safety & Directory Import modal**

Create `web/src/__tests__/EditorAndAI.test.tsx`:
- `AIOrganizeModal previews proposal and only calls apply endpoint with user-selected checkboxes on confirm`
- `ImportCourseModal displays inferred chapters/lessons and submits edited course title on confirm`

- [ ] **Step 2: Run tests to verify failure**

Run: `pnpm --dir web test -- --run src/__tests__/EditorAndAI.test.tsx`
Expected: FAIL

- [ ] **Step 3: Implement `ImportCourseModal.tsx`, `AIOrganizeModal.tsx`, `CourseEditorPage.tsx`, `StoragePage.tsx`, `SettingsPage.tsx`, and wire routes**

- [ ] **Step 4: Run all frontend tests and production build**

Run: `pnpm --dir web test -- --run && pnpm --dir web build`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web
git commit -m "feat(web): add course editor, storage importer, AI organize modal, and settings"
```

---

### Task 11: Docker Multi-Stage Build, Embedded SPA Sync & End-to-End Verification

**Files:**
- Create: `Dockerfile`, `docker-compose.yml`, `Makefile` (convenience `build-web`, `build`, `test`, `docker-build`)
- Modify: `internal/server/embed.go` (copy/embed built `web/dist` during build)
- Test: `internal/server/e2e_test.go`

**Interfaces:**
- Consumes: All backend packages + `web/dist`
- Produces:
  - Production multi-stage `Dockerfile`:
    1. `node:22-alpine` + `pnpm`: installs deps and runs `pnpm build` → `/web/dist`
    2. `golang:1.24-bookworm`: copies `/web/dist` into `internal/server/webdist`, builds static-friendly `CGO_ENABLED=0 go build -o /out/coursefin ./cmd/coursefin`
    3. `debian:bookworm-slim`: installs `ffmpeg`, `ca-certificates`, `tzdata`; copies `/out/coursefin`; creates `/data` & `/media`; exposes `8096`; healthcheck on `/System/Ping`
  - Ready-to-run `docker-compose.yml` mounting `./data:/data` and `./media:/media:ro` on port `8096:8096`

- [ ] **Step 1: Write end-to-end integration test (`internal/server/e2e_test.go`)**

Creates a real temp media directory containing generated sample `.mp4` and `.mkv` course videos (via `ffmpeg -f lavfi`) + `.srt` subtitle + `cover.jpg`, boots full `server.New(cfg)` via `httptest.Server`, and exercises the complete user journey:
1. Setup admin via `/api/v1/setup` → login.
2. Add local source → trigger scan → import folder into a course.
3. Connect as a Jellyfin client (`POST /Users/AuthenticateByName`, `GET /Users/{uid}/Views`, `GET /Users/{uid}/Items`, `GET /Shows/{id}/Seasons`, `GET /Shows/{id}/Episodes`, `GET /Items/{id}/Images/Primary`).
4. Stream `.mp4` via DirectPlay with `Range` header (`206`), stream `.mkv` via HLS VOD remux (`main.m3u8` + segment `.ts`), fetch `.vtt` converted subtitle.
5. Report progress via Jellyfin `POST /Sessions/Playing/Progress` and assert CourseFin `GET /api/v1/home` immediately shows identical resume position.

- [ ] **Step 2: Create `Dockerfile`, `docker-compose.yml`, and `Makefile`**

- [ ] **Step 3: Run full test suite (Go + Web) and Docker build smoke check**

Run:
1. `pnpm --dir web test -- --run && pnpm --dir web build`
2. Sync `web/dist` → `internal/server/webdist` and run `go test -v ./...`
3. `docker build -t coursefin:local .`
4. Run a quick container smoke check (`docker run` with temp `/data`, verify `GET http://localhost:8096/System/Ping` and `GET http://localhost:8096/web/` return `200 OK`, then stop/remove container).
Expected: All unit/integration/E2E tests PASS, Docker image builds cleanly, container healthcheck passes.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile docker-compose.yml Makefile internal/server/e2e_test.go
git commit -m "chore: add multi-stage Dockerfile, compose config, and E2E verification"
```
