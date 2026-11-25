# CourseFin 每课 AI 总结与无字幕语音转写设计文档

- **Date:** 2026-11-25
- **Status:** Approved
- **Repository:** `CourseFin`

---

## 1. Intent & Why

CourseFin 目前已经内置了 OpenAI 兼容的 AI 课程整理能力（用于整理课程标题、简介、标签与章节目录）以及完整的 Web / Jellyfin 播放与字幕能力，但在日常学习场景中缺少「单节课内容级」的辅助工具：

1. **看课前缺少内容预览：** 用户在打开一节课观看前，只能看到文件名/课时标题，无法快速了解本节课讲了哪些模块、重点是什么，也难以按需直达感兴趣的片段。
2. **缺少便于导入笔记的结构化内容：** 用户习惯边看课边在自己的笔记软件（如 Obsidian / Notion 等）里记录，需要把每节课的「视频大纲」和「按时间顺序的讲解内容」一键整理成干净的 Markdown 带入笔记。
3. **大量课程视频没有字幕：** 许多本地课程视频既无外挂字幕也无内嵌字幕；如果只依赖现有字幕文件，AI 总结会在大量无字幕课程上直接失效。同时，既然为无字幕视频做了一次语音转文字，转写出来的带时间戳文稿也应该被持久化复用，并顺便成为可播放的字幕轨。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals

1. **每课两段式结构化 AI 总结：**
   - **AI 总结视频大纲：** 一句话核心概览 + 3~8 条结构化大纲要点（含可选跳转时间戳），帮助看课前快速把握全节脉络。
   - **AI 总结视频内容（按时间顺序）：** 按视频时间轴先后顺序分段梳理讲解内容，每段包含起止时间、段落标题、内容概括与核心知识点列表；点击时间戳可直接跳转播放器到对应时刻。
   - 总结结果持久化存储在 SQLite，切换课时或下次打开秒级展示，支持随时手动「重新生成」。
2. **无字幕全链路闭环（含 AI 字幕托管复用）：**
   - 优先读取已有**外挂字幕**（`subtitle_files`），其次提取视频**内嵌文本字幕**（FFmpeg 提取），再其次复用**已缓存转写稿**（`media_transcripts`）。
   - 若三者皆无，则通过内置 FFmpeg 提取低码率单声道音频（超长视频自动切片适配上游体积限制），调用用户配置的 **OpenAI 兼容语音转文字接口（`/audio/transcriptions`）**（兼容云端 Whisper/Groq/硅基流动，以及自托管本地 Faster-Whisper / Speaches 服务）。
   - ASR 生成的带时间戳 WebVTT 持久化在 SQLite 中（不写入媒体源目录），既用于后续重新总结复用（避免重复转写），也通过 `media.Service` 自动暴露为一条可播放的 **「AI 转写字幕」** 轨道（Web 播放器与 Infuse / VidHub / SenPlayer 等 Jellyfin 客户端均可直接开启）。
   - 当未配置 ASR 接口时，仍允许用户在播放页直接**手动粘贴文稿/字幕**生成总结，或引导前往设置页配置 ASR / 前往编辑页绑定外挂字幕。
3. **单课异步生成与整门课一键批量生成：**
   - 独立后台服务 `internal/summary` 托管单课任务与整门课串行批量队列。
   - 前端通过轮询实时展示细粒度阶段进度（`读取字幕 → 提取音频 → 语音转写 (x/y 片) → AI 总结`），支持随时取消；刷新/切页不中断后台执行，且避免反向代理 HTTP 超时。
4. **超长课程完整覆盖：**
   - 字幕/转写先规整压缩为紧凑时间戳文本；常规时长课时单次调用 LLM 完成；超过单次上下文预算的超长课程自动按时间分块（Map-Reduce）提炼并汇总大纲与完整时间线，不截断丢弃后半节课内容。
5. **播放页右侧双 Tab 交互与一键复制 Markdown：**
   - 在 `CourseDetailPage` 右侧固定栏新增 **「课程目录 / AI 总结」** Tab 切换，边看视频边对照时间线、点击时间戳跳转播放、当前播放时间自动高亮对应段落。
   - 提供 **一键复制单课 Markdown**（含课程/章节元信息、视频大纲、带 `[MM:SS - MM:SS]` 时间戳的内容总结），开箱适配主流 Markdown 笔记工具。

### Non-Goals

- 不内置本地离线 Whisper 模型推理二进制或 Python 运行时（不向 Docker 镜像或单二进制增加大模型依赖）；本地离线转写由用户部署任意兼容 OpenAI `/audio/transcriptions` 协议的独立服务接入。
- 不实现 SSE 流式打字机推送（沿用 CourseFin 现有 `scanner` / `updater` 的状态轮询架构）。
- 不实现下载 `.md` 文件、整门课合并导出 Markdown，或特定笔记软件的插件/URI 集成（本次仅提供单课一键复制 Markdown）。
- 不做图形字幕（PGS / VOBSUB / DVB）的 OCR 识别；仅包含图形字幕的视频视为无文本字幕，走 ASR 或手动粘贴链路。
- 不在 CourseFin 内部提供总结正文在线编辑器（用户可点击重新生成，或复制到个人笔记软件后编辑）。

