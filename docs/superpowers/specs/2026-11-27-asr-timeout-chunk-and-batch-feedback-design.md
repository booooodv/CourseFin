# CourseFin 批量转写进度反馈优化与 ASR 切片/超时动态配置设计文档

- **Date:** 2026-11-27
- **Status:** Implemented
- **Repository:** `CourseFin`

---

## 1. Intent & Why (Root Cause Investigation)

`v1.7.0` 上线两步解耦批量转写与 SQLite 分片断点续转后，在实际使用自建/局域网 ASR 服务（`http://<asr-host>:8790/v1/audio/transcriptions`）执行整门课批量语音转文字时，出现了严重的体验错觉与连续超时故障：界面进度显示推进到 `10/22` 后点击取消，再次查看却发现**没有任何一节转写文稿被保存下来**，仿佛点击取消将进度全部清空、只能从头再来。

排查后端落库事务、超时参数与前端横幅状态渲染后，定位到 **3 层叠加根因**：

1. **大切片 + 短超时导致自建 ASR 队列雪崩**：
   - `DefaultAudioSegmentSeconds` 原固定为 `1200`（20 分钟/片），而 `defaultASRHTTPTimeout` 原固定为 `300s`（5 分钟/片）。
   - 对于算力有限或 CPU 运行的本地 Whisper 服务，只要推理速度慢于 `4x` 实时，单个 20 分钟分片就必然在 300 秒处触发 `context deadline exceeded (Client.Timeout exceeded while awaiting headers)`。
   - 客户端在 300 秒强行断开连接后，ASR 服务端往往仍在后台继续完成被中断的推理，而 CourseFin 串行批量队列随即发起下一节课的请求，导致后续所有课时均在等待响应头阶段连环排队超时，连续 10 节无一成功落库。
2. **运行中批量进度条隐藏失败计数与错误信息**：
   - `BatchSummaryBanner` 原仅展示 `doneCount / total`（`doneCount = completed + skipped + failed`），将失败课会计入总进度，导致前 10 节全部失败时页面仍显示 `批量转写中 10/22`，用户在取消前无法察觉任务其实在连续失败。
3. **无失败取消静默隐藏 + 缺失阶段细粒度文案透传**：
   - 当 `failed === 0` 且手动取消时，`CourseDetailPage` 既不保留结果横幅也不弹保留提示 Toast，无法向用户反馈“已完成 N 节与分片断点均已落库”；
   - 同时 `CourseBatchStatus` 未携带 `CurrentMessage`，导致分片断点复用文案（`复用已转写分片（1/N）...`）与取消中状态（`正在取消...`）在批量横幅中丢失，续跑长视频断点时从 `分片 1/N` 文案退化为通用转写文案，引发“是否从头重转”的误解。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals

1. **缩短默认切片与放宽默认超时，并开放设置页动态热生效**：
   - 将默认音频切片时长由 `1200s` 调整为 `DefaultAudioSegmentSeconds = 300`（5 分钟，允许范围 `60~1200` 秒），将默认单次 ASR HTTP 超时由 `300s` 放宽至 `DefaultASRTimeoutSeconds = 900`（15 分钟，允许范围 `30~3600` 秒）。
   - `ResolveEffectiveSettings` 从 `app_settings` 读取并校验 `ai_asr_segment_seconds` 与 `ai_asr_timeout_seconds`，每次执行 `runASRPipeline`、`computeLessonCapabilities` 与 `computeCourseLessonsPipelineStatus` 时动态按当前切片参数切片与匹配断点，**保存设置后立即对后续任务生效，无需重启服务**。
   - `GET / PUT /api/v1/settings` 暴露并校验上述两个参数；`SettingsPage` 新增对应输入框与无文案冲突的快捷档位按钮（切片：`2 分钟 / 3 分钟 / 5 分钟 / 20 分钟`；超时：`10 分钟 / 15 分钟 / 30 分钟 / 60 分钟`）。
2. **后端批量状态实时透传 `current_message`**：
   - 为 `CourseBatchStatus` 新增 `current_message`（`omitempty`）字段，在 `executeTranscribeBatchItem`、`executeSummaryBatchItem` 及其进度回调、以及 `cancelCourseBatch` 中同步最新进度/取消文案（含 `复用已转写分片（1/N）...`、`正在取消...`），并在 `clearBatchActiveFieldsLocked` 时统一清空。
