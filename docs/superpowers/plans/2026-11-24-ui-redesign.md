# CourseFin Full UI Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the entire CourseFin Web SPA (`web/src/`) into a Citonema-inspired, left-sidebar-primary, full light/dark dual-theme interface covering all 6 pages, v1.1.0's `SystemUpdateSection`, all modals, and shared feedback primitives with zero backend logic changes and zero new runtime dependencies.

**Architecture:** Semantic CSS custom properties (`:root` for Citonema light mode and `[data-theme="dark"]` for matte charcoal dark mode) are mapped into Tailwind v4 utilities via `@theme inline` in `web/src/index.css`, driven by a zero-FOUC inline `<head>` script plus `ThemeProvider`. Authenticated routes mount inside a persistent nested React Router layout (`AuthenticatedShell` = `RequireAuth` + `LibraryProvider` + `Layout` + `<Outlet />`) so the collapsible left sidebar (navigation, libraries, v1.1.0 update badge, theme switcher, account/logout) stays mounted across page transitions while isolated component tests continue to work safely via context fallbacks.

**Tech Stack:** React 19.1, TypeScript 5.8, React Router 7.6, Tailwind CSS 4.1 (`@tailwindcss/vite`), `lucide-react`, `@dnd-kit`, `hls.js`, Vitest 3.2 + Testing Library, Go 1.26 embedded static assets (`make sync-web`).

**Spec:** [docs/superpowers/specs/2026-11-24-ui-redesign-design.md](../specs/2026-11-24-ui-redesign-design.md)

## Global Constraints

- **Zero backend Go business-logic or API changes:** Only `internal/server/webdist/*` changes via `make sync-web`.
- **Zero new npm runtime dependencies:** Do not add packages to `web/package.json`.
- **Base path & embedded delivery:** Runs under `<BrowserRouter basename="/web">`; anti-FOUC init script in `web/index.html` must be pure inline JS with no external fetches.
- **Non-HTTPS LAN compatibility:** Preserve `copyTextToClipboard` fallback in `SettingsPage.tsx`.
- **Accessible names & keyboard ergonomics:** Preserve existing navigation link labels (`课程库`, `存储与导入`, `设置`), update badge link (`/settings#system-update`, `新版本 vX.Y.Z`, `data-testid="mobile-update-dot"`), button `aria-label`s, `Escape` modal close, and video player hotkeys (`Space`, `ArrowLeft`, `ArrowRight`, `F`).
- **Standalone test safety:** `useTheme()`, `useLibrary()`, `useUpdate()`, `useToast()`, and `Tooltip` must never throw when rendered outside providers or when `window.matchMedia` is undefined in jsdom.
- **Auth boundary:** `/login` must never trigger protected `/api/v1/libraries`, `/api/v1/auth/me`, or `/api/v1/system/update` requests.
- **No residual dark-only hardcoding:** Except the pure-black `<video>` playback canvas and intentional inverse white text on solid accent/status buttons, no page/modal/component may retain hardcoded `bg-slate-950`, `bg-slate-900`, `border-slate-800`, or `text-white` that breaks Light Mode contrast.

## Review Focus

1. **Corrupted or unavailable `localStorage` (`coursefin_theme` / `coursefin_sidebar_collapsed`):** Garbage stored values or `localStorage.getItem` throwing in private browsing must gracefully default to `'system'` theme and expanded sidebar without crashing render. → Pinned in Task 1 & Task 3 (`ThemeAndShell.test.tsx`).
2. **Stale `/?library=<deleted-or-unknown-id>` URL query param:** Visiting a bookmarked or recently-deleted library ID must not show an empty phantom header or broken course list; `HomePage` falls back to `'all'` (全部课程) cleanly. → Pinned in Task 4 (`HomePage.test.tsx`).
3. **Transient theater-mode sidebar collapse on `/courses/:courseId`:** Navigating to `/courses/:courseId` collapses desktop sidebar visually to `76px` without mutating `coursefin_sidebar_collapsed` in `localStorage`; clicking expand works transiently on that page, and leaving `/courses/:courseId` restores the persisted state. → Pinned in Task 3 (`ThemeAndShell.test.tsx`).
4. **Re-clicking `新版本 vX.Y.Z` while already on `/settings` + no duplicate link roles:** Desktop sidebar update link sits outside `<NavLink to="/settings">`, mobile top-bar update button uses a distinct `aria-label` while preserving `data-testid="mobile-update-dot"`, and mobile drawer is only mounted when open—so `findByRole('link', { name: /新版本/ })` uniquely matches and re-scrolls `#system-update`. → Pinned in Task 3 & Task 8 (`SystemUpdate.test.tsx`).
5. **Unauthenticated `/login` isolation & pre-login theme switching:** Rendering `AppRoutes` at `/login` with no token makes zero protected API calls while allowing users to switch Light/Dark/System immediately via the top-right `ThemeSwitcher`. → Pinned in Task 3 & Task 8 (`ThemeAndShell.test.tsx`, `HomePage.test.tsx`).

---

## File Structure

- **Create:**
  - `web/src/contexts/ThemeContext.tsx` — `ThemeProvider`, `useTheme`, `ThemeMode`, `ResolvedTheme`, `THEME_STORAGE_KEY`, DOM sync + `matchMedia` listener.
  - `web/src/components/ThemeSwitcher.tsx` — 3-way segmented (`浅色 / 深色 / 自动`) and compact icon-cycle variants.
  - `web/src/components/LibraryFormModal.tsx` — Dual-theme modal for creating and renaming libraries from the sidebar (expanded, collapsed, or mobile drawer).
  - `web/src/contexts/LibraryContext.tsx` — `LibraryProvider`, `useLibrary` holding `libraries`, `username`, `loading`, `catalogVersion`, and library mutation helpers.
  - `web/src/__tests__/ThemeAndShell.test.tsx` — Tests for theme system, `LibraryFormModal`, `LibraryContext`, `Layout` sidebar/drawer/theater-collapse, and `AppRoutes` auth boundary.
