# CourseFin UX Optimizations Design Document

- **Date:** 2026-11-24
- **Status:** Ready for user approval
- **Repository:** `CourseFin`

---

## 1. Intent & Why

CourseFin v1.0.0 already provides core storage indexing, course editing, AI organization, and Jellyfin playback, but three everyday Web UI workflows have noticeable friction:

1. **Manual path entry when adding a storage source:** Users must manually type absolute server paths (e.g., `/Users/name/Courses` or `/media/courses`) in «存储与导入», which is error-prone and inconvenient.
2. **Missing button guidance and silent failures:** No buttons across the Web UI show hover guidance (tooltips), and many actions—most visibly the Jellyfin Server URL «复制» button on non-HTTPS LAN origins—give no feedback or fail silently.
3. **No UI way to delete (or rename) libraries:** After creating a library on the home page, users cannot delete or rename it in the UI even though `PUT/DELETE /api/v1/libraries/{id}` already exist on the backend, and non-empty libraries fail with a foreign-key `409 Conflict` without an easy migration or cascade option.

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals
1. **Server-side local directory picker** in «存储与导入» so admins can browse server directories visually (working identically on local macOS/Linux/Windows binaries and inside Docker containers) and pick an absolute path with one click.
2. **Consistent button tooltips & action feedback toasts** across the entire Web UI, plus HTTP-compatible clipboard copying with explicit success/failure feedback.
3. **Complete library lifecycle management on the home page:** Inline rename and safe deletion of both empty and non-empty libraries (supporting course migration to another library or explicit deletion of course metadata).

### Non-Goals
- Browsing or downloading arbitrary file contents from the server filesystem (the directory picker only lists subdirectories, never reads or serves files).
- Remote storage browsing (WebDAV/S3/Alist) inside the local directory picker.
- Introducing heavy third-party UI libraries (Radix/Floating UI/Sonner) — keep zero extra npm dependencies and stay consistent with existing Tailwind v4 styling.

### Anti-Goals
- **Never touch disk media files:** Deleting a library—even with `force=true` to delete its course records—must only delete database records and app-managed covers inside `dataDir/covers`, never files on user media volumes.
- **Never break existing API compatibility or tests:** `DELETE /api/v1/libraries/{id}` with no query parameters keeps its exact existing `409 Conflict` behavior when a library contains courses; existing button accessible names (`aria-label` / visible text) remain intact.

---

## 3. Constraints

- **Browser sandbox limitation:** Web browsers never expose real OS absolute paths via `<input webkitdirectory>` or `showDirectoryPicker`. A server-side authenticated directory-listing endpoint is required so CourseFin works both locally and in Docker/remote browsers.
- **Non-HTTPS LAN deployments:** Self-hosted servers are commonly accessed via `http://<LAN-IP>:8096`, where `navigator.clipboard.writeText` is unavailable or rejects. Copying must gracefully fall back to a hidden `<textarea>` + `document.execCommand('copy')` and clearly report failure if both fail.
- **Standalone component tests:** Existing Vitest tests mount page components directly in `<MemoryRouter>` without `<App>`. Both `Tooltip` and `useToast` must work safely without requiring a mandatory provider wrapper during isolated rendering.

---

## 4. Approaches Considered

