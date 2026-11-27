# Batch Transcription Progress Feedback & Configurable ASR Chunk/Timeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate silent batch-transcription cancellation confusion and local/CPU Whisper timeouts by separating live success/skip/fail progress, exposing real-time stage messages and explicit cancel preservation notices, lowering the default audio segment to 5 minutes (`300s`), raising the default per-chunk HTTP timeout to 15 minutes (`900s`), and making both settings configurable at runtime without restarting CourseFin.

**Architecture:** Expose `CurrentMessage` on `summary.CourseBatchStatus` across worker callbacks and cancellation, add dynamic `ai_asr_segment_seconds` (`60–1200`, default `300`) and `ai_asr_timeout_seconds` (`30–3600`, default `900`) resolution into `ResolveEffectiveSettings`, FFmpeg extraction, and SQLite chunk checkpoint evaluation, expose validated settings fields on `/api/v1/settings` and `SettingsPage`, and upgrade `BatchSummaryBanner` + `CourseDetailPage` to display live `成功 / 跳过 / 失败` counts, recent failure summaries, `current_message`, and cancel preservation notices.

**Tech Stack:** Go (`CGO_ENABLED=0`, standard library, `modernc.org/sqlite`, `chi/v5`), React 19, TypeScript, Tailwind CSS semantic tokens, Vitest + React Testing Library.

**Spec:** `docs/superpowers/specs/2026-11-27-asr-timeout-chunk-and-batch-feedback-design.md`

---

## Global Constraints

- Do not add new Go or npm dependencies or SQLite migrations.
- Preserve all persisted `media_transcripts` and `media_transcript_chunks` rows across batch/single cancellations.
- Apply updated `ai_asr_segment_seconds` and `ai_asr_timeout_seconds` dynamically on subsequent runs without requiring a process restart.
- Keep `SettingsPage` preset button labels non-colliding across the segment and timeout rows, and use only semantic Tailwind tokens from `web/src/index.css` (zero `LEGACY_CLASS_REGEX` matches).
- Keep injected `http.Client` test overrides intact inside `NewOpenAICompatibleTranscriber`.

## Review Focus

1. **Failure masking vs live breakdown:** Running `BatchSummaryBanner` must visibly split `成功 / 跳过 / 失败` and show the most recent lesson failure immediately instead of hiding errors inside `doneCount/total`.
2. **Cancel feedback & message propagation:** `cancelCourseBatch` must set `CurrentMessage = "正在取消..."` when interrupting an active lesson, `clearBatchActiveFieldsLocked` must clear it between lessons/completion, and `CourseDetailPage` must surface `formatBatchCancelNotice` when cancelled with `failed === 0`.
3. **Checkpoint compatibility on dynamic segment change:** Changing `ai_asr_segment_seconds` at runtime must extract chunks at the new segment length and evaluate partial checkpoints against that matching `segment_sec` (`loadPartialCheckpointProgress` and `computeCourseLessonsPipelineStatus`), cleanly invalidating mismatched partial chunks without touching completed transcripts in `media_transcripts`.
4. **Input validation & safe clamping:** API handlers reject out-of-range values with `HTTP 400`, `ResolveEffectiveSettings` ignores corrupted/out-of-range DB values in favor of defaults (`300s` / `900s`), and `NormalizeASRTimeoutSeconds` clamps direct programmatic callers to `[30, 3600]`.

---

## Tasks

- [x] **Task 1:** Expose `CurrentMessage` on `summary.CourseBatchStatus` (`internal/summary/service.go`), propagate stage/reuse messages in `executeTranscribeBatchItem` & `executeSummaryBatchItem`, set `"正在取消..."` in `cancelCourseBatch`, clear in `clearBatchActiveFieldsLocked`, and add `TestBatchTranscribeCancelPreservesCompletedLessonsAndExposesCurrentMessage` in `internal/summary/service_test.go`.
- [x] **Task 2:** Update `BatchSummaryBanner` (`web/src/components/LessonSummaryPanel.tsx`, `web/src/types/coursefin.ts`) to render `current_message`, live `成功 / 跳过 / 失败` breakdown + latest error, export `formatBatchCancelNotice`, wire cancel preservation toasts in `CourseDetailPage.tsx`, and add Vitest coverage in `web/src/__tests__/LessonSummary.test.tsx`.
- [x] **Task 3:** Update `DefaultASRTimeoutSeconds = 900` (`30–3600`, `NormalizeASRTimeoutSeconds`, `ASRConfig.TimeoutSeconds`) in `internal/ai/asr.go` and `DefaultAudioSegmentSeconds = 300` (`60–1200`) in `internal/summary/audio.go`; wire dynamic config loading through `ResolveEffectiveSettings`, `runASRPipeline`, `checkpoints.go`, and `computeLessonCapabilities`.
- [x] **Task 4:** Expose and validate `ai_asr_segment_seconds` & `ai_asr_timeout_seconds` on `GET / PUT /api/v1/settings` in `internal/api/handlers_ai.go` and add controls with quick presets in `web/src/pages/SettingsPage.tsx` (`web/src/types/coursefin.ts`).
- [x] **Task 5:** Expand tests in `internal/ai/asr_summary_test.go` (`TestNormalizeASRTimeoutSeconds`), `internal/api/api_test.go` (`TestSettingsASRFieldsAndMasking`), `internal/summary/service_test.go` (`TestDynamicASRSegmentSettingAppliesWithoutRestart`), and `web/src/__tests__/SummaryMarkdownAndSettings.test.tsx`.
- [x] **Task 6:** Sync embedded SPA via `make sync-web` and run full verification (`make test`: `go test -race -count=1 ./...` and `pnpm --dir web test`), then persist approved spec and plan docs under `docs/superpowers/`.
