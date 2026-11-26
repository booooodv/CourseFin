# AI 课程封面生图误判与超时重试修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 AI 课程封面生成时 Prompt 触发多模态生图模型“缺少参考图/原图”HTTP 400 拒答、非尺寸 400 盲目重试、测试与实测默认尺寸不一致以及 `120s` 超时偏短且缺少诊断上下文的问题。

**Architecture:** 重构 `internal/cover/styles.go` 的模板结构与 `BuildPrompt` 动态拼装逻辑以彻底消除编辑/参考图触发词；在 `internal/ai/image.go` 增加 `isLikelySizeError` 精准尺寸回退判定、统一 `TestConnection` 尺寸策略、补充耗时/模型/尺寸诊断；并将 `defaultImageHTTPTimeout` 与 `internal/cover/service.go` 的 `defaultCoverHTTP` 统一提高至 `240s`。

**Tech Stack:** Go 1.26 (`net/http`, `regexp`, `testing`), CourseFin `internal/ai` & `internal/cover`.

**Spec:** `docs/superpowers/specs/2026-11-26-ai-cover-prompt-timeout-fix-design.md`

## Global Constraints

- 保持 `StyleTemplate` 所有导出字段与 JSON Schema 完全兼容，不引入任何外部依赖。
- Prompt 中严禁出现任何编辑/参考图触发词及其否定写法（如 `preserve`、`omit`、`reference`、`original`、`edit`、`modify`、`replace`、`remove`、`on the image`）。
- 仅在 `internal/cover`、`internal/ai` 及对应文档范围改动，不触碰无关工作区修改（如 `course-multi-covers` 迁移与数据库测试）。

---

## Tasks

- [x] **Task 1: 重构 `internal/cover/styles.go` 风格字段拆解与 `BuildPrompt` 纯文生图拼装逻辑**
- [x] **Task 2: 更新 `internal/ai/image.go` 与 `internal/cover/service.go`（`240s` 超时、统一 `TestConnection` 尺寸序列、`isLikelySizeError` 与诊断错误信息）**
- [x] **Task 3: 在 `internal/cover/service_test.go` 与 `internal/ai/image_test.go` 补充全风格 Prompt 触发词检查、非尺寸 400 不重试、自动尺寸测试及超时诊断单测**
- [x] **Task 4: 运行 `go test -race -v ./internal/ai/... ./internal/cover/... && go test ./internal/api/... && go test ./...` 全量验证通过**
- [x] **Task 5: 补齐 `docs/superpowers/specs/` 与 `docs/superpowers/plans/` 记录文档**
