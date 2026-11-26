# CourseFin AI 课程封面生图误判与超时重试修复设计文档

- **Date:** 2026-11-26
- **Status:** Implemented (v2 Refined)
- **Repository:** `CourseFin`

---

## 1. Intent & Why (Root Cause Investigation)

在通过 `https://magpie.finnapps.win/v1/images/generations`（模型 `gpt-image-2.6-sunburst`，上游 `P-aixoras` / `aixoras.com`）运行 AI 封面生成时，系统设置中的「测试生图接口」约 12 秒即可返回 `200`，但实际生成课程封面时先后暴露出两层问题：

1. **v1.5.0 阶段**：
   - 默认留空（自动适配）尺寸下的 `120s` 超时：`ai: image request failed: Post ".../v1/images/generations": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`。
   - 显式 `1024x1024` 跑批量生成时上游返回参考图编辑拒答（HTTP 400）：
     - 中文课程 `ICT 2022 交易带教课（中文系列）`：`目前没有可供编辑的原始图片。请上传需要编辑的图片，我就可以按照这份设计要求制作。`
     - 英文课程 `Fabio`：`The prompt refers to a reference image, but the request did not include one. Attach the image (for example via /v1/images/edits) or remove the reference, then try again.`
   - 第一轮修复（`7482a9d` / `v1.6.0`）移除了 `preserve`/`omit` 等触发词、增加了 `isLikelySizeError` 与 `240s` 超时，并统一了 `TestConnection` 尺寸策略。
2. **v1.6.0 升级后复测的残余问题**：
   - 升级到 `v1.6.0` 后单课生成可成功，但批量生成时 `ICT 2022 交易带教课（中文系列）` 与 `Fabio` 仍稳定触发 `The prompt refers to a reference image...` 400 拒答。
   - 进一步排查确认：`v1.6.0` 的 `BuildPrompt` 仍输出约 1,300–2,000 字符的**多段式 Markdown 设计需求清单**（`Create a brand-new standalone ... from scratch`、`Style:`、`- Background:`、`Exact Text to Render...`、`Right-Side Visual Theme (inspired by these subjects)`、`- Subject: ...`），并把未经裁剪的课程简介与章节标题转储其中。对于强多模态编辑向的 `gpt-image-2.6-sunburst` 而言，结构化清单体与章节/简介文本中的词汇仍极易被判定为“附带参考图的设计修改单”。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals

1. **单段式纯自然画面描述（`BuildPrompt` 重构）**：移除所有 Markdown 分段、项目符号、设计规范标题头以及原始 `Overview`/`ChapterTitles` 转储，改写为紧凑的**单段自然英文场景描述**（画面基底 → 左侧排版文字 → 右侧主题视觉 → 整体画风收尾），彻底脱离“需求清单/改图指令”语态。
2. **参考图拒答自动极简兜底重试（`BuildMinimalPrompt` + `cover.Service`）**：在单课 `GenerateCourseCover` 与批量 `runBatch` 共用的 `generateCourseImage` 路径中，若上游仍返回 `reference image` / `/images/edits` / `可供编辑` 等拒答错误，自动剥离课程主题词、标签与附加词，仅保留风格抽象几何与左侧文字，触发 1 次极简 Prompt 重试并持久化实际生效的 Prompt。
3. **仅对尺寸相关 400/422 回退 + 240s 超时与诊断**：保留 `isLikelySizeError` 拦截非尺寸 400 盲目重试、统一 `TestConnection` 自动尺寸链与 `240s` 默认超时。

### Non-Goals

- 不修改前端 UI、API 路由或 `StyleTemplate` 对外导出的 JSON 字段结构。
- 不改动数据库结构（`course-multi-covers` 相关迁移与功能保持独立）。

### Anti-Goals

- **绝不在 Prompt 中使用分段设计说明书模板、项目符号或否定式参考图词汇**（如 `from scratch`、`inspired by`、`no reference image`、`preserve`、`omit`）。
- **绝不对非参考图类错误或非尺寸类 400 做无条件重试**。

---

## 3. Detailed Changes

- **`internal/cover/styles.go`**：
  - 将 `StyleTemplate` 非导出字段重构为 `openingScene`、`leftSidePrefix`、`badgeFormat`、`titleFormat`、`subtitleFormat`、`noSubtitleSuffix`、`rightSideFormat`、`minimalRightSide`、`closingStyle`。
  - 通过共享 `buildSingleParagraphPrompt(style, input, minimal)` 实现 `BuildPrompt` 与 `BuildMinimalPrompt`，输出无换行、无列表标记的紧凑单段自然画面描述，限制标签上限为 4 个且不再注入原始 `Overview` 与 `ChapterTitles`。
- **`internal/cover/service.go`**：
  - 提取共享方法 `generateCourseImage` 与 `isReferenceImagePromptError`，由单课 `GenerateCourseCover` 和批量 `runBatch` 统一调用；命中参考图拒答时自动回退至 `BuildMinimalPrompt` 重试 1 次，并把最终成功的那条 Prompt 记入 `AddCourseCoverInput.Prompt`。
- **`internal/ai/image.go`**：
  - 延续 `240s` 默认超时、`isLikelySizeError` 精准尺寸回退与耗时/模型/尺寸诊断日志；简化 `TestConnection` 默认测试 Prompt 为纯画面描述。
- **`internal/cover/service_test.go` & `internal/ai/image_test.go`**：
  - `TestBuildPrompt_NoEditOrReferenceTriggers` 覆盖全部 6 套风格 × 完整/极简元数据 × `standard`/`minimal` 两种模式，校验单段无换行、无规范清单头、无编辑触发词且不泄露 `Overview`/`ChapterTitles`。
  - 新增 `TestService_ReferenceImageRefusalFallsBackToMinimalPrompt`，验证单课与批量均能在参考图 400 拒答时自动切到极简 Prompt 重试成功并保存实际 Prompt，同时普通错误不会重试。

---

## 4. Verification

```bash
go test -race -v ./internal/cover/... ./internal/ai/... && go test ./...
```
