# CourseFin 批量语音转文字与 AI 总结两步解耦及断点续跑设计

- **Date:** 2026-11-26
- **Status:** Approved
- **Repository:** `CourseFin`

---

## 1. Intent & Why

当前 CourseFin 的批量总结将「语音转文字（ASR）」与「AI 总结（LLM）」绑定在同一个按课时串行的任务流里，导致：

1. **两步进度无法分别查看与统计**：课程状态接口只返回 `summary_lesson_ids`，前端目录看不到哪些课时已有外挂/内嵌字幕或已完成语音转文字、哪些处于转写断点、哪些尚未转写。
2. **两步无法分别延续**：无法在不跑总结的前提下先批量转写剩余课时，也无法只针对已转写/有字幕的课时批量补齐总结，容易出现「以为没转写而重跑、或因 LLM 失败/限流卡住后续转写」的体验问题。
3. **长视频分片无断点保存**：超过 20 分钟的视频在 `runASRPipeline` 中切片转写，但只在全部分片完成后一次性写入 `media_transcripts`；若中途失败、取消或重启，重跑会对同一节课从第 1 片重新转写。
4. **缺少章节级与单课「仅转写」入口**：只能整门课触发批量总结，且单课面板无法仅做语音转文字生成 AI 字幕。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals
1. **两步彻底解耦与各自延续**：
   - 「批量语音转文字」仅处理既无物理文本字幕（外挂/内嵌非图形字幕）也无完整缓存转写稿（`media_transcripts`）的课时（含仅有分片断点进度的课时），自动跳过已有字幕或已转写课时；不依赖主 AI（LLM）配置。
   - 「批量生成总结」仅处理已具备可用文稿（外挂/内嵌文本字幕或完整 `media_transcripts`）的课时，**绝不隐式触发 ASR**；缺少文稿的课时自动跳过并单独计数（`skipped_no_transcript`）。
2. **双独立串行队列（并发度各为 1）**：
   - 拆分为独立的 **ASR 转写 Worker (`transcribeWorker`)** 与 **LLM 总结 Worker (`summaryWorker`)**，同一门课可同时推进批量转写与批量总结互不阻塞，同时各自保持串行并发度 1。
3. **长视频分片级断点续转**：
   - 新增 SQLite 表 `media_transcript_chunks`，每完成一个音频分片立即持久化该分片 cues；失败、取消或服务重启后再次转写自动跳过已完成分片；全部分片完成后在单事务中合并写入 `media_transcripts` 并清空临时分片表。
4. **整门课 + 章节级触发 & 独立状态统计**：
   - 课程状态接口返回每节课两维状态（文稿状态 + 分片断点 + 总结状态）、课程级汇总计数，以及两个独立批量任务状态（`transcribe_batch_job` 与 `summary_batch_job`）。
   - 批量转写与批量总结接口支持可选 `chapter_id`，前端目录顶部支持整门课操作，章节行支持按章触发，课时行独立展示文稿与总结状态。
5. **单课「仅语音转文字」与「转写并生成总结」双入口**：
   - 单课无文稿时同时提供「仅语音转文字」与「转写并生成总结」；若存在分片断点明确展示 `已完成 x/y 分片`；未配主 AI 但配置了 ASR 时仍允许执行「仅语音转文字」。
   - 转写完成（单课或批量）命中当前播放课时后，原地刷新播放器字幕列表并提示 AI 转写字幕可用。

### Non-Goals
- 不支持任意零散课时勾选子集（仅支持整门课与单章节粒度）。
- 不做批量转写完成后自动联动推入批量总结的隐式流水线（由用户显式分别触发）。
- 不将临时 MP3 切片跨任务常驻磁盘（仅持久化 SQLite 分片文本断点，续跑时通过 FFmpeg 确定性重新切片并跳过已完成分片的 ASR 调用）。
- 不改变既有外挂字幕 / 内嵌字幕 / AI 字幕优先级与播放器字幕流索引规则。

### Anti-Goals
- **绝不把「无文稿跳过」与「失败」混淆**：批量总结遇到无字幕且未转写课时归入 `skipped_no_transcript`（计入 `skipped`），不记入 `failed` 或 `errors`。
- **绝不对同一 `lesson_id` 或同一 `media_file_id` 并发执行两路任务（边转写边总结）**。
- **绝不因视频源文件被替换而拼接错位分片**：通过磁盘文件大小与修改时间指纹（`os.Stat` 优先）、`segment_sec` 与 `chunk_total` 校验，不匹配时自动失效旧断点重头转写。
- **绝不向只读媒体目录写入文件**，临时音频切片仍仅存于 `{dataDir}/cache/asr/{jobID}` 并于退出时清理。
- **绝不破坏现有 API 向后兼容**：保留既有字段（`summary_lesson_ids`、`batch_job`）与既有端点路径，仅做增量扩展。