- **Modify:**
  - `web/index.html` — Inline anti-FOUC theme bootstrap script + semantic body classes.
  - `web/src/index.css` — Light/dark CSS custom properties, Tailwind v4 `@theme inline` tokens, elevation shadows, custom scrollbars.
  - `web/src/components/Tooltip.tsx` — Add `'right'` placement clamping + semantic tooltip styling.
  - `web/src/components/Toast.tsx` — Semantic popover + status badge styling.
  - `web/src/components/DeleteLibraryModal.tsx` — Unified dual-theme modal styling.
  - `web/src/components/Layout.tsx` — Collapsible left-sidebar AppShell, mobile header + slide-over drawer, library CRUD integration, v1.1.0 update indicator, theme switcher, account/logout.
  - `web/src/App.tsx` — Root `<ThemeProvider>` + persistent nested `<AuthenticatedShell />` route.
  - `web/src/pages/HomePage.tsx` — Citonema greeting header, spotlight「继续学习」hero card with full-list bounded scroll + `< / >` controls, library-scoped resume & tags, client-side sort, floating `rounded-3xl` course cards, standalone fallback.
  - `web/src/pages/CourseDetailPage.tsx` & `web/src/components/VideoPlayer.tsx` — Theater-first playback layout, sticky playlist (`lg:320px / xl:360px`), course overview below player, dual-theme control bar & unsupported codec card.
  - `web/src/pages/CourseEditorPage.tsx` & `web/src/components/AIOrganizeModal.tsx` — Sticky studio top bar, dual-theme metadata/cover/chapter/lesson editor, storage picker modal, `AIOrganizeModal`, sidebar `bumpCatalogVersion` sync.
  - `web/src/pages/StoragePage.tsx`, `web/src/components/ImportCourseModal.tsx`, `web/src/components/DirectoryPickerModal.tsx` — Dual-theme floating storage & import hub + sidebar `registerLibrary`/`bumpCatalogVersion` sync.
  - `web/src/pages/SettingsPage.tsx` & `web/src/components/SystemUpdateSection.tsx` — `max-w-4xl` dual-theme control center, restyled `SystemUpdateSection` + confirmation modal.
  - `web/src/pages/SetupLoginPage.tsx` — Dual-theme centered welcome card + top-right `ThemeSwitcher`.
  - Existing tests: `web/src/__tests__/UXPrimitives.test.tsx`, `HomePage.test.tsx`, `UXFeatures.test.tsx`, `SystemUpdate.test.tsx`.

---

### Task 1: Design Token System & Zero-FOUC Theme Infrastructure

**Files:**
- Create: `web/src/contexts/ThemeContext.tsx`
- Create: `web/src/__tests__/ThemeAndShell.test.tsx`
- Modify: `web/index.html`
- Modify: `web/src/index.css`

**Interfaces:**
- Consumes: none (foundation layer)
- Produces:
  - `THEME_STORAGE_KEY = 'coursefin_theme'`
  - `export type ThemeMode = 'light' | 'dark' | 'system'`
  - `export type ResolvedTheme = 'light' | 'dark'`
  - `export interface ThemeContextValue { isProvided: boolean; mode: ThemeMode; resolvedTheme: ResolvedTheme; setMode: (next: ThemeMode) => void; cycleMode: () => void; }`
  - `export function ThemeProvider({ children }: { children: ReactNode }): JSX.Element`
  - `export function useTheme(): ThemeContextValue`
  - Tailwind v4 semantic utilities from `web/src/index.css`: `bg-canvas`, `bg-sidebar`, `bg-card`, `bg-subtle`, `bg-muted`, `bg-popover`, `bg-overlay`, `bg-tooltip`, `text-ink-primary`, `text-ink-secondary`, `text-ink-muted`, `text-ink-inverse`, `text-ink-tooltip`, `border-subtle`, `border-default`, `border-strong`, `bg-accent`, `hover:bg-accent-hover`, `bg-accent-soft`, `ring-accent-ring`, `text-accent-ink`, `bg-status-success`, `bg-status-success-soft`, `text-status-success-ink`, `bg-status-warning`, `bg-status-warning-soft`, `text-status-warning-ink`, `bg-status-danger`, `bg-status-danger-soft`, `text-status-danger-ink`, plus `.shadow-card`, `.shadow-card-hover`, `.shadow-sidebar`, `.shadow-float`, `.shadow-accent`.

- [ ] **Step 1: Write failing tests for `ThemeProvider` and `useTheme` in `web/src/__tests__/ThemeAndShell.test.tsx`**

Include test cases asserting:
1. Default mode is `'system'`; when `matchMedia('(prefers-color-scheme: dark)').matches` is `true`, `document.documentElement.dataset.theme === 'dark'`, `document.documentElement.classList.contains('dark') === true`, and `style.colorScheme === 'dark'`. Dispatching a `change` event on the mocked `MediaQueryList` switches DOM attributes to `'light'`.
2. Calling `setMode('light')` / `setMode('dark')` / `setMode('system')` persists to `localStorage.getItem('coursefin_theme')` and syncs `<html data-theme>`/`.dark`; `cycleMode()` deterministically cycles `'light' -> 'dark' -> 'system' -> 'light'`.
3. **Review Focus #1:** Invalid `localStorage` value (`'invalid-theme'`) and a throwing `Storage.prototype.getItem` both safely fall back to `'system'` without throwing, and calling `useTheme()` outside `<ThemeProvider>` returns `{ isProvided: false, mode: 'system', resolvedTheme: 'light' }` safely when `window.matchMedia` is undefined.

- [ ] **Step 2: Run the new test file to verify it fails**

Run: `pnpm --dir web test src/__tests__/ThemeAndShell.test.tsx`
Expected: FAIL because `ThemeContext.tsx` does not exist yet.

- [ ] **Step 3: Implement semantic design tokens in `web/src/index.css` and zero-FOUC script in `web/index.html`**

- In `web/src/index.css`: define all `:root` (light) and `[data-theme="dark"]` (dark) CSS variables with exact hex/rgba values from Spec §5.1.1 & §5.1.2; expose color tokens via `@theme inline`; define `.shadow-card`, `.shadow-card-hover`, `.shadow-sidebar`, `.shadow-float`, `.shadow-accent` utility classes and subtle themed scrollbars; style `body` with `background-color: var(--surface-canvas); color: var(--ink-primary);`.
- In `web/index.html`: add a compact synchronous inline `<script>` inside `<head>` that safely reads `localStorage.getItem('coursefin_theme')` inside `try/catch`, resolves `'dark'` vs `'light'` via `window.matchMedia`, and sets `dataset.theme`, `.dark`, and `style.colorScheme` before CSS paints. Replace `<body>` classes with `bg-canvas text-ink-primary antialiased selection:bg-accent-soft selection:text-accent-ink`.

- [ ] **Step 4: Implement `web/src/contexts/ThemeContext.tsx`**

- Safely read/write `THEME_STORAGE_KEY` inside `try/catch`, validate against `'light' | 'dark' | 'system'`, guard `typeof window.matchMedia === 'function'`, support both `addEventListener('change', ...)` and legacy `addListener`, sync `document.documentElement` inside `useEffect`, and expose `setMode` and deterministic `cycleMode` (`'light' -> 'dark' -> 'system' -> 'light'`; from `'system'` advance to `'light'`).

- [ ] **Step 5: Run tests and TypeScript build to verify Task 1 passes**

Run: `pnpm --dir web test src/__tests__/ThemeAndShell.test.tsx && pnpm --dir web build`
Expected: PASS (all Theme tests green and `tsc --noEmit && vite build` exits 0).

- [ ] **Step 6: Commit Task 1**

```bash
git add web/index.html web/src/index.css web/src/contexts/ThemeContext.tsx web/src/__tests__/ThemeAndShell.test.tsx
git commit -m "feat(web): add semantic dual-theme design tokens and ThemeContext"
```

---

### Task 2: UI Primitives (`Tooltip` Right Placement, `ThemeSwitcher`, `Toast`, `DeleteLibraryModal`, `LibraryFormModal`)

