# CourseFin Internal Self-Update Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an automatic update detection and one-click in-app self-update module for CourseFin that works across both Docker containers and standalone Linux/macOS binaries, backed by a GitHub Releases multi-arch CI workflow.

**Architecture:** Build a zero-dependency Go `internal/updater` package implementing SemVer comparison, GitHub Releases polling (with ETag and mirror fallback), streaming download with mandatory SHA-256 verification, hardened `tar.gz` extraction, `--version` pre-flight check, atomic binary swap with `.bak` backup, and Unix `syscall.Exec` hot-restart. Pair it with a thin POSIX Docker entrypoint (`scripts/docker-entrypoint.sh`) that selects the newer binary between the image and `/data/updates/bin` with crash-rollback protection, four authenticated `/api/v1/system/update*` endpoints, an enhanced `/health` endpoint (`version` + `boot_id`), a React `UpdateContext` + top-bar badge + Settings update card, and `.github/workflows/release.yml`.

**Tech Stack:** Go 1.26 (standard library, `chi`, SQLite), POSIX `sh`/`awk` (Docker entrypoint), GitHub Actions, React 19, TypeScript, Tailwind CSS v4, Lucide icons, Vitest + React Testing Library.

**Spec:** `docs/superpowers/specs/2026-11-24-self-update-design.md`

## Global Constraints

- Zero new external Go or npm dependencies; implement SemVer, SHA-256, `tar.gz` extraction, and GitHub HTTP client with the Go standard library and UI with React/Tailwind/existing `lucide-react`.
- Keep `-ldflags="-s -w -X main.version=$(VERSION)"` compatibility in `cmd/coursefin/main.go` and `Makefile`.
- Keep `server.New(cfg config.Config)` backward-compatible for existing tests; `GET /health` must continue returning HTTP `200` JSON containing `"status":"ok"`.
- Never install unverified binaries: missing `checksums.txt`, missing platform entry, SHA-256 mismatch, tar path traversal/symlink, or failed `--version` self-test must abort before modifying the active binary.
- All `/api/v1/system/update*` routes require admin authentication (`requireAuth`); `COURSEFIN_GITHUB_TOKEN` must never be sent to custom mirror URLs or returned in API responses.
- Frontend `UpdateContext` and `SystemUpdateSection` must never break standalone Vitest tests that mount `SettingsPage` or `Layout` without `<UpdateProvider>` or without mocking `/api/v1/system/update`.
- Web UI copy is Chinese-first, concise, and non-blocking.

## Review Focus

1. **GitHub token isolation with custom mirror proxy:** When both `COURSEFIN_GITHUB_TOKEN` and `update_mirror_url` are set, requests to the mirror host must never carry the `Authorization` header; the token is attached only on direct calls to `UpdateAPIBase`. (Pinned in Task 1)
2. **Tampered or malicious release archive:** SHA-256 mismatch, missing `checksums.txt` entry, or `tar.gz` containing `../` traversal or symlink entries must fail in `verifying` without touching the active executable. (Pinned in Task 2)
3. **Failed `--version` pre-flight check:** If the staged candidate binary exits non-zero or reports a different version than `latest_version`, `StartApply` must fail cleanly, remove the candidate temp file, and keep the running binary untouched. (Pinned in Task 2)
4. **Docker crash-loop auto-rollback:** When a newly updated binary under `/data/updates/bin/coursefin` crashes before binding HTTP (leaving `.pending_update` containing `tried`), `scripts/docker-entrypoint.sh` must restore `.bak` (or fall back to the image binary) and delete `.pending_update` on next container start. (Pinned in Task 4)
5. **Frontend restart recovery under disconnect & rollback:** Dropping `/api/v1/system/update` with network error / `502` / `503` during an active update must transition to `/health` polling instead of showing a fatal error; if `/health` returns a new `boot_id` whose `version` is still the old version, the UI must report rollback rather than success. (Pinned in Task 5)

---

## File Structure

