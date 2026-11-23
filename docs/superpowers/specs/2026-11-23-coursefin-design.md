# CourseFin Design Document

- **Date:** 2026-11-23
- **Status:** Ready for user approval (builder check completed)
- **Repository:** `CourseFin`

---

## 1. Intent & Why

Existing self-hosted media servers like Jellyfin and Emby are built around movies and TV shows. Their indexers assume strict folder hierarchies, naming conventions (`S01E01`), and online scrapers (TMDB/TVDB). Organizing personal video courses—which arrive with inconsistent folder trees, mixed Chinese/English filenames, arbitrary numbering, and no online metadata database—is frustrating and brittle.

**CourseFin** is a Docker-deployable, self-hosted course management platform that:
1. **Decouples physical video storage from logical course organization**, letting the user freely build custom course albums, chapters, lesson order, titles, covers, and tags without touching files on disk.
2. **Exposes a Jellyfin-compatible server API**, so existing Jellyfin clients (especially Infuse, VidHub, SenPlayer, and standard Jellyfin clients) connect directly and browse/play courses with full progress sync.
3. **Provides its own clean Web UI** for course management and browser playback.
4. **Integrates pluggable custom AI endpoints** (OpenAI-compatible) to assist with organizing messy course metadata, while leaving a clean foundation for future storage drivers and AI workflows.

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals (v1)
1. **Effortless Docker deployment:** Single lightweight Docker container (`amd64`/`arm64`) with embedded Web UI, SQLite single-file database, and bundled `ffmpeg`/`ffprobe`. Zero external database or sidecar services required.
2. **Local storage indexing with pluggable driver architecture:** Index local/NAS directories mounted into Docker, backed by an explicit `StorageProvider` abstraction so WebDAV, Alist/OpenList (including 302 direct-link redirects), S3, and cloud drives can be added post-v1 without rewriting core logic.
3. **Decoupled course management:**
   - One-click directory import (infers course, chapters, natural-sorted lessons, local covers, and sidecar subtitles, with preview before creation) plus incremental "check for new files" without overwriting user edits.
   - Full manual editing: assemble lessons from any indexed directories, create/rename/reorder chapters and lessons, customize titles/overviews/covers/backdrops/tags, and link sidecar subtitles.
4. **High-fidelity Jellyfin API compatibility:**
   - Map `Library → Course → Chapter → Lesson` to Jellyfin `CollectionFolder (tvshows) → Series → Season → Episode`, with Course tags exposed for filtering.
   - Fully support third-party players (Infuse, VidHub, SenPlayer) and Jellyfin clients: auth & multi-device sessions, views, item queries/filtering/sorting/search, `Resume`, `NextUp`, `Latest`, images (with resizing), subtitles, `PlaybackInfo`, DirectPlay (HTTP Range), lightweight on-demand Remux, and bidirectional watch-progress sync.
5. **Built-in Web management & player UI:** Manage libraries, storage sources, courses, chapters, lessons, covers, tags, and settings; watch courses directly in the browser with playback speed controls, subtitle switching, and shared progress sync.
6. **Custom OpenAI-compatible AI metadata organization:** Configure custom Base URL, API Key, and Model; run AI-assisted organization on a course (analyzing directory layout, filenames, and optional subtitle snippets) to suggest title, description, tags, chapter grouping, and cleaned lesson titles—always previewed and user-confirmed before applying.

### Non-Goals (Out of v1)
- Remote storage or cloud-drive drivers (WebDAV, Alist/OpenList, S3, 115, Aliyun Drive, Quark, etc.) — only the `StorageProvider` interface is built in v1.
- Online video platform scraping or downloading (Bilibili, YouTube, etc.).
- Full video transcoding (re-encoding H.265/AV1/WMV/VP9 video bitstreams on CPU/GPU). Only DirectPlay and lightweight container remuxing (`-c:v copy`, with optional lightweight audio-only AAC fallback when needed for browser HLS) are in v1.
- Non-video course attachments (PDF handouts, PPT slides, Markdown notes, zip archives).
- Multi-user account management, RBAC, or per-user library permissions (v1 is single-admin mode).
- AI audio transcription (Whisper), vector embeddings, or course Q&A chat.