**Files:**
- Create: `web/src/components/ThemeSwitcher.tsx`
- Create: `web/src/components/LibraryFormModal.tsx`
- Modify: `web/src/components/Tooltip.tsx`
- Modify: `web/src/components/Toast.tsx`
- Modify: `web/src/components/DeleteLibraryModal.tsx`
- Test: `web/src/__tests__/UXPrimitives.test.tsx`, `web/src/__tests__/ThemeAndShell.test.tsx`

**Interfaces:**
- Consumes: semantic tokens & `ThemeContext` from Task 1 (`useTheme`, `ThemeMode`), `api.deleteLibrary`.
- Produces:
  - `TooltipProps`: `placement?: 'top' | 'bottom' | 'right'`
  - `export default function ThemeSwitcher({ variant = 'segmented', tooltipPlacement = 'top' }: { variant?: 'segmented' | 'compact'; tooltipPlacement?: 'top' | 'bottom' | 'right' }): JSX.Element`
  - `LibraryFormModalProps`: `{ isOpen: boolean; mode: 'create' | 'rename'; initialName?: string; submitting?: boolean; error?: string | null; onClose: () => void; onSubmit: (trimmedName: string) => Promise<void> | void; }`
  - `export default function LibraryFormModal(props: LibraryFormModalProps): JSX.Element | null`

- [ ] **Step 1: Write failing tests for `Tooltip` right placement, `ThemeSwitcher`, and `LibraryFormModal`**

- In `web/src/__tests__/UXPrimitives.test.tsx`: add test asserting `<Tooltip content="折叠导航提示" placement="right">` positions to the right of trigger (`left >= trigger.right + 8`), vertically centers with viewport top/bottom clamping, and keeps `whitespace-normal` wrapping.
- In `web/src/__tests__/ThemeAndShell.test.tsx`: add tests for:
  - `ThemeSwitcher`: `variant="segmented"` renders `浅色` / `深色` / `自动` buttons with `aria-pressed` updating `localStorage` & `<html data-theme>`; `variant="compact"` renders `aria-label="切换主题模式"` and cycles modes.
  - `LibraryFormModal`: `mode="create"` renders `role="dialog"`, input labeled `新媒体库名称`, rejects empty input with `请输入媒体库名称`, calls `onSubmit('后端工程')` when clicking `确认创建`, closes on `Escape`; `mode="rename"` prefills `initialName`, renders `重命名媒体库名称`, submits via `保存名称`.

- [ ] **Step 2: Run tests to verify new tests fail**

Run: `pnpm --dir web test src/__tests__/UXPrimitives.test.tsx src/__tests__/ThemeAndShell.test.tsx`
Expected: FAIL on right placement positioning and missing `ThemeSwitcher` / `LibraryFormModal`.

- [ ] **Step 3: Upgrade `Tooltip.tsx`, `Toast.tsx`, `DeleteLibraryModal.tsx`, and create `ThemeSwitcher.tsx` & `LibraryFormModal.tsx`**

- `Tooltip.tsx`: extend `TooltipPlacement = 'top' | 'bottom' | 'right'`. For `'right'`, place `left = rect.right + GAP_PX` (flipping to left of trigger or clamping inside `EDGE_PADDING_PX` if overflowing right viewport edge) and `top = clamp(rect.top + rect.height / 2, EDGE_PADDING_PX + tipHeight / 2, window.innerHeight - EDGE_PADDING_PX - tipHeight / 2)` with `-translate-y-1/2`. Replace dark-only classes with `bg-tooltip text-ink-tooltip border border-white/10 shadow-float`.
- `ThemeSwitcher.tsx`:
  - `variant="segmented"`: `role="group" aria-label="主题模式"` pill container (`bg-muted p-1 rounded-full`) with 3 buttons (`浅色` Sun, `深色` Moon, `自动` Monitor), each with `aria-pressed={mode === opt.id}` and `<Tooltip>`.
  - `variant="compact"`: single rounded icon button (`aria-label="切换主题模式"`) showing current icon + `<Tooltip placement={tooltipPlacement}>` displaying current mode and click hint, invoking `cycleMode()`.
- `Toast.tsx`: restyle toast items to `rounded-2xl bg-popover border border-subtle shadow-float text-ink-primary` with soft semantic icon pills (`bg-status-success-soft text-status-success-ink`, `bg-status-danger-soft text-status-danger-ink`, `bg-accent-soft text-accent-ink`). Keep `role="status"`, `aria-live="polite"`, close button `aria-label="关闭提示"`.
- `DeleteLibraryModal.tsx`: restyle backdrop to `bg-overlay backdrop-blur-sm`, dialog shell to `rounded-3xl border border-subtle bg-popover shadow-float`, option cards to `bg-subtle`/`bg-accent-soft`/`bg-status-danger-soft`, inputs/selects to semantic tokens; keep all accessible labels and delete logic intact.
- `LibraryFormModal.tsx`: implement clean `rounded-3xl bg-popover border border-subtle shadow-float` modal supporting `mode: 'create' | 'rename'`, autofocusing input on open, trimming & validating empty input (`请输入媒体库名称`), closing on `Escape` (unless `submitting`), and showing external or local validation error.

- [ ] **Step 4: Run tests and TypeScript build to verify Task 2 passes**

