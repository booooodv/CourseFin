# CourseFin UX Optimizations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a server-side local directory picker in «存储与导入», consistent hover tooltips + global Toast action feedback (with HTTP-compatible clipboard copying) across all buttons, and library rename/delete (supporting empty, migrate, and force-delete flows) on the home page.

**Architecture:** Extend the Go `/api/v1` router with an authenticated directory-browsing endpoint (`GET /api/v1/fs/dirs`) and transactional migration/force options on `DELETE /api/v1/libraries/{id}`. On the React frontend, add zero-dependency `Toast`, `Tooltip`, and `clipboard` primitives, then build `DirectoryPickerModal` and `DeleteLibraryModal` and roll out tooltips + toasts across all pages and modals.

**Tech Stack:** Go 1.24 (`chi`, SQLite), React 19, TypeScript, Tailwind CSS v4, Lucide icons, Vitest + React Testing Library.

**Spec:** `docs/superpowers/specs/2026-11-24-ux-optimizations-design.md`

## Global Constraints

- Zero new external npm dependencies; use React, Tailwind CSS v4, and existing `lucide-react` icons only.
- Never modify, move, or delete user media files on disk; `force=true` library deletion only deletes DB records and app-managed cover/backdrop files inside `dataDir/covers`.
- `DELETE /api/v1/libraries/{id}` with no query params must preserve its existing `409 Conflict` behavior for non-empty libraries.
- `GET /api/v1/fs/dirs` must require admin auth (`requireAuth`) and list directories only (never read or serve file contents).
- `Tooltip` and `useToast` must remain 100% compatible with standalone component rendering in existing Vitest tests (no mandatory provider crash, no changes to button accessible names).
- Web UI copy is Chinese-first, concise, and action-oriented.

## Review Focus

1. **Broken symlinks & permission-denied directories in `GET /api/v1/fs/dirs`:** Broken symlinks inside a browsed directory must be skipped silently instead of failing the listing; browsing a permission-denied target directory must return HTTP `403`. (Tested in Task 1)
2. **Invalid pre-filled path when opening `DirectoryPickerModal`:** Typing a typo'd path in «本地目录绝对路径» and clicking «浏览目录» must automatically fall back to the server default path with a gentle hint rather than stranding the user on an error screen. (Tested in Task 4)
3. **Plain HTTP LAN clipboard copy (`navigator.clipboard` missing or throwing):** Clicking «复制» must fall back to `document.execCommand('copy')` and show a success Toast, or show an explicit error Toast if both methods fail—never fail silently. (Tested in Tasks 3 & 6)
4. **Deleting the currently active library or the last remaining library with courses:** When no other library exists, the migrate radio option must be disabled with an explanation while force-delete remains usable; deleting the active library must reset `selectedLibraryId` to `'all'` and refresh counts/courses/resume state. (Tested in Task 5)
5. **Tooltips inside overflow/scroll containers & accessible names:** Portaled tooltips must appear on hover/focus without clipping and must never alter the trigger button's accessible name (`getByRole('button', { name })`). (Tested in Task 3)

---

## File Structure

