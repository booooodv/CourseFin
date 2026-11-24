# CourseFin Internal Self-Update Module Design Document

- **Date:** 2026-11-24
- **Status:** Approved
- **Repository:** `booooodv/CourseFin`

---

## 1. Intent & Why

CourseFin is delivered as a single static Go binary (`CGO_ENABLED=0`) with the React Web SPA and SQLite migrations embedded inside, deployed either as a single Docker container or directly as a local/NAS binary. Today:

- The running version (`var version = "1.0.1"` in `cmd/coursefin/main.go`) is not exposed to the Web UI or `/health`, and `Dockerfile` does not inject a build-time version.
- Users have no built-in way to know when a new CourseFin release is available.
- Upgrading requires manually downloading/rebuilding binaries or running Docker commands in a terminal.

This design adds an **internal self-update module** that automatically detects new GitHub Releases, surfaces non-intrusive notifications and release notes in the Web UI, and lets the admin update CourseFin in-place with one click—working in both **Docker container** and **standalone binary** deployments without mounting `/var/run/docker.sock` or requiring external sidecar containers.

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals
1. **Automatic & manual release detection:** Periodically check GitHub Releases (default every 6 hours + short delayed check after startup) and allow on-demand manual checks, comparing normalized SemVer against the running version.
2. **One-click in-app self-update across Docker and standalone binaries:** Download the matching OS/architecture release archive, verify its `SHA-256` checksum and `--version` self-test, atomically swap the binary with backup rollback protection, gracefully shut down HTTP/FFmpeg/SQLite, and hot-restart in place (`syscall.Exec`).
3. **Docker persistence & image-upgrade compatibility:** Persist container self-updates under `/data/updates/bin` via a lightweight Docker entrypoint shim that compares the updated binary's version against the image-bundled `/usr/local/bin/coursefin`—keeping updates across container restarts while automatically yielding to a newer Docker image whenever the user pulls one later.
4. **Network resilience for restricted environments:** Support an optional user-configured GitHub download/API mirror proxy prefix (e.g. `https://gh-proxy.com`) and optional `COURSEFIN_GITHUB_TOKEN` env var for API rate limits, plus `ETag` conditional requests.
5. **End-to-end release pipeline:** Add `.github/workflows/release.yml` to build and publish multi-arch binaries (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`) and `checksums.txt` on every `v*` tag push, and inject `VERSION` during Docker builds.

### Non-Goals
- Updating container OS packages or `ffmpeg` / `ffprobe` binaries inside Docker (still handled by pulling a newer Docker image when needed).
- Mounting `/var/run/docker.sock` or integrating with Watchtower / container orchestrator APIs.
- Supporting pre-release / beta update channels in the UI (stable releases only; `releases/latest` excludes drafts and pre-releases).
- UI manual rollback button (automatic rollback on startup/exec failure is built in; manual version downgrade is out of scope for v1).
- Hot in-place executable replacement on Windows (Windows still gets automatic update detection, release notes, and direct release links, with a clear UI message that one-click replace requires Linux/macOS).

### Anti-Goals
- **Never install unverified binaries:** Missing `checksums.txt`, missing platform hash entry, SHA-256 mismatch, or failed `--version` self-test must abort before touching the active executable.
- **Never brick a Docker container or standalone binary:** Existing binary must be preserved as `.bak` before atomic swap, and unconfirmed new binaries in Docker must automatically roll back on crash restart.
- **Never interrupt video playback unexpectedly:** Never auto-install updates without explicit admin click + confirmation modal; top-bar notifications must be subtle and non-blocking.
- **Never leak secrets or open SSRF vectors:** `COURSEFIN_GITHUB_TOKEN` is never returned to clients; update endpoints require admin auth; mirror URLs restrict scheme to `http`/`https`.

---

## 3. Constraints

- **Existing build flags:** `Makefile` uses `-ldflags="-s -w -X main.version=$(VERSION)"`. Version injection must remain compatible with `-X main.version=...`.
- **Single-process Docker container:** Container runs CourseFin as PID 1 (`restart: unless-stopped`) with `/data` as the persistent volume and `/media:ro` read-only. Updates must persist inside `/data` without requiring root/host socket access.
- **Cross-mount atomicity & `noexec` mounts:** `<data_dir>` and the standalone binary's directory may reside on different filesystems, and `/tmp` (or `/data`) may be mounted `noexec`. Candidate binaries must be staged into the target directory itself before running `--version` and before `os.Rename`.
- **Existing API & test compatibility:**
  - `server.New(cfg config.Config)` signature must stay backward-compatible for existing tests.
  - `GET /health` must continue returning `200 OK` JSON containing `"status":"ok"`.
  - Frontend components (`Layout`, `SettingsPage`) are tested in isolation without `<App>` in Vitest; any new update context must provide a safe fallback when rendered outside `<UpdateProvider>`.
- **Zero extra Go/npm runtime bloat:** Implement semver parsing, tar.gz extraction, SHA-256 verification, and GitHub Releases HTTP client using the Go standard library (no heavy third-party updater SDKs).

---

## 4. Approaches Considered

| Approach | Summary | Trade-offs | Decision |
|---|---|---|---|
| **A. GitHub Releases single-binary self-update (Docker + local)** | Publish multi-arch static binaries + `checksums.txt` via GitHub Actions; `internal/updater` downloads, verifies SHA-256 & `--version`, atomically swaps, and hot-restarts via `syscall.Exec`. Docker uses a thin entrypoint comparing `/data/updates/bin/coursefin` vs image binary. | Works in both Docker and standalone modes with one click; full stack (Go + embedded React + DB migrations) updates atomically; zero `docker.sock` risk and zero extra containers. Does not upgrade container FFmpeg/Debian base packages. | **Selected by user** |
| **B. Local binary self-update + Docker notify-only** | Same detection and local binary swap, but in Docker only displays `docker compose pull && docker compose up -d` commands. | Preserves strict container immutability, but Docker users cannot click to update in-app. | Rejected |
| **C. Docker Watchtower Webhook / `docker.sock` image pull** | Trigger container recreation via Watchtower API or mounted Docker socket. | Upgrades OS/FFmpeg too, but Docker-only and requires extra sidecar setup or unacceptable host-root `docker.sock` risk. | Rejected |

---

## 5. Detailed Design

### 5.1 Versioning, CLI `--version`, and `/health`

1. **`cmd/coursefin/main.go`:**
   - Keep `var version = "1.0.1"` (overridable via `-ldflags="-X main.version=..."`).
   - Add immediate CLI flag check before loading DB/config: if `len(os.Args) > 1` and `os.Args[1]` is `--version`, `-version`, or `version`, print normalized version (leading `v` stripped, e.g. `1.0.1\n`) to stdout and exit `0`.
   - Pass `cfg.Version = version` when calling `server.New(cfg)` (if `cfg.Version == ""`, `server.New` defaults to `"0.0.0-dev"`).
2. **`internal/config/config.go`:**
   - Extend `Config` with:
     - `Version string` (populated by `main.go`)
     - `UpdateRepo string` (`COURSEFIN_UPDATE_REPO`, default `"booooodv/CourseFin"`)
     - `UpdateAPIBase string` (`COURSEFIN_UPDATE_API_BASE`, default `"https://api.github.com"`)
     - `UpdateGitHubToken string` (`COURSEFIN_GITHUB_TOKEN`, default `""`)
     - `UpdateAutoCheckEnv *bool` (`COURSEFIN_UPDATE_AUTO_CHECK`, optional override if explicitly set to `true`/`false`/`1`/`0`)
     - `ManagedBinDir string` (`COURSEFIN_MANAGED_BIN_DIR`, set automatically by Docker entrypoint to `${COURSEFIN_DATA_DIR}/updates/bin`; empty in standalone mode)
3. **`GET /health` (`internal/server/server.go`):**
   - Generate a random 16-hex `boot_id` once inside `server.New`.
   - Return `200 OK` with `Cache-Control: no-store` and body:
     ```json
     {"status":"ok","version":"1.0.1","boot_id":"a1b2c3d4e5f60708"}
     ```
   - `HEAD /health` continues returning `200 OK` with empty body.

---

### 5.2 Release Artifact Specification & CI Workflow

Add `.github/workflows/release.yml`:
- Trigger: `push` to tags matching `v*`, plus `workflow_dispatch`.
- Permissions: `contents: write`.
- Single `ubuntu-latest` job (fast because `CGO_ENABLED=0`):
  1. Build React Web SPA once (`pnpm --dir web install --frozen-lockfile && pnpm --dir web build`) and sync into `internal/server/webdist/`.
  2. Derive `VERSION="${GITHUB_REF_NAME#v}"` (for `workflow_dispatch`, allow optional tag/version input or default to latest git tag).
  3. Cross-compile `CGO_ENABLED=0` static binaries with `-trimpath -ldflags="-s -w -X main.version=${VERSION}"` for:
     - `linux/amd64`
     - `linux/arm64`
     - `darwin/amd64`
     - `darwin/arm64`
  4. Package each binary (mode `0755`, named `coursefin` at archive root) into:
     `coursefin_<VERSION>_<GOOS>_<GOARCH>.tar.gz`
  5. Generate `checksums.txt` using `sha256sum coursefin_*.tar.gz` (`<64-hex>  <filename>`).
  6. Publish GitHub Release attaching all `.tar.gz` archives and `checksums.txt` with auto-generated release notes.
- Update `Dockerfile`:
  - Accept `ARG VERSION=1.0.1` in `go-builder` and pass `-ldflags="-s -w -X main.version=${VERSION}"`.
  - Copy `scripts/docker-entrypoint.sh` to `/usr/local/bin/coursefin-entrypoint.sh` (`chmod 0755`) and set `ENTRYPOINT ["/usr/local/bin/coursefin-entrypoint.sh"]`.
- Update `Makefile` (`docker-build`) and `docker-compose.yml` to pass `VERSION` build-arg.

---

### 5.3 Docker Entrypoint & Crash-Rollback Guard (`scripts/docker-entrypoint.sh`)

```text
Container start
  ├─ IMAGE_BIN=/usr/local/bin/coursefin (version IMG_VER)
  ├─ ACTIVE_BIN=${DATA_DIR}/updates/bin/coursefin
  ├─ BACKUP_BIN=${DATA_DIR}/updates/bin/coursefin.bak
  ├─ PENDING_FILE=${DATA_DIR}/updates/bin/.pending_update
  │
  ├─ If PENDING_FILE exists:
  │    • If PENDING_FILE says "tried":
  │        new binary crashed before healthy startup!
  │        rollback: restore BACKUP_BIN -> ACTIVE_BIN (or remove ACTIVE_BIN if no valid backup),
  │        remove PENDING_FILE
  │    • Else:
  │        mark PENDING_FILE as "tried"
  │
  ├─ Probe ACTIVE_BIN --version -> ACT_VER
  ├─ If ACTIVE_BIN executable & valid SemVer & ACT_VER > IMG_VER:
  │    export COURSEFIN_MANAGED_BIN_DIR="${DATA_DIR}/updates/bin"
  │    exec "$ACTIVE_BIN" "$@"
  └─ Else:
       export COURSEFIN_MANAGED_BIN_DIR="${DATA_DIR}/updates/bin"
       exec "$IMAGE_BIN" "$@"
```

**Startup confirmation inside `cmd/coursefin/main.go`:**
- After `server.New` succeeds AND the TCP listener (`net.Listen("tcp", ":"+cfg.Port)`) is bound, if `cfg.ManagedBinDir != ""`, remove `filepath.Join(cfg.ManagedBinDir, ".pending_update")` (ignoring `os.ErrNotExist`).
- Note on `syscall.Exec` from inside Docker:
  - Before `syscall.Exec`, the updater writes `"pending"` into `${ManagedBinDir}/.pending_update` (actually writes `"tried"` right before `syscall.Exec` because the very next execution IS the first boot attempt of the new binary!).
  - If the new binary boots and binds HTTP successfully → it deletes `.pending_update`.
  - If the new binary crashes before binding HTTP → Docker (`restart: unless-stopped`) restarts the container → `docker-entrypoint.sh` sees `.pending_update` already contains `"tried"` → automatically restores `.bak` (or falls back to `IMAGE_BIN`), deletes `.pending_update`, and boots cleanly!
- SemVer comparison in `docker-entrypoint.sh` uses POSIX `awk` comparing numeric `major`, `minor`, `patch` (stripping optional leading `v` and trailing prerelease/build tags).

---

### 5.4 Backend Updater Service (`internal/updater/`)

Suggested package files:
- `internal/updater/semver.go` — SemVer parser & comparator (`ParseVersion`, `CompareVersion`, `IsValidSemVer`).
- `internal/updater/service.go` — Check, download, verify, stage, swap, state machine, periodic check loop.
- `internal/updater/exec_unix.go` & `internal/updater/exec_other.go` — Build-tagged `ExecReplace(binPath string, args, env []string) error` (`syscall.Exec` on `linux`/`darwin`; returns unsupported error on other OSes).

#### State Machine & Concurrency
- Protected by `sync.RWMutex` + atomic/guarded transition rules:
  - States: `"idle"`, `"checking"`, `"downloading"`, `"verifying"`, `"applying"`, `"restarting"`, `"failed"`.
  - Only one check or apply may run at a time. While in `downloading` / `verifying` / `applying` / `restarting`, `CheckNow` and `StartApply` return `ErrBusy` (`409 Conflict`).
- Persistent settings in `app_settings` table (reusing existing key-value table, no DB migration needed):
  - `update_auto_check`: `"true"` | `"false"` (default `"true"`; overridden by `COURSEFIN_UPDATE_AUTO_CHECK` if set)
  - `update_check_interval_hours`: integer `1..168` (default `6`)
  - `update_mirror_url`: string (default `""`; validated to be empty or `http://`/`https://` URL with host)

#### Checking for Updates (`CheckNow`)
1. Build API URL: `<UpdateAPIBase>/repos/<UpdateRepo>/releases/latest`.
2. Send `GET` with `Accept: application/vnd.github+json`, `User-Agent: CourseFin/<version>`, optional `If-None-Match: <cachedETag>` (unless mirror/repo changed), and optional `Authorization: Bearer <token>` when direct-calling `api.github.com` (never send `COURSEFIN_GITHUB_TOKEN` to third-party mirror URLs!).
3. Mirror fallback strategy when `mirror_url` is set:
   - Try `<mirror_url>/<api_url>` first (without GitHub token, 10s timeout).
   - If the mirror rejects `api.github.com` (non-200/304/404 or network error), automatically fall back to direct `<api_url>` (with optional token, 15s timeout).
4. Response handling:
   - `304 Not Modified`: refresh `last_checked_at`, clear `last_error`, keep cached release info.
   - `404 Not Found`: treat cleanly as no published releases yet (`update_available = false`, `last_error = ""`, update `last_checked_at`).
   - `200 OK`: parse `tag_name`, `name`, `body`, `html_url`, `published_at`, `draft`, `prerelease`, `assets[]`.
     - Ignore if `draft || prerelease`.
     - Find matching platform asset `coursefin_<ver>_<GOOS>_<GOARCH>.tar.gz` and `checksums.txt`.
     - Compare `latest` vs `current` using SemVer (`CompareVersion(latest, current) > 0`).
5. **Periodic loop:** Started by `server.New` under `s.wg`:
   - Initial check after `30s` delay (only if auto-check is enabled; avoids delaying cold startup and respects `ctx.Done()`).
   - Re-reads interval & enabled setting from DB dynamically so setting changes apply without restart.

#### Eligibility (`can_apply` & `cannot_apply_reason`)
Evaluated dynamically from runtime environment + release assets:
- Platform must be `linux` or `darwin` (else: `"当前操作系统不支持应用内热替换，请前往 GitHub Releases 手动下载更新"`).
- Current version must be valid SemVer (else: `"当前为开发构建版本，不支持自动覆盖更新"`).
- Target binary directory must be writable:
  - Docker mode (`ManagedBinDir != ""`): check/ensure `ManagedBinDir` writable.
  - Standalone mode: resolve `os.Executable()` + `filepath.EvalSymlinks()`, verify write permission in `filepath.Dir(exePath)` (if not writable: `"程序所在目录无写入权限，无法执行自动更新"`).
- When `update_available == true`, release must include both matching platform `.tar.gz` and `checksums.txt` (else: `"该发布版本缺少当前平台 (${GOOS}/${GOARCH}) 的更新包或校验文件"`).

#### Apply Pipeline (`StartApply` -> async goroutine)
Runs against server lifecycle context (not HTTP request context):
1. **`downloading`:**
   - Download `checksums.txt` (max 256 KiB; if `mirror_url` set, try mirror-prefixed URL first, fallback to direct URL).
   - Parse expected SHA-256 hex digest for `asset_name`; abort immediately if missing or malformed.
   - Stream-download `asset_name` (max 128 MiB) into `<data_dir>/updates/staging/<asset_name>.part`, computing `sha256.New()` on the fly and updating `progress_bytes`, `total_bytes`, `progress_percent` atomically.
2. **`verifying`:**
   - Confirm computed SHA-256 equals expected hash from `checksums.txt` (case-insensitive constant-time compare). On mismatch, delete staging file and fail.
   - Open `.tar.gz`, enforce decompressed read cap (128 MiB), locate regular file entry whose clean basename is `coursefin` (reject symlinks, hardlinks, `..` traversal).
   - Extract directly into target directory as `<target_dir>/.coursefin.new.<rand>` with mode `0755` and `fsync`.
   - Execute `<target_dir>/.coursefin.new.<rand> --version` with a `5s` timeout; verify exit code `0` and stdout SemVer matches `latest_version`. (This verifies architecture compatibility, binary integrity, and target mount executability/`noexec` before touching the running binary!)
3. **`applying`:**
   - Target path:
     - Docker mode: `<ManagedBinDir>/coursefin` (backup: `<ManagedBinDir>/coursefin.bak`).
     - Standalone mode: `<resolvedExePath>` (backup: `<resolvedExePath>.bak`).
   - If target path currently exists, copy it safely to `<backup>` (`0755`, via temp + rename so backup is atomic).
   - Atomically `os.Rename(candidatePath, targetPath)`.
   - In Docker mode, write `"tried"` to `<ManagedBinDir>/.pending_update`.
   - Clean up staging archive.
4. **`restarting`:**
   - Set state to `"restarting"`.
   - Sleep `400ms` so in-flight status polls observe `"restarting"`.
   - Invoke `restartHook(RestartRequest{ExecPath: targetPath, BackupPath: backupPath})`.
5. **Graceful Shutdown & Hot Exec (`cmd/coursefin/main.go`):**
   - `main.go` receives `RestartRequest` on a buffered channel alongside `sigCtx.Done()` and `errCh`.
   - Runs standard graceful shutdown (`httpSrv.Shutdown(15s)` → `appSrv.Close()`).
   - Calls `updater.ExecReplace(req.ExecPath, os.Args, os.Environ())`.
   - If `ExecReplace` returns an error: logs error, attempts to restore `req.BackupPath` → `req.ExecPath`, and calls `ExecReplace` on the restored binary (or exits `1` so Docker/systemd restarts cleanly).

---

### 5.5 Management REST API (`internal/api/handlers_update.go`)

All routes mounted inside authenticated `/api/v1` group (`requireAuth`):

| Method & Path | Behavior | Status Codes |
|---|---|---|
| `GET /api/v1/system/update` | Returns cached `UpdateStatus` snapshot immediately (never blocks on GitHub). | `200` |
| `PUT /api/v1/system/update/config` | Updates `auto_check_enabled`, `check_interval_hours` (`1..168`), `mirror_url` (validated `http/https` or empty) in `app_settings`; returns updated `UpdateStatus`. | `200`, `400` |
| `POST /api/v1/system/update/check` | Triggers an immediate check against GitHub Releases; returns updated `UpdateStatus`. | `200`, `409` (applying), `502` (upstream error) |
| `POST /api/v1/system/update/apply` | Starts async download/verify/apply/restart pipeline; returns current `UpdateStatus` (`state: "downloading"`). | `202`, `400` (not available / cannot apply), `409` (already busy) |

`UpdateStatus` JSON schema:
```json
{
  "current_version": "1.0.1",
  "latest_version": "1.0.2",
  "update_available": true,
  "release_name": "CourseFin v1.0.2",
  "release_notes": "...",
  "release_url": "https://github.com/booooodv/CourseFin/releases/tag/v1.0.2",
  "published_at": "2026-11-24T12:00:00Z",
  "asset_name": "coursefin_1.0.2_linux_amd64.tar.gz",
  "asset_size": 14820352,
  "deploy_mode": "docker",
  "platform": "linux/amd64",
  "can_apply": true,
  "cannot_apply_reason": "",
  "auto_check_enabled": true,
  "auto_check_locked_by_env": false,
  "check_interval_hours": 6,
  "mirror_url": "",
  "state": "idle",
  "target_version": "",
  "progress_bytes": 0,
  "total_bytes": 0,
  "progress_percent": 0,
  "last_checked_at": "2026-11-24T14:30:00Z",
  "last_error": ""
}
```

---

### 5.6 Frontend Design (`web/src/`)

1. **`UpdateContext.tsx` (`web/src/components/UpdateContext.tsx`):**
   - Mounted inside `App.tsx` (wrapping `<BrowserRouter>`), with a safe no-op default context so isolated Vitest page tests keep passing without wrapping.
   - Exposes `status`, `loading`, `refreshStatus()`, `checkNow()`, `updateConfig(...)`, `applyUpdate()`, and restart-waiting state (`restartingPhase`, `restartError`).
   - Polls `GET /api/v1/system/update` lazily when authenticated (on layout mount + every 5 minutes while idle; every `800ms` while an update is active).
2. **Top Navigation Indicator (`web/src/components/Layout.tsx`):**
   - When `status?.update_available` is true:
     - Desktop header (beside «退出登录»): renders compact emerald pill button `↑ 新版本 v{latest_version}` with Tooltip `"发现新版本 v{latest_version}，点击前往设置页查看并更新"`, navigating to `/settings#system-update`.
     - Mobile nav bar: renders a subtle emerald indicator dot on the «设置» icon.
3. **Settings Page Update Card (`web/src/components/SystemUpdateSection.tsx`, rendered in `SettingsPage.tsx`):**
   - Mounted with `id="system-update"` right below the «Jellyfin 客户端连接指南» section (and auto-scrolled into view when URL hash is `#system-update`).
   - Displays:
     - Current version badge (`v1.0.1`), deployment mode badge (`Docker 容器` / `本地二进制`), platform (`linux/amd64`), last checked timestamp.
     - Update status area:
       - Up-to-date message when `!update_available`.
       - Highlighted card when `update_available`: target version, release date, external link to GitHub Release page, scrollable plain-text Release Notes (safe text rendering, no raw HTML injection).
       - Clear explanation + manual download link when `!can_apply`.
       - Inline error banner when `last_error` is non-empty.
     - Live progress panel during `downloading` / `verifying` / `applying` / `restarting` (stage text, percentage bar, MB progress).
     - Action buttons: «检查更新» + «立即更新到 v{latest_version}» (all wrapped with `<Tooltip>` and emitting `<Toast>` feedback).
     - Secondary confirmation modal before calling `POST /api/v1/system/update/apply`, warning that playback will pause briefly while all course data and progress remain intact.
     - Update preferences sub-panel: «自动检查更新» toggle (disabled with note if locked by `COURSEFIN_UPDATE_AUTO_CHECK` env), check interval select (`1 / 6 / 12 / 24 小时`), optional «GitHub 加速代理前缀» input with security note ("仅填写可信加速代理地址") and «保存更新配置» button.
   - **Restart recovery flow:**
     - Captures `/health` `boot_id` before initiating apply.
     - Once status enters `restarting` or polling encounters network/502/503 disconnect during apply, switches to polling `GET /health` every `1s` up to `60s`.
     - When `/health` responds `200` with `version === targetVersion`: displays success toast `"已更新至 v{targetVersion}，正在刷新页面..."` and calls `window.location.reload()`.
     - When `/health` responds `200` with a new `boot_id` whose `version !== targetVersion`: stops waiting and shows an error banner indicating the update rolled back to `v{version}`.
     - On 60s timeout: displays clear status message with a manual «立即刷新» button.

---

### 5.7 Security & Documentation Updates

- Update `.env.example` with `COURSEFIN_UPDATE_REPO`, `COURSEFIN_UPDATE_AUTO_CHECK`, `COURSEFIN_GITHUB_TOKEN`.
- Update `SECURITY.md` with a note on the self-update security boundary (mandatory SHA-256 verification, admin-only access, trust warning for custom mirror URLs).
- Update `README.md` documenting the built-in self-update feature.

---

## 6. Left to the Builder

- Exact Tailwind spacing, icons, and visual transitions for the top-bar update pill, `SystemUpdateSection`, and confirmation modal (staying consistent with existing dark slate/indigo theme).
- Exact helper function boundaries inside `internal/updater/` and unit test fakes (local `httptest.Server` serving mock GitHub Release JSON, `checksums.txt`, and dynamically compiled or shell-script dummy binaries for `--version` / atomic swap / rollback tests).
- Exact handling of `workflow_dispatch` inputs in `.github/workflows/release.yml`.
- Rebuilding embedded frontend assets (`make sync-web` / `make build`) and running full `make test` (`go test -race ./...` + Vitest).
