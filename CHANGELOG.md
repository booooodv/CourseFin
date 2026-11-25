# Changelog

## v1.3.0 (2026-11-25)

播放器体验、字幕系统、导入 AI 整理与课程管理交互全面升级：

- **Artplayer 嵌入式 Web 播放器与三层按需转码播放**：
  - 嵌入 `Artplayer` + `hls.js` 替换原生 `<video>` 控件，提供中文界面、网页全屏、画中画、截图、倍速记忆、字幕偏移/字号调节与迷你进度条，并采用 XSS 安全的字幕渲染避免编程课程尖括号泛型被误解析；
  - 后端播放协商升级为四档决策（浏览器直放含保守 HEVC 探测 → H.264 零转码 HLS → 按需 FFmpeg `libx264` / `yuv420p` 偶数分辨率补齐 HLS 转码 → 无 FFmpeg 时的原生客户端推荐），彻底解决 HEVC MKV、10-bit/4:4:4 录屏、奇数分辨率及老编码视频无法在浏览器直接播放的问题，并在直放解码失败时自动单次降级至兼容转码模式。
- **字幕自动匹配、多编码转码、存量重匹配与手动绑定**：
  - 新增数据库迁移 `002_subtitle_manual_bind.sql`，升级字幕匹配器支持 Unicode NFC 规范化（兼容 macOS NFD 路径）、字幕子目录与唯一兄弟目录匹配、宽松分隔符、清晰度标签剥离以及严格 ISO 639 语言白名单；服务启动自动重匹配历史未关联字幕；
  - 新增 UTF-8 BOM / UTF-16 LE/BE / GB18030(GBK) 字幕编码自动解码（兼顾截断边界防乱码），统一覆盖 WebVTT 播放与 AI 字幕摘录；
  - 播放器自动选择首选字幕轨道并持久化偏好（`coursefin_subtitle_pref`）；新增 `GET /api/v1/sources/{id}/subtitles` 与 `PUT /api/v1/subtitles/{id}/bind`，支持在课程编辑页随时查看、搜索并手动绑定或解绑课时字幕（扫描不会覆盖手动决策）。
- **导入弹窗 AI 智能整理**：新增 `POST /api/v1/import/ai/organize` 预览接口，在「导入为课程」弹窗内即可一键调用 AI 整理标题、简介、标签与章节课时结构，支持分项勾选对比应用与「恢复初始识别」，且按 `media_file_id` 准确保留重组后的课时时长。
- **存储与导入页课程归属标识**：后端聚合去重的已入课/未入课计数与关联课程引用，前端存储源卡片展示「全部已入课 / 未入课 N 个」及扫描字幕匹配统计，子目录与文件列表展示所属课程快捷链接、支持「全部 / 未入课 / 已入课」筛选并在导入后自动刷新。
- **课程目录环形进度条与品牌视觉升级**：播放页右侧课程目录改用 18×18 SVG 顺时针环形进度条（随播放实时推进，当前课时环内带 `▶`，已完成显示绿色 `✓`）；同步上线全新的 CourseFin 品牌 Logo 组件（`CourseFinLogo`）及 `/favicon.svg` / `/favicon.ico` 图标服务。

## v1.2.0 (2026-11-24)

Web 端全站 UI 大改版（左侧栏主架构与明/暗双主题）：

- **明/暗双主题设计系统与零闪烁引擎**：基于 Tailwind v4 `@theme inline` 与语义化 CSS 变量建立完整的浅色（Citonema 风格冰灰白画布 + 纯白大圆角悬浮卡片 + 清透天蓝强调色）与深色（哑光炭灰画布 + 抬升石墨卡片）双主题体系，支持「浅色 / 深色 / 跟随系统」三态切换与本地持久化，并在 `web/index.html` 内联首屏防闪烁脚本；全站统一 WCAG AA 可读对比度与可组合阴影/高亮环体系。
- **单层可折叠左侧栏主骨架与跨页媒体库同步**：重构 `Layout` 为桌面可展开（`260px`）/ 可折叠（`76px` 图标条）左侧主导航栏与移动端顶部栏 + 侧滑抽屉，通过持久化嵌套路由与 `LibraryContext` 将「我的媒体库」（含课程计数、新建、重命名、删除）接入全站左侧栏并同步 URL `/?library=<id>`；进入 `/courses/:courseId` 播放页时左侧栏临时剧场折叠以保障笔记本观影宽度；完整融入 v1.1.0 自更新提醒胶囊与移动端红点。
- **全站 6 大页面、自更新模块与弹窗全面焕新**：
  - 首页升级为时间问候顶栏 + Citonema 风格「继续学习」大焦点卡（支持多节进行中课时无截断快速切换）+ 单媒体库作用域隔离的标签与客户端排序 + 内嵌圆角海报网格；
  - 播放详情页重构为剧场优先布局（首屏突出 16:9 播放器与右侧吸顶目录，课程概览下沉至播放器下方），同步适配双主题播放控制条与编码回退提示卡；
  - 课程编辑页新增吸顶工作台顶栏与双区卡片编排；存储与导入页、系统设置页（含 `SystemUpdateSection` 与确认弹窗）、登录/初始化页（支持登录前切换明暗主题）以及全部弹窗、Tooltip（新增右侧定位与点击收起）与 Toast 均完成双主题统一改造。

## v1.1.0 (2026-11-24)

内置自更新模块与自动化多架构发版：