Run: `pnpm --dir web test src/__tests__/UXPrimitives.test.tsx src/__tests__/ThemeAndShell.test.tsx src/__tests__/UXFeatures.test.tsx && pnpm --dir web build`
Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add web/src/components/Tooltip.tsx web/src/components/ThemeSwitcher.tsx web/src/components/Toast.tsx web/src/components/DeleteLibraryModal.tsx web/src/components/LibraryFormModal.tsx web/src/__tests__/UXPrimitives.test.tsx web/src/__tests__/ThemeAndShell.test.tsx
git commit -m "feat(web): upgrade Tooltip, Toast, DeleteLibraryModal and add ThemeSwitcher & LibraryFormModal"
```

---

### Task 3: `LibraryContext`, Persistent Nested Shell (`App.tsx`), and Left-Sidebar `Layout.tsx`

**Files:**
- Create: `web/src/contexts/LibraryContext.tsx`
- Modify: `web/src/components/Layout.tsx`
- Modify: `web/src/App.tsx`
- Test: `web/src/__tests__/ThemeAndShell.test.tsx`, `web/src/__tests__/SystemUpdate.test.tsx`

**Interfaces:**
- Consumes: `ThemeContext` & `ThemeSwitcher` (Task 1), `LibraryFormModal`, `DeleteLibraryModal`, `Tooltip` (`placement="right"`) (Task 2), `useUpdate` (`UpdateContext.tsx`), `useToast` (`Toast.tsx`), `api` (`client.ts`).
- Produces:
  - `SIDEBAR_COLLAPSED_STORAGE_KEY = 'coursefin_sidebar_collapsed'`
  - `export interface LibraryContextValue { isProvided: boolean; libraries: Library[]; username: string; loading: boolean; catalogVersion: number; refreshLibraries: () => Promise<Library[]>; createLibrary: (name: string) => Promise<Library>; renameLibrary: (id: string, name: string) => Promise<Library>; registerLibrary: (lib: Library) => void; notifyLibraryDeleted: (deleted: Library, mode: DeleteLibraryMode, targetLibraryName?: string) => void; bumpCatalogVersion: () => void; }`
  - `export function LibraryProvider({ children }: { children: ReactNode }): JSX.Element`
  - `export function useLibrary(): LibraryContextValue`
  - `export default function Layout({ children }: { children?: ReactNode }): JSX.Element` (renders `children ?? <Outlet />`)

- [ ] **Step 1: Write failing tests for `LibraryContext`, `Layout`, theater auto-collapse, and `AppRoutes` auth boundary in `web/src/__tests__/ThemeAndShell.test.tsx`**

Include test cases asserting:
1. **Sidebar library navigation & CRUD:** Inside `ToastProvider + ThemeProvider + MemoryRouter + LibraryProvider + Layout`, sidebar fetches `/api/v1/libraries` and `/api/v1/auth/me`, renders username + libraries under `我的媒体库`, clicking a library navigates to `/?library=lib-dev`, creating a library via `+` (`新建媒体库`) opens `LibraryFormModal`, posts `POST /api/v1/libraries`, adds it to sidebar, and shows toast; renaming (`重命名媒体库`) puts `PUT /api/v1/libraries/:id`; deleting active library (`删除媒体库`) opens `DeleteLibraryModal` and navigates back to `/`.
2. **Desktop collapse persistence & Review Focus #1:** Clicking collapse toggle toggles sidebar collapsed state (`data-collapsed="true"`), saves `'true'` to `coursefin_sidebar_collapsed`; invalid/throwing `localStorage` gracefully defaults to expanded.
3. **Review Focus #3 (Theater transient collapse):** When route changes from `/` to `/courses/c1`, sidebar transiently shows `data-collapsed="true"` while `localStorage.getItem('coursefin_sidebar_collapsed')` stays `null`/`'false'`; clicking expand on `/courses/c1` expands transiently (`data-collapsed="false"`); navigating back to `/` restores persisted expanded state. Entering `/courses/c1/edit` does NOT auto-collapse.
4. **Mobile drawer:** Clicking mobile menu button (`打开导航菜单`) mounts slide-over drawer (`role="dialog" aria-label="导航菜单"`), pressing `Escape` or clicking a nav item closes and unmounts it.
5. **Review Focus #5 (Unauthenticated `/login` isolation):** Rendering `<ThemeProvider><ToastProvider><UpdateProvider><MemoryRouter initialEntries={['/login']}><AppRoutes /></MemoryRouter></UpdateProvider></ToastProvider></ThemeProvider>` with no token only calls `/api/v1/setup/status` and NEVER calls `/api/v1/libraries`, `/api/v1/auth/me`, or `/api/v1/system/update`.

- [ ] **Step 2: Run tests to verify new tests fail**

Run: `pnpm --dir web test src/__tests__/ThemeAndShell.test.tsx`
Expected: FAIL because `LibraryContext.tsx` does not exist and `Layout.tsx` is still the old top bar.

- [ ] **Step 3: Implement `web/src/contexts/LibraryContext.tsx`**

- On mount, fetch `api.listLibraries()` and `api.getMe().catch(() => null)` (guarded against unmount).
- Implement `refreshLibraries`, `createLibrary`, `renameLibrary`, `registerLibrary` (deduping by `id`), `notifyLibraryDeleted` (triggers success toast matching existing copy + `refreshLibraries()`), and `bumpCatalogVersion` (increments `catalogVersion` and silently refreshes `libraries` so sidebar `course_count` stays accurate after course import/edit/delete).
- Provide safe default context with `isProvided: false` so standalone `<Layout>` and `<HomePage>` unit tests do not fire unmocked `/api/v1/libraries` calls.

- [ ] **Step 4: Rebuild `web/src/components/Layout.tsx` into the Citonema-inspired Left-Sidebar AppShell**

- Desktop sidebar (`hidden lg:flex sticky top-0 h-screen shrink-0 bg-sidebar border-r border-subtle shadow-sidebar flex-col transition-[width] duration-200`, `w-[260px]` expanded vs `w-[76px]` collapsed, exposing `data-collapsed={effectiveCollapsed ? 'true' : 'false'}`).
- Theater auto-collapse logic: `isPlayerRoute = /^\/courses\/[^/]+$/.test(location.pathname)`. Maintain `userCollapsed` (persisted in `localStorage` inside `try/catch`) and `theaterOverride` (`boolean | null`, reset to `null` whenever `isPlayerRoute` or `location.pathname` changes). `effectiveCollapsed = isPlayerRoute ? (theaterOverride ?? true) : userCollapsed`. Clicking the toggle on player route flips `theaterOverride`; elsewhere flips and persists `userCollapsed`.
- Sidebar sections:
  1. **Brand header:** Squircle blue-gradient `GraduationCap` tile + `CourseFin` wordmark (hidden when collapsed) + collapse/expand toggle button (`PanelLeftClose` / `PanelLeftOpen`, tooltip `收起侧边栏` / `展开侧边栏`).
  2. **Navigation (`导航`):** `课程库` (`/`), `存储与导入` (`/storage`), `设置` (`/settings`). Active indicator: left 3px vertical pill `bg-accent` + `bg-accent-soft text-ink-primary font-semibold`. When `location.pathname === '/' && searchParams.get('library')`, deactivate `课程库` bar so only the active library row shows the indicator.
  3. **My Libraries (`我的媒体库` — rendered when `libCtx.isProvided`):** Header with `+` button (`aria-label="新建媒体库"`, tooltip `"新建一个课程分类媒体库"`). Scrollable list (`flex-1 overflow-y-auto`): each library in `role="group" aria-label={`媒体库 ${lib.name}`}` with click button navigating to `/?library=${encodeURIComponent(lib.id)}`, count badge, and hover/focus/active `重命名媒体库` (`Pencil`) & `删除媒体库` (`Trash2`) buttons. Collapsed mode shows squircle initial badges with `Tooltip placement="right"` (`${lib.name} (${lib.course_count})`).
  4. **Footer:**
     - **v1.1.0 Update Pill (when `hasUpdate`):** Standalone `<Link to="/settings#system-update">` (NOT nested inside `设置` NavLink) styled with `bg-status-success-soft text-status-success-ink`, `ArrowUp` + `新版本 ${latestVersionLabel}` (or centered circular icon + `sr-only` text + `Tooltip placement="right"` when collapsed), preserving tooltip `"发现新版本 ${latestVersionLabel}，点击前往设置页查看并更新"`.
     - **`ThemeSwitcher`**: `variant={effectiveCollapsed ? 'compact' : 'segmented'}` (`tooltipPlacement="right"` when collapsed).
     - **Account & Logout row**: circular initial avatar badge + `username` (expanded) + `退出登录` button (`LogOut`).
- Mobile `< lg` top bar (`flex lg:hidden sticky top-0 z-30 h-14 items-center justify-between bg-sidebar/90 backdrop-blur-md border-b border-subtle px-4`):
  - Left: menu button (`aria-label="打开导航菜单"`) + brand mark.
  - Right: when `hasUpdate`, compact `<Link to="/settings#system-update" aria-label="查看新版本更新">` showing emerald badge with `<span data-testid="mobile-update-dot" aria-hidden="true" />` (distinct `aria-label` so `findByRole('link', { name: /新版本 v.../ })` in `SystemUpdate.test.tsx` matches uniquely!); plus `<ThemeSwitcher variant="compact" tooltipPlacement="bottom" />`.
- Mobile slide-over drawer (conditionally rendered ONLY when `mobileDrawerOpen === true`): `role="dialog" aria-modal="true" aria-label="导航菜单"`, closes on backdrop click, `Escape`, or clicking any nav/library/update link.
- Mount `LibraryFormModal` (for create/rename) and `DeleteLibraryModal` inside `Layout`. When deleting the currently active URL library, navigate to `/` cleanly.
- Main area: `<main className="flex-1 min-w-0 px-4 sm:px-6 lg:px-8 py-6 sm:py-8"><div className="mx-auto w-full max-w-[1400px]">{children ?? <Outlet />}</div></main>`.

- [ ] **Step 5: Refactor `web/src/App.tsx` to wrap `<ThemeProvider>` and nested `<AuthenticatedShell />`**

- `App` wraps `<ThemeProvider><ToastProvider><UpdateProvider><BrowserRouter basename="/web"><AppRoutes /></BrowserRouter></UpdateProvider></ToastProvider></ThemeProvider>`.
- `AppRoutes` keeps `/login` outside `RequireAuth`, and groups `/`, `/courses/:courseId`, `/courses/:courseId/edit`, `/storage`, `/settings` under `<Route element={<RequireAuth><LibraryProvider><Layout /></LibraryProvider></RequireAuth>}>`.

- [ ] **Step 6: Run tests to verify Task 3 and full existing `SystemUpdate.test.tsx` pass**

Run: `pnpm --dir web test src/__tests__/ThemeAndShell.test.tsx src/__tests__/SystemUpdate.test.tsx src/__tests__/HomePage.test.tsx`
Expected: PASS (all existing `SystemUpdate.test.tsx` and `HomePage.test.tsx` tests remain green alongside new shell tests).

- [ ] **Step 7: Commit Task 3**

```bash
git add web/src/contexts/LibraryContext.tsx web/src/components/Layout.tsx web/src/App.tsx web/src/__tests__/ThemeAndShell.test.tsx
git commit -m "feat(web): build persistent left-sidebar AppShell, LibraryContext, and nested routes"
```

---

### Task 4: Citonema-Inspired `HomePage` Redesign

**Files:**
- Modify: `web/src/pages/HomePage.tsx`
- Test: `web/src/__tests__/HomePage.test.tsx`, `web/src/__tests__/UXFeatures.test.tsx`

**Interfaces:**
- Consumes: `useLibrary` (Task 3), `useSearchParams` (`?library=<id>`), `DeleteLibraryModal` (standalone fallback), `Tooltip`, `useToast`, `api`.
- Produces: Redesigned `HomePage` supporting both shell-integrated (`libCtx.isProvided === true`) and standalone (`libCtx.isProvided === false`) modes.

- [ ] **Step 1: Write failing tests for Citonema spotlight hero (>5 resume items), single-library scoping, stale `?library=` fallback, and client-side sorting**

In `web/src/__tests__/HomePage.test.tsx`, add tests asserting:
1. **Spotlight hero with >5 resume lessons (e.g., 7 items):** Shows `继续学习`, counter `1 / 7`, renders all 7 lessons inside the scrollable quick-switch list (none dropped), clicking the 6th item or clicking next chevron (`下一个进行中课时`) switches the spotlight preview and updates counter to `2 / 7` / `6 / 7`, and the spotlight CTA link points to `/courses/<id>?lesson=<id>`.
2. **Single-library scoping (`/?library=lib-design` inside `LibraryProvider + Layout`):** Header shows `设计艺术`,「继续学习」filters out `lib-dev` lessons (hiding hero when `lib-design` has none), tag chips only show `UI设计` (not `Go`/`React`), and clicking「查看全部」returns to `/`.
3. **Review Focus #2 (Stale `/?library=nonexistent-id`):** Falls back cleanly to `全部课程` displaying all courses instead of an empty broken view.
4. **Client-side sort:** Switching sort selector (`排序方式`) to `学习进度` reorders rendered course cards by `played_percentage` descending.

- [ ] **Step 2: Run `HomePage.test.tsx` to verify new tests fail**

Run: `pnpm --dir web test src/__tests__/HomePage.test.tsx`
Expected: FAIL on spotlight counter/controls, library scoping, stale param fallback, and sort selector.

- [ ] **Step 3: Redesign `web/src/pages/HomePage.tsx`**

- **Library resolution:**
  - When `libCtx.isProvided`: read `?library=` from `useSearchParams()`. Validate against loaded `libraries`; if non-empty and not found in `libraries` (once loaded), resolve `activeLibraryId = 'all'` (Review Focus #2). Reload home/courses/tags whenever `libCtx.catalogVersion` increments.
  - When `!libCtx.isProvided` (standalone test fallback): retain local `selectedLibraryId` + clean dual-theme library pill bar + inline create/rename/delete forms so all isolated unit tests in `HomePage.test.tsx` and `UXFeatures.test.tsx` pass seamlessly.
- **Top Greeting & Search Header (Citonema rhythm):**
  - Left: local-hour greeting (`早上好 / 上午好 / 下午好 / 晚上好` + Sun/Moon icon) + primary `<h1>` (`全部课程` or active library name) + count badge (`共 N 门`) + quick `查看全部` chip when viewing a single library.
  - Right: pill search input (`rounded-full bg-card border border-subtle shadow-card`, placeholder `搜索课程名称、讲师或标签...`, clear button) + `+ 新建空白课程` primary-tinted pill button (opens a clean dual-theme inline card/modal; on creation calls `libCtx.bumpCatalogVersion()` before navigating to edit page).
- **Citonema Spotlight「继续学习」Hero Card:**
  - `scopedResume`: on `'all'` shows full `resumeLessons`; on specific `activeLibraryId` filters by `item.library_id === activeLibraryId`. Reset `activeResumeIdx` to `0` when `activeLibraryId` or `scopedResume.length` changes.
  - When `scopedResume.length > 0`: render floating `rounded-3xl bg-card border border-subtle shadow-card p-5 sm:p-6`.
    - Focused item displays 16:9 `rounded-2xl` thumbnail (using `item.cover_path || item.thumbnail_path ? item.id : item.course_id` to preserve thumbnail fallback logic), progress bar, `{pct}%` pill, floating circular blue Play CTA (`bg-accent text-ink-inverse shadow-accent`), course title, lesson title, chapter title, and direct `<Link to={`/courses/${focused.course_id}?lesson=${focused.id}`}>` containing `继续播放` (plus clickable title/poster link so `getByRole('link', { name: /第 1 节/ })` matches cleanly).
    - When `scopedResume.length === 1`: balanced horizontal 2-column layout (5:7).
    - When `scopedResume.length > 1`: right-hand sub-panel with header `{activeResumeIdx + 1} / {scopedResume.length}` + prev/next buttons (`aria-label="上一个进行中课时"` / `aria-label="下一个进行中课时"`) and bounded scrollable list (`max-h-[296px] overflow-y-auto`) showing ALL items (active item has left `bg-accent` vertical bar + `bg-accent-soft`).
- **Tag Filter & Sort Strip:**
  - Compute `scopedTags`: on `'all'` use `tags` from `/api/v1/tags`; on specific library aggregate counts from that library's courses. Clear `selectedTag` if absent from `scopedTags`.
  - Render wrap pill chips + `<select aria-label="排序方式">` (`default` 默认顺序 / `updated` 最近更新 / `progress` 学习进度 / `title` 标题排序).
- **Course Poster Grid:**
  - `grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-6`.
  - Floating `rounded-3xl bg-card border border-subtle shadow-card hover:shadow-card-hover hover:-translate-y-0.5 transition-all` cards with inset `p-2.5 pb-0` `rounded-2xl` 16:10 poster, refined dual-theme fallback gradients, clean completed/duration overlays, tags (max 3), slim progress bar, and footer actions (`开始学习`/`继续学习` + `编辑` keeping `aria-label={`编辑 ${course.title}`}`).
- **Empty States:** Dual-theme floating cards preserving `还没有导入任何课程` → `前往存储与导入` and `清除筛选条件`.

- [ ] **Step 4: Run tests to verify Task 4 passes**

Run: `pnpm --dir web test src/__tests__/HomePage.test.tsx src/__tests__/UXFeatures.test.tsx src/__tests__/ThemeAndShell.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit Task 4**