### Anti-Goals

- **绝不向用户存储源（媒体目录）写入任何文件：** ASR 临时音频切片仅写在 `{dataDir}/cache/asr/` 并在任务结束/取消/服务关闭时删除；AI 转写字幕持久化在 SQLite `media_transcripts` 表中，彻底兼容 Docker `/media:ro` 只读挂载。
- **绝不因存储扫描丢失或破坏 AI 字幕：** AI 字幕独立存于 `media_transcripts`，不混入 `subtitle_files`，`scanner` 二次扫描不会将其标记为 `missing`。
- **绝不泄漏 FFmpeg 子进程或并发打爆上游限流：** 后台任务统一受控于 `internal/summary` 的串行执行队列（并发度 1），全部 FFmpeg 与 HTTP 请求均绑定任务 `context.Context`，取消任务或服务优雅停机时立即中止。
- **绝不静默泄露主 AI API Key 到第三方 ASR 域名：** 当用户配置了与主 AI 不同的独立 `ai_asr_base_url` 且留空 `ai_asr_api_key` 时，不得把主 `ai_api_key` 转发给该 ASR 地址（既防密钥泄露，也兼容无需认证的本地 Whisper 容器）。
- **绝不覆盖失败重生成前的有效总结：** 重新生成失败或中途取消时，数据库中原有的 `lesson_summaries` 记录必须保持不变。

---

## 3. Constraints

- **单二进制与零额外系统依赖：**
  - 后端保持 `CGO_ENABLED=0` 纯 Go 静态编译，不引入新的外部 Go 依赖（使用标准库 `mime/multipart`、`net/http`、`encoding/json`、`os/exec` 与现有 `modernc.org/sqlite`）。
  - 音频提取与内嵌字幕提取复用运行时 `ffmpeg` / `ffprobe`（Dockerfile 已内置；若运行环境无 `ffmpeg`，有外挂字幕或手动粘贴文稿的课时仍可正常生成总结，仅无字幕自动 ASR / 内嵌字幕提取提示缺少 FFmpeg）。
- **前端样式与测试约束：**
  - 前端不引入新 npm 依赖；全部 UI 使用 `web/src/index.css` 语义化颜色 token（`bg-card`, `bg-subtle`, `text-ink-primary`, `bg-accent`, `bg-accent-soft`, `text-accent-ink`, `text-status-danger-ink` 等），兼容浅色与深色双主题，禁止硬编码 Tailwind 色板。
  - Vitest 运行于 `jsdom` 环境，所有新增组件与页面测试需可稳定 mock API 并测试完整交互流程。
- **向后兼容性：**
  - `GET/PUT /api/v1/settings`、`GET /api/v1/courses/{id}`、`GET /api/v1/lessons/{id}/playback` 及全部 Jellyfin API 保持向后兼容（仅增量新增字段）。
  - 已有 `ai_max_subtitle_chars` 配置继续专用于「课程 AI 整理」的字幕摘录预算，单课 AI 总结使用独立的完整上下文预算策略，互不干扰。

---

## 4. Approaches Considered & Decisions

| 方案 | 核心思路 | 权衡对比 | 决策 |
|---|---|---|---|
| **方案 A：独立摘要任务服务 + DB 托管 AI 字幕 + 长文自动分段** | 新增 `internal/summary` 后台任务与串行队列服务；ASR 转写 WebVTT 存 SQLite `media_transcripts` 并由 `media.Service` 暴露为 `AI 转写字幕` 轨；超长课程自动 Map-Reduce 分段总结；前端轮询阶段进度并支持取消。 | 兼容 `/media:ro` 只读卷、规避反代 504 超时、切页不中断批量任务、AI 字幕自动惠及 Web 与 Jellyfin 客户端、不截断长课程；比纯同步实现多 1 个后台服务包与 2 张表。 | **已选定（用户选择）** |
| **方案 B：轻量同步接口 + 写盘 `.vtt` 到视频目录** | 单课走同步 HTTP 请求，批量由前端循环发请求；ASR `.vtt` 写到视频同级目录依赖重扫；长字幕直接按字数上限截断。 | 后端代码最少；但 Docker 只读卷直接失败、长视频易 504 超时、关页即中断批量任务、截断丢失后半节课内容。 | 未采用 |
| **方案 C：方案 A + SSE 流式打字机推送** | 在方案 A 基础上对 LLM 总结增加 `stream: true` + SSE 推送正文。 | 单课等待体感略快；但输出为嵌套结构的严格 JSON（大纲+时间线），流式解析与防 UI 抖动复杂度高且易受反代缓冲影响；阶段轮询已与项目其他长任务保持一致。 | 未采用 |

---

## 5. Detailed Design

### 5.1 数据库迁移：`internal/db/migrations/003_lesson_ai_summary.sql`

新增两张表，均绑定级联删除（`ON DELETE CASCADE`），不修改任何既有表结构：

