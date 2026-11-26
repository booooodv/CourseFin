# AI 课程封面生成功能实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 CourseFin 增加「AI 端到端直出课程封面」功能，内置 6 套统一风格模板并支持悬停/触摸样图预览，覆盖系统设置、课程编辑页单课预览应用与首页批量生成。

**Architecture:** 后端在 `internal/ai/image.go` 实现 OpenAI Images / OpenAI Chat / Gemini 三协议生图客户端，在 `internal/cover/` 封装风格模板、配置解析、预览缓存与单并发批量任务服务，并通过 `internal/api/handlers_cover.go` 暴露 REST 端点；前端通过共享 `CoverStylePicker`（含 Portal 样图预览）在 `SettingsPage`、`CourseEditorPage` 与 `HomePage` 完成闭环交互。

**Tech Stack:** Go 1.26 (`CGO_ENABLED=0`), `modernc.org/sqlite`, `chi/v5`, `golang.org/x/image/webp`, React 19, TypeScript, Tailwind CSS v4, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-26-ai-course-cover-design.md`

## Global Constraints

- 后端保持 `CGO_ENABLED=0` 纯 Go 编译，不引入任何新的外部 Go 依赖。
- 前端不引入新的 npm 依赖；全部 UI 严格使用 `web/src/index.css` 语义化颜色 Token，禁止硬编码 Tailwind 色板类（`slate`/`indigo`/`rose`/`emerald`/`sky`）。
- 绝不向用户媒体目录写入文件：预览存 `{dataDir}/cache/cover_previews/`，正式封面存 `{dataDir}/covers/`。
- 密钥防泄露：仅当协议为 OpenAI 兼容且 `ai_image_base_url` 留空时才允许复用主 `ai_api_key`；独立 `ai_image_base_url` 或 `gemini` 原生协议绝不转发主 `ai_api_key`。
- 向后兼容：新增页面挂载请求必须静默容错，确保已有前端测试零回归。

---

## Tasks

- [x] **Task 1: 压缩并内置 6 套风格样图至 `web/public/cover-styles/` 并建立设计与计划文档**
- [x] **Task 2: 在 `internal/db/db.go` 增加 `cache/cover_previews` 目录初始化**
- [x] **Task 3: 实现 `internal/ai/image.go` 多协议生图客户端与 `internal/ai/image_test.go` 单测**
- [x] **Task 4: 实现 `internal/cover/`（`styles.go`, `config.go`, `service.go`, `service_test.go`）**
- [x] **Task 5: 扩展 `internal/api/handlers_ai.go`、新建 `handlers_cover.go` 与 `cover_api_test.go`、更新 `router.go` 与 `internal/server/server.go`**
- [x] **Task 6: 更新 `web/src/types/coursefin.ts`、`web/src/api/client.ts` 并实现 `CoverStylePicker.tsx`**
- [x] **Task 7: 集成 `SettingsPage.tsx`、`AICoverModal.tsx` + `CourseEditorPage.tsx`、`BatchCoverModal.tsx` + `HomePage.tsx`**
- [x] **Task 8: 新增 `web/src/__tests__/CoverGeneration.test.tsx` 并运行 `go test -race`、`pnpm test`、`make build` 全量验证**