| Area | Approaches Considered | Trade-offs | Decision |
|---|---|---|---|
| **1. Directory Picker** | **A. Server-side directory browser API + Modal (with smart root shortcuts & auto-fill name)** vs. B. Root-only `/` browser vs. C. OS native dialog (`osascript`/shell) | A works everywhere (local/Docker/remote) with great ergonomics; B is slower to navigate; C breaks Docker and remote browser access. | **A (Selected by user)** |
| **2. Button Guidance** | **A. Hover Tooltip + Global Toast + HTTP clipboard fallback** vs. B. Toast only vs. C. Tooltip only | A solves both pre-click clarity (especially icon-only buttons) and post-click confirmation (copy/save/delete/scan) in one cohesive system; B/C each leave half the UX gap. | **A (Selected by user)** |
| **3. Non-Empty Library Deletion** | **A. Delete modal supporting Migrate-to-Library OR Force-Delete course records** vs. B. Empty-only restriction vs. C. Force-delete only | A prevents accidental course loss while never trapping users in dead ends; B forces tedious manual course-by-course editing; C risks unintended metadata loss. | **A (Selected by user)** |
| **4. Library Management Entry** | **A. Direct rename/delete icon buttons on Home library tabs (hover/active visible)** vs. B. Delete-only icon on tabs vs. C. Separate «管理媒体库» modal | A gives immediate zero-detour access right where the user sees the library tabs and fixes rename at the same time; C adds extra clicks. | **A (Selected by user)** |

---

## 5. Detailed Design

### 5.1 Server-Side Local Directory Picker

#### Backend API: `GET /api/v1/fs/dirs`
- **Route:** Protected by `requireAuth` inside `/api/v1` (`internal/api/handlers_fs.go` or `handlers_storage.go`).
- **Query parameters:**
  - `path` (optional string): Absolute directory path to inspect. When omitted or empty, resolves default in priority order: `/media` (if exists and is a readable dir) → `os.UserHomeDir()` (if exists and is a readable dir) → filesystem root (`/` on Unix; first readable drive root on Windows).
  - `show_hidden` (optional bool, default `false`): When `true`/`1`, includes directories whose name starts with `.`.
- **Validation & Security:**
  - Must be authenticated admin session.
  - Non-empty `path` must satisfy `filepath.IsAbs(p)`; cleaned with `filepath.Clean`.
  - Returns `400` if path is not absolute or is not a directory, `404` if not found, `403` if unreadable (`os.IsPermission`).
  - Lists **directories only** (`entry.IsDir()`, or for symlinks, `os.Stat(fullPath)` reports `IsDir()`; broken or unreadable symlink targets are skipped silently).
  - Entries sorted using `catalog.NaturalCompare`.
- **Response (`200 OK`):**
  ```json
  {
    "current_path": "/Users/finn/Courses",
    "parent_path": "/Users/finn",
    "entries": [
      { "name": "Go 实战", "path": "/Users/finn/Courses/Go 实战" }
    ],
    "roots": [
      { "label": "媒体目录 (/media)", "path": "/media" },
      { "label": "主目录", "path": "/Users/finn" },
      { "label": "根目录 (/)", "path": "/" }
    ]
  }
  ```
  - `parent_path` is `""` when `current_path` is a filesystem root (`filepath.Dir(current) == current`).
  - `roots` only includes paths that actually exist as directories on the host, deduplicated.

#### Frontend: `DirectoryPickerModal.tsx` + `StoragePage.tsx`
- Add `api.browseFsDirs(path?: string, showHidden?: boolean)` in `web/src/api/client.ts` and corresponding types in `web/src/types/coursefin.ts`.
- In `StoragePage.tsx`, next to `#source-path-input` («本地目录绝对路径»), render a «浏览目录» button (`FolderOpen` icon) with tooltip `"浏览服务器本地文件夹并选择目录路径"`.
- Opening `DirectoryPickerModal`:
  - Attempts to open at `sourceRootPath.trim()` if non-empty; if the backend returns an error for that initial path (e.g., typo), automatically falls back to the server default path and shows a gentle info note.
  - Shows quick-jump root buttons (`roots`), «返回上级» button, clickable path breadcrumb segments, «显示隐藏目录» checkbox, and an inline keyword filter box for subdirectories in the current folder.
  - Clicking a subdirectory enters it immediately; footer displays the active absolute path and «取消» / «选择此目录» buttons.
  - Confirming populates `sourceRootPath`; if `sourceName.trim()` is empty, auto-fills `sourceName` with the selected folder's basename (unless filesystem root).

---