- **Modify `internal/config/config.go` & `internal/config/config_test.go`:** Add `Version`, `UpdateRepo`, `UpdateAPIBase`, `UpdateGitHubToken`, `UpdateAutoCheckEnv`, `ManagedBinDir`.
- **Create `internal/updater/semver.go`:** `SemVer`, `NormalizeVersion`, `ParseVersion`, `IsValidSemVer`, `CompareVersion`.
- **Create `internal/updater/exec_unix.go` & `internal/updater/exec_other.go`:** Build-tagged `SupportsHotExec()`, `ExecReplace(binPath string, args, env []string) error`.
- **Create `internal/updater/service.go`:** `Service`, `Options`, `Status`, `ConfigInput`, `RestartRequest`, `ErrBusy`, `ErrInvalidConfig`, `ErrCannotApply`, `NewService`, `Status`, `UpdateConfig`, `CheckNow`, `StartPeriodicLoop`, `RestartCh`.
- **Create `internal/updater/apply.go`:** `StartApply`, streaming download with caps & progress, `checksums.txt` parsing & SHA-256 verification, safe `tar.gz` extraction, `--version` pre-flight, atomic `.bak` backup & swap, Docker `.pending_update` marker.
- **Create `internal/updater/updater_test.go`:** Full unit & integration tests for SemVer, config persistence, GitHub check/ETag/404/mirror/token isolation, download/verify/extract/pre-flight/swap/rollback error cases.
- **Create `internal/api/handlers_update.go` & modify `internal/api/router.go`:** Authenticated `/api/v1/system/update`, `/config`, `/check`, `/apply` handlers.
- **Modify `internal/server/server.go` & `cmd/coursefin/main.go`:** Wire `updater.Service`, periodic check loop, `RestartCh()`, `/health` (`version` + `boot_id`), CLI `--version`, listener-before-clearing-`.pending_update`, graceful shutdown + `ExecReplace` with backup fallback.
- **Create `scripts/docker-entrypoint.sh` & `internal/updater/entrypoint_test.go`:** Version-selecting Docker entrypoint with `.pending_update` rollback guard and shell integration tests.
- **Modify `Dockerfile`, `Makefile`, `docker-compose.yml` & create `.github/workflows/release.yml`:** Inject `VERSION`, install entrypoint, publish multi-arch `.tar.gz` + `checksums.txt` on `v*` tag push.
- **Modify `web/src/types/coursefin.ts`, `web/src/api/client.ts`, `web/src/App.tsx`:** Update/health types, API methods, `<UpdateProvider>` mount.
- **Create `web/src/components/UpdateContext.tsx`:** Shared update status, active polling, and `/health` restart-recovery state machine.
- **Create `web/src/components/SystemUpdateSection.tsx`, modify `web/src/components/Layout.tsx` & `web/src/pages/SettingsPage.tsx`:** Top-bar update pill, mobile dot, Settings update card, confirmation modal, and config controls.
- **Create `web/src/__tests__/SystemUpdate.test.tsx`:** Vitest coverage for `UpdateContext`, `Layout` badge, `SystemUpdateSection`, restart recovery, and rollback handling.
- **Modify `.env.example`, `SECURITY.md`, `README.md` & rebuild `internal/server/webdist/`:** Documentation updates and full verification.

---

### Task 1: Config Extensions, SemVer & `internal/updater` Core (Check, Config, Mirror, Eligibility)

**Files:**
- Modify: `internal/config/config.go:8-36`
- Modify: `internal/config/config_test.go`
- Create: `internal/updater/semver.go`
- Create: `internal/updater/exec_unix.go`
- Create: `internal/updater/exec_other.go`
- Create: `internal/updater/service.go`
- Test: `internal/updater/updater_test.go`

**Interfaces:**
- Consumes: `database/sql` (`app_settings` table from `internal/db`), `internal/config.Config`.
- Produces:
  - `config.Config` fields: `Version`, `UpdateRepo`, `UpdateAPIBase`, `UpdateGitHubToken`, `UpdateAutoCheckEnv *bool`, `ManagedBinDir`.
  - `updater.NormalizeVersion(raw string) string`, `updater.ParseVersion(raw string) (SemVer, error)`, `updater.IsValidSemVer(raw string) bool`, `updater.CompareVersion(a, b string) (int, error)`.
  - `updater.SupportsHotExec() bool`, `updater.ExecReplace(binPath string, args, env []string) error`.
  - `updater.Options`, `updater.Status`, `updater.ConfigInput`, `updater.RestartRequest`, `updater.Service`:
    - `NewService(opts Options) *Service`
    - `(*Service).Status(ctx context.Context) Status`
    - `(*Service).UpdateConfig(ctx context.Context, in ConfigInput) (Status, error)`
    - `(*Service).CheckNow(ctx context.Context) (Status, error)`
    - `(*Service).StartPeriodicLoop(ctx context.Context)`
    - `(*Service).RestartCh() <-chan RestartRequest`
  - Sentinel errors: `ErrBusy`, `ErrInvalidConfig`, `ErrCannotApply`, `ErrUpstream`.

- [ ] **Step 1: Write failing config tests in `internal/config/config_test.go`**

Extend `TestLoadDefaults` and `TestLoadFromEnv` to assert:
- Defaults: `UpdateRepo == "booooodv/CourseFin"`, `UpdateAPIBase == "https://api.github.com"`, `UpdateGitHubToken == ""`, `UpdateAutoCheckEnv == nil`, `ManagedBinDir == ""`.
- Env overrides: `COURSEFIN_UPDATE_REPO="acme/cf"`, `COURSEFIN_UPDATE_API_BASE="https://gh.local/api/"` (trailing slash trimmed), `COURSEFIN_GITHUB_TOKEN="ghp_test"`, `COURSEFIN_UPDATE_AUTO_CHECK="false"` (`*cfg.UpdateAutoCheckEnv == false`), `COURSEFIN_MANAGED_BIN_DIR="/data/updates/bin"`.

- [ ] **Step 2: Run config tests to verify failure**

Run: `go test -v ./internal/config/...`
Expected: FAIL (undefined fields on `config.Config`).

- [ ] **Step 3: Implement config extensions in `internal/config/config.go`**

Add the six fields to `Config` and load them in `Load()` (`COURSEFIN_UPDATE_REPO`, `COURSEFIN_UPDATE_API_BASE`, `COURSEFIN_GITHUB_TOKEN`, `COURSEFIN_UPDATE_AUTO_CHECK` parsed via `strconv.ParseBool`, `COURSEFIN_MANAGED_BIN_DIR`), keeping `Version` empty until set by `main.go`.