```sql
-- Cached transcripts (ASR-generated WebVTT or manually pasted text) keyed by media_file_id
CREATE TABLE IF NOT EXISTS media_transcripts (
    media_file_id TEXT PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
    source TEXT NOT NULL,              -- 'asr' | 'manual'
    language TEXT NOT NULL DEFAULT 'und',
    model TEXT NOT NULL DEFAULT '',
    content_vtt TEXT NOT NULL DEFAULT '',
    plain_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Persisted per-lesson AI summaries keyed by lesson_id
CREATE TABLE IF NOT EXISTS lesson_summaries (
    lesson_id TEXT PRIMARY KEY REFERENCES lessons(id) ON DELETE CASCADE,
    media_file_id TEXT NOT NULL DEFAULT '',
    source_type TEXT NOT NULL DEFAULT '',   -- 'external_subtitle' | 'embedded_subtitle' | 'asr' | 'manual'
    source_label TEXT NOT NULL DEFAULT '',  -- human-readable source note, e.g. '外挂字幕 (zh)' / 'AI 语音转写'
    has_timestamps INTEGER NOT NULL DEFAULT 1,
    model TEXT NOT NULL DEFAULT '',
    overview TEXT NOT NULL DEFAULT '',
    outline_json TEXT NOT NULL DEFAULT '[]',
    timeline_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

同时在 `internal/db/db.go` 的 `ensureDataDirs` 中补充 `{dataDir}/cache/asr` 目录创建。

#### 结构化 JSON Schema

- **`outline_json`** (`[]OutlineItem`):
  ```json
  [
    {
      "title": "模块标题",
      "summary": "1~2 句话说明本模块核心内容",
      "start_sec": 0
    }
  ]
  ```
  - `start_sec` 为 `*int`（整数秒，可选；无时间戳文稿时为 `null`）。
- **`timeline_json`** (`[]TimelineSection`):
  ```json
  [
    {
      "start_sec": 0,
      "end_sec": 315,
      "title": "段落主题",
      "summary": "该时间段内讲解内容概括",
      "key_points": ["核心知识点 1", "关键命令或步骤 2"]
    }
  ]
  ```
  - `start_sec` / `end_sec` 均为 `*int`（整数秒；无时间戳文稿时为 `null`）。

---

### 5.2 系统配置扩展（`app_settings` & `SettingsPage`）

在 `app_settings` 中新增以下 4 个可选配置键（默认值均为空字符串）：

| Key | 含义 | 说明 |
|---|---|---|
| `ai_asr_base_url` | 语音转文字接口 Base URL | 留空时默认复用 `ai_base_url`；例如 `https://api.openai.com/v1`、`https://api.groq.com/openai/v1`、`https://api.siliconflow.cn/v1`、`http://whisper:8000/v1` |
| `ai_asr_api_key` | 语音转文字 API Key | 返回时脱敏为 `****`，附 `ai_asr_api_key_set: bool` 与 `clear_ai_asr_api_key: bool` |
| `ai_asr_model` | 语音转文字模型名称 | 例如 `whisper-1`、`whisper-large-v3-turbo`、`FunAudioLLM/SenseVoiceSmall`；**非空且有效 ASR Base URL 非空即视为已启用 ASR** |
| `ai_asr_language` | 语音识别语言提示（可选） | ISO-639-1 语言代码，留空为自动检测，常用 `zh`、`en` |

#### 有效 ASR 配置解析规则 *(Our call)*

```
effectiveASRBaseURL = trim(ai_asr_base_url) != "" ? trim(ai_asr_base_url) : trim(ai_base_url)
effectiveASRAPIKey  = trim(ai_asr_api_key) != ""  ? trim(ai_asr_api_key)
                    : (trim(ai_asr_base_url) == "" ? trim(ai_api_key) : "")
asrConfigured       = effectiveASRBaseURL != "" && trim(ai_asr_model) != ""
aiConfigured        = trim(ai_base_url) != "" && trim(ai_model) != ""
```

这样既能让使用同一家云服务商（如 OpenAI / 硅基流动）的用户只填一个模型名即可复用主地址与密钥，又能防止填写了独立本地 Whisper 地址时把主 LLM 密钥误发到另一个主机。

#### ASR 连接测试：`POST /api/v1/settings/ai/asr/test`

- 接受可选的未保存表单覆盖参数（`asr_base_url` / `asr_api_key` / `asr_model` / `asr_language`，以及未保存的 `ai_base_url` / `ai_api_key` 以便正确计算回退）。
- 服务端在内存中构造一段 0.5 秒 16kHz 16-bit 单声道静音 WAV（纯 Go 构造约 16KB，无需调用 FFmpeg），以 `multipart/form-data` 提交到 `{effectiveASRBaseURL}/audio/transcriptions`（携带 `model`、`file`、`response_format=json`）。
- 收到任意 HTTP 2xx 响应即认为接口地址、认证和模型名有效，返回 `{"ok": true}`；非 2xx 返回 `502 Bad Gateway` 与格式化后的上游错误信息。

---

### 5.3 AI 与语音转写客户端扩展（`internal/ai`）

#### 1) 语音转文字客户端 (`internal/ai/asr.go`)