### 5.2 Button Tooltips, Global Toast System & Clipboard Fallback

#### 1. `Tooltip` (`web/src/components/Tooltip.tsx`)
- Lightweight, dependency-free React component wrapping any interactive trigger element.
- Displays a compact dark pill (`bg-slate-800/95 border-slate-700 text-slate-100 text-xs shadow-lg`) on mouse hover (short 150ms delay to avoid flicker) and keyboard `focus-visible`.
- Rendered via `createPortal(..., document.body)` positioned from the trigger's `getBoundingClientRect()` (auto-flips below if near top viewport edge, clamped horizontally) so tooltips are never clipped by `overflow-hidden` or scrollable containers.
- Keeps the wrapped button's `aria-label` and text untouched (links via `aria-describedby` only while open) so screen readers and Testing Library queries remain unaffected.

#### 2. `ToastProvider` & `useToast` (`web/src/components/Toast.tsx`)
- Mounted inside `App.tsx` so every route and modal shares a single top-right floating toast stack (max 4 visible, auto-dismiss after 2.8s, manual close button, `role="status"` / `aria-live="polite"`, variants: `success` / `error` / `info`).
- Safe default context so components rendered standalone in unit tests without `<ToastProvider>` do not throw.

#### 3. Clipboard Utility (`web/src/utils/clipboard.ts`)
- `copyTextToClipboard(text: string): Promise<boolean>`:
  1. Uses `navigator.clipboard.writeText(text)` when available in a secure context.
  2. On error or non-secure HTTP context, creates an off-screen read-only `<textarea>`, selects it, and invokes `document.execCommand('copy')`, removing the node afterward.
  3. Returns `true` on success, `false` otherwise.
- In `SettingsPage.tsx`:
  - Server URL «复制» button uses `copyTextToClipboard`: displays inline «已复制» checkmark + `toast.success('已复制服务器地址到剪贴板')`, or `toast.error('复制失败，请手动选中文本复制')` on failure.
  - Add a matching «复制» button beside the Jellyfin username (`settings?.jellyfin_username || 'admin'`) with tooltip and toast feedback.

#### 4. Whole-App Button Audit
- Wrap action buttons and icon-only buttons with concise, helpful Chinese tooltips across:
  - `Layout.tsx` (navigation items, 退出登录)
  - `HomePage.tsx` (全部, 媒体库标签, 重命名/删除媒体库, 新建媒体库, 新建空白课程, 清空搜索, 标签筛选, 开始学习, 编辑)
  - `StoragePage.tsx` (添加存储源, 浏览目录, 扫描, 删除存储源, 导入当前目录, 一键导入为课程, 文件状态筛选)
  - `SettingsPage.tsx` (复制服务器地址, 复制用户名, 测试连接, 保存设置, 更新密码, 注销设备会话)
  - `CourseDetailPage.tsx` (返回课程库, 收起/展开目录, 编辑课程, 上一节, 下一节, 标记已看/未看)
  - `CourseEditorPage.tsx` (检查新文件, AI 智能整理, 删除课程, 添加/移除标签, 保存元数据, 上传封面, 从课时帧选取封面, 上传背景图, 添加章节, 拖拽排序手柄, 从存储添加视频, 章节与课时的上移/下移/重命名/删除)
  - `VideoPlayer.tsx` (全屏按钮, 倍速与字幕提示)
  - Modals (`ImportCourseModal.tsx`, `AIOrganizeModal.tsx`, `DirectoryPickerModal.tsx`, `DeleteLibraryModal.tsx`)
- Emit consistent Toast feedback on key mutations (copy, create/rename/delete library, add/delete/scan storage source, save settings, AI test, password change, revoke session, save metadata, cover/backdrop upload, chapter/lesson CRUD & reorder, toggle played, import course, AI apply).

---

### 5.3 Library Deletion & Rename

