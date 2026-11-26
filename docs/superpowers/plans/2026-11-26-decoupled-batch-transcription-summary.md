# Decoupled Batch Transcription & AI Summary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decouple course batch speech-to-text transcription and AI summarization into two independently tracked, resumable, concurrently runnable steps with chunk-level ASR checkpoints, chapter-level triggers, and single-lesson transcribe-only controls.

**Architecture:** Split `internal/summary` into two serial workers (`transcribeWorker` and `summaryWorker`) coordinated via a `sync.Cond` per-lesson/per-media collision gate, backed by a new `media_transcript_chunks` SQLite checkpoint table (`005_transcript_checkpoints.sql`). Expose dedicated lesson/course transcription REST endpoints alongside extended pure-SQL pipeline status/counts (`lessons`, `counts`, `transcript_lesson_ids`, `transcribe_batch_job`, `summary_batch_job`), and update `CourseDetailPage` + `LessonSummaryPanel` to control and visualize both steps independently.

**Tech Stack:** Go (`CGO_ENABLED=0`, standard library, `modernc.org/sqlite`, `chi/v5`), React 18, TypeScript, Tailwind CSS semantic tokens, Vitest + React Testing Library.

**Spec:** `docs/superpowers/specs/2026-11-26-decoupled-batch-transcription-summary-design.md`

---

## Global Constraints

- Do not add new Go or npm dependencies.
- Never write any file into user media storage directories; keep temporary audio chunks in `{dataDir}/cache/asr/{jobID}` and always clean up.
- Frontend styling must strictly use semantic tokens in `web/src/index.css` (no hardcoded `slate/indigo/rose/emerald/sky` classes matching `LEGACY_CLASS_REGEX`).
- Maintain backward compatibility on existing API endpoints and response fields (`summary_lesson_ids`, `batch_job`).
- Keep `CourseDetailPage` right sidebar controls compact within `320px`–`360px`.

## Review Focus

1. **Cross-worker collision on same lesson or shared `media_file_id`:** `transcribeWorker` and `summaryWorker` must never execute concurrently on the same `lesson_id` or `media_file_id`; waiting on `sync.Cond` must wake immediately on completion, cancellation, or shutdown without deadlock.
2. **Un-rescanned video replacement invalidates checkpoints:** `os.Stat`-first fingerprinting (`size:mtime_rfc3339nano`) must immediately invalidate stale chunks when a disk video is replaced before storage rescan.
3. **Silent chunk checkpoint vs empty full transcript:** A silent chunk (`0` cues) must still persist an empty checkpoint row so retry skips it, while an all-empty completed run must purge checkpoints and fail with `empty_transcript`.
4. **Opposite-action conflict vs two-phase cancel:** Opposite-action single requests return `409 lesson_job_conflict`; cancelling `ActionTranscribeAndSummarize` during ASR preserves chunk checkpoints without entering summary, and cancelling during LLM preserves completed `media_transcripts` and prior `lesson_summaries`.
5. **Pure-ASR mode without AI configured:** Single-lesson "仅语音转文字", no-summary "重新转写", and whole-course / chapter-scoped "批量转写" must complete end-to-end and refresh player AI subtitles even when LLM (`ai_base_url`/`ai_model`) is unconfigured.

---

## Tasks

- [x] **Task 0:** Persist approved design spec and implementation plan docs.
- [ ] **Task 1:** Add `internal/db/migrations/005_transcript_checkpoints.sql` (`media_transcript_chunks`) and migration cascade test in `internal/db/db_test.go`.
- [ ] **Task 2:** Implement `internal/summary/checkpoints.go`, chunk-level checkpoint resume + atomic finalization in `runASRPipeline`, `DeleteLessonSummary` cleanup, and pure-SQL `GetCourseSummaryStatus` (`lessons`, `counts`, `transcript_lesson_ids`, `ai_subtitle_updated_at`) + `LessonCapabilities` partial fields; verify with `go test ./internal/summary`.
- [ ] **Task 3:** Split `internal/summary/service.go` into `transcribeWorker` and `summaryWorker` with `sync.Cond` collision gate, `StartLessonTranscribe`, `CancelLessonTranscribe`, single-lesson ASR→LLM handoff, `ErrLessonJobConflict`, and decoupled `StartCourseTranscribeBatch` / `StartCourseSummaryBatch` (`chapter_id`, `skipped_no_transcript`, `skipped_already_done`); verify with `go test -race ./internal/summary`.
- [ ] **Task 4:** Register `/lessons/{id}/transcript/generate|cancel` and `/courses/{id}/transcripts/generate|cancel`, extend `/courses/{id}/summaries/generate` for `chapter_id`, map `409 lesson_job_conflict`, and add API tests in `internal/api/api_test.go`; verify with `go test -race ./internal/api`.
- [ ] **Task 5:** Update `web/src/types/coursefin.ts`, `web/src/api/client.ts`, and `web/src/components/LessonSummaryPanel.tsx` (dual batch banners with skip breakdown, partial checkpoint notice, single-lesson transcribe-only + pure-ASR support); verify with `pnpm exec tsc --noEmit`.
- [ ] **Task 6:** Upgrade `web/src/pages/CourseDetailPage.tsx` (separate stats row, dual batch buttons, chapter-level triggers without nested buttons, per-lesson transcript/summary icons, `ai_subtitle_updated_at` in-place subtitle refresh) and expand `web/src/__tests__/LessonSummary.test.tsx`; verify with `pnpm test`.
- [ ] **Task 7:** Run final full-stack verification (`go test -race ./...` and `cd web && pnpm test && pnpm build`) and confirm clean commit history.