- **Create `internal/api/handlers_fs.go`:** Authenticated server-side directory listing handler (`handleBrowseFSDirs`) and root-shortcut discovery.
- **Modify `internal/api/router.go`:** Register `GET /api/v1/fs/dirs`.
- **Modify `internal/catalog/service.go`:** Add `DeleteLibraryOptions` and `DeleteLibraryWithOptions` (supporting default restrict, `MoveToLibraryID`, and `Force` with `dataDir/covers` cleanup).
- **Modify `internal/api/handlers_catalog.go`:** Parse `move_to_library_id` and `force` query params in `handleDeleteLibrary`.
- **Modify `web/src/types/coursefin.ts`:** Add `FsDirEntry`, `FsRootShortcut`, `FsBrowseResponse`, `DeleteLibraryOptions`.
- **Modify `web/src/api/client.ts`:** Add `api.browseFsDirs` and extend `api.deleteLibrary`.
- **Create `web/src/utils/clipboard.ts`:** `copyTextToClipboard` with Clipboard API + `execCommand` fallback.
- **Create `web/src/components/Toast.tsx`:** `ToastProvider`, `useToast`, floating viewport stack.
- **Create `web/src/components/Tooltip.tsx`:** Portaled hover/focus tooltip wrapper.
- **Modify `web/src/App.tsx`:** Wrap app in `<ToastProvider>`.
- **Create `web/src/components/DirectoryPickerModal.tsx`:** Interactive server directory browser modal.
- **Modify `web/src/pages/StoragePage.tsx`:** Integrate «浏览目录» button, auto-fill source name, tooltips, and toasts.
- **Create `web/src/components/DeleteLibraryModal.tsx`:** Confirmation dialog supporting empty, migrate, and force-delete flows.
- **Modify `web/src/pages/HomePage.tsx`:** Add rename/delete icon controls on library pills, inline rename bar, delete modal integration, tooltips, and toasts.
- **Modify `web/src/pages/SettingsPage.tsx`:** Upgrade copy button with `copyTextToClipboard` + Toast, add username copy button, add tooltips & toasts.
- **Modify `web/src/components/Layout.tsx`, `web/src/pages/CourseDetailPage.tsx`, `web/src/pages/CourseEditorPage.tsx`, `web/src/components/VideoPlayer.tsx`, `web/src/components/ImportCourseModal.tsx`, `web/src/components/AIOrganizeModal.tsx`:** Roll out concise tooltips on buttons and Toast feedback on key mutations.
- **Tests:** `internal/api/api_test.go`, `internal/catalog/catalog_test.go`, `web/src/__tests__/UXPrimitives.test.tsx`, `web/src/__tests__/UXFeatures.test.tsx`.

---

### Task 1: Backend Local Directory Browser (`GET /api/v1/fs/dirs`)

**Files:**
- Create: `internal/api/handlers_fs.go`
- Modify: `internal/api/router.go:102-112`
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: `h.requireAuth`, `catalog.NaturalCompare`, `writeJSON`, `writeError`.
- Produces: HTTP `GET /api/v1/fs/dirs?path=<abs>&show_hidden=<bool>` returning:
  ```go
  type fsDirEntry struct { Name string `json:"name"`; Path string `json:"path"` }
  type fsRootShortcut struct { Label string `json:"label"`; Path string `json:"path"` }
  type fsBrowseResponse struct {
      CurrentPath string           `json:"current_path"`
      ParentPath  string           `json:"parent_path"`
      Entries     []fsDirEntry     `json:"entries"`
      Roots       []fsRootShortcut `json:"roots"`
  }
  ```

- [ ] **Step 1: Write failing API tests in `internal/api/api_test.go`**

Add `TestBrowseFSDirs` covering:
1. Unauthenticated `GET /api/v1/fs/dirs` returns `401`.
2. Authenticated `GET /api/v1/fs/dirs` with no `path` returns `200`, non-empty `current_path`, and non-empty `roots`.
3. Authenticated `GET /api/v1/fs/dirs?path=<tempDir>` with subdirectories `10_go`, `2_rust`, `.hidden`, a regular file `readme.txt`, a symlink to a subdir `link_dir`, and a broken symlink `broken_link`:
   - Default (`show_hidden` omitted) returns `["2_rust", "10_go", "link_dir"]` (natural sort, files & `.hidden` & `broken_link` excluded).
   - `show_hidden=true` includes `.hidden`.
   - `parent_path` equals `filepath.Dir(tempDir)`.
4. Browsing filesystem root (`/` on Unix) returns `parent_path == ""`.
5. Relative `path=relative/dir` returns `400`; regular file path returns `400`; nonexistent path returns `404`; unreadable `0000` dir (when not running as root) returns `403`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestBrowseFSDirs ./internal/api/...`
Expected: FAIL (`404` route not found).

- [ ] **Step 3: Implement `internal/api/handlers_fs.go` and register route in `internal/api/router.go`**

- Register `pr.Get("/fs/dirs", h.handleBrowseFSDirs)` inside the `requireAuth` group in `internal/api/router.go`.
- In `internal/api/handlers_fs.go`, implement `handleBrowseFSDirs`, `defaultBrowsePath()`, `availableFSRoots()`, and `isDirEntryDirectory(parentDir string, entry fs.DirEntry) bool`:
  - `availableFSRoots()`: check `/media` (label `"媒体目录 (/media)"`), `os.UserHomeDir()` (label `"主目录"`), and filesystem root (`/` with label `"根目录 (/)"`, or Windows drive letters `A:\`..`Z:\`), deduplicating by cleaned path and keeping only existing directories.
  - `defaultBrowsePath()`: first existing dir from `["/media", homeDir, root]`.
  - Resolve `parent_path`: `parent := filepath.Dir(cleanPath); if parent == cleanPath { parent = "" }`.
  - Sort `entries` by `catalog.NaturalCompare(a.Name, b.Name) < 0`.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -race -v -run TestBrowseFSDirs ./internal/api/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-11-24-ux-optimizations-design.md docs/superpowers/plans/2026-11-24-ux-optimizations.md internal/api/handlers_fs.go internal/api/router.go internal/api/api_test.go
git commit -m "feat(api): add authenticated server directory browser endpoint"
```

