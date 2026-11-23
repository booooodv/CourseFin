# CourseFin

自托管、单容器 Docker 部署的个人课程管理平台——把物理文件索引与逻辑课程彻底解耦，同时对外提供高兼容度的 **Jellyfin 协议接口**，让 Infuse、VidHub、SenPlayer、Swiftfin、Findroid 等客户端以及内置 Web 播放器都能直接流畅看课并同步学习进度。

## 为什么做 CourseFin？

Jellyfin / Emby 的索引与刮削体系围绕电影和电视剧设计，管理散乱目录结构、中英文混排命名的课程非常痛苦。CourseFin 将**物理存储索引**和**逻辑课程专辑**完全分离：

- **绝不改动硬盘原文件**：支持只读挂载（`:ro`），整理课程、重排章节、改名、删课不会触碰原始文件。
- **原生映射到 Jellyfin**：`媒体库 → 课程专辑(Series) → 章节(Season) → 课时(Episode)`，课程标签自动同步为 Jellyfin 的 `Tags` 与 `Genres`。
- **轻量单容器交付**：Go 单二进制 + 内嵌 React Web 端 + SQLite（WAL 模式，零外部数据库依赖）+ FFmpeg。

---

## v1.0.0 核心功能

- **可插拔存储与增量扫描**：首版支持本地目录 / NAS 挂载（内置路径穿越与软链逃逸保护），底层抽象 `StorageProvider`（含 302 `DirectURL` 钩子，便于后续扩展 WebDAV / Alist / OpenList / S3）；支持增量 `ffprobe`、自动截取课时缩略图、外挂字幕匹配，以及临时断挂载自动标记 `missing` 并无损恢复。
- **自定义课程编排与一键导入**：支持按目录一键导入预览（自动识别封面、多级目录映射章节、中英文与小数自然排序）、幂等增量「检查新文件」、跨目录自由拼装课程、拖拽排序章节与课时、上传或截取课时帧作为封面、标签分类管理。
- **DirectPlay + 按需秒级跳转 VOD HLS 转封装**：
  - 原生支持 HTTP Range（`206 Partial Content` / `HEAD` / `ETag`）直连播放。
  - 浏览器播放 `.mkv` / `.flv` / `.ts` 等容器时按需通过 FFmpeg `-c:v copy` 生成可任意时间点秒级跳转的 VOD HLS 切片（几乎不占 CPU；不兼容编码如 WMV/RV40 在 Web 端明确提示改用原生播放器）。
  - 支持外挂与内嵌文本字幕（`.srt` 自动实时转 WebVTT）。
- **高兼容度 Jellyfin 协议**：支持根路径 `/` 及 `/emby` 大小写无关兼容、全部主流 Jellyfin 鉴权方式、`DeviceProfile` 播放协商、`/socket` WebSocket KeepAlive、`Views` / `Items` / `Seasons` / `Episodes` / `Genres` / `Tags` / `Resume` / `NextUp` / `Latest` / `Search` 及多端进度双向同步（≥90% 自动完播）。
- **内置中文 Web 管理与播放端**：开箱即用初始化向导、课程库首页与继续学习栏、HTML5 / `hls.js` 播放器（0.75x–2.0x 倍速、字幕切换、键盘快捷键、断点续播与切课自动落盘）、拖拽式课程编辑器、存储导入页与设置页。
- **自定义 AI 课程元数据整理**：支持任意 OpenAI 兼容接口（OpenAI / DeepSeek / 通义千问 / OpenRouter / Ollama / NewAPI 等），基于目录、文件名与可选字幕片段生成课程标题、简介、标签与章节划分建议，严格校验所有 `lesson_id` 一一对应并**先预览勾选确认再事务性应用**。

---

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
mkdir -p data media
docker compose up -d --build
```

将课程目录放到 `./media`（或修改 `docker-compose.yml` 挂载你自己的 NAS / 本地课程路径，建议使用 `:ro` 只读挂载）。

### 方式二：本地源码编译运行

前置要求：Go `1.26+`、Node `22+`、`pnpm 12+`、`ffmpeg` 与 `ffprobe`。

```bash
make build
mkdir -p data media
COURSEFIN_PORT=8096 COURSEFIN_DATA_DIR=./data ./coursefin
```

### 访问与接入

- **Web 管理与播放端**：浏览器打开 `http://localhost:8096`（自动跳转至 `/web/` 完成管理员账号初始化）。
- **Jellyfin 客户端（Infuse / VidHub / SenPlayer / Swiftfin / Findroid 等）**：
  - 服务器地址：`http://<你的服务器IP>:8096`（或独立子域名 `https://course.example.com`）
  - 账号密码：使用初始化时创建的管理员账号登录即可。

---

## 环境变量

| 变量名 | 默认值 | 说明 |
|---|---|---|
| `COURSEFIN_PORT` | `8096` | HTTP 监听端口 |
| `COURSEFIN_DATA_DIR` | `/data` | SQLite 数据库、封面、缩略图与临时缓存目录 |
| `COURSEFIN_ADMIN_USER` | *(空)* | 可选：首次启动自动创建的管理员用户名（跳过网页初始化向导） |
| `COURSEFIN_ADMIN_PASS` | *(空)* | 可选：与 `COURSEFIN_ADMIN_USER` 配合使用的初始密码 |
| `COURSEFIN_LOG_LEVEL` | `info` | 日志级别：`debug` / `info` / `warn` / `error` |
| `COURSEFIN_BASE_URL` | *(空)* | 可选：反代场景下的对外根域名提示（如 `https://course.example.com`，不支持子路径反代） |

---

## 开发与测试

```bash
# 运行完整 Go (-race) 与前端 Vitest 测试
make test

# 仅构建并同步前端静态资源到 internal/server/webdist
make sync-web

# 构建本地 Docker 镜像
make docker-build
```

设计文档与实现计划见 [docs/superpowers/specs/2026-11-23-coursefin-design.md](docs/superpowers/specs/2026-11-23-coursefin-design.md) 与 [docs/superpowers/plans/2026-11-23-coursefin.md](docs/superpowers/plans/2026-11-23-coursefin.md)。

---

## 后续路线图

- 扩展更多 `StorageProvider` 驱动：WebDAV、Alist / OpenList（含 302 直链重定向）、S3 兼容存储
- 扩展更多基于 `AIProvider` 的学习辅助能力（字幕转写、课程摘要与知识检索）
- 多用户与独立学习进度支持