- 新增结构体：
  ```go
  type ASRConfig struct {
      BaseURL  string
      APIKey   string
      Model    string
      Language string
  }

  type TranscriptCue struct {
      StartSec float64
      EndSec   float64
      Text     string
  }

  type TranscribeResult struct {
      Language string
      Text     string
      Cues     []TranscriptCue
  }
  ```
- 新增 `OpenAICompatibleTranscriber`（可复用于 `summary.Service` 与 `handleTestASR`）：
  - URL 规范化：去除末尾 `/`，若已以 `/audio/transcriptions` 结尾则保留，否则追加 `/audio/transcriptions`。
  - 默认超时 `300 * time.Second`（大音频分片转写可能耗时 1~2 分钟）。
  - **多格式兼容策略 *(Our call)*：**
    1. 优先以 `response_format=verbose_json` 请求；若返回 200 且包含 `segments` 数组，直接解析 `start`/`end`/`text` 与 `language`。
    2. 若返回 200 但 `segments` 为空且 `text` 非空（部分中转站或非时间戳模型），直接返回 `Text`（由调用方按当前音频切片起止时间封装为一条时间段 cue）。
    3. 若因上游不支持 `verbose_json` 返回 HTTP `400`/`422`，依次自动降级重试 `response_format=srt`（解析 SRT 时间轴）→ `response_format=json`（仅提取 `text`）。

#### 2) 课时 AI 总结能力 (`internal/ai/summary.go`)

- 扩展 `Provider` 接口或在 `OpenAICompatibleProvider` 上新增方法（并单独为总结调用使用更长超时 `180 * time.Second` 的 HTTP Client，或允许 `ChatCompletion` 复用调用方传入的带超时 `http.Client`）：
  - `SummarizeLesson(ctx context.Context, input LessonSummaryInput) (*LessonSummaryOutput, error)` — 单次完整总结（常规课时）。
  - `SummarizeLessonChunk(ctx context.Context, input LessonChunkInput) ([]TimelineSection, error)` — 超长课程 Map 阶段（针对单个时间切片产出按时间顺序的 `timeline`）。
  - `SynthesizeLessonOutline(ctx context.Context, input LessonOutlineSynthInput) (overview string, outline []OutlineItem, err error)` — 超长课程 Reduce 阶段（根据合并后的全节 `timeline` 提炼整节 `overview` 与 `outline`）。
- **Prompt 约束：**
  - 明确要求严格返回合法 JSON（无 Markdown 代码围栏与多余说明），服务端继续用 `stripMarkdownJSONFence` 兜底清洗。
  - **总结输出语言 *(Our call)*：** 默认使用**中文**总结并保留英文专有名词、函数名、命令与代码标识（兼顾中文课与常见英文技术课程看课笔记需求）。
  - `outline` 要求提炼 3~8 个逻辑模块，结构清晰、不冗长，便于看课前 30 秒扫完。
  - `timeline` 要求严格按时间先后覆盖整节课内容（单调递增，不遗漏后半段），每段输出简明小标题、`summary` 和适合直接记笔记的 `key_points`。
  - 当 `has_timestamps == false` 时，明确指示模型将 `start_sec` / `end_sec` 置为 `null`，按文稿出现顺序分节总结。
- **结果校验与规范化 (`ValidateAndNormalizeSummary`)：**
  - 校验 `outline` 与 `timeline` 均非空；清理空白字符串与空 `key_points`。
  - 当 `has_timestamps == true` 时：将所有 `start_sec` / `end_sec` 钳位到 `[0, duration_sec]`（若已知视频时长），确保 `end_sec >= start_sec`，并按 `start_sec` 稳定排序。
  - 当 `has_timestamps == false` 时：统一将所有时间戳规范化为 `nil`。

---

### 5.4 媒体层集成：AI 转写字幕暴露与统一字幕文本读取（`internal/media/subtitle.go`）

为了让 ASR 转写出来的 WebVTT 同时成为 Web 播放器与 Jellyfin 客户端的可用字幕，并且让 `internal/summary` 无需重复实现外挂/内嵌字幕解码和 FFmpeg 提取逻辑：

1. **在 `media.StreamDescriptor` 上新增内部字段 `IsAIGenerated bool`（不改变现有字段含义）。**
2. **更新 `media.Service.BuildStreamDescriptors(ctx, mediaFileID, streamsJSON)`：**
   - 在追加嵌入式流与活跃 `subtitle_files` 之后，查询 `media_transcripts`：
     ```sql
     SELECT language FROM media_transcripts
     WHERE media_file_id = ? AND source = 'asr' AND content_vtt != ''
     ```
   - 若命中，则追加一条 `StreamDescriptor`：
     - `Index: nextIndex`（排在所有物理字幕之后，保证既有外挂/内嵌流索引稳定不变）
     - `Type: "subtitle"`、`Codec: "vtt"`、`Format: "vtt"`
     - `Language`: 表中记录的语言（空时为 `"und"`）
     - `Title: "AI 转写字幕"`
     - `IsDefault: false`（绝不抢占真实外挂/内嵌默认字幕；当无其他字幕时，前端 `pickInitialSubtitle` 会自动选中该唯一字幕轨）
     - `IsExternal: true`、`IsAIGenerated: true`
