# CourseFin AI 课程封面生图误判与超时重试修复设计文档

- **Date:** 2026-11-26
- **Status:** Implemented
- **Repository:** `CourseFin`

---

## 1. Intent & Why (Root Cause Investigation)

在通过 `https://magpie.finnapps.win/v1/images/generations`（模型 `gpt-image-2.6-sunburst`，上游 `P-aixoras` / `aixoras.com`）运行 AI 封面生成时，系统设置中的「测试生图接口」约 12 秒即可返回 `200`，但实际生成课程封面时出现两类故障：

1. **默认留空（自动适配）尺寸下的 `120s` 超时**：
   - 报错：`ai: image request failed: Post "https://magpie.finnapps.win/v1/images/generations": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`。
   - 对比验证发现：`TestConnection` 在尺寸留空时强制覆盖为 `1024x1024` 且仅发送极简英文短 Prompt；而正式生图在尺寸留空时按 `["1536x1024", "1792x1024", "1024x1024", ""]` 顺序发送 `1536x1024` 与约 1500+ 字符的中英排版 Prompt。
2. **显式设为 `1024x1024` 跑批量生成时上游返回参考图编辑拒答（HTTP 400）**：
   - 中文课程 `ICT 2022 交易带教课（中文系列）`：`ai: image upstream returned HTTP 400: aixoras.com: 400 Bad Request: 目前没有可供编辑的原始图片。请上传需要编辑的图片，我就可以按照这份设计要求制作。`
   - 英文课程 `Fabio`：`ai: image upstream returned HTTP 400: aixoras.com: 400 Bad Request: The prompt refers to a reference image, but the request did not include one. Attach the image (for example via /v1/images/edits) or remove the reference, then try again.`

结合源码逐项排查，确认由以下三处问题共同导致：

1. **`internal/cover/styles.go` 的 Prompt 措辞触发多模态模型误判为“参考图编辑”**：
   - `BuildPrompt` 末尾带有 `preserve clear contrast...`，开头为 `Design a ...`，正文中包含 `on the image`。
   - 各风格模板静态 `promptBlueprint` 均包含 `Badge style: ...` 与 `Subtitle style: ...`，但在无角标或无副标题时，`BuildPrompt` 后半段又追加 `omit badge completely.` / `omit subtitle completely.`，前后矛盾且带有明显“基于原图做修改/删除”的语义，导致多模态生图后端将其路由或判定为需要输入原图的图像编辑任务。
2. **`internal/ai/image.go` 对非尺寸类 HTTP 400/422 盲目重试 4 次**：
   - `generateOpenAIImages` 在自动尺寸序列 `["1536x1024", "1792x1024", "1024x1024", ""]` 中，只要收到任意 HTTP `400` 或 `422` 就会继续尝试后续尺寸，即使上游返回的是明确提示“缺少参考图/提示词拒答”而非尺寸不支持，放大无效请求耗时与渠道重试。
3. **`120s` 默认超时偏紧、`TestConnection` 尺寸脱节且缺少诊断上下文**：
   - `defaultImageHTTPTimeout` 与 `defaultCoverHTTP` 均为 `120 * time.Second`；
   - `TestConnection` 在留空/`auto` 时强制改为 `1024x1024`，掩盖了正式默认首发 `1536x1024` 的兼容性与耗时；
   - `doPostJSON` 网络/超时失败未携带实际耗时、`model` 与 `size`，不利于排查中转网关或上游超时。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals

1. **纯文生图无歧义 Prompt 构造**：重构 `StyleTemplate` 字段拆分与 `BuildPrompt`，明确声明从零创建独立封面插画，消除 `preserve`、`omit`、`on the image` 等触发词，并仅在存在角标或副标题时输出对应的样式说明与文案条目。
2. **仅对尺寸相关 400/422 执行回退**：新增 `isLikelySizeError`，仅当上游报错信息涉及尺寸/分辨率/画幅（或响应体为空）时才继续回退下一候选尺寸；参考图拒答等非尺寸错误立即透传返回。
3. **统一测试与实测尺寸策略 + 放宽超时并补充诊断**：将默认生图超时统一提升至 `240s`，移除 `TestConnection` 对留空/`auto` 的 `1024x1024` 强制覆盖，并在请求异常中附带耗时、`model` 和 `size`。

### Non-Goals

- 不修改前端界面、请求接口或 `StyleTemplate` 对外导出的 JSON 字段结构。
- 不改动数据库结构与无关工作区变更（如 `course-multi-covers` 迁移与文档）。

### Anti-Goals

- **绝不在 Prompt 中写入否定式参考图词汇**（如 `"no reference image"`、`"no original image"`、`"no source image"`），避免关键词检测器仅凭词根误匹配。
- **绝不吞掉或盲目重试非尺寸类上游 HTTP 400 错误**。

---

## 3. Detailed Changes

- **`internal/cover/styles.go`**：
  - 将 `StyleTemplate.promptBlueprint` 拆分为 `styleHeading`、`backgroundSpec`、`leftColumnBackdrop`、`badgeStyleSpec`、`titleStyleSpec`、`subtitleStyleSpec`、`noSubtitleAccentSpec`、`rightColumnSpec` 八个非导出字段，保留全部导出字段与 JSON 标签不变。
  - 更新全部 6 套内置风格描述与 `BuildPrompt`：开头采用 `Create a brand-new standalone horizontal 16:10 digital course cover illustration from scratch...`，按 `badge`/`subtitle` 是否为空动态拼装样式描述与文案段落，移除全部编辑/参考图触发词。
- **`internal/ai/image.go`**：
  - `defaultImageHTTPTimeout` 由 `120 * time.Second` 调整为 `240 * time.Second`。
  - `TestConnection` 改用独立文生图提示词，并直接使用 `g.cfg.Size`，保持与正式生图一致的默认尺寸与回退顺序。
  - 新增 `isLikelySizeError(body []byte, triedSize string) bool`，在 `generateOpenAIImages` 的 HTTP 400/422 分支中校验通过后才继续尺寸回退。
  - `doPostJSON` 记录请求耗时并返回 `ai: image request failed after %s: %w`；`generateOpenAIImages`、`generateOpenAIChat`、`generateGemini` 错误中追加 `(model=..., size=...)` 或 `(model=...)`。
- **`internal/cover/service.go`**：
  - `defaultCoverHTTP` 由 `120 * time.Second` 同步调整为 `240 * time.Second`。
- **`internal/cover/service_test.go` & `internal/ai/image_test.go`**：
  - 新增 `TestBuildPrompt_NoEditOrReferenceTriggers`（遍历全部 6 套风格 × 完整/极简元数据）。
  - 新增 `TestHTTPImageGenerator_OpenAIImages_DoesNotRetryNonSize400`、`TestHTTPImageGenerator_TestConnection_UsesAutoSizeSequence`、`TestHTTPImageGenerator_OpenAIImages_TimeoutErrorIncludesDiagnostics`。

---

## 4. Verification

已执行以下全量测试并全部通过（无跳过或放宽断言）：

```bash
go test -race -v ./internal/ai/... ./internal/cover/... && go test ./internal/api/... && go test ./...
```