- [ ] **Step 4: Run config tests to verify pass**

Run: `go test -race -v ./internal/config/...`
Expected: PASS.

- [ ] **Step 5: Write failing updater SemVer, Config, Check & Mirror tests in `internal/updater/updater_test.go`**

Add:
- `TestSemVer`: verifies `NormalizeVersion("v1.2.3") == "1.2.3"`, `CompareVersion("1.0.2", "1.0.1") > 0`, `CompareVersion("1.0.10", "1.0.2") > 0`, `CompareVersion("1.0.2-rc.1", "1.0.2") < 0`, `CompareVersion("v1.0.1", "1.0.1") == 0`, `IsValidSemVer("dev") == false`.
- `TestCheckNowSuccessAndETag`: local `httptest.Server` returning a valid GitHub release JSON (`tag_name: "v1.2.0"`, matching `coursefin_1.2.0_<GOOS>_<GOARCH>.tar.gz` + `checksums.txt`, `ETag: "etag-1"`). Asserts first `CheckNow` returns `UpdateAvailable == true`, `LatestVersion == "1.2.0"`, `CanApply == true`, `AssetName` populated; second `CheckNow` sends `If-None-Match: "etag-1"`, server returns `304`, and cached release data is preserved with refreshed `LastCheckedAt`.
- `TestCheckNowNoReleases404AndPrerelease`: `404` returns `nil` error with `UpdateAvailable == false` and `LastError == ""`; a `prerelease: true` or `draft: true` release is ignored (`UpdateAvailable == false`).
- `TestMirrorFallbackAndTokenIsolation`: sets `UpdateGitHubToken: "secret-tok"` and configures `mirror_url` to a mock mirror server:
  1. When mirror succeeds (`200`), assert the mirror server received **empty** `Authorization` header (token never leaked).
  2. When mirror returns `403`, assert updater falls back to direct `UpdateAPIBase` server AND direct server receives `Authorization: Bearer secret-tok`.
- `TestUpdateConfigValidationAndPersistence`: persists `auto_check_enabled`, `check_interval_hours`, `mirror_url` in SQLite `app_settings`; rejects interval `<1` or `>168` and rejects `file:///etc/passwd` mirror URL with `ErrInvalidConfig`; confirms `UpdateAutoCheckEnv` sets `AutoCheckLockedByEnv == true` and overrides DB value.
- `TestEligibilityChecks`: verifies `DeployMode == "docker"` when `ManagedBinDir != ""` vs `"binary"` when empty; verifies `CanApply == false` with non-empty `CannotApplyReason` for `CurrentVersion: "dev"` or when release lacks the current platform asset.

- [ ] **Step 6: Run updater tests to verify failure**

Run: `go test -v ./internal/updater/...`
Expected: FAIL (package/symbols undefined).

- [ ] **Step 7: Implement `semver.go`, `exec_unix.go`, `exec_other.go`, and `service.go` in `internal/updater/`**

Key implementation details pinned by spec:
- `semver.go`: SemVer 2.0.0 numeric `major.minor.patch` + prerelease precedence (`a` without prerelease > `b` with prerelease; dot-separated numeric vs lexical comparison).
- `exec_unix.go` (`//go:build linux || darwin`): `SupportsHotExec() = true`, `ExecReplace` delegates to `syscall.Exec`.
- `exec_other.go` (`//go:build !linux && !darwin`): `SupportsHotExec() = false`, `ExecReplace` returns unsupported error.
- `service.go`:
  - `Options` accepts `DB *sql.DB`, `DataDir`, `CurrentVersion`, `Repo`, `APIBase`, `GitHubToken`, `AutoCheckEnv *bool`, `ManagedBinDir`, `HTTPClient *http.Client`, `GOOS`, `GOARCH` (defaulting to `runtime.GOOS`/`runtime.GOARCH`), `ExecutablePath func() (string, error)` (default `os.Executable`), `InitialCheckDelay time.Duration` (default `30 * time.Second`).
  - `Status` JSON tags match Section 5.5 of the spec verbatim.
  - `CheckNow` enforces 2 MiB limit on release JSON, uses `ETag` only when `repo` and `mirror_url` haven't changed, sends `User-Agent: CourseFin/<version>` and `Accept: application/vnd.github+json`, never sends `GitHubToken` to mirror URLs.
  - `StartPeriodicLoop` waits `InitialCheckDelay` (respecting `ctx.Done()`), checks if auto-check is enabled, then loops sleeping for the dynamically read interval (`check_interval_hours`) using a re-checkable timer (or ticker with short wake/config notify channel so interval changes take effect cleanly).

- [ ] **Step 8: Run updater tests to verify pass**

Run: `go test -race -v ./internal/updater/...`
Expected: PASS.

- [ ] **Step 9: Commit Task 1**

```bash
git add docs/superpowers/specs/2026-11-24-self-update-design.md docs/superpowers/plans/2026-11-24-self-update.md internal/config/config.go internal/config/config_test.go internal/updater/semver.go internal/updater/exec_unix.go internal/updater/exec_other.go internal/updater/service.go internal/updater/updater_test.go
git commit -m "feat(updater): add config extensions, semver, and GitHub release checker"
```