---

### Task 2: Backend Enhanced Library Deletion (`move_to_library_id` & `force`)

**Files:**
- Modify: `internal/catalog/service.go:350-366`
- Modify: `internal/api/handlers_catalog.go:75-82`
- Test: `internal/catalog/catalog_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: `Service.db`, `Service.dataDir`, `ErrConflict`, `ErrNotFound`, `ErrInvalidInput`.
- Produces:
  - `type DeleteLibraryOptions struct { MoveToLibraryID string; Force bool }`
  - `func (s *Service) DeleteLibraryWithOptions(ctx context.Context, libraryID string, opts DeleteLibraryOptions) error`
  - `func (s *Service) DeleteLibrary(ctx context.Context, libraryID string) error` (unchanged signature, delegates with empty options)
  - HTTP `DELETE /api/v1/libraries/{id}?move_to_library_id=<id>` and `DELETE /api/v1/libraries/{id}?force=true`

- [ ] **Step 1: Write failing tests in `internal/catalog/catalog_test.go` and `internal/api/api_test.go`**

1. In `internal/catalog/catalog_test.go`, add `TestDeleteLibraryWithOptions`:
   - Empty library deletes cleanly with default options.
   - Non-empty library returns `ErrConflict` with default options.
   - Passing both `MoveToLibraryID` and `Force: true` returns `ErrInvalidInput`.
   - `MoveToLibraryID == libraryID` returns `ErrInvalidInput`; nonexistent `MoveToLibraryID` returns `ErrNotFound`.
   - Valid `MoveToLibraryID` atomically moves all courses to target library, deletes source library, and preserves chapters/lessons.
   - `Force: true` deletes source library + all its courses/chapters/lessons, deletes app-managed cover file in `dataDir/covers`, and leaves `media_files` intact.
2. In `internal/api/api_test.go`, add `TestDeleteLibraryAPIModes`:
   - `DELETE /api/v1/libraries/{id}` with courses returns `409`.
   - `DELETE /api/v1/libraries/{id}?move_to_library_id={other}&force=true` returns `400`.
   - `DELETE /api/v1/libraries/{id}?move_to_library_id={other}` returns `200` and moves courses to `{other}`.
   - `DELETE /api/v1/libraries/{id}?force=true` returns `200` and removes library + its courses.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestDeleteLibraryWithOptions|TestDeleteLibraryAPIModes" ./internal/catalog/... ./internal/api/...`
Expected: FAIL (compile error: `DeleteLibraryOptions` / `DeleteLibraryWithOptions` undefined).

- [ ] **Step 3: Implement `DeleteLibraryWithOptions` and update `handleDeleteLibrary`**

- In `internal/catalog/service.go`:
  - Add `DeleteLibraryOptions` struct and `DeleteLibraryWithOptions(ctx, libraryID, opts)` using a SQLite transaction for `MoveToLibraryID` and `Force` modes.
  - On `Force`, collect `cover_path, backdrop_path` before deleting courses, execute `DELETE FROM courses WHERE library_id = ?` then `DELETE FROM libraries WHERE id = ?`, commit, then remove non-empty `filepath.Base(p)` files from `filepath.Join(s.dataDir, "covers")`.
- In `internal/api/handlers_catalog.go`:
  - Parse `q := r.URL.Query()`: `moveTo := strings.TrimSpace(q.Get("move_to_library_id"))`, `forceRaw := strings.ToLower(strings.TrimSpace(q.Get("force")))`, `force := forceRaw == "true" || forceRaw == "1"`, call `h.cat.DeleteLibraryWithOptions(...)`.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -race -count=1 ./internal/catalog/... ./internal/api/...`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/catalog/service.go internal/api/handlers_catalog.go internal/catalog/catalog_test.go internal/api/api_test.go
git commit -m "feat(catalog): support course migration and force delete when deleting libraries"
```