```bash
git add web/src/pages/HomePage.tsx web/src/__tests__/HomePage.test.tsx
git commit -m "feat(web): redesign HomePage with Citonema spotlight hero, scoped libraries, and dual-theme cards"
```

---

### Task 5: Theater-First `CourseDetailPage` & Dual-Theme `VideoPlayer`

**Files:**
- Modify: `web/src/pages/CourseDetailPage.tsx`
- Modify: `web/src/components/VideoPlayer.tsx`
- Test: `web/src/__tests__/VideoPlayer.test.tsx`

**Interfaces:**
- Consumes: semantic design tokens (Task 1), `Tooltip`, `useToast`, `VideoPlayerProps` (unchanged).
- Produces: Theater-first `CourseDetailPage` and dual-theme `VideoPlayer` (all existing playback/HLS/progress/hotkey behaviors unchanged).

- [ ] **Step 1: Add layout-order & dual-theme test assertion in `web/src/__tests__/VideoPlayer.test.tsx`**

Add a test verifying that on `CourseDetailPage`:
- Compact header shows `返回课程库`, course title, progress summary, `收起目录` toggle, and `编辑课程`.
- The `<video>` player element appears in DOM order **before** the course overview section (`compareDocumentPosition` confirms video precedes course overview text) so video is theater-first above the fold.
- Unsupported codec fallback renders `Infuse`, `VidHub`, `SenPlayer` and codec badges without hardcoded dark-only `bg-slate-900` classes.