3. **更新 `media.Service.ServeSubtitle(...)`：**
   - 命中 `target.IsAIGenerated` 时，从 `media_transcripts.content_vtt` 读取 WebVTT 字节；若客户端请求 `vtt`（默认）直接返回，若 Jellyfin 客户端请求 `srt`/`ass` 则走现有 `convertSubtitleBytesWithFFmpeg` 转换后返回。
4. **新增导出方法供 `internal/summary` 复用：**
   - `ExtractBestSubtitleVTT(ctx context.Context, mediaFileID string) (vttText string, sourceType string, sourceLabel string, found bool, err error)`
     - 按优先级寻找：① 活跃外挂字幕（优先 `is_default` → 中文轨 → 首条）→ ② 非图形内嵌字幕（优先 `is_default` → 中文轨 → 首条）。
     - 自动复用 `DecodeSubtitleToUTF8`、`ConvertSRTToVTT` 及 FFmpeg ASS/内嵌提取，统一返回标准 UTF-8 WebVTT。
   - `ResolveLocalMediaPath(ctx context.Context, mediaFileID string) (absPath string, durationMs int64, hasAudio bool, err error)`
     - 解析本地文件路径、时长，并根据 `streams_json` 判断是否存在音轨（若 `streams_json` 为空未探测则默认按有音轨交给 FFmpeg 探测）。
   - `FFmpegBin() string` / `FFprobeBin() string`（或提供 `ExtractAudioChunks(ctx, absPath, workDir, chunkSec)` 封装在 `internal/media` 或 `internal/summary` 中）。

---

### 5.5 独立摘要与队列服务（`internal/summary`）

新增 `internal/summary` 包，挂载到 `server.Server` 并在 `server.Close()` 时取消上下文并 `Wait()`（与 `scanner.Service`、`updater.Service` 生命周期完全一致）。

#### 核心状态模型

```go
type JobStage string

const (
    StageQueued          JobStage = "queued"
    StageReadingSubtitle JobStage = "reading_subtitle"
    StageExtractingAudio JobStage = "extracting_audio"
    StageTranscribing    JobStage = "transcribing"
    StageSummarizing     JobStage = "summarizing"
    StageCompleted       JobStage = "completed"
    StageFailed          JobStage = "failed"
    StageCancelled       JobStage = "cancelled"
)
```

- **单课任务状态 (`LessonJobStatus`)：**
  - `lesson_id`, `course_id`, `running`, `stage`, `message`, `chunk_index`, `chunk_total`, `source_type`, `error`, `error_code`, `started_at`, `finished_at`
- **整门课批量任务状态 (`CourseBatchStatus`)：**
  - `course_id`, `running`, `total`, `completed`, `skipped`, `failed`, `current_lesson_id`, `current_lesson_title`, `current_stage`, `chunk_index`, `chunk_total`, `errors []BatchLessonError`, `started_at`, `finished_at`

#### 队列与并发控制 *(Our call)*

- 全局采用单一后台串行 worker（并发度 1），原因：
  1. FFmpeg 抽音频与音频切片在低配 NAS 上串行更稳，不争抢播放转码 CPU；
  2. 大多数 OpenAI 兼容 LLM / Whisper 接口对并发请求有严格 RPM/TPM 限制，串行顺序执行最不容易触发 429 限流。
- 单课点击生成与整门课批量生成共享同一套 `runLessonSummary(ctx, req)` 核心执行管线：
  - **单课生成优先级**：若此时有其他课程的批量任务在排队，单课任务排入单课优先队列（当前正在执行的单个课时完成后立即执行该单课，再继续批量队列），确保用户正在看的那一节课响应及时。*(Our call)*
  - 同一 `lesson_id` 正在执行或已在队列中时，再次触发直接返回当前任务快照（幂等防重）。
  - 取消单课任务：若正在运行则调用该任务的 `cancel()`，若在等待队列则直接移除并标为 `cancelled`。
  - 取消整门课批量任务：调用当前批量任务的 `cancel()`（若当前正在执行的课时属于该批次则立即中止），并清空该批次剩余待处理课时。

#### 单课执行管线（`runLessonSummary`）

1. **阶段 `reading_subtitle`（获取文本来源）：**
   - 若请求携带 `manual_transcript`（非空）：
     - 清洗并识别是否包含 SRT/VTT 时间轴（含 `-->`）；若有则转为标准 WebVTT 存 `media_transcripts.content_vtt`（`has_timestamps = true`），否则存 `media_transcripts.plain_text`（`has_timestamps = false`），`source = 'manual'`。
   - 否则依次尝试：
     1. 若 `!force_retranscribe`：调用 `media.ExtractBestSubtitleVTT` 检查外挂/内嵌文本字幕。命中即进入步骤 4。
     2. 若 `!force_retranscribe`：查询 `media_transcripts` 缓存。若存在有效 `content_vtt` 或 `plain_text`，直接复用，跳过音频抽取与 ASR！
     3. 进入 ASR 流程（步骤 2~3）。