---

### Task 3: Frontend Primitives (`Toast`, `Tooltip`, `clipboard`) & API Client Extensions

**Files:**
- Modify: `web/src/types/coursefin.ts`
- Modify: `web/src/api/client.ts:240-244, 394-441`
- Create: `web/src/utils/clipboard.ts`
- Create: `web/src/components/Toast.tsx`
- Create: `web/src/components/Tooltip.tsx`
- Modify: `web/src/App.tsx`
- Create: `web/src/__tests__/UXPrimitives.test.tsx`

**Interfaces:**
- Consumes: `request`, `toQueryString` in `web/src/api/client.ts`.
- Produces:
  - Types in `web/src/types/coursefin.ts`: `FsDirEntry`, `FsRootShortcut`, `FsBrowseResponse`, `DeleteLibraryOptions`
  - API methods in `web/src/api/client.ts`:
    - `api.browseFsDirs(path?: string, showHidden?: boolean): Promise<FsBrowseResponse>`
    - `api.deleteLibrary(id: string, options?: DeleteLibraryOptions): Promise<{ ok: boolean }>`
  - `copyTextToClipboard(text: string): Promise<boolean>` in `web/src/utils/clipboard.ts`
  - `ToastProvider` & `useToast(): { toast: { success(msg: string): void; error(msg: string): void; info(msg: string): void } }` in `web/src/components/Toast.tsx`
  - `Tooltip({ content, placement, children }: { content?: string; placement?: 'top' | 'bottom'; children: ReactElement })` in `web/src/components/Tooltip.tsx`

- [ ] **Step 1: Write failing tests in `web/src/__tests__/UXPrimitives.test.tsx`**

Test cases:
1. `copyTextToClipboard` uses `navigator.clipboard.writeText` when available and returns `true`.
2. `copyTextToClipboard` falls back to `document.execCommand('copy')` when `navigator.clipboard` is undefined or rejects, and returns `true` when `execCommand` returns `true` and `false` when both fail.
3. `ToastProvider` + `useToast` renders floating success/error/info toasts (`role="status"`), allows dismissing via close button, and `useToast` called without `<ToastProvider>` does not throw.
4. `Tooltip` renders tooltip (`role="tooltip"`) on mouse hover and keyboard focus, hides on mouse leave/blur, and preserves the wrapped button's accessible name (`getByRole('button', { name: '复制' })`).
5. `api.browseFsDirs('/media', true)` calls `GET /api/v1/fs/dirs?path=%2Fmedia&show_hidden=true`; `api.deleteLibrary('lib-1', { moveToLibraryId: 'lib-2' })` calls `DELETE /api/v1/libraries/lib-1?move_to_library_id=lib-2`; `api.deleteLibrary('lib-1', { force: true })` calls `DELETE /api/v1/libraries/lib-1?force=true`.

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --dir web test src/__tests__/UXPrimitives.test.tsx`
Expected: FAIL (modules do not exist).

- [ ] **Step 3: Implement types, API client methods, `clipboard.ts`, `Toast.tsx`, `Tooltip.tsx`, and wire `<ToastProvider>` in `App.tsx`**

- `Tooltip.tsx`: Clone single child element (preserving its existing event handlers `onMouseEnter`, `onMouseLeave`, `onFocus`, `onBlur`) or wrap in an `inline-flex` span; use a small `120ms` hover open timer (instant on focus), compute viewport-clamped position from `getBoundingClientRect()` (auto-flipping top↔bottom), render via `createPortal` into `document.body` with `role="tooltip"` and link `aria-describedby` while open. If `content` is empty/undefined, render `children` as-is.
- `Toast.tsx`: Manage up to 4 active toasts with `2800ms` auto-dismiss timers (cleaned up on unmount), floating fixed top-right (`z-[60]`), distinct emerald/rose/indigo accents. Provide a safe no-op default context value.
- `App.tsx`: Wrap `<BrowserRouter>` content with `<ToastProvider>`.

- [ ] **Step 4: Run tests to verify pass**

Run: `pnpm --dir web test src/__tests__/UXPrimitives.test.tsx`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/types/coursefin.ts web/src/api/client.ts web/src/utils/clipboard.ts web/src/components/Toast.tsx web/src/components/Tooltip.tsx web/src/App.tsx web/src/__tests__/UXPrimitives.test.tsx
git commit -m "feat(web): add Toast, Tooltip, clipboard fallback, and API client extensions"
```