---

### Task 2: `internal/updater` Apply Pipeline (Download, SHA-256, Safe Tar.gz, `--version` Pre-flight, Atomic Swap)

**Files:**
- Create: `internal/updater/apply.go`
- Modify: `internal/updater/service.go`
- Modify: `internal/updater/updater_test.go`

**Interfaces:**
- Consumes: `updater.Service` internals from Task 1 (`mu`, `status`, `latestRelease`, `restartCh`, mirror helper, `ManagedBinDir`, `executablePath`).
- Produces:
  - `(*Service).StartApply(lifecycleCtx context.Context) (Status, error)`
  - `(*Service).Wait()` (waits for active apply goroutine to exit cleanly on shutdown/tests).

- [ ] **Step 1: Write failing apply pipeline tests in `internal/updater/updater_test.go`**

Create a helper `buildReleaseTarGz(t, entryName, content, mode)` and executable shell script stub (`#!/bin/sh\nif [ "$1" = "--version" ]; then echo "1.2.0"; exit 0; fi`) and add tests:
- `TestStartApplyDockerModeSuccess`: runs `CheckNow` then `StartApply(ctx)` with `ManagedBinDir` set and an existing v1.0.1 stub at `<ManagedBinDir>/coursefin`. Waits for `<-svc.RestartCh()`. Asserts:
  - `req.ExecPath == filepath.Join(managedDir, "coursefin")` and `req.BackupPath == filepath.Join(managedDir, "coursefin.bak")`.
  - `<ManagedBinDir>/coursefin --version` outputs `1.2.0`.
  - `<ManagedBinDir>/coursefin.bak --version` outputs `1.0.1`.
  - `<ManagedBinDir>/.pending_update` exists and contains `"tried"`.
  - Final `Status.State == "restarting"` and `ProgressPercent == 100`.
- `TestStartApplyStandaloneBinarySuccess`: `ManagedBinDir == ""`, `ExecutablePath` pointing to a temp binary stub. Asserts atomic swap, `.bak` preserved, no `.pending_update` created, `RestartCh()` receives `ExecPath == exePath`.
- `TestStartApplyRejectsTamperedChecksum`: `checksums.txt` has wrong SHA-256 -> `State == "failed"`, `LastError` mentions checksum mismatch, active binary untouched.
- `TestStartApplyRejectsTarTraversalAndSymlink`: `.tar.gz` contains `../evil` or symlink `coursefin` -> `State == "failed"`, active binary untouched.
- `TestStartApplyRejectsFailedVersionPreflight`: candidate binary prints `9.9.9` (mismatch) or exits `1` -> `State == "failed"`, candidate temp file cleaned up, active binary untouched.
- `TestStartApplyConcurrencyBusy`: while download is paused on a channel in mock HTTP handler, second `StartApply` and `CheckNow` both return `ErrBusy`.

- [ ] **Step 2: Run apply tests to verify failure**

Run: `go test -v -run TestStartApply ./internal/updater/...`
Expected: FAIL (`StartApply` undefined).

- [ ] **Step 3: Implement `internal/updater/apply.go`**

Implement `StartApply(lifecycleCtx context.Context) (Status, error)` and helper stages:
- Pre-checks under `s.mu`: reject `ErrBusy` if applying/restarting/checking; reject `ErrCannotApply` if `!updateAvailable`, `!canApply`, or missing asset/checksums URLs. Transition `state = "downloading"`, reset progress/error, set `target_version = latestVersion`, launch goroutine tracked by `s.wg`.
- Constants: `maxChecksumsBytes = 256 << 10`, `maxArchiveBytes = 128 << 20`, `maxBinaryBytes = 128 << 20`, `preflightTimeout = 5 * time.Second`, `restartGraceDelay = 400 * time.Millisecond` (configurable on `Options.RestartGraceDelay` for fast tests!).
- Stage 1 (`downloading`): fetch `checksums.txt` (mirror-first if `mirror_url` set, fallback direct, no GitHub token on mirror), parse `<64-hex>  <filename>` matching `assetName`; stream-download archive to `<dataDir>/updates/staging/<assetName>.part` via `io.LimitReader(resp.Body, maxArchiveBytes+1)` tee'd into `sha256.New()`, updating `progress_bytes`, `total_bytes`, `progress_percent`.
- Stage 2 (`verifying`): constant-time compare hex SHA-256; open gzip+tar, reject non-regular entries or `..` paths or `size > maxBinaryBytes`, locate clean entry `"coursefin"` (or `path.Base == "coursefin"` without `..`), extract directly into `<targetDir>/.coursefin.new.<rand>` with mode `0755` + `Sync()`; run `exec.CommandContext(timeoutCtx, candidatePath, "--version")` with minimal env (`PATH=...`) and confirm `NormalizeVersion(stdout) == NormalizeVersion(targetVersion)`. Always `defer os.Remove(candidatePath)` until renamed.
- Stage 3 (`applying`): resolve `targetPath` (`<ManagedBinDir>/coursefin` vs resolved `executablePath`) and `backupPath` (`targetPath + ".bak"`). If `targetPath` exists, copy atomically to `backupPath` (write temp in `targetDir` + `os.Rename`). `os.Rename(candidatePath, targetPath)`. If `ManagedBinDir != ""`, write `"tried\n"` to `filepath.Join(ManagedBinDir, ".pending_update")`. Clean up staging `.part` file.
- Stage 4 (`restarting`): set `state = "restarting"`, `progress_percent = 100`, wait `restartGraceDelay` (respecting `lifecycleCtx.Done()`), send `RestartRequest{ExecPath: targetPath, BackupPath: backupPath}` non-blockingly to `s.restartCh`.

