# Changelog

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