---

## 3. Constraints
- 后端纯 Go（`CGO_ENABLED=0`），不引入新 Go 第三方依赖；SQLite 启用 WAL、`foreign_keys(1)`、`busy_timeout(5000)`、`MaxOpenConns(4)`。
- 前端不引入新 npm 依赖，样式全部使用 `web/src/index.css` 语义化 token，兼容浅/深色主题，禁止硬编码 Tailwind 色板。
- 播放页右侧边栏宽度为 `320px`（`lg`）至 `360px`（`xl`），所有统计、按钮、章节微操作与课时标识必须紧凑不溢出。

---

## 4. Approaches Considered & Decision

| 方案 | 核心做法 | 权衡对比 | 决策 |
|---|---|---|---|
| **方案 A（已选定）** | 双独立串行 Worker（ASR / LLM 各并发 1）+ SQLite 分片断点表 + `sync.Cond` 跨 Worker 互斥门禁 | 两步互不阻塞，各自串行保护 NAS CPU 与上游限流；重启与中断后可精确延续 | **已选定** |
| 方案 B | 单 Worker 共享队列区分任务类型 + 分片断点表 | 并发更简单，但长批量转写会完全阻塞已转完课时的批量总结 | 未采用 |
| 方案 C | 方案 A + 转写完一节自动推入总结队列 | 自动化更高，但动态批次总数与取消边界复杂，不符合显式两步分离控制 | 未采用 |

---

## 5. Detailed Design

### 5.1 数据库迁移：`internal/db/migrations/005_transcript_checkpoints.sql`

```sql
CREATE TABLE IF NOT EXISTS media_transcript_chunks (
    media_file_id TEXT NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    chunk_total INTEGER NOT NULL,
    segment_sec INTEGER NOT NULL,
    media_fingerprint TEXT NOT NULL DEFAULT '',
    offset_sec REAL NOT NULL DEFAULT 0,
    duration_sec REAL NOT NULL DEFAULT 0,
    language TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    cues_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (media_file_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS idx_media_transcript_chunks_media ON media_transcript_chunks(media_file_id);
```

- **文件指纹计算 (`computeMediaFingerprint`)**：`runASRPipeline` 优先以 `os.Stat(absPath)` 计算 `"{size}:{mod_time_utc_rfc3339nano}"`（与 `internal/scanner/scanner.go` 入库格式一致），`os.Stat` 不可用时回退读取 DB `media_files.file_size` 与 `media_files.mod_time`；均无则返回 `""`。这样即便文件被替换后尚未重新扫描，也会立刻失效旧断点；同时 `GetCourseSummaryStatus` 仍保持纯 SQL 零磁盘 IO。
- **断点校验与复用**：
  1. `forceRetranscribe == true` 时先删除该 `media_file_id` 所有分片断点。
  2. 提取音频分片 `chunks`（长度 `N`）；加载已有分片断点，仅当全部记录满足 `chunk_total == N`、`segment_sec == currentSegmentSec`、`media_fingerprint == currentFingerprint` 且 `0 <= chunk_index < N` 时复用；否则清理旧断点从头转写。（更换 ASR 模型不自动作废已完成分片；需整节重转时通过 `force_retranscribe: true` 清理。）
  3. 每转完 1 片立即使用带 5s 超时的 `context.WithoutCancel(ctx)`（在 `lifecycleCtx.Err() == nil` 前提下）UPSERT 写入 `media_transcript_chunks`，即便该片纯静音（0 cues）也落库 `cues_json = "[]"` 防止重复调用 ASR。
  4. 全部 `N` 片就绪后：若合并 cues 为空则清空 `media_transcript_chunks` 并返回 `empty_transcript`；否则在**单事务**中 UPSERT `media_transcripts (source = 'asr', ...)` 并 `DELETE FROM media_transcript_chunks WHERE media_file_id = ?`。
- `DeleteLessonSummary(ctx, lessonID, deleteTranscript=true)` 在事务内同步删除 `media_transcripts` 与 `media_transcript_chunks`。

### 5.2 两阶段状态模型与纯 SQL 统计