- [ ] **Step 4: Run all updater tests to verify pass**

Run: `go test -race -v ./internal/updater/...`
Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add internal/updater/apply.go internal/updater/service.go internal/updater/updater_test.go
git commit -m "feat(updater): implement verified download, preflight check, and atomic binary swap"
```

---

### Task 3: REST API Endpoints, Server Integration, `/health` Enhancement & `cmd/coursefin` Hot-Restart

**Files:**
- Create: `internal/api/handlers_update.go`
- Modify: `internal/api/router.go:34-158`
- Modify: `internal/server/server.go:30-170`
- Modify: `cmd/coursefin/main.go:1-75`
- Test: `internal/api/api_test.go`, `internal/server/server_test.go`

**Interfaces:**
- Consumes: `updater.Service`, `updater.ConfigInput`, `updater.Status`, `updater.RestartRequest`, `updater.ExecReplace`, `updater.NormalizeVersion`.
- Produces:
  - `api.Deps.Updater *updater.Service`
  - HTTP routes (all under `requireAuth`):
    - `GET /api/v1/system/update` -> `200` `updater.Status`
    - `PUT /api/v1/system/update/config` -> `200` `updater.Status` / `400`
    - `POST /api/v1/system/update/check` -> `200` `updater.Status` / `409` / `502`
    - `POST /api/v1/system/update/apply` -> `202` `updater.Status` / `400` / `409`
  - `(*server.Server).RestartCh() <-chan updater.RestartRequest`
  - `GET /health` -> `200` `{"status":"ok","version":"<ver>","boot_id":"<16-hex>"}\n` with `Cache-Control: no-store`.
  - CLI `coursefin --version` / `-version` / `version` -> prints normalized version and exits `0`.

- [ ] **Step 1: Write failing API and server tests in `internal/api/api_test.go` and `internal/server/server_test.go`**

- In `internal/api/api_test.go`: add `TestSystemUpdateEndpoints` verifying:
  1. Unauthenticated `GET /api/v1/system/update` returns `401`.
  2. Authenticated `GET /api/v1/system/update` returns `200` with `current_version`.
  3. `PUT /api/v1/system/update/config` persists valid `auto_check_enabled`, `check_interval_hours`, `mirror_url` and returns `400` on invalid mirror scheme (`ftp://bad`) or interval `0`.
  4. `POST /api/v1/system/update/check` against mock GitHub server returns `200` with `update_available: true`, and returns `502` when upstream fails.
  5. `POST /api/v1/system/update/apply` returns `202` when update available, `400` when no update available, and `409` when already applying.
- In `internal/server/server_test.go`: assert `GET /health` returns JSON with `status == "ok"`, non-empty `version`, non-empty `boot_id`, and `Cache-Control: no-store`.

- [ ] **Step 2: Run API & server tests to verify failure**

Run: `go test -v -run "TestSystemUpdateEndpoints|TestServer" ./internal/api/... ./internal/server/...`
Expected: FAIL (`404` on `/api/v1/system/update`, missing `version`/`boot_id` on `/health`).

- [ ] **Step 3: Implement `handlers_update.go`, wire `router.go`, `server.go`, and `cmd/coursefin/main.go`**

- `internal/api/handlers_update.go`: implement `handleGetUpdateStatus`, `handleUpdateUpdateConfig`, `handleCheckUpdate`, `handleApplyUpdate` (return `503` if `h.updater == nil`).
- `internal/api/router.go`: add `Updater *updater.Service` to `Deps` and `handler`; register the 4 routes inside the `requireAuth` group.
- `internal/server/server.go`:
  - Generate `bootID` (`crypto/rand` 8 bytes hex) in `New`.
  - Resolve version (`updater.NormalizeVersion(cfg.Version)`, fallback `"0.0.0-dev"`).
  - Create `updaterSvc := updater.NewService(...)`, pass to `api.NewHandler`, start `updaterSvc.StartPeriodicLoop(ctx)` under `srv.wg`, wait on `updaterSvc.Wait()` in `Close()`, expose `func (s *Server) RestartCh() <-chan updater.RestartRequest`.
  - Update `/health` handler to emit `{"status":"ok","version":...,"boot_id":...}\n` with `Cache-Control: no-store`.
- `cmd/coursefin/main.go`:
  - Before `config.Load()`: if `len(os.Args) > 1` matches `--version`, `-version`, or `version`, print `updater.NormalizeVersion(version)` and `os.Exit(0)`.
  - Set `cfg.Version = version`.
  - Open listener via `ln, err := net.Listen("tcp", ":"+cfg.Port)` before starting `httpSrv.Serve(ln)`. Once listening succeeds, if `cfg.ManagedBinDir != ""`, remove `filepath.Join(cfg.ManagedBinDir, ".pending_update")`.
  - In `select`, add `case req := <-appSrv.RestartCh():` -> run graceful shutdown (`httpSrv.Shutdown(15s)` + `appSrv.Close()`), then call `updater.ExecReplace(req.ExecPath, os.Args, os.Environ())`; if `ExecReplace` fails, log error, try restoring `req.BackupPath` -> `req.ExecPath` and calling `ExecReplace`, otherwise `os.Exit(1)`.