2. **阶段 `extracting_audio`（FFmpeg 抽音频切片）：**
   - 前置校验：检查 ASR 已配置（否则失败 `error_code = "asr_not_configured"`）、FFmpeg 可用（否则 `ffmpeg_unavailable`）、本地文件路径可解析、视频存在音轨（否则 `no_audio_track`）。
   - 创建 `{dataDir}/cache/asr/{jobID}/`（`defer os.RemoveAll` 确保任何退出路径都清理干净）。
   - 执行 FFmpeg 提取 16kHz 单声道 48kbps MP3 并按每片 1200 秒（20 分钟，约 7.2MB/片，远低于 25MB 接口上限）自动切片：
     ```
     ffmpeg -hide_banner -loglevel error -i <absPath> -vn -ac 1 -ar 16000 -b:a 48k \
       -f segment -segment_time 1200 -reset_timestamps 1 <workDir>/chunk_%03d.mp3
     ```
   - 扫描生成的 `chunk_*.mp3` 列表并按名称排序；对每片通过 `ffprobe` 读取精确时长以计算累计偏移秒数 `offsetSec`（`ffprobe` 不可用或失败时回退为 `idx * 1200`）。
3. **阶段 `transcribing`（分片语音转文字并持久化 AI 字幕）：**
   - 顺序遍历各分片，每片更新 `chunk_index = i+1, chunk_total = N`，调用 `OpenAICompatibleTranscriber.TranscribeChunk(ctx, chunkPath)`。
   - 将返回的 cues 全部加上当前分片的 `offsetSec` 偏移并汇总；若上游仅返回无时间戳纯文本，则为该分片生成 `[offsetSec, offsetSec + chunkDur]` 的分片级 cue。
   - 将完整合并后的 cues 序列化为标准 `WEBVTT` 文本，写入（UPSERT）SQLite `media_transcripts`（`source = 'asr'`）。
4. **阶段 `summarizing`（规整文本 + 单次或 Map-Reduce 总结）：**
   - 将 WebVTT 解析为 cues，去除 HTML/VTT/ASS 格式标签与相邻重复滚动字幕行，将短句按 ~15 秒窗口或 ~200 字符合并为紧凑的 `[MM:SS] 文本`（超过 1 小时用 `[H:MM:SS]`）。
   - 阈值判定：`singlePassMaxRunes = 24000` *(Our call)*：
     - **常规课时（≤ 24,000 字符）：** 单次调用 `SummarizeLesson`，直接获得 `overview` + `outline` + `timeline`。
     - **超长课时（> 24,000 字符）：** 按 cue 边界切成每块约 18,000 字符的 `M` 块：
       - Map（`chunk_index = 1..M, chunk_total = M+1`）：逐块调用 `SummarizeLessonChunk` 生成对应时间段的 `timeline` 列表并按序拼接。
       - Reduce（`chunk_index = M+1, chunk_total = M+1`）：将全节合并后的 `timeline` 标题、起止时间与摘要传给 `SynthesizeLessonOutline` 生成全局 `overview` 与 `outline`。
   - 校验并规范化结果后，UPSERT 写入 `lesson_summaries`，将任务标记为 `completed`。

---

### 5.6 REST API 设计（`internal/api`）

全部注册在受 `requireAuth` 保护的 `/api/v1` 路由组下：

| Method & Path | 用途 | 关键请求 / 响应 |
|---|---|---|
| `GET /api/v1/settings` | 获取系统与 AI/ASR 设置 | 在现有字段基础上新增 `ai_asr_base_url`, `ai_asr_api_key`, `ai_asr_api_key_set`, `ai_asr_model`, `ai_asr_language`, `ai_configured`, `ai_asr_configured` |
| `PUT /api/v1/settings` | 保存系统与 AI/ASR 设置 | 新增可选字段 `ai_asr_base_url`, `ai_asr_api_key`, `clear_ai_asr_api_key`, `ai_asr_model`, `ai_asr_language` |
| `POST /api/v1/settings/ai/asr/test` | 测试语音转文字接口连接 | 发送 0.5s 静音 WAV 验证 `/audio/transcriptions`，返回 `{"ok": true}` |
| `GET /api/v1/lessons/{id}/summary` | 获取单课已保存总结、当前任务状态与能力检测 | 返回 `{ lesson_id, summary, job, capabilities: { has_subtitle, has_cached_transcript, transcript_source, ai_configured, asr_configured, has_ffmpeg } }` |
| `POST /api/v1/lessons/{id}/summary/generate` | 启动单课 AI 总结任务 | 请求 `{ force?: bool, force_retranscribe?: bool, manual_transcript?: string }`；校验配置不满足时返回 `400`（带 `code`: `ai_not_configured` / `asr_not_configured`）；成功入队返回 `202` + `LessonJobStatus` |
| `POST /api/v1/lessons/{id}/summary/cancel` | 取消单课总结任务 | 返回 `200` + `LessonJobStatus` |
| `DELETE /api/v1/lessons/{id}/summary` | 删除单课已保存总结 | 可选 `?delete_transcript=true` 同时清除缓存转写稿；返回 `{"ok": true}` |
| `GET /api/v1/courses/{id}/summaries/status` | 获取课程已总结课时 ID 列表及批量任务实时状态 | 返回 `{ course_id, summary_lesson_ids: string[], batch_job: CourseBatchStatus \| null }` |
| `POST /api/v1/courses/{id}/summaries/generate` | 启动整门课批量 AI 总结 | 请求 `{ mode?: "missing_only" \| "all" }`（默认 `missing_only`）；返回 `202` + `CourseBatchStatus`；若无待处理课时直接返回 `200` |
| `POST /api/v1/courses/{id}/summaries/cancel` | 取消整门课批量任务 | 返回 `200` + `CourseBatchStatus` |