- **`LessonCapabilities` 增量字段**：`partial_transcript_chunks int`、`partial_transcript_total int`。
- **`LessonJobStatus` 增量字段**：`action LessonJobAction`（`"transcribe" | "summarize" | "transcribe_and_summarize"`）。
- **`CourseBatchStatus` 增量字段**：`kind BatchKind`（`"transcribe" | "summarize"`）、`chapter_id`、`chapter_title`、`skipped_already_done`、`skipped_no_transcript`（`skipped = skipped_already_done + skipped_no_transcript`）。
- **`CourseSummariesStatus`（纯 SQL 计算）**：
  - 保留 `summary_lesson_ids` 与 `batch_job`（同步指向 `summary_batch_job`）。
  - 新增 `transcript_lesson_ids`（全部 `transcript_ready == true` 的课时 ID，与 `counts.transcript_ready` 一致）。
  - 新增 `lessons []LessonPipelineStatus`（含 `lesson_id`, `chapter_id`, `has_subtitle`, `has_cached_transcript`, `transcript_status` [`external_subtitle|embedded_subtitle|asr|manual|partial|none`], `transcript_ready`, `transcribed_chunks`, `total_chunks`, `ai_subtitle_updated_at`, `has_summary`）。
  - 新增 `counts CoursePipelineCounts`（`total_lessons`, `subtitle_lessons`, `transcribed_lessons`, `transcript_ready`, `partial_lessons`, `missing_transcript`, `summarized_lessons`, `ready_to_summarize`）。
  - 新增 `transcribe_batch_job` 与 `summary_batch_job`。

### 5.3 双独立后台队列与并发协调

- **双独立 Worker**：`transcribeWorker`（`singleTranscribeQueue` > `batchTranscribeQueue`）与 `summaryWorker`（`singleSummaryQueue` > `batchSummaryQueue`）。
- **跨 Worker 碰撞保护**：通过 `sync.Cond` 在进入活动槽位前检查对方 Worker 是否正在处理同一 `lesson_id` 或同一 `media_file_id`；若冲突则等待对方释放后再重检 DB 状态，避免并发冲突或死锁；所有取消与停机事件广播唤醒等待者。
- **单课交接与冲突**：
  - `StartLessonTranscribe` 仅校验 `ASRConfigured`，不依赖 `AIConfigured`；`StartLessonSummary` 已有文稿直接入 `singleSummaryQueue`，需 ASR 时入 `singleTranscribeQueue` 并在完成后保持 `Running=true` 无缝交接给 `singleSummaryQueue`。
  - 同方向已有任务幂等返回快照；反方向任务冲突返回 `ErrLessonJobConflict`（HTTP 409，`code: "lesson_job_conflict"`）。
- **批量解耦与按章执行**：
  - `StartCourseTranscribeBatch` 与 `StartCourseSummaryBatch` 支持可选 `chapter_id` 与 `mode`（`"missing_only" | "all"`），各自独立防重与取消，同一课程允许同时运行转写批次与总结批次。
  - `StartCourseSummaryBatch` 仅消费 `transcript_ready == true` 的课时，绝不触发 ASR。

### 5.4 REST API 扩展

- 新增：`POST /api/v1/lessons/{id}/transcript/generate`、`POST /api/v1/lessons/{id}/transcript/cancel`、`POST /api/v1/courses/{id}/transcripts/generate`、`POST /api/v1/courses/{id}/transcripts/cancel`。
- 扩展：`POST /api/v1/courses/{id}/summaries/generate` 支持可选 `chapter_id`；`GET /api/v1/courses/{id}/summaries/status` 返回分离状态与双批次状态。

### 5.5 前端交互

- **课程目录顶部**：显示 `已学 X/N · 文稿 Y/N · 总结 Z/N`；并排「批量转写 / 继续转写 / 文稿已齐」与「批量总结 / 继续总结 / 全部已总结」双按钮；独立展示双进度横幅与取消、错误详情、跳过明细 Toast。
- **章节行**：标题行右侧提供紧凑的章节级转写与总结按钮（独立 `<button>`，不嵌套）。
- **课时行**：独立展示文稿图标（字幕 / AI 转写 / 断点进度 `x/y`）与总结图标（`Sparkles`）。
- **AI 转写字幕实时刷新**：基于 `ai_subtitle_updated_at` 变化及单课转写完成事件原地刷新 `VideoPlayer` 字幕。
- **单课面板**：
  - 未配主 AI 但已配 ASR 时仍支持「仅语音转文字 / 重新转写」。
  - 无文稿且已配 AI + ASR 时显示断点提示与「转写并生成总结」/「仅语音转文字（继续转写）」双按钮。
  - 无总结但已有缓存转写时提供「生成 AI 总结」与纯转写「重新转写」（不耗 LLM）；已有总结的工具栏保留「重新转写」（重转并更新总结）。