- [ ] **Step 4: Run Go tests and verify CLI `--version`**

Run: `go test -race -v ./internal/api/... ./internal/server/... ./internal/updater/... && go run ./cmd/coursefin --version`
Expected: All tests PASS and `--version` prints `1.0.1`.

- [ ] **Step 5: Commit Task 3**

```bash
git add internal/api/handlers_update.go internal/api/router.go internal/api/api_test.go internal/server/server.go internal/server/server_test.go cmd/coursefin/main.go
git commit -m "feat(server): expose system update API, health boot metadata, and hot-exec restart"
```

---

### Task 4: Docker Entrypoint (`scripts/docker-entrypoint.sh`), Build Version Injection & GitHub Release Workflow

**Files:**
- Create: `scripts/docker-entrypoint.sh`
- Create: `internal/updater/entrypoint_test.go`
- Create: `.github/workflows/release.yml`
- Modify: `Dockerfile:13-45`
- Modify: `Makefile:1-22`
- Modify: `docker-compose.yml:1-15`

**Interfaces:**
- Consumes: `coursefin --version` CLI contract from Task 3, `.pending_update` marker from Task 2.
- Produces:
  - Executable `/usr/local/bin/coursefin-entrypoint.sh` exporting `COURSEFIN_MANAGED_BIN_DIR="${COURSEFIN_DATA_DIR:-/data}/updates/bin"` and `exec`-ing the selected binary as PID 1.
  - `.github/workflows/release.yml` producing `coursefin_<VERSION>_<GOOS>_<GOARCH>.tar.gz` (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`) + `checksums.txt`.

- [ ] **Step 1: Write failing entrypoint shell behavior tests in `internal/updater/entrypoint_test.go`**

Skip on Windows (`runtime.GOOS == "windows"`). Invoke `sh ../../scripts/docker-entrypoint.sh` with overridden `COURSEFIN_IMAGE_BIN` and `COURSEFIN_DATA_DIR` pointing to temp fake binaries (each prints its version on `--version` and prints `RUNNING:<label>:$COURSEFIN_MANAGED_BIN_DIR` on normal exec), verifying all 5 scenarios:
1. Fresh `/data` (no updated binary) -> executes `IMAGE_BIN` and exports `COURSEFIN_MANAGED_BIN_DIR=<data>/updates/bin`.
2. Active `/data/updates/bin/coursefin` (`1.2.0`) > `IMAGE_BIN` (`1.0.1`) -> executes active binary.
3. Newer `IMAGE_BIN` (`1.3.0`) >= active binary (`1.2.0`) -> executes `IMAGE_BIN` (image upgrade automatically supersedes older `/data` binary).
4. Corrupt active binary (fails `--version`) -> falls back to valid `BACKUP_BIN` (if newer than image) or `IMAGE_BIN`.
5. Crash-loop guard: `.pending_update` contains `"tried"`, active binary is `1.2.0`, backup is `1.1.0`, image is `1.0.1` -> restores backup `1.1.0` to active binary, deletes `.pending_update`, and executes `1.1.0` (and when no backup exists, deletes active binary and falls back to `IMAGE_BIN`).

- [ ] **Step 2: Run entrypoint tests to verify failure**

Run: `go test -v -run TestDockerEntrypoint ./internal/updater/...`
Expected: FAIL (`scripts/docker-entrypoint.sh` does not exist).

- [ ] **Step 3: Create `scripts/docker-entrypoint.sh`, update `Dockerfile`, `Makefile`, `docker-compose.yml`, and create `.github/workflows/release.yml`**

- `scripts/docker-entrypoint.sh` (`chmod +x`): POSIX `#!/bin/sh` with `set -eu`, using `IMAGE_BIN="${COURSEFIN_IMAGE_BIN:-/usr/local/bin/coursefin}"`, `DATA_DIR="${COURSEFIN_DATA_DIR:-/data}"`, `BIN_DIR="${DATA_DIR}/updates/bin"`, `ACTIVE_BIN="${BIN_DIR}/coursefin"`, `BACKUP_BIN="${BIN_DIR}/coursefin.bak"`, `PENDING_FILE="${BIN_DIR}/.pending_update"`, and a pure `awk` SemVer `semver_gt()` helper comparing numeric `major.minor.patch` (ignoring leading `v` and prerelease suffix). Implements the exact flowchart from Spec Section 5.3.
- `Dockerfile`: add `ARG VERSION=1.0.1` in `go-builder`, pass `-ldflags="-s -w -X main.version=${VERSION}"`, `COPY scripts/docker-entrypoint.sh /usr/local/bin/coursefin-entrypoint.sh`, `chmod 0755`, `ENTRYPOINT ["/usr/local/bin/coursefin-entrypoint.sh"]`.
- `Makefile`: update `docker-build` to pass `--build-arg VERSION=$(VERSION)`.
- `docker-compose.yml`: add `args: VERSION: "1.0.1"` under `build:`.
- `.github/workflows/release.yml`: single `ubuntu-latest` job triggered on `push.tags: ['v*']` and `workflow_dispatch`; builds Web SPA once with Node 22 + pnpm 12, syncs into `internal/server/webdist`, cross-compiles `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` with `CGO_ENABLED=0` and `-X main.version=${VERSION}`, tars each `coursefin` binary (`0755`) into `coursefin_${VERSION}_${OS}_${ARCH}.tar.gz`, generates `checksums.txt`, and publishes via `softprops/action-gh-release@v2`.

- [ ] **Step 4: Run entrypoint tests and validate shell syntax**

Run: `sh -n scripts/docker-entrypoint.sh && go test -race -v -run TestDockerEntrypoint ./internal/updater/...`
Expected: PASS.

- [ ] **Step 5: Commit Task 4**

```bash
git add scripts/docker-entrypoint.sh internal/updater/entrypoint_test.go Dockerfile Makefile docker-compose.yml .github/workflows/release.yml
git commit -m "build(release): add Docker version-aware entrypoint and GitHub release workflow"
```

---

### Task 5: Frontend Types, API Client & Shared `UpdateContext` (with Restart Recovery)

**Files:**
- Modify: `web/src/types/coursefin.ts`
- Modify: `web/src/api/client.ts`
- Create: `web/src/components/UpdateContext.tsx`
- Modify: `web/src/App.tsx`
- Test: `web/src/__tests__/SystemUpdate.test.tsx`

**Interfaces:**
- Consumes: HTTP `/api/v1/system/update*` and `/health` from Task 3.
- Produces:
  - Types in `web/src/types/coursefin.ts`: `UpdateState`, `UpdateStatus`, `UpdateConfigInput`, `HealthStatus`.
  - Methods in `web/src/api/client.ts`: `api.getUpdateStatus()`, `api.updateUpdateConfig(input)`, `api.checkForUpdate()`, `api.applyUpdate()`, `api.getHealth()`.
  - `UpdateProvider`, `useUpdate()` from `web/src/components/UpdateContext.tsx` returning:
    `{ isProvided, status, loading, checking, applying, restartPhase, restartError, refreshStatus, checkNow, saveConfig, startApply }`
    where `restartPhase: 'idle' | 'waiting' | 'succeeded' | 'rolled_back' | 'timeout'`.

- [ ] **Step 1: Write failing `UpdateContext` tests in `web/src/__tests__/SystemUpdate.test.tsx`**

Test:
1. Safe standalone fallback: calling `useUpdate()` outside `<UpdateProvider>` does not throw and returns `status: null`, `restartPhase: 'idle'`.
2. Initial fetch + `startApply()` restart recovery (upgrade success): captures initial `boot_id` from `/health`, triggers `POST /api/v1/system/update/apply`, polls `GET /api/v1/system/update` (progressing `downloading` -> `restarting` or network error `TypeError`), switches to polling `/health`, and when `/health` returns `version === targetVersion` sets `restartPhase === 'succeeded'` and invokes injected `onReload` callback.
3. Restart recovery detects automatic rollback: when `/health` returns a new `boot_id` with the **old** `version` (`1.0.1` instead of `1.0.2`), transitions to `restartPhase === 'rolled_back'` with clear Chinese `restartError` and does NOT reload.

- [ ] **Step 2: Run Vitest to verify failure**

Run: `pnpm --dir web test -- SystemUpdate.test.tsx`
Expected: FAIL (missing `UpdateContext` module / API methods).

- [ ] **Step 3: Implement types, API client methods, `UpdateContext.tsx`, and mount in `App.tsx`**

- Add `UpdateState`, `UpdateStatus`, `UpdateConfigInput`, `HealthStatus` in `web/src/types/coursefin.ts` matching Section 5.5 JSON fields exactly.
- Add `getUpdateStatus`, `updateUpdateConfig`, `checkForUpdate`, `applyUpdate`, `getHealth` (with `cache: 'no-store'`, `authRequired = false`) in `web/src/api/client.ts`.
- Implement `UpdateContext.tsx` accepting optional props `pollIntervalMs` (default `800`), `healthPollIntervalMs` (default `1000`), `restartTimeoutMs` (default `60000`), and `onReload` (default `() => window.location.reload()`) for clean deterministic testing.
- Mount `<UpdateProvider>` inside `App.tsx` (inside `<ToastProvider>`, wrapping `<BrowserRouter>`).

- [ ] **Step 4: Run Vitest to verify pass**

Run: `pnpm --dir web test -- SystemUpdate.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit Task 5**

```bash
git add web/src/types/coursefin.ts web/src/api/client.ts web/src/components/UpdateContext.tsx web/src/App.tsx web/src/__tests__/SystemUpdate.test.tsx
git commit -m "feat(web): add update API client and UpdateContext with restart recovery"
```

---

### Task 6: Frontend UI — Top Navigation Indicator & Settings Update Card

**Files:**
- Create: `web/src/components/SystemUpdateSection.tsx`
- Modify: `web/src/components/Layout.tsx`
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/__tests__/SystemUpdate.test.tsx`

**Interfaces:**
- Consumes: `useUpdate()` from Task 5, `useToast()`, `Tooltip`, `api` from existing codebase.
- Produces:
  - `SystemUpdateSection` component (`id="system-update"`) rendered inside `SettingsPage.tsx`.
  - Top-bar update pill in `Layout.tsx` (desktop pill + mobile emerald dot when `status?.update_available`).

- [ ] **Step 1: Write failing UI tests in `web/src/__tests__/SystemUpdate.test.tsx`**

Add tests covering:
1. `Layout` renders clickable pill `新版本 v1.0.2` (and mobile dot) when `update_available === true`, and hides it when `update_available === false`; clicking navigates to `/settings#system-update`.
2. `SettingsPage` + `SystemUpdateSection` displays current version, deploy mode (`Docker 容器` / `本地二进制`), release notes (rendered as safe text, never executing raw `<script>`/`<img onerror>`), «检查更新» button, «立即更新到 v1.0.2» button opening a confirmation modal before calling `POST /api/v1/system/update/apply`, and live download progress bar (`60%`).
3. Update config controls inside `SystemUpdateSection`: toggles auto-check (disabled when `auto_check_locked_by_env === true`), selects check interval, enters mirror URL, saves via `PUT /api/v1/system/update/config` with toast confirmation.
4. Unsupported/cannot-apply state (`can_apply: false`): hides/disables «立即更新» button, displays `cannot_apply_reason` and GitHub Releases external link.

- [ ] **Step 2: Run Vitest to verify failure**

Run: `pnpm --dir web test -- SystemUpdate.test.tsx`
Expected: FAIL (`SystemUpdateSection` missing, `Layout` pill missing).

- [ ] **Step 3: Implement `SystemUpdateSection.tsx` and integrate into `Layout.tsx` and `SettingsPage.tsx`**

- `Layout.tsx`: call `useUpdate()` (and trigger `refreshStatus()` once on authenticated mount if `status === null`); when `status?.update_available`, render desktop emerald pill linking to `/settings#system-update` wrapped in `<Tooltip>` and mobile dot on «设置».
- `SystemUpdateSection.tsx`:
  - Supports both `<UpdateProvider>` mode and standalone fallback (when rendered inside `<SettingsPage />` without `<UpdateProvider>`, quietly fetches `api.getUpdateStatus().catch(() => null)` so existing `SettingsPage` tests never fail).
  - Implements all elements from Spec Section 5.6: version/mode/platform badges, up-to-date vs new-version card, safe plain-text release notes (`whitespace-pre-wrap`), confirmation modal before apply, live progress bar (`downloading`/`verifying`/`applying`/`restarting`), restart-waiting/rollback/timeout banners (with manual «立即刷新» button on timeout), and config sub-form (`auto_check_enabled`, `check_interval_hours` select `1/6/12/24`, `mirror_url` input with security helper text, «保存更新配置»).
- `SettingsPage.tsx`: render `<SystemUpdateSection />` directly below «Jellyfin 客户端连接指南»; scroll `#system-update` into view when `window.location.hash === '#system-update'`.

- [ ] **Step 4: Run full frontend test suite to verify zero regressions**

Run: `pnpm --dir web test`
Expected: All existing and new Vitest tests PASS.

- [ ] **Step 5: Commit Task 6**

```bash
git add web/src/components/SystemUpdateSection.tsx web/src/components/Layout.tsx web/src/pages/SettingsPage.tsx web/src/__tests__/SystemUpdate.test.tsx
git commit -m "feat(web): add top-bar update badge and Settings system update module"
```

---

### Task 7: Documentation, Embedded SPA Sync & Full Verification

**Files:**
- Modify: `.env.example`
- Modify: `SECURITY.md`
- Modify: `README.md`
- Modify: `internal/server/webdist/*` (generated by `make sync-web`)

**Interfaces:**
- Consumes: All code and workflows from Tasks 1–6.
- Produces: Synchronized embedded frontend assets, compiled `coursefin` binary, updated documentation, and green full Go + Vitest suites.

- [ ] **Step 1: Update `.env.example`, `SECURITY.md`, and `README.md`**

- `.env.example`: document optional `COURSEFIN_UPDATE_REPO`, `COURSEFIN_UPDATE_AUTO_CHECK`, `COURSEFIN_GITHUB_TOKEN`.
- `SECURITY.md`: add Section 6 documenting the self-update security boundary (admin-only API, mandatory `SHA-256` verification, `--version` pre-flight, zero `docker.sock`, trust caution for custom mirror proxy URLs).
- `README.md`: document the built-in automatic update detection & one-click self-update capability and new environment variables in the table.

- [ ] **Step 2: Rebuild & sync embedded SPA assets and build binary**

Run: `make build && ./coursefin --version`
Expected: Web build succeeds, `internal/server/webdist` synced, `./coursefin` builds cleanly, `./coursefin --version` outputs `1.0.1`.

- [ ] **Step 3: Run full Go (`-race`) and frontend test suites**

Run: `make test`
Expected: All Go packages (`-race -count=1`) and all Vitest tests PASS with zero failures.

- [ ] **Step 4: Commit Task 7**

```bash
git add .env.example SECURITY.md README.md internal/server/webdist
git commit -m "docs: document internal self-update module and sync embedded web assets"
```
