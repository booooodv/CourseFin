# CourseFin

![Version](https://img.shields.io/badge/version-v1.6.0-indigo)
![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/Web-React%2019%20%2B%20TypeScript-61DAFB?logo=react&logoColor=black)
![Docker](https://img.shields.io/badge/Docker-Single%20Container-2496ED?logo=docker&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-green)

> A lightweight, Docker-deployable self-hosted course management platform that decouples physical video storage from custom course organization, exposes a **Jellyfin-compatible API**, ships with an embedded dual-theme Web manager & Artplayer player, and provides OpenAI-compatible AI course organization, AI cover generation with cohesive style templates, per-lesson AI summaries, and subtitle-free ASR transcription.

自托管、单容器 Docker 部署的个人课程管理平台——把物理文件索引与逻辑课程彻底解耦，提供高兼容度的 **Jellyfin 协议接口**，内置明/暗双主题 Web 管理与播放端，并集成 **AI 课程整理**、**AI 统一风格封面生成**、**每课结构化 AI 总结（大纲 + 时间线）** 与 **无字幕语音转文字（ASR）托管字幕**，让 Infuse、VidHub、SenPlayer、Swiftfin、Findroid 与浏览器端都能流畅看课、同步进度并高效做笔记。

---

## 为什么做 CourseFin？

Jellyfin / Emby 的索引与刮削体系围绕电影和电视剧设计，管理散乱目录结构、中英文混排命名的课程非常痛苦。CourseFin 将**物理存储索引**和**逻辑课程专辑**完全分离：

- **绝不改动硬盘原文件**：支持只读挂载（`:ro`），整理课程、重排章节、改名、删课、生成 AI 转写字幕均只在 `/data` 与 SQLite 元数据层完成，不会触碰原始文件。
- **原生映射到 Jellyfin**：`媒体库 → 课程专辑(Series) → 章节(Season) → 课时(Episode)`，课程标签自动同步为 Jellyfin 的 `Tags` 与 `Genres`，AI 转写字幕同样自动暴露给原生客户端。
- **轻量单容器交付**：Go 单二进制 + 内嵌 React Web 端 + SQLite（WAL 模式，零外部数据库依赖）+ FFmpeg。

---

## 架构概览

```text
┌──────────────────────────────┐     ┌───────────────────────────────────────┐
│ Jellyfin 客户端               │     │ 浏览器（CourseFin 双主题 Web SPA）     │
│ Infuse / VidHub / SenPlayer  │     │ 课程管理 / 拖拽编排 / Artplayer 播放器 │
│ Swiftfin / Findroid / 官方端  │     │ AI 整理 / 每课总结 / Markdown 笔记复制 │
└──────────────┬───────────────┘     └──────────────────┬────────────────────┘
               │  Jellyfin REST / WS (/ & /emby)        │  /api/v1 & /web
               ▼                                        ▼
┌────────────────────────────────────────────────────────────────────────────┐
│                        CourseFin 单 Docker 容器 (:8096)                     │
│                                                                            │
│  Auth & Multi-Device Sessions    │  Catalog / Natural-Sort Importer        │
│  DirectPlay + VOD HLS Remux/Transcode (FFmpeg)                             │
│  Subtitle Hub (外挂/内嵌/多编码转码/AI 转写字幕 → WebVTT)                    │
│  Lesson AI Summary & Serial Batch Queue (Map-Reduce + FFmpeg Audio Chunks) │
│                                                                            │
│  ┌────────────────┐   ┌────────────────────┐   ┌────────────────────────┐  │
│  │ SQLite (/data) │   │ StorageProvider    │   │ AI / ASR Client        │  │
│  │ (WAL Mode)     │   │ └─ LocalFS         │   │ └─ OpenAI-Compat API   │  │
│  └────────────────┘   └─────────┬──────────┘   └────────────────────────┘  │
└─────────────────────────────────┼──────────────────────────────────────────┘
                                  ▼
                    只读挂载课程目录 (/media:ro)
```

---

## 核心功能

- **AI 课程封面生成、统一风格模板与多封面历史画廊（v1.5.0 / v1.6.0）**：
  - **6 套统一视觉风格与样图预览**：内置「现代书封」「暗夜科技」「柔和 3D 黏土」「温暖扁平插画」「极简蓝图」「质感影院」6 套双栏构图模板，悬停、聚焦或轻触预览图标即可直接查看对应参考样图；
  - **多协议自填生图 API 与安全隔离**：支持 OpenAI 标准生图（`/images/generations`）、OpenAI 对话出图（`/chat/completions`）及 Google Gemini 原生接口（`:generateContent`），自动适配尺寸与精准错误回退（`240s` 超时与纯文生图无歧义 Prompt）、独立地址与 Gemini 模式绝不透传主 AI 密钥，并支持连通性测试；
  - **多关联封面留存、快速切换与首页批量生成**：AI 单课/批量生成、本地上传、课时帧选取及目录导入均追加保存历史封面并默认将最新设为当前展示封面（绝不写入只读 `/media` 目录），在课程编辑页与 AI 封面弹窗内可随时浏览、一键切回历史封面或删除旧版本；首页支持按媒体库或全部课程一键批量补齐缺失封面或统一新增封面，后台串行队列支持进度轮询、单课容错与随时取消。
- **每课 AI 总结与无字幕语音转写（v1.4.0 新增）**：
  - **两段式结构化总结**：每节课可生成 **AI 总结视频大纲**（核心一句话概览 + 结构化要点与跳转时间）与 **AI 总结视频内容（按时间顺序）**（起止时间区间、段落概括与核心知识点）；超长课程（> 24,000 字）自动按时间分块 Map-Reduce 汇总，避免截断丢失后半段内容；
  - **一键导入笔记与时间线联动**：播放页右侧栏提供 **「课程目录 / AI 总结」** 双 Tab，点击时间戳精准跳转播放、随播放进度高亮当前段落，点击 **「复制 Markdown」** 一键粘贴至 Obsidian / Notion 等笔记工具；
  - **无字幕视频全链路闭环**：按「外挂字幕 → 内嵌文本字幕 → 已缓存转写稿 → FFmpeg 自动抽取压缩音频分片并调用 OpenAI 兼容 `/audio/transcriptions` 接口（支持云端 Whisper/Groq/硅基流动及自托管本地 Faster-Whisper / Speaches 服务，跨域名密钥严格隔离）→ 手动粘贴文稿」自动流转；
  - **AI 转写字幕持久化复用**：ASR 生成的 WebVTT 存入 SQLite `media_transcripts`（不写入只读 `/media` 目录，不受定时扫描影响），自动作为 **「AI 转写字幕」** 轨道供 Web 播放器与 Jellyfin 客户端开启，重新总结时直接复用免重复转写；
  - **单课优先 + 整门课批量队列**：后台串行队列支持整门课一键批量生成（查看阶段进度、单课失败容错隔离明细、随时取消），批量运行中手动触发当前课时自动插队优先处理。
- **Artplayer 嵌入式 Web 播放器与三层按需转码播放**：
  - 嵌入 `Artplayer` + `hls.js`，提供中文界面、倍速记忆（0.75x–2.0x）、字幕轨道偏好记忆、字幕字号与时间偏移调节、网页全屏/浏览器全屏、画中画、截图、迷你进度条与快捷键；
  - 后端支持浏览器直放（HTTP Range 206 + 保守 HEVC 探测）→ H.264 零视频转码 VOD HLS 封装 → 按需 FFmpeg `libx264` 兼容转码（覆盖 HEVC MKV、10-bit/4:4:4 录屏、奇数分辨率及 WMV/FLV/AVI 等老编码，并在直放解码失败时自动降级）。
- **智能字幕匹配、多编码解码与手动绑定**：
  - 支持 Unicode NFC 规范化（兼容 macOS NFD 路径）、字幕子目录（`Subs/`、`Subtitles/`、`字幕/` 等）与同级/兄弟目录匹配、宽松分隔符及清晰度标签剥离；
  - 自动解码 UTF-8 BOM / UTF-16 LE/BE / GB18030(GBK) 字幕为 UTF-8 WebVTT，支持在课程编辑页手动绑定或解绑外挂字幕（二次扫描不会覆盖手动决策）。
- **明/暗双主题 Web 管理端与自定义课程编排**：
  - 浅色 / 深色 / 跟随系统三态无闪烁主题，可折叠单层左侧栏、首页 Citonema 风格继续学习焦点卡、播放页剧场优先布局与右侧目录 SVG 环形观看进度条；
  - 支持服务端目录可视化浏览、存储源与目录已入课/未入课状态筛选、目录一键预览导入（自然排序、自动识封面、**导入弹窗内置 AI 智能整理**）、幂等增量「检查新文件」、跨目录自由拼装、拖拽排序章节课时与多媒体库管理。
- **自定义 AI 课程元数据整理**：
  - 支持任意 OpenAI 兼容 `/chat/completions` 接口（OpenAI / DeepSeek / 通义千问 / OpenRouter / Ollama / NewAPI 等），在导入前或课程编辑页生成标题、简介、标签与章节课时重组建议，严格校验所有 `lesson_id` 一一对应并**先预览勾选确认再事务性应用**。
- **高兼容度 Jellyfin 协议**：
  - 支持根路径 `/` 及 `/emby` 大小写无关兼容、全部主流 Jellyfin 鉴权方式、`DeviceProfile` 播放协商、`/socket` WebSocket KeepAlive、`Views` / `Items` / `Seasons` / `Episodes` / `Genres` / `Tags` / `Resume` / `NextUp` / `Latest` / `Search` 及多端进度双向同步（≥90% 自动完播）。
- **内置自动检测与一键自更新**：
  - 定期（默认每 6 小时）及手动检测 GitHub Releases 新版本，在顶栏提示版本徽标并在「设置 → 系统更新」展示更新日志；支持 **Docker 容器**（无需挂载 `docker.sock`，更新二进制持久化于 `/data/updates/bin` 并在拉取新版镜像时自动让位于镜像内置版本）和 **Linux / macOS 本地二进制**执行 `SHA-256` 校验、`--version` 预检、原子替换备份与平滑热重启（失败自动回滚），支持配置 GitHub 加速代理前缀。

---

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
mkdir -p data media
cp .env.example .env   # 可选：自定义端口或初始化管理员账号
docker compose up -d --build
```

将课程目录放到 `./media`（或修改 [docker-compose.yml](docker-compose.yml) 挂载你自己的 NAS / 本地课程路径，建议始终保持 `:ro` 只读挂载）。

### 方式二：本地源码编译运行

前置要求：Go `1.26+`、Node `22+`、`pnpm 12+`、`ffmpeg` 与 `ffprobe`。

```bash
make build
mkdir -p data media
COURSEFIN_PORT=8096 COURSEFIN_DATA_DIR=./data ./coursefin
```

### 访问与客户端接入

- **Web 管理与播放端**：浏览器打开 `http://localhost:8096`（自动跳转至 `/web/` 完成管理员账号初始化）。
- **Jellyfin 客户端（Infuse / VidHub / SenPlayer / Swiftfin / Findroid 等）**：
  - 服务器地址：`http://<你的服务器IP>:8096`（或独立子域名 `https://course.example.com`，不支持子路径反代）
  - 账号密码：使用初始化时创建的管理员账号登录即可。

### 配置 AI 总结与无字幕语音转文字（可选）

前往 Web 端 **「系统设置 → 服务器与 AI 接口配置」**：

1. **AI 文本模型（用于课程整理与每课 AI 总结）**：填写 `AI Base URL`、`AI 模型名称` 与 `AI API Key`，点击「测试连接」验证。
2. **语音转文字（ASR，用于无字幕视频自动转写并生成 AI 字幕）**：
   - 填写 `ASR 模型名称`（如 `whisper-1`、`whisper-large-v3-turbo`、`FunAudioLLM/SenseVoiceSmall`）即启用；
   - `ASR Base URL` 留空时默认复用上方 AI 地址与密钥；也可填写独立的云端或自建本地 Whisper 服务地址（如 `faster-whisper-server` / `speaches`，独立域名下留空 Key 不会透传主 AI 密钥，兼容免密本地容器）；
   - 点击「测试语音转文字」验证后保存，即可在播放页右侧「AI 总结」Tab 单课生成或整门课批量生成。

---

## 环境变量

完整示例见 [.env.example](.env.example)：

| 变量名 | 默认值 | 说明 |
|---|---|---|
| `COURSEFIN_PORT` | `8096` | HTTP 监听端口 |
| `COURSEFIN_DATA_DIR` | `/data` | SQLite 数据库、封面、缩略图与临时缓存目录 |
| `COURSEFIN_ADMIN_USER` | *(空)* | 可选：首次启动自动创建的管理员用户名（跳过网页初始化向导） |
| `COURSEFIN_ADMIN_PASS` | *(空)* | 可选：与 `COURSEFIN_ADMIN_USER` 配合使用的初始密码 |
| `COURSEFIN_LOG_LEVEL` | `info` | 日志级别：`debug` / `info` / `warn` / `error` |
| `COURSEFIN_BASE_URL` | *(空)* | 可选：反代场景下的对外根域名提示（如 `https://course.example.com`，不支持子路径反代） |
| `COURSEFIN_UPDATE_REPO` | `booooodv/CourseFin` | 可选：自定义内置自动更新检查的 GitHub 仓库（`owner/repo`） |
| `COURSEFIN_UPDATE_AUTO_CHECK` | *(空)* | 可选：强制覆盖网页端「自动检查更新」开关（`true` / `false`；留空则由设置页控制，默认开启） |
| `COURSEFIN_GITHUB_TOKEN` | *(空)* | 可选：直连 GitHub Releases API 时使用的只读 Token（用于提升限流额度，绝不发送给镜像代理或前端） |

---

## 安全提示

如果计划将 CourseFin 暴露到公网，请先阅读 [SECURITY.md](SECURITY.md) 中的完整自托管安全检查清单，重点注意：

- 通过反向代理（Nginx / Caddy / Cloudflare Tunnel）启用 **HTTPS** 并对登录接口配置速率限制。
- 妥善保护宿主机 `./data` 目录权限（`coursefin.db` 保存本地调用 AI / ASR / 生图所需的 `ai_api_key`、`ai_asr_api_key` 与 `ai_image_api_key`），切勿提交至 Git 或公开暴露。
- 始终对课程目录使用只读挂载（`:ro`），不要向不可信人员分享管理员账号。

---

## 开发、测试与贡献

欢迎参与贡献，详细开发与 PR 指南请参阅 [CONTRIBUTING.md](CONTRIBUTING.md)。

```bash
# 运行完整 Go (-race) 与前端 Vitest 测试
make test

# 仅构建并同步前端静态资源到 internal/server/webdist
make sync-web

# 构建本地 Docker 镜像
make docker-build
```

设计文档、实现计划与变更日志：
- [CHANGELOG.md](CHANGELOG.md)
- [docs/superpowers/specs/2026-11-23-coursefin-design.md](docs/superpowers/specs/2026-11-23-coursefin-design.md)
- [docs/superpowers/specs/2026-11-24-self-update-design.md](docs/superpowers/specs/2026-11-24-self-update-design.md)
- [docs/superpowers/specs/2026-11-24-ui-redesign-design.md](docs/superpowers/specs/2026-11-24-ui-redesign-design.md)
- [docs/superpowers/specs/2026-11-24-player-subtitle-import-ux-design.md](docs/superpowers/specs/2026-11-24-player-subtitle-import-ux-design.md)
- [docs/superpowers/specs/2026-11-25-lesson-ai-summary-design.md](docs/superpowers/specs/2026-11-25-lesson-ai-summary-design.md)
- [docs/superpowers/specs/2026-11-26-ai-course-cover-design.md](docs/superpowers/specs/2026-11-26-ai-course-cover-design.md)
- [docs/superpowers/specs/2026-11-26-ai-cover-prompt-timeout-fix-design.md](docs/superpowers/specs/2026-11-26-ai-cover-prompt-timeout-fix-design.md)
- [docs/superpowers/specs/2026-11-26-course-multi-covers-design.md](docs/superpowers/specs/2026-11-26-course-multi-covers-design.md)

---

## 后续路线图

- 扩展更多 `StorageProvider` 驱动：WebDAV、Alist / OpenList（含 302 直链重定向）、S3 兼容存储
- 基于已生成课程总结与字幕的跨课程知识检索与问答
- 多用户与独立学习进度支持
- 可选硬件转码加速（NVENC / VAAPI / QSV / VideoToolbox）

---

## 开源协议与免责声明

- **开源协议**：本项目源码基于 [MIT License](LICENSE) 开源。
- **FFmpeg 组件说明**：CourseFin 源码本身不链接任何 FFmpeg 库，仅在运行时通过外部进程调用系统安装的 `ffmpeg` / `ffprobe` CLI；Docker 镜像内通过 Debian 软件源安装的 FFmpeg 组件遵循其各自的 GPL/LGPL 许可协议。
- **Jellyfin 商标声明**：CourseFin 是一个独立开发的开源项目，与 Jellyfin 官方项目无任何隶属、背书或赞助关系，仅对外实现兼容现有客户端接入的 API 协议子集。