- [ ] **Step 2: Run `VideoPlayer.test.tsx` to verify new assertion fails**

Run: `pnpm --dir web test src/__tests__/VideoPlayer.test.tsx`
Expected: FAIL because current `CourseDetailPage` renders the bulky metadata card above `<VideoPlayer>`.

- [ ] **Step 3: Redesign `web/src/components/VideoPlayer.tsx` and `web/src/pages/CourseDetailPage.tsx`**

- `VideoPlayer.tsx`:
  - Outer shell: `rounded-3xl border border-subtle bg-card shadow-card overflow-hidden`.
  - Inner `<video>` viewport stays pure `bg-black` for media contrast.
  - Bottom control strip: `border-t border-subtle bg-card px-4 py-3 text-xs text-ink-secondary`, with themed pill `<select>`s (`bg-subtle border-default text-ink-primary`), subtle `<kbd>` shortcut chips (`Space`, `←/→`, `F`), and fullscreen button.
  - Unsupported codec fallback card: `rounded-3xl border border-status-warning/30 bg-card shadow-card` with `bg-status-warning-soft text-status-warning-ink` icon tile, `bg-accent-soft text-accent-ink` recommended app pills, and `bg-subtle text-ink-secondary` codec badges.
- `CourseDetailPage.tsx`:
  - Compact top strip: `返回课程库` pill link + course `<h1>` title + completion badge + compact progress pill (`已学 X/Y 节 · Z%`) + right controls (`收起目录`/`展开目录` + `编辑课程`).
  - Theater grid: `sidebarOpen ? 'lg:grid-cols-[minmax(0,1fr)_320px] xl:grid-cols-[minmax(0,1fr)_360px]' : 'grid-cols-1'`.
  - Left column order: (1) `<VideoPlayer />`, (2) Current Lesson Action Card (`rounded-3xl bg-card shadow-card p-5`: chapter eyebrow, lesson `<h2>`, `上一节` / `下一节` / `标记已看|标记未看`, optional lesson overview), (3) **Course Overview & Metadata Card** (`rounded-3xl bg-card shadow-card p-5`: compact rounded cover, instructor, chapter/lesson counts, duration, full overview, tags, full progress bar).
  - Right column sticky playlist (`rounded-3xl bg-card border border-subtle shadow-card overflow-hidden lg:sticky lg:top-6`): chapter headers on `bg-subtle/70`, scrollable `max-h-[calc(100vh-8rem)]`, active lesson row with Citonema left vertical bar (`bg-accent`) + `bg-accent-soft` + bold `text-ink-primary`, played checkmark in `text-status-success`, tabular-nums duration.

- [ ] **Step 4: Run `VideoPlayer.test.tsx` to verify Task 5 passes**