3. **前端运行中拆显成功/失败与取消保留显式提示**：
   - 运行中的 `BatchSummaryBanner` 消费 `batchJob.current_message`，并在进度条下方明确拆显 `成功 X` / `跳过 Y` / `失败 Z` 与最近一条失败课时的错误摘要。
   - 新增 `formatBatchCancelNotice`，当手动取消批量转写或批量总结且 `failed === 0` 时弹出明确的 Toast 保留说明（告知已保存节数并提示点击「继续转写」/「继续总结」续跑，或提示分片断点已自动保留）；存在失败时仍弹出汇总错误 Toast 并保留红色失败明细横幅。

### Non-Goals

- **不新增数据库迁移**：复用迁移 `005_transcript_checkpoints.sql` 中 `media_transcript_chunks.segment_sec` 字段，切分时长修改后未完成的旧分片断点会自动失效重抽，而已完成并合入 `media_transcripts` 的转写文稿与 `lesson_summaries` 不受任何影响。
- **不引入新的第三方 Go 或 npm 依赖**。
- **不改动课程目录与单课总结面板既有核心交互结构**。

### Anti-Goals

- **绝不在取消批量任务时回滚或删除已提交的 `media_transcripts` 或 `media_transcript_chunks` 记录**。
- **绝不在修改 ASR 切片时长或超时设置后要求重启 CourseFin 进程**。
- **绝不在前端组件中引入违反 `LEGACY_CLASS_REGEX` 的硬编码旧色板类名**（仅使用 `web/src/index.css` 语义化颜色 Token）。

---

## 3. Detailed Changes

### Backend

- **`internal/ai/asr.go`**：
  - 定义 `DefaultASRTimeoutSeconds = 900`、`MinASRTimeoutSeconds = 30`、`MaxASRTimeoutSeconds = 3600` 及 `NormalizeASRTimeoutSeconds(sec int) int`。
  - 在 `ASRConfig` 增加 `TimeoutSeconds int`，更新 `NewOpenAICompatibleTranscriber`（仅在未注入自定义 `httpClient` 时按 `NormalizeASRTimeoutSeconds(cfg.TimeoutSeconds)` 构造 `&http.Client{Timeout: ...}`）。
- **`internal/summary/audio.go`**：
  - 调整 `DefaultAudioSegmentSeconds = 300`，新增 `MinAudioSegmentSeconds = 60` 与 `MaxAudioSegmentSeconds = 1200`。
- **`internal/summary/service.go`**：
  - `AIConfigSnapshot` 增加 `ASRSegmentSeconds int`，`ResolveEffectiveSettings` 解析 `ai_asr_segment_seconds`（60–1200，非法/超限回落默认 `300`）与 `ai_asr_timeout_seconds`（30–3600，非法/超限回落默认 `900`）。
  - `Service` 区分测试显式指定的 `hasCustomSegmentSec` 与默认运行路径，新增 `effectiveAudioSegmentSec` / `resolveExpectedSegmentSec`；`runASRPipeline` 在非自定义 `AudioExtractor` 时按当次 `cfg` 的 `segSec` 构造 `NewFFmpegAudioExtractor` 并校验/写入对应 `segment_sec` 断点；`computeLessonCapabilities` 同步传入当前生效 `segSec`。
  - `CourseBatchStatus` 新增 `CurrentMessage string`（`json:"current_message,omitempty"`），在 `cancelCourseBatch`（设为 `"正在取消..."`）、`clearBatchActiveFieldsLocked`（清空）、`executeTranscribeBatchItem` 与 `executeSummaryBatchItem`（初始阶段文本及 `report` 回调同步）全链路更新。
- **`internal/summary/checkpoints.go`**：
  - 更新 `loadPartialCheckpointProgress(ctx, mediaFileID, expectedSegSec)` 与 `computeCourseLessonsPipelineStatus` 使用动态解析的 `expectedSegSec` 校验 `media_transcript_chunks`。