---

### Task 4: Frontend Local Directory Picker (`DirectoryPickerModal` + `StoragePage`)

**Files:**
- Create: `web/src/components/DirectoryPickerModal.tsx`
- Modify: `web/src/pages/StoragePage.tsx`
- Create: `web/src/__tests__/UXFeatures.test.tsx`

**Interfaces:**
- Consumes: `api.browseFsDirs`, `FsBrowseResponse`, `Tooltip`, `useToast`.
- Produces:
  - `DirectoryPickerModal({ isOpen, initialPath, onClose, onSelect }: { isOpen: boolean; initialPath?: string; onClose: () => void; onSelect: (selectedPath: string) => void })`

- [ ] **Step 1: Write failing tests in `web/src/__tests__/UXFeatures.test.tsx`**

Add `describe('DirectoryPickerModal & StoragePage')`:
1. Clicking «添加存储源» then «浏览目录» opens the modal, loads `/api/v1/fs/dirs`, displays root shortcuts and subdirectories, navigates into a clicked subdirectory, and confirms «选择此目录»: populates `#source-path-input` AND auto-fills `#source-name-input` with the folder basename when empty (does not overwrite if user already typed a custom name).
2. If `#source-path-input` contains an invalid path before clicking «浏览目录» (first `browseFsDirs(badPath)` returns 404), modal automatically falls back to `browseFsDirs()` default path and displays a fallback notice instead of staying broken.
3. Toggling «显示隐藏目录» re-queries with `show_hidden=true`; typing in directory filter filters displayed subdirectories.
4. Clicking «删除存储源» / «扫描» / adding a source emits appropriate Toast messages and buttons render tooltips on hover.

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --dir web test src/__tests__/UXFeatures.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement `DirectoryPickerModal.tsx` and update `StoragePage.tsx`**

- `DirectoryPickerModal.tsx`:
  - Modal dialog (`role="dialog"`, `aria-modal="true"`, `aria-labelledby="dir-picker-title"`).
  - Supports Escape key close, root shortcut chips, «返回上级» button (disabled when `!parent_path`), clickable breadcrumb segments, «显示隐藏目录» toggle, local filter input for `entries`, scrollable folder list, and footer showing current path + «取消» + «选择此目录».