Run: `pnpm --dir web test src/__tests__/VideoPlayer.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit Task 5**

```bash
git add web/src/components/VideoPlayer.tsx web/src/pages/CourseDetailPage.tsx web/src/__tests__/VideoPlayer.test.tsx
git commit -m "feat(web): redesign CourseDetailPage theater-first layout and dual-theme VideoPlayer"
```

---

### Task 6: Studio `CourseEditorPage` & Dual-Theme `AIOrganizeModal`

**Files:**
- Modify: `web/src/pages/CourseEditorPage.tsx`
- Modify: `web/src/components/AIOrganizeModal.tsx`
- Test: `web/src/__tests__/EditorAndAI.test.tsx`

**Interfaces:**
- Consumes: semantic design tokens (Task 1), `useLibrary` (`bumpCatalogVersion`, Task 3), `Tooltip`, `useToast`, `@dnd-kit`, `api`.
- Produces: Redesigned `CourseEditorPage` (preserving exported `resolveDragReorder`) and `AIOrganizeModal`.

- [ ] **Step 1: Add test assertion verifying `CourseEditorPage` notifies `LibraryContext` on library change / course delete and renders sticky top bar**

In `web/src/__tests__/EditorAndAI.test.tsx`, verify existing `CourseEditorPage` and `AIOrganizeModal` flows plus check that changing `所属媒体库` and saving metadata updates cleanly when wrapped in `<LibraryProvider>` (triggering library refresh so sidebar counts stay accurate).

- [ ] **Step 2: Redesign `web/src/pages/CourseEditorPage.tsx` and `web/src/components/AIOrganizeModal.tsx`**

- `CourseEditorPage.tsx`:
  - **Sticky top studio bar** (`sticky top-0 z-20 -mx-4 px-4 sm:-mx-6 sm:px-6 lg:-mx-8 lg:px-8 py-3 bg-canvas/85 backdrop-blur-md border-b border-subtle`): `返回课程播放`, course title, and action cluster (`检查新文件`, primary `AI 智能整理`, `删除课程` with inline confirm pill).
  - Feedback banners & New Files Sync panel migrated to semantic status/accent cards (`rounded-2xl`).
  - Section 1 (`lg:grid-cols-3 gap-6`): left 2-col Metadata card (`rounded-3xl bg-card border border-subtle shadow-card p-6`) + right 1-col Cover & Backdrop card (`rounded-3xl bg-card border border-subtle shadow-card p-6`) with inset `rounded-2xl` previews and themed frame picker list.
  - Section 2: Chapter & Lesson Organizer with inline `+ 添加章节` form and `SortableChapterCard` (`rounded-3xl bg-card border border-subtle shadow-card`, recessed `bg-subtle/60` header band, drag highlight `ring-2 ring-accent shadow-card-hover`) + `SortableLessonRow` (`rounded-2xl border border-subtle bg-subtle/40 hover:bg-subtle`, themed subtitle/missing badges, drag handle, step/rename/delete controls).
  - Call `libCtx.bumpCatalogVersion()` after saving metadata when `library_id` changed, and after deleting a course.
  - Restyle embedded "从存储添加视频" modal to unified `bg-overlay backdrop-blur-sm` + `rounded-3xl bg-popover border border-subtle shadow-float`.
- `AIOrganizeModal.tsx`:
  - Restyle backdrop, `rounded-3xl bg-popover border border-subtle shadow-float` container, diff comparison cards (`bg-subtle` for original vs `bg-accent-soft border-accent-ring` for AI suggestion), checkboxes, error/loading states, and footer actions to semantic tokens; preserve all checkbox labels and safety invariants.

- [ ] **Step 3: Run `EditorAndAI.test.tsx` to verify Task 6 passes**

Run: `pnpm --dir web test src/__tests__/EditorAndAI.test.tsx`
Expected: PASS.

- [ ] **Step 4: Commit Task 6**

```bash
git add web/src/pages/CourseEditorPage.tsx web/src/components/AIOrganizeModal.tsx web/src/__tests__/EditorAndAI.test.tsx
git commit -m "feat(web): redesign CourseEditorPage studio layout and AIOrganizeModal"
```

---

### Task 7: Dual-Theme `StoragePage`, `ImportCourseModal`, & `DirectoryPickerModal`

**Files:**
- Modify: `web/src/pages/StoragePage.tsx`
- Modify: `web/src/components/ImportCourseModal.tsx`
- Modify: `web/src/components/DirectoryPickerModal.tsx`
- Test: `web/src/__tests__/EditorAndAI.test.tsx`, `web/src/__tests__/UXFeatures.test.tsx`

**Interfaces:**
- Consumes: semantic design tokens (Task 1), `useLibrary` (`registerLibrary`, `bumpCatalogVersion`, Task 3), `Tooltip`, `useToast`, `api`.
- Produces: Dual-theme `StoragePage`, `ImportCourseModal`, and `DirectoryPickerModal` synced with sidebar library state.

- [ ] **Step 1: Add failing test asserting `StoragePage` syncs newly created libraries and imported courses with `LibraryProvider` sidebar**

In `web/src/__tests__/UXFeatures.test.tsx`, add a test rendering `StoragePage` inside `LibraryProvider + Layout`, importing a course (and creating a library inside `ImportCourseModal`), and verifying the new library appears in the left sidebar immediately.

- [ ] **Step 2: Run `UXFeatures.test.tsx` to verify the new test fails**

Run: `pnpm --dir web test src/__tests__/UXFeatures.test.tsx`
Expected: FAIL because `StoragePage` does not yet invoke `registerLibrary` / `bumpCatalogVersion`.

- [ ] **Step 3: Redesign `web/src/pages/StoragePage.tsx`, `web/src/components/ImportCourseModal.tsx`, and `web/src/components/DirectoryPickerModal.tsx`**

- `StoragePage.tsx`:
  - Clean page header (`存储与导入` + subtitle + pill `+ 添加存储源` primary CTA).
  - Collapsible Add Storage Source card (`rounded-3xl bg-card border border-subtle shadow-card p-6`) with themed inputs and `浏览目录` button.
  - Storage Sources 2-col grid: `rounded-3xl bg-card shadow-card` cards with active left accent indicator / `ring-2 ring-accent-ring`, drive icon tile (`bg-accent-soft text-accent-ink`), scan report summary bar (`bg-subtle`), animated `扫描` button, and danger-soft `删除` icon.
  - Directory & File Browser card (`rounded-3xl bg-card border border-subtle shadow-card p-6`): pill breadcrumb trail + `导入当前目录` CTA; subdirectory rows with `bg-status-warning-soft text-status-warning-ink` folder icon tiles and `一键导入为课程` buttons; media file section with segmented status filter (`bg-muted p-0.5 rounded-xl`), pill search box, missing count badge, and scrollable themed rows.
  - Wire `onLibraryCreated={(lib) => { setLibraries(...); libCtx.registerLibrary(lib); }}` and `onImported={(course) => { ...; libCtx.bumpCatalogVersion(); }}` so the left sidebar updates immediately after importing courses or creating a library inside the modal.
- `ImportCourseModal.tsx` & `DirectoryPickerModal.tsx`:
  - Migrate both modals completely to `bg-overlay backdrop-blur-sm` + `rounded-3xl bg-popover border border-subtle shadow-float`, recessed `bg-subtle` toolbars/lists, themed inputs/checkboxes/buttons, keeping all accessible names (`选择本地目录`, `选择此目录`, `返回上级`, `显示隐藏目录`, `课程标题`, `所属媒体库`, `确认导入`) and `Escape` handling intact.

- [ ] **Step 4: Run tests to verify Task 7 passes**

Run: `pnpm --dir web test src/__tests__/EditorAndAI.test.tsx src/__tests__/UXFeatures.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit Task 7**

```bash
git add web/src/pages/StoragePage.tsx web/src/components/ImportCourseModal.tsx web/src/components/DirectoryPickerModal.tsx web/src/__tests__/UXFeatures.test.tsx
git commit -m "feat(web): redesign StoragePage, ImportCourseModal, and DirectoryPickerModal"
```

---