---

### 5.7 前端交互与组件设计（`web/src`）

#### 1) `SettingsPage.tsx`：新增「语音转文字（ASR）」配置区块
- 紧跟现有「OpenAI 兼容接口（AI 文本模型）」区块下方，以清晰的分隔标题展示「语音转文字接口（用于无字幕视频自动转写并生成 AI 字幕）」：
  - **ASR 模型名称**（如 `whisper-1`、`whisper-large-v3-turbo`、`FunAudioLLM/SenseVoiceSmall`；留空表示关闭自动转写）
  - **ASR Base URL**（选填，留空默认复用上方 AI Base URL）
  - **ASR API Key**（选填，带「清除已存 Key」勾选框；当 ASR Base URL 留空时提示「留空复用上方 AI API Key」，当填写了独立 Base URL 时提示「留空表示该服务无需认证」）
  - **识别语言提示**（选填下拉或输入框：自动检测 / 中文 `zh` / 英文 `en` / 自定义）
  - 独立 **「测试语音转文字」** 按钮（调用 `POST /api/v1/settings/ai/asr/test`）与测试结果提示。

#### 2) `VideoPlayer.tsx`：暴露跳转句柄与无中断字幕轨道刷新
- 通过 `forwardRef` + `useImperativeHandle` 暴露 `VideoPlayerHandle`：
  ```ts
  export interface VideoPlayerHandle {
    seekTo: (seconds: number, autoPlay?: boolean) => void;
  }
  ```
  - `seekTo(seconds, autoPlay = true)` 安全钳位到 `[0, duration]`，设置 `video.currentTime`（若 `autoPlay` 且当前处于暂停态则尝试 `video.play()`）。
- **无中断刷新 AI 字幕轨：** 将字幕轨道更新与 Artplayer 实例销毁/重建解耦（仅在 `playback.lesson_id / play_session_id / stream_url / mode` 变化时重建实例；`subtitles` 变化时原地刷新 Artplayer 控制栏与设置面板的字幕选项列表）。当当前课时 ASR 完成后，`CourseDetailPage` 重新拉取该课时的字幕列表并原地注入，同时给出 Toast 提示「已生成 AI 转写字幕，可在播放器字幕菜单开启」。

#### 3) `CourseDetailPage.tsx` 右侧双 Tab + `LessonSummaryPanel.tsx`
- **右侧栏顶部双 Tab：**
  - **Tab 1 — 课程目录**：保持现有章节折叠与环形进度条；为每个在 `summary_lesson_ids` 中的课时右侧附加一个轻巧的 `Sparkles` 图标（Tooltip：「已生成 AI 总结」）；目录顶部增加紧凑的「批量总结」按钮（触发 `missing_only` 批量生成；若全部已生成则显示「全部已总结」并支持重新批量生成）。
  - **Tab 2 — AI 总结**：渲染新组件 `LessonSummaryPanel`，接收 `course`、`activeEntry`、`currentTimeSec`、`onSeek(sec)`、`onSummaryUpdated(lessonId, sourceType)`、`batchJob` 与批量控制回调。