- `StoragePage.tsx`:
  - Place «浏览目录» button inline beside `#source-path-input`.
  - When `onSelect(selectedPath)` fires: `setSourceRootPath(selectedPath)`; if `!sourceName.trim()`, derive basename from `selectedPath` (splitting `/` and `\`, trimming trailing slashes) and set `sourceName`; show `toast.info('已选择目录路径')`.
  - Wrap all action buttons in `StoragePage.tsx` with concise `Tooltip`s and add `toast.success` / `toast.error` feedback for create source, delete source, start scan, and course imported.

- [ ] **Step 4: Run tests to verify pass**

Run: `pnpm --dir web test`
Expected: Both existing (`EditorAndAI.test.tsx`, `HomePage.test.tsx`, `VideoPlayer.test.tsx`) and new tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/DirectoryPickerModal.tsx web/src/pages/StoragePage.tsx web/src/__tests__/UXFeatures.test.tsx
git commit -m "feat(web): add server directory picker modal to StoragePage"
```

---

### Task 5: Frontend Library Delete & Rename (`DeleteLibraryModal` + `HomePage`)

**Files:**
- Create: `web/src/components/DeleteLibraryModal.tsx`
- Modify: `web/src/pages/HomePage.tsx`
- Modify: `web/src/__tests__/UXFeatures.test.tsx`

**Interfaces:**
- Consumes: `api.deleteLibrary`, `api.updateLibrary`, `Library`, `Tooltip`, `useToast`.
- Produces:
  - `DeleteLibraryModal({ library, libraries, onClose, onDeleted }: { library: Library | null; libraries: Library[]; onClose: () => void; onDeleted: (deleted: Library, mode: 'empty' | 'move' | 'force', targetLibraryName?: string) => void })`

- [ ] **Step 1: Write failing tests in `web/src/__tests__/UXFeatures.test.tsx`**

Add `describe('HomePage library delete & rename')`:
1. Deleting an empty library (`course_count: 0`): clicking `删除媒体库 <name>` opens confirmation modal, confirming calls `DELETE /api/v1/libraries/<id>` (no query params), removes library from tabs, resets active filter to `'all'`, and shows a success toast.
2. Deleting a non-empty library when another library exists: modal defaults to «迁移课程到其他媒体库并删除», lets user pick target library and confirm, calls `DELETE /api/v1/libraries/<id>?move_to_library_id=<target>`, reloads data, and shows a migration+delete toast.
3. Deleting a non-empty library via force mode (including when it is the **only** library so migrate option is disabled): selecting «连带删除这 N 门课程记录» and confirming calls `DELETE /api/v1/libraries/<id>?force=true`, reloads data, and shows a success toast.
4. Renaming a library: clicking `重命名媒体库 <name>` opens inline rename input prefilled with current name, submitting calls `PUT /api/v1/libraries/<id>`, updates tab label, and shows a success toast.

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --dir web test src/__tests__/UXFeatures.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement `DeleteLibraryModal.tsx` and update `HomePage.tsx`**

- `DeleteLibraryModal.tsx`:
  - Renders when `library !== null`.
  - Computes `otherLibraries = libraries.filter((l) => l.id !== library.id)`.
  - Empty library: simple confirmation + note `"不会影响本地硬盘上的视频文件"`.
  - Non-empty library: two radio options:
    - `'move'`: `"迁移课程到其他媒体库并删除"` + `<select>` of `otherLibraries` (disabled with hint `"暂无其他媒体库可迁入，可先新建媒体库或选择下方删除"` when `otherLibraries.length === 0`).
    - `'force'`: `"连带删除这 ${library.course_count} 门课程记录"` + warning `"仅删除 CourseFin 中的课程目录与观看进度，绝不删除本地视频文件"`.
  - Default mode: `library.course_count === 0 ? 'empty' : otherLibraries.length > 0 ? 'move' : 'force'`.
- `HomePage.tsx`:
  - Render each library tab as a pill container (`group inline-flex items-center rounded-xl ...`) with the main select button (`{lib.name} ({lib.course_count})`) plus compact `Pencil` (`aria-label="重命名媒体库 ${lib.name}"`) and `Trash2` (`aria-label="删除媒体库 ${lib.name}"`) icon buttons wrapped in `Tooltip` (always visible on active tab and on hover/focus-within).
  - Add inline rename form panel (closes create-library form when opened and vice versa).
  - Wire `DeleteLibraryModal`: on delete success, if `selectedLibraryId === deleted.id` reset to `'all'`, update `newCourseLibraryId` if it matched `deleted.id`, call `loadData()` to keep library counts / courses / tags / resume rail consistent, and fire `toast.success(...)`.
  - Add `Tooltip`s to all HomePage buttons (`全部`, library tabs, `新建媒体库`, `新建空白课程`, `清空搜索`, tags, `开始学习`, `编辑`) and `toast.success` on create library.

- [ ] **Step 4: Run tests to verify pass**

Run: `pnpm --dir web test`
Expected: All PASS (including `HomePage.test.tsx`!).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/DeleteLibraryModal.tsx web/src/pages/HomePage.tsx web/src/__tests__/UXFeatures.test.tsx
git commit -m "feat(web): add library deletion modal and inline rename on HomePage"
```

---

### Task 6: Whole-App Button Tooltips, Toast Feedback & Settings Copy Enhancement

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/components/Layout.tsx`
- Modify: `web/src/pages/CourseDetailPage.tsx`
- Modify: `web/src/pages/CourseEditorPage.tsx`
- Modify: `web/src/components/VideoPlayer.tsx`
- Modify: `web/src/components/ImportCourseModal.tsx`
- Modify: `web/src/components/AIOrganizeModal.tsx`
- Modify: `web/src/__tests__/UXFeatures.test.tsx`

**Interfaces:**
- Consumes: `Tooltip`, `useToast`, `copyTextToClipboard`.
- Produces: Consistent button hover guidance and Toast feedback across all pages and modals.

- [ ] **Step 1: Write failing tests in `web/src/__tests__/UXFeatures.test.tsx`**

Add `describe('SettingsPage copy fallback & tooltips')`:
1. When `navigator.clipboard` is unavailable (simulating HTTP LAN) and `document.execCommand` returns `true`, clicking «复制» on Server URL shows «已复制» AND renders Toast `"已复制服务器地址到剪贴板"`.
2. When both clipboard API and `execCommand` fail, clicking «复制» shows error Toast `"复制失败，请手动选中复制"` instead of failing silently.
3. Clicking copy button beside Jellyfin username (`aria-label="复制用户名"`) copies username and shows Toast `"已复制用户名到剪贴板"`.
4. Hovering the Server URL «复制» button displays its tooltip (`role="tooltip"`).

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --dir web test src/__tests__/UXFeatures.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Update `SettingsPage.tsx` and roll out `Tooltip` + `useToast` across remaining pages and modals**

- `SettingsPage.tsx`:
  - Replace raw `navigator.clipboard.writeText` with `copyTextToClipboard`, emit `toast.success` / `toast.error`.
  - Add a copy button next to Jellyfin username (`aria-label="复制用户名"`, tooltip `"复制 Jellyfin 登录用户名"`).
  - Add `Tooltip` to every action button (复制服务器地址, 复制用户名, 测试连接, 保存设置, 更新密码, 注销会话) and `toast.success`/`toast.error` on save settings, test AI, change password, revoke session.
- `Layout.tsx`: Add `Tooltip` to nav items and «退出登录».
- `CourseDetailPage.tsx`: Add `Tooltip` to «返回课程库», «收起/展开目录», «编辑课程», «上一节», «下一节», «标记已看/未看»; add `toast.success`/`toast.error` on `handleTogglePlayed`.
- `CourseEditorPage.tsx`: Add `Tooltip` to «返回课程播放», «检查新文件», «AI 智能整理», «删除课程», «添加标签», «保存元数据», «上传封面», «从课时帧选取封面», «上传背景图», «添加章节», and all chapter/lesson icon buttons (拖拽排序, 从存储添加视频, 上移, 下移, 重命名, 删除, 关闭弹窗); fire `toast.success` in `showNotice` and `toast.error` on errors.
- `VideoPlayer.tsx`: Add `Tooltip` to «全屏» button (`"切换全屏播放（快捷键 F）"`), plus tooltips on倍速 & 字幕 controls.
- `ImportCourseModal.tsx` & `AIOrganizeModal.tsx`: Add `Tooltip` to header close, inline create library, add/remove tag, regenerate, confirm buttons, and `toast.success` on inline library creation.

- [ ] **Step 4: Run tests to verify pass**

Run: `pnpm --dir web test`
Expected: All PASS across all test suites.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/SettingsPage.tsx web/src/components/Layout.tsx web/src/pages/CourseDetailPage.tsx web/src/pages/CourseEditorPage.tsx web/src/components/VideoPlayer.tsx web/src/components/ImportCourseModal.tsx web/src/components/AIOrganizeModal.tsx web/src/__tests__/UXFeatures.test.tsx
git commit -m "feat(web): add button tooltips, toast feedback, and HTTP clipboard fallback"
```

---

### Task 7: Sync Embedded Web Assets & Full End-to-End Verification

**Files:**
- Modify: `internal/server/webdist/*` (generated via `make sync-web`)
- Binary: `coursefin` (rebuilt via `make build`)

- [ ] **Step 1: Run full frontend typecheck, build, asset sync, and Go binary build**

Run: `make build`
Expected: `tsc --noEmit`, `vite build`, copy to `internal/server/webdist/`, and `go build` all exit 0 cleanly.

- [ ] **Step 2: Run full Go & frontend test suites**

Run: `make test`
Expected: `go test -race -count=1 ./...` and `pnpm --dir web test` exit 0 with zero failures.

- [ ] **Step 3: Commit synced webdist assets (do not commit local binary if untracked/ignored — check `git status`)**

Run: `git status -s`
Commit `internal/server/webdist` if tracked in git (notice `internal/server/webdist` is tracked from earlier commits, while check if `coursefin` binary is in `.gitignore`).

```bash
git add internal/server/webdist
git commit -m "chore(web): rebuild and sync embedded frontend assets"
```