- **自动版本检测与安全自更新**：新增零外部依赖的 `internal/updater` 模块，支持启动延迟检查与定时检查 GitHub Releases（默认每 6 小时，支持 `ETag` 节流、忽略预发布版本及可选下载加速代理前缀并严格隔离 `COURSEFIN_GITHUB_TOKEN`）；下载阶段强制执行 `checksums.txt` `SHA-256` 常量时间校验、安全 `tar.gz` 解包与目标目录 `--version` 预检，通过 `.bak` 原子备份与 Unix `syscall.Exec` 原地热重启完成升级。
- **Docker 持久更新与防变砖回滚**：新增版本感知启动脚本 `scripts/docker-entrypoint.sh`，在镜像内 `/usr/local/bin/coursefin` 与持久卷 `/data/updates/bin/coursefin` 间按语义版本号择新以 PID 1 运行（无需挂载 `docker.sock`），并配合 `.pending_update` 启动确认标记在启动崩溃时自动回滚至 `.bak` 或镜像内置版本；`Dockerfile` 与 `docker-compose.yml` 同步注入构建版本号。
- **管理 API、健康元数据与 Web 更新中心**：新增管理员鉴权接口 `GET /api/v1/system/update`、`PUT /api/v1/system/update/config`、`POST /api/v1/system/update/check`、`POST /api/v1/system/update/apply`，扩展 `GET /health` 返回 `version` 与 `boot_id`；Web 端顶栏新增非侵入式新版本提示胶囊与移动端红点，设置页新增「系统版本与更新」卡片（纯文本安全渲染 Release Notes、二次确认弹窗、实时进度条、断连后 `/health` 轮询等待重载、自动回滚识别及更新偏好配置）。
- **自动化多架构发布流水线**：新增 `.github/workflows/release.yml`，推送 `v*` 标签即可自动构建内嵌前端并交叉编译 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64` 压缩包与 `checksums.txt` 发布至 GitHub Releases（自动为预发布标签设置 `prerelease: true`）。

## v1.0.1 (2026-11-24)

Web 管理端易用性与交互优化：

- **本地目录可视化选择**：新增受管理员认证保护的服务端目录浏览接口 `GET /api/v1/fs/dirs`，在「存储与导入」页面增加「浏览目录」弹窗，支持快捷入口（`/media` / 主目录 / 根目录）、逐级浏览、面包屑导航、显示隐藏目录、关键词筛选、无效路径自动回退以及存储源名称自动填充。
- **全站按钮提示引导与操作反馈**：新增 Portal 定位悬停提示（Tooltip，支持自动防边缘裁切与自适应换行）及全局浮动 Toast 气泡反馈，覆盖首页、存储导入、课程播放/编辑与设置页核心操作按钮；修复非 HTTPS 局域网环境下复制失败无提示的问题，新增 HTTP 降级复制方案与 Jellyfin 用户名一键复制按钮。
- **媒体库删除与重命名**：扩展 `DELETE /api/v1/libraries/{id}` 支持 `?move_to_library_id=` 事务迁移课程及 `?force=true` 连带清理课程目录记录（仅删数据库与受管封面，绝不改动本地媒体文件）；首页媒体库标签栏新增内联重命名与删除确认弹窗。

## v1.0.0 (2026-11-24)

首个正式发布版本：

- **基础与认证**：单管理员极简账号体系、SQLite（WAL + 外键）嵌入式迁移、32 位无连字符标准 Jellyfin UUID、多设备会话管理及 4 种 Jellyfin 鉴权方式兼容。
- **存储与扫描**：可插拔 `StorageProvider` 抽象（预留 302 `DirectURL` 钩子）、防路径穿越与软链逃逸的 `LocalFS` 驱动、增量 `ffprobe` 元数据探测、自动缩略图提取、外挂字幕语言匹配、断挂载无损 `missing`/`active` 恢复。
- **课程目录与导入**：物理文件与逻辑课程完全解耦，支持媒体库 / 课程 / 章节 / 课时 / 标签自由编排、中英文及小数自然排序、目录一键预览导入、幂等增量检查新文件、统一 Jellyfin Tick 观看进度与自动完播。
- **流媒体引擎**：DirectPlay HTTP Range（`206` / `HEAD` / `ETag`）与 302 钩子、按需 `-ss` 秒级跳转 VOD HLS 转封装（`-c:v copy` 与音频按需转 AAC）、外挂/内嵌字幕提取与 `.srt`→`WEBVTT` 转换、SHA-256 缓存图片缩放。
- **Jellyfin 兼容层**：根路径 `/` 与 `/emby` 大小写无关兼容、`CollectionFolder → Series → Season → Episode` 映射、`Tags`/`Genres`/`GenreIds`/`TagIds` 筛选、`DeviceProfile` 协商、`/socket` WebSocket KeepAlive、`/DisplayPreferences`、`/Items/Counts`、`Resume`、`NextUp`、`Latest` 及播放进度双向同步。
- **AI 课程元数据整理**：OpenAI 兼容 `/chat/completions` 客户端与连接测试、严格 `lesson_id` 一一对应校验、字幕预算可控（支持设 `0` 完全关闭字幕发送）、前端勾选预览确认后事务性应用。
- **Web 端与 Docker 部署**：React 19 + TypeScript + Tailwind 内嵌 SPA（首页、内置 HTML5/`hls.js` 播放器、拖拽式课程编辑器、存储导入页、设置页）、多阶段 `Dockerfile`、`docker-compose.yml` 与端到端联调测试。
