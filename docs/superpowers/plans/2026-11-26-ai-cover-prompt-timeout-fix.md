# AI 课程封面生图误判与超时重试修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 彻底解决 AI 课程封面在 `gpt-image-2.6-sunburst` 等多模态模型下触发“缺少参考图/原图”HTTP 400 拒答、非尺寸 400 盲目重试、测试与实测默认尺寸不一致及 `120s` 超时缺少诊断的问题。

**Architecture:** 在 `internal/cover/styles.go` 将 6 套风格重写为紧凑的单段纯自然画面描述（`BuildPrompt`）并提供剥离主题词的纯几何兜底提示词（`BuildMinimalPrompt`），不再注入原始课程简介和章节列表；在 `internal/cover/service.go` 为单课与批量抽取共享 `generateCourseImage` 实现“参考图拒答一次极简兜底重试”；在 `internal/ai/image.go` 保留 `240s` 超时、`isLikelySizeError` 精准尺寸回退与统一 `TestConnection` 自动尺寸策略。

**Tech Stack:** Go 1.26 (`net/http`, `regexp`, `testing`), CourseFin `internal/ai` & `internal/cover`.

**Spec:** `docs/superpowers/specs/2026-11-26-ai-cover-prompt-timeout-fix-design.md`

## Global Constraints

- 保持 `StyleTemplate` 与 `PromptInput` 导出字段及 JSON Schema 完全不变，不修改前端与数据库结构。
- Prompt 必须为紧凑单段纯画面描述（无换行、无 Markdown 列表、无规范清单头，且不包含 `preserve`/`omit`/`reference`/`original`/`edit`/`from scratch`/`inspired by` 等触发词）。
- 参考图拒答兜底仅针对明确匹配参考图错误的特征触发最多 1 次，避免放大正常报错成本。

---

## Tasks

- [x] **Task 1 (v1): 优化 `internal/ai/image.go` 与 `internal/cover/service.go`（`240s` 超时、`isLikelySizeError` 精准尺寸回退、统一 `TestConnection` 尺寸链与诊断上下文）**
- [x] **Task 2 (v2): 重构 `internal/cover/styles.go` 为单段纯画面自然描述并新增 `BuildMinimalPrompt`，去除原始章节/简介注入**
- [x] **Task 3 (v2): 在 `internal/cover/service.go` 抽取单课与批量共享 `generateCourseImage`，对参考图拒答自动用 `BuildMinimalPrompt` 重试 1 次；同步简化 `TestConnection` 提示词**
- [x] **Task 4: 更新 `internal/cover/service_test.go` 与 `internal/ai/image_test.go`（单段无触发词检查、参考图拒答单课/批量兜底测试、非尺寸 400 不重试、自动尺寸测试及超时诊断测试）**
- [x] **Task 5: 运行 `go test -race -v ./internal/cover/... ./internal/ai/... && go test ./...` 全量验证并更新 superpowers 记录**