- **`internal/api/handlers_ai.go`**：
  - `settingsPayload`、`buildSettingsResponse` 与 `handleUpdateSettings` 支持 `ai_asr_segment_seconds`（校验 `[60, 1200]`）与 `ai_asr_timeout_seconds`（校验 `[30, 3600]`），超限返回 `HTTP 400`。

### Frontend & Embedded Assets

- **`web/src/types/coursefin.ts`**：
  - 为 `CourseBatchSummaryJob` 增加 `current_message?: string`，为 `SettingsPayload` 和 `UpdateSettingsInput` 增加 `ai_asr_segment_seconds?: number` 与 `ai_asr_timeout_seconds?: number`。
- **`web/src/components/LessonSummaryPanel.tsx`**：
  - 导出 `formatBatchCancelNotice(batchJob)`。
  - 更新 `BatchSummaryBanner` 运行态：向 `formatSummaryJobStageText` 传入 `batchJob.current_message`，在进度条下方使用语义化 Token 渲染实时 `成功 / 跳过 / 失败` 计数行及 `最近失败` 摘要。
- **`web/src/pages/CourseDetailPage.tsx`**：
  - 在 `batchJob` 与 `transcribeBatchJob` 的运行状态结束监听中区分正常完成、含失败停止与无失败手动取消：含失败走 `toast.error(formatBatchResultSummary(...))`，无失败取消走 `toast.info(formatBatchCancelNotice(...))`，正常完成走 `toast.success(formatBatchResultSummary(...))`。
- **`web/src/pages/SettingsPage.tsx`**：
  - 新增 `aiAsrSegmentSeconds`（默认 `'300'`）与 `aiAsrTimeoutSeconds`（默认 `'900'`）表单状态、提交钳位及互不重名的快捷档位按钮。
- **`internal/server/webdist/`**：
  - 通过 `make sync-web` 同步构建产物。

### Tests

- **`internal/ai/asr_summary_test.go`**：新增 `TestNormalizeASRTimeoutSeconds`。
- **`internal/api/api_test.go`**：扩展 `TestSettingsASRFieldsAndMasking`，验证默认值（`300` / `900`）、合法更新（`180` / `1800`）与超限 400 校验（`10` / `5000`）。
- **`internal/summary/service_test.go`**：
  - 扩展 `TestResolveEffectiveSettingsKeyIsolation` 校验切片与超时解析及越界兜底；
  - 将 `TestCourseSummaryStatusPipelineCountsAndStatuses` 预置分片从硬编码 `1200` 改为 `summary.DefaultAudioSegmentSeconds`；
  - 新增 `TestBatchTranscribeCancelPreservesCompletedLessonsAndExposesCurrentMessage`（验证取消后已完成节入库、分片断点保留、续跑不重复请求 ASR 以及 `CurrentMessage` 实时同步与取消态 `"正在取消..."`）；
  - 新增 `TestDynamicASRSegmentSettingAppliesWithoutRestart`（验证修改 `ai_asr_segment_seconds = "180"` 后免重启直接按 180s 落库与识别断点）。
- **`web/src/__tests__/LessonSummary.test.tsx`**：新增测试验证运行中 `成功 / 跳过 / 失败`、最近失败摘要、`current_message` 渲染、含失败取消提示与无失败取消保留 Toast。
- **`web/src/__tests__/SummaryMarkdownAndSettings.test.tsx`**：扩展 ASR 设置测试覆盖 `300` / `900` 默认值展示、点击 `3 分钟` / `30 分钟` 快捷档位与保存提交载荷。

---

## 4. Verification

```bash
make sync-web && make test
```

- `make sync-web`：`tsc --noEmit && vite build` 通过，已同步至 `internal/server/webdist/`。
- `make test`：
  - `go test -race -count=1 ./...`：全部 15 个含测试的 Go 包（`internal/ai`、`internal/api`、`internal/auth`、`internal/catalog`、`internal/config`、`internal/cover`、`internal/db`、`internal/id`、`internal/jellyfin`、`internal/media`、`internal/scanner`、`internal/server`、`internal/storage`、`internal/summary`、`internal/updater`）全部通过（0 竞态、0 失败）。
  - `pnpm --dir web test`：全部 12 个测试文件、139 项 Vitest 测试全部通过。