### Task 8: Dual-Theme `SettingsPage`, `SystemUpdateSection`, & `SetupLoginPage`

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/components/SystemUpdateSection.tsx`
- Modify: `web/src/pages/SetupLoginPage.tsx`
- Test: `web/src/__tests__/SystemUpdate.test.tsx`, `web/src/__tests__/UXFeatures.test.tsx`, `web/src/__tests__/HomePage.test.tsx`

**Interfaces:**
- Consumes: semantic design tokens & `ThemeSwitcher` (Task 1), `useUpdate`, `useToast`, `Tooltip`, `copyTextToClipboard`.
- Produces: Dual-theme `SettingsPage`, `SystemUpdateSection` (+ its confirmation modal), and `SetupLoginPage` (with top-right `ThemeSwitcher`).

- [ ] **Step 1: Add pre-login theme toggle test in `web/src/__tests__/HomePage.test.tsx`**

Add a test inside `describe('SetupLoginPage', ...)` asserting that `SetupLoginPage` renders the top-right theme switcher (`aria-label="切换主题模式"` or `aria-label="主题模式"`) and toggling it updates `document.documentElement.dataset.theme` before authentication.

- [ ] **Step 2: Run `HomePage.test.tsx` to verify the new test fails**

Run: `pnpm --dir web test src/__tests__/HomePage.test.tsx`
Expected: FAIL because `SetupLoginPage` does not render `ThemeSwitcher` yet.

- [ ] **Step 3: Redesign `web/src/pages/SettingsPage.tsx`, `web/src/components/SystemUpdateSection.tsx`, and `web/src/pages/SetupLoginPage.tsx`**

- `SettingsPage.tsx`:
  - Comfortable `max-w-4xl space-y-6` container.
  - Card 1 (Jellyfin Connection Guide): `rounded-3xl bg-card border border-accent-ring/60 shadow-card p-6` with soft accent radial tint, recessed `bg-subtle` mono boxes for Server URL and Username with copy buttons (preserving `copyTextToClipboard` + toast + inline checkmark), and 3 client setup sub-cards (`Infuse`, `VidHub`, `SenPlayer`).
  - Card 2: `<SystemUpdateSection />` retained in slot #2 with existing `#system-update` smooth scroll `useEffect`.
  - Card 3 (Server & AI Config): `rounded-3xl bg-card border border-subtle shadow-card p-6`, clear subsection divider for OpenAI-compatible AI settings, themed inputs/checkboxes, footer with `测试连接` + `保存设置`.
  - Card 4 (Admin Password) & Card 5 (Active Sessions): `rounded-3xl bg-card border border-subtle shadow-card p-6`, clean responsive table with `当前会话` emerald pill and danger-soft `注销` button.
- `SystemUpdateSection.tsx` (~792 lines):
  - Full semantic migration across every state (loading, idle/up-to-date, update-available card, plain-text release notes `<pre>`/`<div>`, active download/verify/apply/restart progress bar, restart waiting/succeeded/rolled-back/timeout banners, cannot-apply reason banner, auto-check locked-by-env notice, config form, and the **update confirmation modal** at lines 711–787).
  - Keep `#system-update` id, all button labels, tooltips, validation rules, and state machine logic 100% untouched.
- `SetupLoginPage.tsx`:
  - Full-screen `min-h-screen bg-canvas text-ink-primary` with subtle ambient glow (`bg-accent/10 blur-3xl`), top-right floating `<ThemeSwitcher variant="compact" tooltipPlacement="bottom" />`, and centered `rounded-3xl border border-subtle bg-card p-8 sm:p-10 shadow-float max-w-md` card with squircle blue brand tile, themed icon-prefixed inputs, and vibrant primary blue submit button.

- [ ] **Step 4: Run tests to verify Task 8 passes**

Run: `pnpm --dir web test src/__tests__/SystemUpdate.test.tsx src/__tests__/UXFeatures.test.tsx src/__tests__/HomePage.test.tsx src/__tests__/EditorAndAI.test.tsx`
Expected: PASS (all 23 tests in `SystemUpdate.test.tsx` + all `SettingsPage` / `SetupLoginPage` tests green).

- [ ] **Step 5: Commit Task 8**

```bash
git add web/src/pages/SettingsPage.tsx web/src/components/SystemUpdateSection.tsx web/src/pages/SetupLoginPage.tsx web/src/__tests__/HomePage.test.tsx
git commit -m "feat(web): redesign SettingsPage, SystemUpdateSection, and SetupLoginPage for dual themes"
```

---

### Task 9: Legacy-Style Audit, Embedded Web Sync, Full Test Suite & Live Browser Visual Verification

**Files:**
- Delete: `web/src/pages/Placeholders.tsx` (unused v0.1 scaffolding containing hardcoded dark classes)
- Modify: `internal/server/webdist/*` (generated via `make sync-web`)
- Audit: `web/src/**/*.{ts,tsx,css}`

**Interfaces:**
- Consumes: All completed frontend tasks (Tasks 1–8) and `Makefile` targets (`make sync-web`, `make test`, `make build`).
- Produces: Verified production bundle synced to `internal/server/webdist`, clean audit, passing Go + Vitest suites, and visual confirmation in Light & Dark modes.

- [ ] **Step 1: Remove unused `web/src/pages/Placeholders.tsx` and run hardcoded dark-theme residue audit across `web/src/`**

Verify `Placeholders.tsx` has zero references, delete it, and run:
`grep -rnE "bg-slate-(950|900|800)|border-slate-(800|700)|text-slate-(100|200|300|400|500)|bg-indigo-|text-indigo-" web/src/ --exclude-dir=__tests__`
Expected: Zero unintended matches (fix any missed legacy utility classes before syncing).

- [ ] **Step 2: Build, sync embedded frontend assets, and run the complete Go + Web test suite**

Run: `make sync-web && make test && make build`
Expected:
- `pnpm --dir web build` (`tsc --noEmit && vite build`) exits 0
- `go test -race -count=1 ./...` exits 0
- `pnpm --dir web test` exits 0 (all test files & assertions green)
- `./coursefin` binary builds cleanly.

- [ ] **Step 3: Perform live browser visual verification in both Light & Dark modes**

Start `./coursefin` on a temporary local port with a temporary data dir (using `COURSEFIN_ADMIN_USER=admin COURSEFIN_ADMIN_PASS=admin123`) and inspect via the browser subagent:
- Login / Setup page (Light + Dark)
- HomePage (Light + Dark, desktop `1440px` + mobile `390px`), verifying left sidebar, library creation/switching, and theme toggle persistence across reload
- StoragePage, SettingsPage (including `SystemUpdateSection`), and CourseDetailPage / CourseEditorPage (verifying `1180px` laptop theater auto-collapse and Light/Dark readability)
- Stop the temporary local server cleanly afterward and remove temp data dir.
Fix any visual/spacing/contrast issues found, re-run `make sync-web && make test`, and ensure clean diff.

- [ ] **Step 4: Commit synced embedded assets (and any visual polish fixes)**

```bash
git add web/src internal/server/webdist
git commit -m "chore(web): rebuild and sync redesigned dual-theme frontend assets"
```
