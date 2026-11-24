# CourseFin 全站 UI 大改版设计文档

- **Date:** 2026-11-24
- **Status:** Approved (Builder Review Passed)
- **Repository:** `CourseFin` (`v1.1.0`, commit `ca7de9d`)
- **Reference Inspiration:** [Citonema - Light Version | Movie Dashboard Design (Dribbble #18417634)](https://dribbble.com/shots/18417634-Citonema-Light-Version-Movie-Dashboard-Design)

---

## 1. Intent & Why

CourseFin v1.1.0 已经具备完整的本地课程索引、Jellyfin 协议兼容、HLS 秒级跳转播放、拖拽编排、AI 元数据整理与内置自更新能力，但当前的 Web 前端界面（`web/src/`）存在明显体验短板：

1. **视觉模板感强、仅支持单一深色模式**：全站硬编码 `slate-950 / slate-900 / border-slate-800 / indigo-600`，缺乏统一语义令牌与白天模式，在日间桌面环境下对比僵硬、层次单调。
2. **顶部导航架构不适合多媒体库扩展**：当前媒体库标签、新建/重命名/删除按钮和搜索框全部横向堆叠在首页中部，媒体库稍多即换行拥挤，且离开首页后无法快速切换媒体库。
3. **关键页面信息层级不够克制**：
   - 首页「继续学习」与普通课程网格视觉权重接近，缺少清晰的焦点入口；
   - 播放页（`CourseDetailPage`）在视频播放器上方放置了大块课程元数据卡片，把核心视频画面压低至折叠线附近；
   - 课程编辑页（`CourseEditorPage`）操作分散、页面纵长，高频操作（保存、AI 整理、检查新文件）在滚动后不便触达。

本次改版以 **Citonema 的轻盈悬浮卡片语言**为视觉蓝本，将全站升级为**单层可折叠左侧栏为主导航骨架 + 完整的白天/黑夜双主题设计体系**，覆盖全部 6 个页面、`SystemUpdateSection` 自更新模块、全部弹窗与基础反馈原语。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals
1. **单层可折叠左侧边栏主架构**：将主导航、我的媒体库（含课程计数、新建、重命名、删除）、版本更新提醒、明暗主题切换器、账号与退出登录统一收束至左侧边栏；桌面端支持展开/折叠为精简图标条，移动端收进平滑侧滑抽屉。
2. **完整的白天/黑夜双主题系统**：基于 Tailwind v4 `@theme inline` + 语义化 CSS 变量构建统一设计令牌，支持「浅色 / 深色 / 跟随系统」三态切换、持久化记忆与首屏防闪烁（Zero-FOUC）。
3. **Citonema 风格的空气感层次与页面级版式重塑**：
   - 浅色模式采用柔和冰灰白画布（`#F4F6FB`）+ 纯白大圆角浮层卡片（`#FFFFFF`）+ 柔和漫反射阴影 + 清透天蓝强调色（`#1E88E5`）；
   - 深色模式采用哑光深炭灰画布（`#0F1117`）+ 抬升石墨卡片（`#181C27`）+ 克制微边 + 提亮天蓝（`#3B82F6`）；
   - 首页升级为问候头 + Citonema 风格「继续学习」大焦点卡 + 宽敞课程海报网格；
   - 播放页改为剧场优先布局（首屏即视频播放器与右侧目录，进入播放页时全局左侧栏临时折叠为图标条以保证笔记本宽屏观影宽度，课程简介下沉至播放器下方）；
   - 课程编辑页增加吸顶工作台顶栏与清晰双区划分；
   - 存储与导入、系统设置（含 `SystemUpdateSection` 及更新确认弹窗）、登录/初始化页与全部弹窗统一迁入新视觉系统。
4. **零功能回退与单二进制无膨胀**：完整保留 v1.1.0 现有全部课程管理、播放、导入、AI 整理、内置自更新、目录浏览、复制回退、Tooltip 与 Toast 能力，保持零新增外部 npm 运行时依赖。

### Non-Goals
- 修改任何 Go 后端业务逻辑或 REST / Jellyfin / 自更新 API 协议（后端仅通过 `make sync-web` 同步重新打包后的前端静态资源）。
- 引入 Citonema 原图中的社交功能（好友、群聊、一起看派对右侧栏、论坛等）——CourseFin 聚焦个人私有课程学习与管理，保留两栏骨架（左侧栏 + 宽敞主内容区）。
- 引入重型第三方 UI 组件库（如 Radix UI / MUI / Ant Design / shadcn 全家桶）。
- 新增需要后端支持的新功能（如多用户头像上传、全局跨页搜索接口、服务端保存主题偏好）。

### Anti-Goals
- **绝不因视觉改版破坏现有可用性或无障碍语义**：保留清晰的中文文案、核心导航与按钮可访问名称（如 `课程库`、`存储与导入`、`设置`、`新版本 vX.Y.Z`）、键盘操作（`Escape` 关闭弹窗、播放器 `Space / ← / → / F` 快捷键、Tooltip 键盘聚焦触发）与高对比度（满足 WCAG AA）。
- **绝不在切换路由时导致侧边栏反复闪烁或重复拉取接口**：改用嵌套布局路由持久化侧边栏、更新状态与媒体库状态。
- **绝不出现双主题割裂**：不能留有任何残留的硬编码 `bg-slate-950 / text-white / border-slate-800` 导致浅色模式下文字看不清或色块发黑（视频画面区域 `<video>` 容器除外，播放画布保持纯黑以保证观影对比度）。

---

## 3. Constraints

1. **技术栈约束**：React `19.1` + TypeScript `5.8` + React Router `7.6` + Tailwind CSS `4.1` (`@tailwindcss/vite`) + `lucide-react` + `@dnd-kit` + `hls.js`，**不新增 npm 依赖**。
2. **嵌入式交付约束**：构建产物由 `make sync-web` 复制到 `internal/server/webdist`，运行在 `/web` 基础路径下（`<BrowserRouter basename="/web">`）；`index.html` 中的防闪烁脚本必须为纯内联脚本，不依赖外部请求。
3. **非 HTTPS 局域网兼容**：所有新组件继续兼容 `http://<LAN-IP>:8096` 环境，剪贴板复制保持 `copyTextToClipboard` 回退机制。
4. **测试环境约束**：Vitest + jsdom 环境中 `window.matchMedia` 默认可能未定义，且大量既有单元测试会独立挂载单个页面/弹窗组件（无 `<App>` 包裹）。`ThemeContext`、`LibraryContext`、`UpdateContext`、`ToastProvider` 与 `Tooltip` 均需提供安全的默认上下文/环境守卫，独立渲染时不得抛错。
5. **未登录状态约束**：`/login` 页面不得触发受保护的 `/api/v1/libraries`、`/api/v1/auth/me` 或 `/api/v1/system/update` 请求（避免 401 副作用）；因此 `LibraryProvider` 位于 `RequireAuth` 鉴权边界之内，`UpdateProvider` 继续保持现有的 `getToken()` 守卫。

---

## 4. Approaches Considered

| 方案 | 核心做法 | 主要取舍 | 结论 |
|---|---|---|---|
| **方案一：语义化 CSS 变量主题体系 + 零依赖整站版式重做** | 在 `index.css` 用 Tailwind v4 `@theme inline` + `[data-theme="dark"]` 统一管理所有表面/文字/边框/阴影/状态令牌；以 `ThemeProvider` + `LibraryProvider` + 持久化 `Layout` 侧边栏驱动全站 6 页、`SystemUpdateSection` 与全部弹窗重设计，零新增依赖。 | 颜色和阴影源头唯一、明暗切换零闪烁、打包零膨胀、全站视觉和交互结构一次性跃升；弹窗/Tooltip 沿用并打磨现有轻量实现。 | **已选定（用户确认）** |
| **方案二：引入 Radix / shadcn + 逐组件写 `dark:` 类名** | 引入 `@radix-ui/*` 原语，并在各页面组件逐处手写 `bg-white dark:bg-slate-900` 成对类名。 | 无障碍原语开箱即用；但引入额外依赖、大量重复 `dark:` 样板难以统一调色，且容易大面积破坏既有测试断言与轻量交付原则。 | 放弃 |
| **方案三：仅换壳（顶栏改侧边栏 + 叠加浅色补丁）** | 保持 6 个页面现有排布不变，只把导航改成侧边栏并补浅色类名。 | 改动最快；但首页缺少焦点层次、播放页元数据仍挤压播放器、编辑页依然拥挤，浅色下套旧暗色边框显生硬，达不到“一整版大改版”目标。 | 放弃 |

---

## 5. Detailed Design

### 5.1 视觉语言与双主题设计令牌（Design System）

> *[我方拍板]* 配色数值、默认主题为「跟随系统」、以及亮暗双模式统一采用天蓝强调色，均为我方拟定的默认视觉体系。

#### 5.1.1 语义令牌表（定义于 `web/src/index.css`）

| 语义角色 | CSS 变量 | 浅色模式 (`:root`) | 深色模式 (`[data-theme="dark"]`) | 用途 |
|---|---|---|---|---|
| 画布底色 | `--surface-canvas` | `#F4F6FB` | `#0F1117` | 页面整体背景 |
| 侧边栏底色 | `--surface-sidebar` | `#FFFFFF` | `#141720` | 左侧主导航栏背景 |
| 卡片表面 | `--surface-card` | `#FFFFFF` | `#181C27` | 浮层卡片、内容容器 |
| 柔和次表面 | `--surface-subtle` | `#EEF1F8` | `#202636` | 输入框底、筛选胶囊、悬浮态、次按钮 |
| 较深次表面 | `--surface-muted` | `#E3E8F3` | `#283044` | 进度条底槽、强悬浮态、分段器槽 |
| 浮层/弹窗 | `--surface-popover` | `#FFFFFF` | `#1B202D` | Modal、Toast、菜单面板 |
| 遮罩层 | `--surface-overlay` | `rgba(15, 23, 42, 0.42)` | `rgba(6, 8, 14, 0.72)` | Modal 与移动抽屉背景遮罩 |
| Tooltip 背景 | `--surface-tooltip` | `#181B26` | `#272E40` | 悬浮提示气泡（高对比） |
| 主文字 | `--ink-primary` | `#181B26` | `#F1F5F9` | 标题、正文主内容、激活导航 |
| 次文字 | `--ink-secondary` | `#4B5568` | `#B4BDD0` | 简介、表单标签、普通导航 |
| 弱文字 | `--ink-muted` | `#7C859B` | `#7B859D` | 辅助说明、分组微标题、时长/时间 |
| 反白文字 | `--ink-inverse` | `#FFFFFF` | `#FFFFFF` | 强调色实心按钮上的文字 |
| Tooltip 文字 | `--ink-tooltip` | `#F8FAFC` | `#F1F5F9` | Tooltip 气泡文字 |
| 极细边框 | `--border-subtle` | `rgba(24, 27, 38, 0.07)` | `rgba(255, 255, 255, 0.07)` | 卡片极浅描边、内部分割线 |
| 标准边框 | `--border-default` | `rgba(24, 27, 38, 0.13)` | `rgba(255, 255, 255, 0.12)` | 输入框、描边按钮 |
| 强调边框 | `--border-strong` | `rgba(24, 27, 38, 0.22)` | `rgba(255, 255, 255, 0.22)` | 悬浮边框、高对比分割 |
| 主强调色 | `--accent` | `#1E88E5` | `#3B82F6` | 主按钮、播放键、进度条、左侧激活竖条 |
| 强调悬浮 | `--accent-hover` | `#1565C0` | `#60A5FA` | 主按钮 Hover |
| 强调浅底 | `--accent-soft` | `rgba(30, 136, 229, 0.11)` | `rgba(59, 130, 246, 0.16)` | 激活行底色、标签高亮底 |
| 强调描边/光环 | `--accent-ring` | `rgba(30, 136, 229, 0.28)` | `rgba(59, 130, 246, 0.36)` | Focus ring、选中卡片边 |
| 强调文字 | `--accent-ink` | `#1565C0` | `#60A5FA` | 浅底/卡片上的强调文字（保证 AA 对比度） |
| 成功语义 | `--status-success` / `-soft` / `-ink` | `#10B981` / `rgba(16,185,129,0.12)` / `#047857` | `#10B981` / `rgba(16,185,129,0.16)` / `#34D399` | 已完成、正常状态、新版本可用、成功反馈 |
| 警示语义 | `--status-warning` / `-soft` / `-ink` | `#F59E0B` / `rgba(245,158,11,0.14)` / `#B45309` | `#F59E0B` / `rgba(245,158,11,0.16)` / `#FBBF24` | 文件夹图标、回退提示、编码提示 |
| 危险语义 | `--status-danger` / `-soft` / `-ink` | `#EF4444` / `rgba(239,68,68,0.11)` / `#B91C1C` | `#F43F5E` / `rgba(244,63,94,0.16)` / `#FB7185` | 删除、缺失文件、错误反馈 |

#### 5.1.2 阴影与圆角体系（Citonema 悬浮质感核心）

- **圆角阶梯**：
  - 顶级内容卡片 / 焦点卡 / 弹窗：`rounded-3xl`（24px）
  - 封面图内嵌圆角 / 章节卡 / 表单控件组：`rounded-2xl`（16px）
  - 按钮、输入框、列表行：`rounded-xl`（12px）或胶囊 `rounded-full`（搜索栏、主题切换器、主 CTA、圆形播放键）
- **阴影变量**：
  - `--shadow-card`：浅色 `0 1px 2px rgba(24,27,38,0.04), 0 8px 24px -6px rgba(24,27,38,0.06)`；深色 `0 1px 2px rgba(0,0,0,0.28), 0 8px 24px -8px rgba(0,0,0,0.42)`
  - `--shadow-card-hover`：浅色 `0 4px 12px rgba(24,27,38,0.06), 0 18px 36px -10px rgba(30,136,229,0.14)`；深色 `0 6px 16px rgba(0,0,0,0.36), 0 18px 36px -10px rgba(59,130,246,0.20)`
  - `--shadow-sidebar`：浅色 `4px 0 24px -12px rgba(24,27,38,0.07)`；深色 `4px 0 28px -12px rgba(0,0,0,0.55)`
  - `--shadow-float`：弹窗与 Toast 浮层阴影
  - `--shadow-accent`：蓝色圆形播放键与主按钮的柔和蓝光阴影
- **Tailwind v4 集成方式**：在 `web/src/index.css` 中使用 `@theme inline { --color-canvas: var(--surface-canvas); ... }` 暴露全部语义色类（`bg-canvas`, `bg-card`, `bg-sidebar`, `bg-subtle`, `text-ink-primary`, `border-subtle`, `bg-accent` 等），并定义 `.shadow-card` / `.shadow-card-hover` / `.shadow-sidebar` / `.shadow-float` / `.shadow-accent` 工具类与双主题自定义细滚动条。

#### 5.1.3 主题状态与防闪烁机制

- **文件**：`web/src/contexts/ThemeContext.tsx` + `web/index.html` 内联初始化脚本。
- **存储键**：`localStorage` 键 `coursefin_theme`，取值 `'light' | 'dark' | 'system'`，缺省或非法值回退为 `'system'`。
- **DOM 同步**：根据 `resolvedTheme`（`'light' | 'dark'`）同步设置：
  - `document.documentElement.dataset.theme = resolvedTheme`
  - `document.documentElement.classList.toggle('dark', resolvedTheme === 'dark')`
  - `document.documentElement.style.colorScheme = resolvedTheme`
- **系统主题监听**：当 `mode === 'system'` 时订阅 `window.matchMedia('(prefers-color-scheme: dark)')` 的 `change` 事件（使用 `matchMedia` 存在性检查以兼容 jsdom）。
- **防闪烁（Zero-FOUC）**：在 `web/index.html` `<head>` 内联一段极短同步 `<script>`，在首帧渲染前完成 `data-theme`、`.dark` 与 `colorScheme` 写入，移除 `<body>` 上原有的硬编码 `bg-slate-950 text-slate-100`，改由语义类控制背景与文字色。
- **独立渲染安全**：`useTheme()` 在无 `<ThemeProvider>` 时返回安全默认对象，不抛出异常。

---

### 5.2 应用外壳、路由持久化与媒体库共享状态

#### 5.2.1 持久化嵌套路由与 Provider 组合（`web/src/App.tsx`）

将当前每个路由重复包裹 `<RequireAuth><Layout>...</Layout></RequireAuth>` 重构为嵌套布局路由，同时完整保留 v1.1.0 的 `<UpdateProvider>`：

- 根组件层级：`<ThemeProvider>` → `<ToastProvider>` → `<UpdateProvider>` → `<BrowserRouter basename="/web">` → `<AppRoutes />`。
- `/login` 路由独立渲染 `<SetupLoginPage />`（不受 `RequireAuth` 包裹，不挂载 `LibraryProvider`）。
- 受保护路由统一挂载在 `<Route element={<RequireAuth><AuthenticatedShell /></RequireAuth>}>` 下，子路由为 `/`、`/courses/:courseId`、`/courses/:courseId/edit`、`/storage`、`/settings`；`AuthenticatedShell` 内部挂载 `<LibraryProvider>` 与 `<Layout><Outlet /></Layout>`。
- **效果**：跨页面切换时左侧边栏保持挂载不闪烁、折叠状态与已加载媒体库列表持续保留；且 `<Layout>` 仍保留可选 `children?: ReactNode` 属性，兼容单元测试中 `<Layout><HomePage /></Layout>` 的直接渲染方式。

#### 5.2.2 共享媒体库上下文（`web/src/contexts/LibraryContext.tsx`）

- **职责**：集中管理左侧边栏与各业务页共享的 `libraries: Library[]`、当前登录用户名 `username: string`（通过 `/api/v1/auth/me` 静默获取，失败时降级显示 `'管理员'`）、`loading` 状态及变更同步计数器 `catalogVersion: number`。
- **核心方法**：
  - `refreshLibraries()`：重新拉取 `GET /api/v1/libraries` 并递增 `catalogVersion`（供 `HomePage` 同步刷新课程与继续学习数据）。
  - `createLibrary(name)`：调用 `api.createLibrary(name)`，更新列表、递增 `catalogVersion`、弹出成功 Toast 并返回新建的 `Library`。
  - `renameLibrary(id, name)`：调用 `api.updateLibrary(id, name)`，原地更新列表、递增 `catalogVersion` 并弹出成功 Toast。
  - `notifyLibraryDeleted(deleted, mode, targetName)`：触发对应 Toast，并刷新媒体库与 `catalogVersion`。
  - `registerLibrary(lib)`：供 `ImportCourseModal` 内顺手新建媒体库时同步写入侧边栏列表，避免刷新延迟。
  - `bumpCatalogVersion()`：供课程导入、编辑改库、删除课程等操作通知侧边栏更新各媒体库的 `course_count`（内部触发静默 `refreshLibraries`）。
- **URL 同步约定**：
  - `/` 表示查看「全部课程」；`/?library=<libraryId>` 表示筛选查看指定媒体库。
  - 在任意页面点击左侧边栏中的某个媒体库，立即 `navigate('/?library=' + encodeURIComponent(lib.id))`；点击「课程库」主导航回到 `/`。
  - 当正在浏览 `/?library=<deletedId>` 且该媒体库被删除时，自动回跳 `/`（全部课程）。
- **独立渲染降级（测试友好）**：`LibraryContext` 提供 `isProvided: boolean` 标志。若 `HomePage` 在孤立单元测试中被直接渲染（未包裹 `LibraryProvider`），`HomePage` 自行使用接口拉取到的 `libraries` 并在页内优雅渲染备用媒体库筛选条，保证既有孤立测试零破坏；正式应用与集成测试均运行在 `LibraryProvider + Layout` 的左侧边栏模式下。

#### 5.2.3 左侧主导航栏与自更新提醒集成（`web/src/components/Layout.tsx`）

- **桌面端（`lg` ≥ 1024px）**：
  - 固定在视口左侧的全高侧边栏（`sticky top-0 h-screen`），展开宽度 `260px`，折叠宽度 `76px`（用户手动偏好持久化至 `localStorage` 键 `coursefin_sidebar_collapsed`），带平滑过渡 `transition-[width] duration-200`。
  - **剧场模式临时折叠** *[我方拍板]*：当路由匹配课程播放页 `/courses/:courseId`（不含 `/edit`）时，左侧边栏**临时自动折叠为 `76px` 图标条**，且**不覆写**用户的 `localStorage` 偏好；用户在播放页内仍可手动点开临时展开，离开播放页后自动恢复其持久化偏好。这样既避免 `1024px–1366px` 笔记本下左右双栏挤压播放器，又保持了侧边栏偏好不被意外改写。
  - **顶部品牌区**：圆角 squircle 渐变蓝图标块（`GraduationCap`）+ `CourseFin` 粗体字标 + 折叠/展开按钮（`PanelLeftClose` / `PanelLeftOpen`，带 Tooltip）。
  - **分组一 · 导航（微标题 `导航`）**：
    - `课程库`（`BookOpen`，路径 `/`）
    - `存储与导入`（`FolderInput`，路径 `/storage`）
    - `设置`（`Settings`，路径 `/settings`——保持与现有路由及测试一致的简明标签）
    - **Citonema 激活态**：左侧贴边 3px 蓝色圆角竖条指示器 + `bg-accent-soft` 圆角胶囊背景 + 粗体主文字 + 强调色图标。
    - 激活规则：当路径为 `/` 且无 `?library=` 参数时，激活「课程库」；当处于 `/?library=<id>` 时，激活对应媒体库行（全栏保持唯一激活指示器）；进入 `/courses/:id` 播放或编辑页时，保持「课程库」轻量激活以指示所属模块。
  - **分组二 · 我的媒体库（微标题 `我的媒体库` + 右侧 `+` 新建按钮）**：
    - 占据中间弹性滚动区（`flex-1 overflow-y-auto`），容纳多达数十个媒体库而不撑破底部。
    - 展开态每行以 `role="group" aria-label={`媒体库 ${lib.name}`}` 包裹：
      - 主点击按钮显示文件夹/书架微图标 + 媒体库名（超长省略）+ 右侧课程数圆角角标；
      - 悬浮、聚焦或当前激活时显示紧凑的「重命名媒体库」（`Pencil`）与「删除媒体库」（`Trash2`）图标按钮（带 Tooltip）。
    - 折叠态（76px）：主导航居中显示图标 + 右侧 Tooltip（`placement="right"`）；媒体库以首字圆角徽章展示 + 右侧 Tooltip（显示「媒体库名 (N 门)」）；底部 `+` 图标可直接唤起新建弹窗。
    - 新建与重命名媒体库统一通过轻量弹窗 `LibraryFormModal` 完成（避免在 260px 窄栏或折叠态下挤压输入框），删除沿用并换肤 `DeleteLibraryModal`（均挂载于 `Layout` 内，使全站任意页面都可安全管理媒体库）。
  - **底部工具区（固定沉底，细顶部分割线）**：
    - **自更新提醒胶囊（`hasUpdate` 时展示）** *[我方拍板]*：
      - 独立置于主题切换器正上方（不嵌套在 `设置` `<NavLink>` 内，保证合法 HTML 且已在 `/settings` 页时再次点击仍触发 `#system-update` 平滑滚动）；
      - 展开态渲染为翠绿柔和底完整胶囊 `<Link to="/settings#system-update">`（`ArrowUp` 图标 + `新版本 vX.Y.Z` + Tooltip `"发现新版本 vX.Y.Z，点击前往设置页查看并更新"`）；
      - 折叠态（`76px`）收缩为居中圆形翠绿 `ArrowUp` 图标链接（带 `sr-only` 文本 `新版本 vX.Y.Z` 与 `Tooltip placement="right"`）。
    - **明暗主题切换器**：展开态为三段式胶囊切换器（`浅色` Sun / `深色` Moon / `自动` Monitor，带 `aria-pressed` 与 Tooltip）；折叠态为单图标一键循环切换按钮（附当前模式 Tooltip）。
    - **账号与退出栏**：左侧圆形首字头像徽章（致敬 Citonema 左下角用户头像位）+ 用户名；右侧紧凑「退出登录」按钮（`LogOut` + Tooltip）。
- **移动端 / 平板端（`< lg`，< 1024px）**：
  - 顶部吸顶紧凑导航栏（`h-14 bg-sidebar/90 backdrop-blur-md border-b border-subtle`）：左侧菜单按钮（`Menu`，打开左侧抽屉）、品牌标、右侧操作组包含：**当 `hasUpdate` 时展示醒目的紧凑更新提示按钮**（`<Link to="/settings#system-update">`，内含 `data-testid="mobile-update-dot"` 绿点徽标，无需打开抽屉即可一眼看到更新并直达更新区）+ 一键明暗主题切换按钮。
  - 侧滑抽屉复用完整展开态侧边栏内容（同样包含完整「新版本 vX.Y.Z」胶囊），支持点击遮罩或按 `Escape` 关闭，点击任意导航、更新胶囊或媒体库后自动收起抽屉。

---

### 5.3 页面级改版规范（6 个核心页面 + 自更新模块）

#### 5.3.1 首页课程库（`web/src/pages/HomePage.tsx`）

1. **顶部问候与检索栏（Citonema Header 节奏）**：
   - 左侧：按本地时间动态展示温和问候（早上好 / 上午好 / 下午好 / 晚上好 + 对应太阳/月亮微图标）与当前视图主标题（如「全部课程」或当前选中的媒体库名 + 课程总数统计；处于具体媒体库视图时提供一键「查看全部」小胶囊）。
   - 右侧：胶囊形悬浮搜索框（`rounded-full bg-card shadow-card`，实时按标题/讲师/简介/标签过滤，含清空按钮）+ 「新建空白课程」按钮（点击弹出轻量创建表单弹窗或顶部展开条，仅当至少存在一个媒体库时展示）。
2. **单媒体库视图作用域（`/?library=<id>`）** *[我方拍板]*：
   - 在 `/`（「全部课程」）视图下：「继续学习」展示全局进行中课时，标签筛选条展示全局标签及计数。
   - 在 `/?library=<id>`（单个媒体库）视图下：「继续学习」自动按 `item.library_id === activeLibraryId` 过滤（该媒体库无进行中课时时隐藏焦点卡，避免在「设计艺术」标题下出现「编程开发」课程的割裂感），切换媒体库时自动重置焦点卡选中索引至 `0`；标签筛选条及计数按该媒体库内课程客户端聚合（若当前已选标签在新媒体库中不存在则自动清除）。
3. **「继续学习」Citonema 风格焦点大卡（当前视图存在进行中课时时展示）**：
   - 标题行：`继续学习` + 进行中数量角标。
   - 主体为一个大圆角双区悬浮白卡/炭灰卡（`rounded-3xl bg-card shadow-card p-5 sm:p-6`）：
     - **左侧主焦点区**：大圆角 16:9 课时缩略图（缺图时降级为课程封面或柔和渐变），左下角渐变遮罩展示进度条与百分比胶囊（如 `50%`），**悬浮圆形蓝色主播放按钮（`h-12 w-12 rounded-full bg-accent text-white shadow-accent`）**；右侧/下方配课程名小标、粗体课时名、章节名与「继续播放」主按钮链接（`/courses/${item.course_id}?lesson=${item.id}`）。
     - **右侧快速切换列表（仅当当前视图 `visibleResume.length > 1` 时显示，类似 Citonema 的 `Watching 1/8`）** *[我方拍板]*：
       - 顶部显示 `当前序号 / 总数`（如 `1 / 8`）与紧凑上下切换箭头（`<` / `>`）；
       - 列表保留**全部**进行中课时（不截断第 6–20 条），装在高度约 4–5 行的有界滚动容器（`max-h-[296px] overflow-y-auto`）中，选中项带左侧蓝色竖条高亮，点击切换左侧焦点预览，同时每项附直达播放操作；
       - 当 `visibleResume.length === 1` 时，隐藏右侧列表，左侧主焦点区自动展开为均衡的左右图文 5:7 布局。
4. **标签筛选与排序条**：
   - 存在标签时展示横向可换行胶囊标签（`全部标签` + 各标签含计数），激活态使用 `bg-accent text-white shadow-accent`；
   - 右侧增加轻量纯前端排序下拉（`默认顺序` / `最近更新` / `学习进度` / `标题排序`，不新增后端接口）。
5. **课程海报网格**：
   - 响应式 `grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-6`。
   - 每张课程卡片（`rounded-3xl bg-card shadow-card hover:shadow-card-hover hover:-translate-y-0.5 transition-all`）：
     - 上半部为内嵌留白圆角 16:10 封面容器（`p-2.5 pb-0` + `rounded-2xl overflow-hidden`），右上角仅在完播时显示翠绿「已完成」胶囊，底部轻磨砂条展示 `X 节 · Y 章` 与总时长；无封面时采用适配明暗双主题的柔和多色梯度占位海报。
     - 下半部为标题（两行截断）、讲师名、最多 3 枚柔和标签、纤细双色进度条（已学 X/Y 节 + 百分比），底部操作行提供柔和主色「开始学习 / 继续学习」胶囊按钮 + 紧凑「编辑」图标按钮（保留 `aria-label="编辑 <课程名>"`）。
6. **空状态**：
   - 无任何课程：居中浮层卡片，配有蓝底图标徽章、清晰引导文案与「前往存储与导入」主按钮。
   - 筛选无结果：居中浮层卡片与「清除筛选条件」按钮。

#### 5.3.2 课程详情与播放页（`web/src/pages/CourseDetailPage.tsx` & `VideoPlayer.tsx`）

1. **剧场优先顶栏**：
   - 精简单行顶栏：左侧「返回课程库」+ 课程标题 + 完成状态徽章 + 精简进度胶囊（`已学 X/Y 节 · Z%`）；右侧「收起目录 / 展开目录」按钮与「编辑课程」按钮。
   - **关键改进**：移除原版压在视频上方的 220px 巨型封面简介卡片，使 16:9 视频画面在首屏直接占据视觉中心。
2. **主体两栏剧场网格**（结合 5.2.3 的播放页左侧栏临时折叠至 `76px`）：
   - 目录展开时采用 `lg:grid-cols-[minmax(0,1fr)_320px] xl:grid-cols-[minmax(0,1fr)_360px]`（在 `1024px` 下播放器仍拥有 ~`560px+` 宽幅，`1280px` 下 ~`800px+`），收起目录时全宽单栏。
   - **左栏**：
     - `VideoPlayer`：外层 `rounded-3xl bg-card shadow-card overflow-hidden`；内部 `<video>` 视口保持纯黑 `bg-black` 保证观影沉浸感；下方控制条迁入语义主题（`bg-card border-t border-subtle`），包含精致的倍速选择器、字幕选择器、快捷键提示微胶囊（`Space` / `← →` / `F`）与全屏按钮。不支持的编码回退卡同步适配双主题（柔琥珀图标徽章 + `Infuse / VidHub / SenPlayer` 推荐胶囊 + 容器/音视频编码标签）。
     - **当前课时操作卡**（`rounded-3xl bg-card shadow-card p-5`）：左侧显示当前章节名与粗体课时标题；右侧「上一节」「下一节」次按钮 + 「标记已看 / 标记未看」主操作按钮；若有课时简介则在其下方以柔和分割线呈现。
     - **课程概览卡片**（收纳于操作卡下方）：展示小尺寸圆角封面、讲师、章节/课时统计、总时长、完整课程简介、全部标签与整体学习进度条。
   - **右栏（吸顶可折叠章节目录）**：
     - `rounded-3xl bg-card shadow-card overflow-hidden lg:sticky lg:top-6`，内部列表限高 `max-h-[calc(100vh-8rem)]` 独立滚动。
     - 章节折叠头使用柔和次表面 `bg-subtle/70`；课时行在激活态显示 Citonema 风格左侧蓝色竖条 + `bg-accent-soft` 底色 + 粗体主文字，已学课时配翠绿勾选图标，右侧 `tabular-nums` 时长对齐。

#### 5.3.3 课程编辑页（`web/src/pages/CourseEditorPage.tsx`）

1. **吸顶工作台顶栏**：
   - 滚动时贴顶（`sticky top-0 z-20 -mx-4 px-4 sm:-mx-6 sm:px-6 py-3 bg-canvas/85 backdrop-blur-md border-b border-subtle`），左侧「返回课程播放」+ 课程标题；右侧高频操作组：「检查新文件」、主色调「AI 智能整理」、「删除课程」（含内联二次确认胶囊）。
2. **清晰分卡工作区**：
   - 新文件增量检查面板（触发后展开）：柔和蓝色浅底悬浮卡片，支持勾选与一键「追加到课程」。
   - **区块一 · 课程元数据与视觉封面**（`lg:grid-cols-3 gap-6`）：
     - 左 2 列：大圆角元数据表单卡（标题、讲师、所属媒体库、简介、可增删标签输入与「保存元数据」主按钮）。
     - 右 1 列：封面与背景图视觉卡（16:10 封面预览 + 上传封面 + 从课时帧选取封面折叠列表；16:9 背景横幅预览 + 上传背景图）。
   - **区块二 · 章节与课时编排**：
     - 顶部标题说明 + 右侧行内「新章节名称」输入框与「添加章节」按钮。
     - 每个章节为独立 `rounded-3xl bg-card shadow-card` 卡片（保留完整 `@dnd-kit` 拖拽排序与上下移动按钮、行内重命名、跨章节下拉移动、「从存储添加视频」弹窗、字幕格式与缺失状态标签），拖拽态高亮 `ring-2 ring-accent shadow-card-hover`。

#### 5.3.4 存储与导入页（`web/src/pages/StoragePage.tsx`）

- **顶栏**：页面标题、一句话说明与「添加存储源」胶囊主按钮。
- **添加存储源面板**：展开后呈现大圆角卡片，目录路径旁保留「浏览目录」按钮唤起 `DirectoryPickerModal`。
- **已配置存储源网格**：2 列大圆角卡片，当前选中的存储源以蓝色左竖条或清晰强调边框标示；卡片内整合存储图标徽章、路径、已索引数量、上次扫描时间、扫描结果统计微条（总视频 / 新增 / 字幕 / 缺失）以及带旋转动画的「扫描」按钮与「删除」图标按钮。
- **目录与文件浏览器卡**：大圆角统一卡片，顶部可点击面包屑 +「导入当前目录」按钮；下方分为「子目录列表」（暖琥珀文件夹图标块 + 视频数 +「一键导入为课程」按钮）与「媒体文件列表」（分段状态筛选 `全部 / 正常 / 缺失` + 搜索框 + 缺失告警胶囊 + 状态圆角徽章）。

#### 5.3.5 系统与连接设置页（`web/src/pages/SettingsPage.tsx` & `SystemUpdateSection.tsx`）

- 容器限制在舒适阅读宽度 `max-w-4xl`。
- **卡片 1 · Jellyfin 客户端连接指南焦点卡**：柔和蓝调浮层卡片，突出展示 Server URL 与登录用户名两个等宽字体只读框（各带一键「复制」按钮与 inline 已复制状态反馈），下方 3 列展示 `Infuse` / `VidHub` / `SenPlayer` 接入指引子卡。
- **卡片 2 · 系统版本与在线更新（`SystemUpdateSection.tsx`）**：
  - 保留原位（`#system-update` 锚点及 URL hash 平滑滚动逻辑不变），完整迁入新语义令牌体系（`rounded-3xl bg-card shadow-card`，内部信息格使用 `bg-subtle`，可用更新提示使用 `bg-status-success-soft` 柔绿层次，纯文本更新日志区块使用 `bg-subtle border border-subtle`，下载/校验/热重启进度条使用 `bg-accent` 与清晰阶段文案，错误与只读/锁定环境提示使用 `status-warning` / `status-danger` 语义色）。
  - 其内置**更新确认弹窗**同步升级为统一弹窗视觉（`bg-overlay backdrop-blur-sm` + `rounded-3xl bg-popover shadow-float`）。
- **卡片 3 · 服务器与 AI 配置卡**：通过视觉子标题清晰分隔「基础服务配置」与「AI 智能整理接口（OpenAI 兼容）」，底部左置「测试连接」次按钮、右置「保存设置」主按钮。
- **卡片 4 & 5 · 管理员密码卡 & 已登录设备会话卡**：统一大圆角卡片；会话表使用清晰水平滚动表格，当前会话配翠绿徽章，注销按钮采用柔和危险色胶囊。

#### 5.3.6 初始化与登录页（`web/src/pages/SetupLoginPage.tsx`）

- 全屏 `bg-canvas` 画布配极轻柔和径向光晕，右上角放置简洁的主题切换按钮（浅色/深色/自动），让首次部署或登录前也能自由切换视觉模式。
- 居中 `rounded-3xl bg-card shadow-float p-8 sm:p-10 max-w-md` 卡片，内含大圆角蓝色品牌标、居中主标题与说明、带前缀图标的柔和输入框，以及带柔和蓝光阴影的全宽主按钮。

---

### 5.4 弹窗、Toast 与 Tooltip 升级

1. **统一弹窗视觉规范**（`ImportCourseModal`、`AIOrganizeModal`、`DirectoryPickerModal`、`DeleteLibraryModal`、`SystemUpdateSection` 更新确认弹窗、`CourseEditorPage` 内嵌添加视频弹窗、新增 `LibraryFormModal`）：
   - 统一遮罩 `bg-overlay backdrop-blur-sm`、面板 `rounded-3xl bg-popover border border-subtle shadow-float`、头部柔和色图标徽章 + 关闭图标、内部柔和滚动区与清晰底部操作栏。
   - `LibraryFormModal`（新建 / 重命名媒体库复用）：支持 `mode: 'create' | 'rename'`，自动聚焦输入框，支持 `Enter` 提交与 `Escape` 关闭，保留无障碍标签（`新媒体库名称` / `重命名媒体库名称`、`确认创建` / `保存名称`）。
2. **`Tooltip.tsx` 增强**：
   - 扩展 `placement?: 'top' | 'bottom' | 'right'`，其中 `'right'` 专为折叠态左侧边栏图标服务（垂直居中于触发器右侧，并在空间不足或靠近上下边缘时自动钳制）。
   - 气泡样式改为 `bg-tooltip text-ink-tooltip border border-white/10 shadow-float`，在浅色与深色模式下都具备清晰对比度；保留既有 `whitespace-normal` 换行与视口边缘防裁切逻辑。
3. **`Toast.tsx` 升级**：
   - 使用 `bg-popover border border-subtle shadow-float` 浮层卡片 + 左侧语义色图标底块（成功翠绿 / 错误珊瑚红 / 提示天蓝）+ 主文字色，保持 `role="status"`、`aria-live="polite"`、最多 4 条、2.8s 自动消失与手动关闭按钮。

---

### 5.5 测试与验收计划

- **前端单元测试更新与新增（Vitest）**：
  - 新增 `ThemeAndShell.test.tsx`：验证 `ThemeProvider`（默认 `system`、`light`/`dark` 切换、`localStorage` 持久化、`data-theme`/`.dark` 同步、`matchMedia` 响应）、`Layout` 左侧栏展开/折叠持久化、进入 `/courses/:id` 剧场模式临时折叠且不覆盖 `localStorage`、移动端抽屉开关、跨页点击侧边栏媒体库跳转 `/?library=<id>`、以及通过侧边栏 `LibraryFormModal` / `DeleteLibraryModal` 完成媒体库新建/重命名/删除。
  - 更新 `HomePage.test.tsx` 与 `UXFeatures.test.tsx`：覆盖 `<Layout>` + `<LibraryProvider>` 模式下的左侧栏媒体库筛选与管理、`/?library=<id>` 作用域下的继续学习/标签过滤、Citonema 焦点卡（含 `>5` 条进行中课时完整可切换与直达播放）及独立挂载降级兼容性。
  - 回归并对齐 `SystemUpdate.test.tsx`（1926 行）：验证新侧边栏/折叠态/移动顶栏中的「新版本 vX.Y.Z」链接与 `mobile-update-dot`、已在 `/settings` 页点击再次滚动至 `#system-update`，以及 `SystemUpdateSection` 全套状态机与确认弹窗交互。
  - 扩展 `UXPrimitives.test.tsx`：覆盖 `Tooltip` 的 `placement="right"` 定位钳制测试。
  - 运行既有全部测试（`VideoPlayer.test.tsx`、`EditorAndAI.test.tsx`、`UXPrimitives.test.tsx`、`UXFeatures.test.tsx`、`HomePage.test.tsx`、`SystemUpdate.test.tsx`）确保 100% 通过。
- **全仓构建与后端测试**：执行 `make sync-web`、`make test`（包含 `go test -race -count=1 ./...` 与 `pnpm --dir web test`）以及 `make build`。
- **真机浏览器双主题视觉验收**：启动本地构建服务，在浏览器中逐页检查**浅色模式与深色模式**下的桌面宽屏（≥1280px）、笔记本宽度（1024px–1280px）和移动窄屏（~390px）布局、对比度、阴影层次、侧边栏折叠、剧场播放页、自更新模块、弹窗及播放器观感。

---

## 6. Left to the Builder

以下实现级细节交由实施阶段按本规范自由打磨：

- 语义 CSS 变量的具体细微透明度微调（如边框透明度 ±0.02、阴影模糊半径），前提是保持浅色柔和空气感与深色哑光层次及 WCAG AA 可读性。
- 是否将重复的按钮/输入框/卡片样式封装为 `web/src/components/ui/` 内的轻量组件或通用 Tailwind 类常量（只要不引入新外部 npm 依赖）。
- 首页「继续学习」焦点卡在窄屏/平板端的自适应折行细节与课程卡片无封面渐变色盘的具体配色组合。
- 课程详情页「课程概览」下方卡片默认展开或支持大段简介折叠展开的小交互打磨。
- 侧边栏媒体库列表为空时的简洁占位引导文案与动画过渡时长（建议 150–200ms）。
