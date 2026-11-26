# Course Multi-Covers Implementation Plan

> **For agentic workers:** Execute step-by-step with verification after each subsystem and full end-to-end verification at completion.

**Goal:** Upgrade CourseFin's course cover system from single-file overwrite to per-course multiple associated covers (`course_covers`) so every generated, uploaded, frame-picked, or imported cover is preserved while the newest becomes active by default and historical covers remain accessible, activatable, and deletable.

**Architecture:** Add migration `004_course_covers.sql` with automatic backfill of existing `courses.cover_path` records, implement `internal/catalog/covers.go` as the single lifecycle manager keeping `courses.cover_path` and `updated_at` in sync with active cover changes, delegate cover image serving to `internal/media/image.go`, refactor `internal/cover/service.go` and `internal/api` to append covers directly without temporary preview state, and surface a shared `CourseCoverGallery` component in `CourseEditorPage` and `AICoverModal`.

**Tech Stack:** Go (`database/sql`, `modernc.org/sqlite`, `chi`), React + TypeScript + Tailwind CSS v4 semantic tokens, Vitest + Testing Library.

**Spec:** [2026-11-26-course-multi-covers-design.md](file:///Users/finn/Documents/Code/CourseFin/docs/superpowers/specs/2026-11-26-course-multi-covers-design.md)

## Execution Tasks

- [x] **Task 1:** Persist spec and plan docs (`docs/superpowers/specs/2026-11-26-course-multi-covers-design.md`, `docs/superpowers/plans/2026-11-26-course-multi-covers.md`).
- [x] **Task 2:** Add `internal/db/migrations/004_course_covers.sql`, clean unused preview dir in `internal/db/db.go`, and add migration + backfill + cascade tests in `internal/db/db_test.go`.
- [x] **Task 3:** Create `internal/catalog/covers.go` and integrate `CourseDetail.Covers`, deletion cleanup, and `CommitImport` cover registration in `internal/catalog/service.go` and `internal/catalog/importer.go` with tests in `internal/catalog/catalog_test.go`.
- [x] **Task 4:** Add `ServeImageFile` and `ServeCoverFile` in `internal/media/image.go` with tests in `internal/media/media_test.go`.
- [x] **Task 5:** Refactor `internal/cover/service.go` to append & activate directly for single-course and batch generation; update `internal/cover/service_test.go`.
- [x] **Task 6:** Add `/courses/{id}/covers*` endpoints, update `/cover/generate` and `/cover` handlers, remove obsolete preview endpoints in `internal/api`, and update `internal/api/cover_api_test.go`.
- [x] **Task 7:** Update `web/src/types/coursefin.ts`, `web/src/api/client.ts`, and create `web/src/components/CourseCoverGallery.tsx`.
- [x] **Task 8:** Update `web/src/components/AICoverModal.tsx`, `web/src/pages/CourseEditorPage.tsx`, and `web/src/components/BatchCoverModal.tsx`.
- [x] **Task 9:** Update `web/src/__tests__/CoverGeneration.test.tsx`, run `pnpm --dir web test`, `make sync-web`, `go test -race -count=1 ./...`, and `go build ./...`.