#### Backend: Enhanced `DELETE /api/v1/libraries/{id}`
- Add `DeleteLibraryOptions` in `internal/catalog/service.go`:
  ```go
  type DeleteLibraryOptions struct {
      MoveToLibraryID string
      Force           bool
  }
  ```
- Keep `DeleteLibrary(ctx, libraryID)` delegating to `DeleteLibraryWithOptions(ctx, libraryID, DeleteLibraryOptions{})` for full backward compatibility.
- Behavior of `DeleteLibraryWithOptions`:
  - Reject if both `MoveToLibraryID != ""` and `Force == true` (`ErrInvalidInput`).
  - **Default (neither set):** Existing behavior — deletes library; returns `ErrConflict` (`409`) if courses exist, `ErrNotFound` (`404`) if missing.
  - **`MoveToLibraryID` set:**
    - Reject if normalized `MoveToLibraryID == libraryID` (`ErrInvalidInput`).
    - In a transaction: verify both source and target libraries exist (`ErrNotFound` if either missing); `UPDATE courses SET library_id = ?, updated_at = ? WHERE library_id = ?`; `DELETE FROM libraries WHERE id = ?`; commit.
  - **`Force == true`:**
    - In a transaction: verify source library exists (`ErrNotFound` if missing); query `cover_path, backdrop_path` of courses in that library; `DELETE FROM courses WHERE library_id = ?` (cascades to chapters, lessons, course_tags, playback_progress); `DELETE FROM libraries WHERE id = ?`; commit.
    - After commit, clean up only app-managed cover/backdrop files under `filepath.Join(s.dataDir, "covers")` matching `DeleteCourse` behavior. Never touch media volumes.
- Update `handleDeleteLibrary` in `internal/api/handlers_catalog.go` to read optional query params `move_to_library_id` and `force` (`true`/`1`).
- Update `api.deleteLibrary(id, options?: { moveToLibraryId?: string; force?: boolean })` in `web/src/api/client.ts`.

#### Frontend: `HomePage.tsx` + `DeleteLibraryModal.tsx`
- Each library tab in `HomePage.tsx` renders as a pill container (`group inline-flex items-center rounded-xl ...`):
  - Primary button displays `{lib.name} ({lib.course_count})` and selects the library.
  - Adjacent compact icon buttons (visible on active tab and on pill hover/focus-within; always focusable for keyboard/touch usability):
    - **Rename** (`Pencil`, tooltip `"重命名媒体库"`, `aria-label="重命名媒体库 ${lib.name}"`): opens an inline edit bar prefilled with `lib.name`, submits `api.updateLibrary(lib.id, trimmed)`, updates state + shows toast.
    - **Delete** (`Trash2`, tooltip `"删除媒体库"`, `aria-label="删除媒体库 ${lib.name}"`): opens `DeleteLibraryModal`.
- `DeleteLibraryModal.tsx`:
  - **Empty library (`course_count === 0`):** Direct confirmation dialog noting local video files are unaffected.
  - **Non-empty library (`course_count > 0`):** Radio choice between:
    1. **迁移课程到其他媒体库并删除** (default when `otherLibraries.length > 0`, with `<select>` target picker; disabled with hint when no other library exists).
    2. **连带删除这 N 门课程记录** (clear warning that only CourseFin catalog records and watch progress are removed, never local files on disk).
  - On confirm: calls `api.deleteLibrary(lib.id, opts)`, resets `selectedLibraryId` to `'all'` if the deleted library was selected, refreshes home/courses/libraries/tags data so counts and continue-watching stay consistent, and shows a success toast.

---

## 6. Left to the Builder

- Exact Tailwind spacing/animation polish for `Tooltip`, `Toast`, `DirectoryPickerModal`, and `DeleteLibraryModal`.
- Exact wording of per-button Chinese tooltips while keeping them short (4–18 chars) and action-oriented.
- Rebuilding embedded frontend assets (`make sync-web` / `make build`) and running full Go + Vitest suites to verify zero regressions.