### Anti-Goals (Failure Modes to Avoid)
- **Never mutate disk media:** CourseFin must work seamlessly with read-only media mounts (`:ro`). Organizing, renaming, or deleting courses/lessons never moves, renames, or deletes source files.
- **Never impose rigid disk naming rules:** Messy folders must be importable and reorganizable freely.
- **Never silently clobber manual curation:** Re-scanning storage or running AI must never silently overwrite user-edited titles, covers, tags, or ordering.
- **Never split watch state:** Progress updated in Infuse/VidHub must appear immediately in CourseFin Web and vice versa.
- **Never require heavyweight deployment:** Idle memory and setup friction must remain NAS/VPS-friendly (comparable to Alist or Navidrome).

---

## 3. Constraints

- **Deployment footprint:** Single container, single writable `/data` volume (SQLite DB, cached/uploaded images, temporary remux cache), one or more read-only media mounts (e.g. `/media/...:ro`). Default port `8096` (Jellyfin convention, configurable via env var).
- **Single-user auth with Jellyfin protocol fidelity:** Although CourseFin v1 has only one admin account (no user management CRUD UI), it must still implement Jellyfin's standard authentication flow (`Users/AuthenticateByName`, `AccessToken`, multi-device sessions) so every Jellyfin client logs in and stays authenticated normally across multiple devices simultaneously.
- **Stable item identifiers:** All Libraries, Courses, Chapters, Lessons, and Tags must have persistent, deterministic 32-char lowercase hex IDs (standard Jellyfin format, UUID without hyphens). Clients like Infuse aggressively cache metadata by Item ID; IDs must never drift across restarts or re-scans.
- **HTTP Range correctness:** DirectPlay endpoints must support `HEAD`, `Range`, `206 Partial Content`, `Accept-Ranges: bytes`, `Content-Length`, and `ETag` cleanly for instant seeking in Infuse, VidHub, and HTML5 `<video>`.
- **Language:** Web UI is Chinese-first (matching the user's language), with clean text structure suitable for future i18n. *(Our call)*

---

## 4. Approaches Considered

| Approach | Summary | Trade-offs | Decision |
|---|---|---|---|
| **1. Go Backend + Web SPA (Single Container + SQLite)** | Go server implements storage indexing, course API, Jellyfin API layer, HTTP Range streaming, and FFmpeg remux; SPA built and embedded via `embed.FS`; single SQLite DB. | Two languages (Go + TS); we maintain the Jellyfin protocol surface ourselves. Lowest memory footprint (~30–50MB idle), single binary, rock-solid concurrent streaming & FFmpeg piping, strict typing for Jellyfin DTOs. | **Selected by user** |
| **2. Full-Stack TypeScript (Node.js/Fastify + Web SPA, Single Container + SQLite)** | pnpm monorepo with shared TS types across backend and frontend; Drizzle + SQLite. | Single language & easy TS contributions, but noticeably higher idle RAM (~150–250MB) and larger container image on low-spec NAS/VPS. | Rejected |
| **3. Official Jellyfin Base + CourseFin Sidecar (NFO/STRM)** | Official Jellyfin container handles playback/API; CourseFin generates shadow `.strm`/`.nfo` trees and proxies requests. | 100% Jellyfin API/transcode compatibility out of the box, but requires two heavy containers, suffers from Jellyfin library scan lag & NFO sync quirks, and blocks clean 302 cloud-drive redirects later. | Rejected |

---

## 5. Architecture Overview

```text
┌─────────────────────────────────────────────────────────────────────────┐
│                     CourseFin Single Docker Container                   │
│                                                                         │
│  ┌──────────────────────────┐       ┌────────────────────────────────┐  │
│  │  Jellyfin Compat Layer   │       │   CourseFin Admin & Web API    │  │
│  │  (/System, /Users,       │       │   (/api/v1/...) + Embedded SPA │  │
│  │   /Items, /Shows,        │       │   (React/TS Management +       │  │
│  │   /Videos, /Sessions...) │       │    Built-in Video Player)      │  │
│  └────────────┬─────────────┘       └───────────────┬────────────────┘  │
│               └──────────────────┬──────────────────┘                   │
│                                  ▼                                      │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │                      Core Domain Services                         │  │
│  │  Auth/Sessions │ Course/Catalog │ Scanner/Prober │ Progress Sync  │  │
│  │  Media Streamer & Remuxer (FFmpeg) │ Image Cache │ AI Organizer   │  │
│  └──────────┬────────────────────┬────────────────────┬──────────────┘  │
│             ▼                    ▼                    ▼                 │
│  ┌────────────────────┐ ┌─────────────────┐ ┌────────────────────────┐  │
│  │  SQLite (/data)    │ │ StorageProvider │ │      AIProvider        │  │
│  │  (WAL mode)        │ │  └─ LocalFS (v1)│ │  └─ OpenAI-Compat (v1) │  │
│  └────────────────────┘ │  └─ Future...   │ └────────────────────────┘  │
│                         └────────┬────────┘                             │
└──────────────────────────────────┼──────────────────────────────────────┘
                                   ▼
                        Read-only Media Volumes
                           (/media/courses)
```

---

## 6. Data Model & Domain Design

All entity IDs are stored and exposed as 32-character lowercase hex UUIDs (e.g., `a1b2c3d4e5f6...`), matching Jellyfin's canonical ID format.

### 6.1 Physical Index Layer
- **`storage_sources`**: `id`, `name`, `driver` (`"local"`), `root_path`, `config_json`, `enabled`, `last_scanned_at`, `created_at`, `updated_at`.
- **`media_files`**: `id`, `source_id`, `relative_path` (unique per `source_id`), `file_name`, `file_size`, `mod_time`, `status` (`"active"` | `"missing"`), `duration_ms`, `container`, `video_codec`, `audio_codec`, `width`, `height`, `bitrate`, `streams_json` (raw `ffprobe` stream summary), `thumbnail_path` (relative under `/data/thumbs`), `probed_at`, `created_at`, `updated_at`.
- **`subtitle_files`**: `id`, `source_id`, `relative_path` (unique per `source_id`), `media_file_id` (nullable FK to `media_files`), `language` (e.g. `zh`, `en`, `und`), `title`, `format` (`srt`, `vtt`, `ass`, `ssa`), `is_default`, `status` (`"active"` | `"missing"`).

### 6.2 Logical Course Layer
- **`libraries`**: `id`, `name`, `cover_path`, `sort_order`, `created_at`, `updated_at`.
  - A default library (e.g. `"我的课程"`) is seeded on first setup so users can import courses immediately. *(Our call)*
- **`courses`**: `id`, `library_id`, `title`, `sort_title`, `overview`, `instructor`, `cover_path` (Primary poster), `backdrop_path`, `source_id` (nullable, set when imported from a folder), `source_root_path` (nullable, relative path for incremental new-file sync), `created_at`, `updated_at`.
- **`chapters`**: `id`, `course_id`, `title`, `sort_order` (1-based integer, maps to Jellyfin `Season` `IndexNumber`), `overview`, `created_at`, `updated_at`.
  - Every course has at least one chapter (even flat courses get a default chapter `sort_order = 1`, e.g. `"正片"` or `"第 1 章"`), guaranteeing a consistent `Series → Season → Episode` tree across all Jellyfin clients. *(Our call)*
- **`lessons`**: `id`, `course_id`, `chapter_id`, `media_file_id` (FK to `media_files`), `title`, `overview`, `sort_order` (1-based integer within chapter, maps to Jellyfin `Episode` `IndexNumber`), `cover_path` (optional custom thumbnail; falls back to `media_files.thumbnail_path`), `created_at`, `updated_at`.
  - Deleting a lesson or course never deletes the underlying `media_files` row or disk file. Multiple lessons may reference the same `media_file_id` if a user includes a video in two custom courses.
- **`tags`** & **`course_tags`**: `tags(id, name UNIQUE)`, `course_tags(course_id, tag_id)`.

### 6.3 Playback State, Auth, and Settings
- **`playback_progress`**: `lesson_id` (PK, FK to `lessons`), `position_ticks` (int64, 1 tick = 100ns = `ms * 10000`), `played` (bool), `play_count` (int), `last_played_at`, `updated_at`.
  - Auto-mark `played = true` when `position_ticks >= 90%` of media duration (and reset `position_ticks = 0` on completion, matching Jellyfin conventions). *(Our call)*
  - Chapter and Course watch statistics (`Played`, `PlayedPercentage`, `UnplayedItemCount`) are computed from their child lessons.
- **`users`**: Single admin row (`id`, `username`, `password_hash` via bcrypt/argon2, `created_at`, `updated_at`).
- **`sessions`**: `token` (PK, secure random hex), `user_id`, `device_id`, `device_name`, `client_name`, `client_version`, `last_active_at`, `created_at`.
  - Re-authenticating with the same `(user_id, device_id)` replaces or refreshes that device's session without invalidating other devices.
- **`app_settings`**: Key-value store for persistent `server_id` (UUID), `server_name` (default `"CourseFin"`), scan schedule, and AI provider configuration (`base_url`, `api_key`, `model`, `max_subtitle_chars`).

---

## 7. Core Subsystems

### 7.1 Pluggable Storage & Media Scanner
- **`StorageProvider` interface** (Go):
  - `Walk(ctx, rootRelPath) (<-chan FileEntry, error)`
  - `Stat(ctx, relPath) (FileInfo, error)`
  - `Open(ctx, relPath) (io.ReadSeekCloser, error)`
  - `DirectURL(ctx, relPath) (url string, ok bool, err error)` — returns `("", false, nil)` for `LocalFS`; designed specifically so future Alist/OpenList/WebDAV/S3 drivers can return 302 redirect URLs for DirectPlay.
- **LocalFS driver (v1):** Enforces strict path traversal protection (`filepath.Clean` + verifying resolved path stays under `root_path`).
- **Supported extensions:**
  - Video: `.mp4`, `.mkv`, `.webm`, `.mov`, `.m4v`, `.avi`, `.flv`, `.ts`, `.wmv`, `.rmvb`.
  - Subtitle: `.srt`, `.vtt`, `.ass`, `.ssa`.
  - Cover image candidates during folder import: `cover.*`, `poster.*`, `folder.*`, `thumb.*` (`.jpg`, `.jpeg`, `.png`, `.webp`).
- **Scanning & Probing behavior:**
  - Incremental: skips unchanged files (`file_size` + `mod_time` unchanged and already probed).
  - Missing files: marked `status = "missing"` rather than deleted, so courses remain intact if a NAS mount temporarily disconnects; automatically restored to `"active"` when the file returns.
  - Background worker pool runs `ffprobe` (duration, container, video/audio/embedded subtitle streams, bitrate, resolution) and extracts a fallback video frame thumbnail via `ffmpeg` (at 10% duration, capped between 5s and 60s) saved under `/data/thumbs`. *(Our call)*
  - Auto-matches sidecar subtitles in the same directory whose filename starts with the video's basename (e.g. `01_intro.mp4` matches `01_intro.srt`, `01_intro.zh.srt`, `01_intro.en.vtt`), inferring language codes when present.

### 7.2 Course Import & Manual Curation
- **One-Click Directory Import:**
  1. User browses indexed folders under a `StorageSource` and selects a target folder as a course root.
  2. Preview endpoint returns proposed structure before writing:
     - Course title = folder name; auto-detected cover image copied to `/data/covers` if present.
     - Direct video files in root -> Chapter 1 (`"正片"` or `"第 1 章"`).
     - Immediate child subdirectories containing videos -> each becomes a Chapter named after the subdirectory. Deeper nested videos are grouped into their top-level chapter with subfolder prefix preserved in the proposed lesson title (editable in preview). *(Our call)*
     - Chapters and lessons sorted via natural numeric ordering (handles `1, 2, 10`, zero-padded `01`, `1.2`, and Chinese numerals like `第一章`, `第2讲`).
  3. User adjusts library, title, tags, or chapter structure if desired, then confirms import.
- **Incremental Sync ("检查新文件"):** For courses linked to `(source_id, source_root_path)`, detects newly indexed `active` video files under that path not yet in the course and presents them for one-click append to matching/new chapters—never modifying existing chapters or lessons.
- **Manual Assembly & Editing:** Create empty courses, add any indexed `media_files` from any source/directory, drag-and-drop reorder chapters and lessons (including moving a lesson between chapters), batch rename lessons, upload custom cover/backdrop images or select a lesson frame as course cover, manage tags, and inspect/link subtitles.

### 7.3 Jellyfin API Compatibility Layer
CourseFin serves the Jellyfin REST protocol directly at root (`/`) on the main HTTP server (default port `8096`). Root-path / origin-only hosting is required (`http://ip:8096` or a dedicated subdomain such as `https://course.example.com`; reverse-proxy subpaths are explicitly out of scope).

Compatibility targets:
- **First-class:** **Infuse, VidHub, SenPlayer**, and **CourseFin Web**.
- **Broad Jellyfin client compatibility:** Standard **Jellyfin official apps (iOS/Android/TV), Swiftfin, and Findroid** — supported via core `DeviceProfile` negotiation in `PlaybackInfo` and standard auxiliary endpoints (excluding full video bitstream re-encoding).

- **Course-to-Jellyfin Mapping:**
  - `Library` → `CollectionFolder` (`CollectionType: "tvshows"`)
  - `Course` → `Series` (`Tags` and `Genres` both populated from Course tags so clients filtering by either Genre or Tag work seamlessly *(Our call)*)
  - `Chapter` → `Season` (`ParentIndexNumber` / `IndexNumber` = `chapter.sort_order`, `Name` = `chapter.title`)
  - `Lesson` → `Episode` (`ParentIndexNumber` = `chapter.sort_order`, `IndexNumber` = `lesson.sort_order`, `SeriesId` = `course.id`, `SeasonId` = `chapter.id`, `RunTimeTicks` = `duration_ms * 10000`)
- **Authentication & Compatibility Parsing:**
  - Accept tokens via `Authorization: MediaBrowser ... Token="..."` (and `Emby ...`), `X-Emby-Token`, `X-MediaBrowser-Token`, and query param `api_key`.
  - Support case-insensitive route matching for Jellyfin paths and optional `/emby` prefix alias (used by some third-party clients like older Infuse configs). *(Our call)*
- **Required Endpoint Groups:**
  1. **System, Branding & Auxiliary Client Endpoints:**
     - `GET /System/Info/Public`, `GET /System/Info`, `GET /System/Ping`, `GET /System/Endpoint`, `GET /Branding/Configuration`
     - `GET /QuickConnect/Enabled` (returns `false`)
     - `GET|POST /DisplayPreferences/{displayPreferencesId}` (persisted or sensible JSON defaults per client so official Jellyfin apps don't error)
     - `GET /Items/Counts` (returns `SeriesCount`, `EpisodeCount`, etc.)
     - `GET /socket` (lightweight WebSocket upgrade & keepalive/noop handler so official Jellyfin clients and Swiftfin maintain a healthy connection without reconnect loops)
  2. **Users & Sessions:** `GET /Users/Public`, `POST /Users/AuthenticateByName`, `GET /Users/Me`, `GET /Users/{userId}`, `POST /Sessions/Logout`, `POST /Sessions/Capabilities`, `POST /Sessions/Capabilities/Full`, `GET /Sessions`.
  3. **Library & Catalog:**
     - `GET /Users/{userId}/Views`
     - `GET /Users/{userId}/Items` & `GET /Items` (supporting `ParentId`, `IncludeItemTypes`, `ExcludeItemTypes`, `Recursive`, `SortBy`, `SortOrder`, `StartIndex`, `Limit`, `SearchTerm`, `Tags`, `Genres`, `Ids`, `Filters` (`IsPlayed`, `IsUnplayed`, `IsResumable`), `Fields`)
     - `GET /Users/{userId}/Items/{itemId}` & `GET /Items/{itemId}`
     - `GET /Users/{userId}/Items/Resume`, `GET /Users/{userId}/Items/Latest`
     - `GET /Shows/NextUp`, `GET /Shows/{seriesId}/Seasons`, `GET /Shows/{seriesId}/Episodes`
     - `GET /Genres`, `GET /Tags`, `GET /Items/Filters`, `GET /Items/Filters2`, `GET /Search/Hints`
     - `GET /Users/{userId}/Items/{itemId}/Similar`, `GET /Users/{userId}/Items/{itemId}/Intros`, `GET /Users/{userId}/Items/{itemId}/SpecialFeatures` (return clean empty/related responses so official clients don't 404)
  4. **Images:**
     - `GET /Items/{itemId}/Images/{imageType}` (`Primary`, `Backdrop`, `Thumb`, optional `/{imageIndex}`) supporting `maxWidth`, `maxHeight`, `quality`, `tag`, `ETag`, and on-disk resized cache.
     - Fallback rules: Lesson `Primary` falls back to `media_files.thumbnail_path`; Course `Primary` falls back to first lesson's thumbnail if no custom cover is set.
  5. **Playback, `DeviceProfile` Negotiation & Subtitles:**
     - `POST /Items/{itemId}/PlaybackInfo` & `GET /Items/{itemId}/PlaybackInfo`:
       - Evaluates incoming `DeviceProfile` (`DirectPlayProfiles`, `TranscodingProfiles`, `SubtitleProfiles`) when provided: selects `DirectPlay` when container + video/audio codecs match `DirectPlayProfiles` (or when no `DeviceProfile` is supplied, as with Infuse/VidHub), and offers HLS remux (`TranscodingUrl` with `SupportsTranscoding: true`) when the container or audio codec is unsupported but the video codec can be stream-copied (`-c:v copy`).
     - `GET|HEAD /Videos/{itemId}/stream` & `GET|HEAD /Videos/{itemId}/stream.{container}` (DirectPlay via HTTP Range streaming)
     - `GET /Videos/{itemId}/master.m3u8` & `GET /Videos/{itemId}/main.m3u8` + on-demand HLS VOD segment delivery `/Videos/{itemId}/hls1/main/{segment}.ts` (or `.m4s`) + `DELETE /Videos/ActiveEncodings`
     - `GET /Videos/{itemId}/{mediaSourceId}/Subtitles/{index}/Stream.{format}` (serves sidecar subtitles and extracts embedded text subtitles on demand; automatically converts `.srt` → `.vtt` when `.vtt` is requested)
  6. **Progress & UserData:**
     - `POST /Sessions/Playing`, `POST /Sessions/Playing/Progress`, `POST /Sessions/Playing/Stopped`
     - `POST /Users/{userId}/PlayedItems/{itemId}`, `DELETE /Users/{userId}/PlayedItems/{itemId}`
     - `GET|POST /Users/{userId}/Items/{itemId}/UserData`

### 7.4 Playback Engine: DirectPlay + On-Demand Seekable VOD HLS Remux
- **DirectPlay (Default for Infuse/VidHub/SenPlayer & natively compatible browser MP4/WebM):**
  - Uses `StorageProvider.DirectURL` if available (future 302 redirect); otherwise opens the file via `StorageProvider.Open` and serves with Go's `http.ServeContent` (full HTTP Range support, minimal memory).
- **On-Demand Seekable VOD HLS Remux (via FFmpeg):**
  - Used when the Web browser or a Jellyfin client requests HLS remuxing for a container it cannot play natively (e.g., `.mkv`, `.flv`, `.ts`, `.mov`) whose video codec can be copied without re-encoding (`-c:v copy`, e.g., `h264`, or `hevc` when the client supports it).
  - **Instant arbitrary seek & resume:** Instead of sequential remuxing from `t=0`, the server synthesizes a complete fixed-segment-duration VOD `.m3u8` playlist upfront from the probed media duration (Jellyfin-style). When a client requests an ungenerated segment (e.g., resuming at `1:15:00` or scrubbing ahead), the remux manager starts/restarts FFmpeg at `-ss <segment_start>` (`-copyts` / consistent PTS/output-ts-offset) to produce segments forward from that point on demand.
  - **Audio handling:** Uses `-c:a copy` if the audio stream is already browser/HLS-compatible (AAC/MP3/Opus), or lightweight audio-only `-c:a aac` transcoding when audio is AC3/DTS/FLAC/PCM so playback has sound with minimal CPU overhead.
  - **Lifecycle & disk hygiene:** Remux sessions are keyed by `(lesson_id, play_session_id)`, cached under `/data/cache/remux`, bounded in disk usage (only generated/requested segments are written), and automatically terminated & cleaned up after idle timeout (e.g., 5 minutes) or explicit `DELETE /Videos/ActiveEncodings`.
  - **Unsupported video codec fallback:** If a video codec truly requires full video re-encoding that browsers cannot decode (e.g., legacy WMV/MPEG-2/RV40), the CourseFin Web UI displays a clear, friendly message recommending opening the course in a native Jellyfin player (Infuse / VidHub / SenPlayer) instead of silently failing. *(Our call)*

### 7.5 Built-in Web UI (CourseFin App)
Served at `/web` (with `/` redirecting to `/web` for browser requests while preserving Jellyfin API paths, or SPA served at `/web/*` so zero route collisions exist with Jellyfin endpoints like `/Items`, `/Users`, `/Videos` — **using `/web` prefix for SPA and `/api/v1` for CourseFin management API cleanly prevents any route conflict with Jellyfin!** *(Our call)*).

Core screens:
1. **Setup / Login:** First-run setup creates the single admin account (can also be pre-seeded via `COURSEFIN_ADMIN_USER` and `COURSEFIN_ADMIN_PASS`).
2. **Course Library Home:** Continue Watching rail, library tabs, tag filter chips, instant search, course card grid showing cover, title, tags, and progress bar.
3. **Course Detail & Web Player:** HTML5 video player (`video` + `hls.js` for remuxed HLS), chapter/lesson playlist sidebar, playback speed control (`0.75x`–`2.0x`), subtitle selector, keyboard shortcuts (space, arrows, fullscreen), and automatic progress reporting (every 10s, on pause, and on completion).
4. **Course Editor:**
   - Metadata panel: title, overview, instructor, library selector, tags editor, cover/backdrop uploader (plus "use lesson frame" picker).
   - Structure panel: chapter list & lesson list with drag-and-drop reordering across chapters, inline lesson renaming, "Add from Storage" picker, "Check New Files" incremental sync, and subtitle inspector.
   - "AI 智能整理" modal: runs AI metadata organization, displays side-by-side preview of suggested title/overview/tags/chapters/lesson titles with checkboxes so the user chooses what to apply.
5. **Storage & Importer:** Configure/manage local root directories, trigger scan, see scan progress & missing files, browse directory tree and trigger "一键导入为课程" with structure preview.
6. **Settings:**
   - Jellyfin connection info card (server URL, username, quick tips for Infuse/VidHub/SenPlayer).
   - AI Provider configuration (Base URL, API Key masked after save, Model name, "测试连接" button).
   - Change admin password & view/revoke active device sessions.

### 7.6 AI Metadata Organization (`AIProvider`)
- **Abstraction:** `AIProvider` Go interface with `OrganizeCourse(ctx, CourseOrganizeInput) (*CourseOrganizeProposal, error)` and `TestConnection(ctx) error`, plus a generic chat-completion primitive so future AI features (summaries, notes, Q&A) build on the same configured provider.
- **v1 Implementation:** `OpenAICompatibleProvider` calling `{base_url}/chat/completions` (compatible with OpenAI, DeepSeek, Qwen/DashScope, OpenRouter, Ollama, OneAPI/NewAPI, Gemini OpenAI endpoint, etc.).
- **Input Context:** Folder/source path hints, current course title, current chapters and lesson raw filenames/titles, and optional small subtitle excerpts (e.g., first ~200 characters per lesson, capped to a safe total character budget) when subtitles exist.
- **Structured Output (JSON):**
  - `title` (cleaned course title)
  - `overview` (concise course summary)
  - `tags` (3–8 normalized topic tags)
  - `chapters`: ordered list of `{ title, lessons: [{ lesson_id, title }] }` (every existing `lesson_id` must be accounted for; backend validates completeness before showing preview).
- **Safety:** Strictly preview-and-confirm in the UI. Nothing is applied until the user clicks confirm.

---

## 8. Deployment & Configuration

### Environment Variables
- `COURSEFIN_PORT` (default `8096`)
- `COURSEFIN_DATA_DIR` (default `/data`)
- `COURSEFIN_ADMIN_USER` / `COURSEFIN_ADMIN_PASS` (optional; skips setup wizard if provided on fresh DB)
- `COURSEFIN_LOG_LEVEL` (default `info`)
- `COURSEFIN_BASE_URL` (optional origin-only public URL hint, e.g. `https://course.example.com`, used for Settings display and reverse-proxy URL hints; runtime subpath hosting is out of scope)

### Docker Packaging
- Multi-stage `Dockerfile`:
  1. Node/pnpm stage builds the Web SPA into static assets.
  2. Go stage compiles the backend binary with embedded Web SPA assets.
  3. Minimal runtime image (`alpine` or `debian:bookworm-slim`) installing `ffmpeg`, `ca-certificates`, `tzdata`.
- Example `docker-compose.yml` mounts `/data` (rw) and `/media` (ro), exposes `8096:8096`, and defines a healthcheck against `/System/Ping`.

---

## 9. Decisions Left to the Builder

The builder is free to choose sensible defaults for:
- Go HTTP router/framework (`chi`, `gin`, `echo`, or `net/http`) and SQLite driver (`modernc.org/sqlite` vs `mattn/go-sqlite3`, using WAL mode + foreign keys).
- Database migration mechanism (embedded SQL migrations).
- Exact frontend stack within TypeScript SPA (React + Vite + Tailwind + UI primitives + drag-and-drop library).
- Internal Go package layout and API pagination defaults for `/api/v1/*`.
- Exact image resizing library/parameters, FFmpeg HLS segment duration (e.g. 4–6s), and remux session timeout/cleanup policy.
- Exact AI system prompt engineering and JSON retry/repair handling.
- Structured logging format and test harness structure (though Jellyfin API contract tests against real DTO shapes are strongly expected).