- **`LessonSummaryPanel` 状态与视觉层次：**
  1. **批量任务横幅**（只要 `batchJob?.running` 就在面板顶部显示）：展示 `批量生成中 3/12 · 正在转写「课时名」(2/4)` + 进度条 +「取消」按钮。
  2. **已有总结展示**：
     - 顶部工具条：来源徽章（如 `外挂字幕` / `内嵌字幕` / `AI 语音转写` / `手动文稿`）+ 主按钮 **「复制 Markdown」**（复制成功切换为「已复制」图标反馈 + Toast）+ 次按钮 **「重新生成」**（若来源为 `asr` 或有缓存转写，补充轻量操作「重新转写」以便在更换 ASR 模型后强制重转）。
     - **区块一：AI 总结视频大纲**
       - 顶部淡底色卡片展示一句话概览（`overview`）。
       - 编号大纲列表：每条显示标题、简述，以及（若 `start_sec != null`）可点击的 `MM:SS` 跳转时间标签。
     - **区块二：AI 总结视频内容（按时间顺序）**
       - 纵向时间线分段卡片：每段头部为可点击的时间区间按钮（如 `05:30 - 12:10`，当播放器当前时间 `currentTimeSec` 落在该区间内时以 `bg-accent-soft` + 左侧 `bg-accent` 指示条高亮）、段落标题；下方展示段落摘要与圆点核心要点列表。
  3. **未生成空状态**（依据 `capabilities` 给出精准引导）：
     - 有外挂/内嵌字幕或已有缓存转写：主按钮「生成 AI 总结」。
     - 无字幕但已配置 ASR：说明将自动抽取音频并语音转写（同步生成 AI 字幕），主按钮「转写并生成总结」+ 折叠次入口「手动粘贴文稿」。
     - 无字幕且未配置 ASR：提示「当前课时无字幕且未配置语音转文字」，直接提供「前往配置语音转文字」链接 + 展开的**手动粘贴文稿输入框**（支持粘贴纯文本或 SRT/VTT 后点击「根据文稿生成总结」）+「去编辑页绑定字幕」链接。
     - 未配置主 AI 模型：提示并引导前往 `/settings`。
  4. **单课生成中状态**：
     - 展示分阶段步骤指示与提示文案（`正在读取字幕...` / `正在提取视频音频...` / `正在语音转写（分片 2/4）...` / `正在生成 AI 总结...`）+「取消」按钮。
     - 若此前已有旧总结，生成中/失败仅以顶部状态卡片呈现，下方仍可继续阅读与复制旧总结。

#### 4) Markdown 复制格式（`web/src/utils/summaryMarkdown.ts`）

点击「复制 Markdown」调用 `formatLessonSummaryMarkdown`，通过现有 `copyTextToClipboard` 写入剪贴板：

```markdown
# {lesson.title}

> 课程：{course.title} · 章节：{chapterTitle}

## AI 总结视频大纲

{summary.overview}

1. **{item.title}**（`00:00`）：{item.summary}
2. **{item.title}**（`05:30`）：{item.summary}

## AI 总结视频内容（按时间顺序）

### [00:00 - 05:30] {section.title}

{section.summary}

- {key_point_1}
- {key_point_2}
```

（当 `has_timestamps === false` 或某项时间戳为 `null` 时，自动省略时间标注，保持 Markdown 整洁。）

---

## 6. Verification Strategy

1. **后端单元测试 & 集成测试（`go test ./...`）：**
   - `internal/db`：验证迁移 `003_lesson_ai_summary.sql` 正确建表与外键级联删除（删除 lesson 自动清理 summary；删除 media_file 自动清理 transcript）。
   - `internal/ai`：基于 `httptest.Server` 测试 `OpenAICompatibleTranscriber`（`verbose_json` 正常解析、400 降级到 `srt` / `json`、超时与错误格式化）与 `SummarizeLesson` / Map-Reduce 分块汇总及结果校验（时间戳钳位、排序、空字段校验、无时间戳模式）。
   - `internal/media`：验证 `BuildStreamDescriptors` 与 `ServeSubtitle` 正确暴露并提供 `media_transcripts` 中的 AI 转写字幕，且不干扰原有外挂/内嵌字幕索引；验证 `ExtractBestSubtitleVTT` 优先级（外挂 > 内嵌，跳过图形字幕）。
   - `internal/summary`：验证完整任务管线（外挂字幕直读、缓存转写复用、无字幕 FFmpeg + mock ASR 分片偏移合并 + AI 字幕入库、手动粘贴纯文本/SRT、超长文本 Map-Reduce、整门课批量队列含单课容错与取消、单课重新生成失败保留旧总结）。
   - `internal/api`：验证设置页 ASR 字段脱敏/回退规则、`/settings/ai/asr/test`、单课与批量总结 API 鉴权、状态码与错误码。
2. **前端测试（`pnpm test` & `pnpm build`）：**
   - `summaryMarkdown.test.ts`：覆盖带时间戳（含 ≥ 1 小时 `H:MM:SS`）与无时间戳 Markdown 生成。
   - `LessonSummary.test.tsx`：覆盖右侧栏「课程目录 / AI 总结」Tab 切换、时间戳点击触发 `seekTo`、当前播放段高亮、一键复制 Markdown、无字幕已配 ASR / 未配 ASR 手动粘贴文稿、阶段轮询与取消、整门课批量进度展示。
   - `SettingsPage` 测试补充：ASR 配置保存、独立域名不透传主 Key 提示与测试按钮交互。

---

## 7. Open / Left to Builder

以下具体实现细节留给实现阶段根据代码上下文微调，不影响产品行为：
- 字幕压缩合并窗口的具体参数（如 15s / 200 runes）及 Map-Reduce 分块字符上限（建议 24,000 / 18,000 runes，可在测试中按实际模型表现微调）。
- 轮询退避策略（建议任务活跃期 1.5s 轮询，页面隐藏时暂停轮询，结束后立即停止）。
- `LessonSummaryPanel` 内部拆分的子组件粒度与 Lucide 图标选择。
- 音频分片大小/码率微调（默认 16kHz 单声道 48kbps MP3，20 分钟/片 ≈ 7.2MB；若某音频流封装不兼容 MP3 则回退 WAV/FLAC 或调整 FFmpeg 参数）。
