# CourseFin AI 课程封面生成设计文档

- **Date:** 2026-11-26
- **Status:** Approved
- **Repository:** `CourseFin`

---

## 1. Intent & Why

CourseFin 课程列表与 Jellyfin 客户端目前依赖视频默认截帧或用户手工上传图片作为课程封面。当媒体库包含大量课程时，视频首帧风格杂乱、分辨率参差且缺少清晰课程标题辨识度，影响首页卡片与电视端/移动端浏览体验。

本功能面向**课程封面（`courses.cover_path`）**引入端到端 AI 生图能力，并通过**内置统一风格模板**保证全库多门课程封面呈现整齐一致的视觉格调与清晰排版。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals

1. **多协议自填 AI 生图配置：**
   - 系统设置页新增独立「AI 封面生图接口」区块，支持 `openai_images`（`/images/generations`）、`openai_chat`（`/chat/completions`）与 `gemini`（原生 `:generateContent`）三种协议。
   - 支持配置模型名、Base URL、API Key（脱敏存储与清除）、默认尺寸/画幅、全局默认风格，并提供不写盘的「测试生图接口」按钮。
2. **6 套内置统一风格模板 + 样图预览：**
   - 内置 `editorial_modern`（现代书封，默认）、`dark_tech`（暗夜科技）、`clay_3d`（柔和 3D 黏土）、`warm_illustration`（温暖扁平插画）、`blueprint_grid`（极简蓝图）、`cinematic_studio`（质感影院）。
   - 前端所有风格选择器支持悬停、键盘聚焦及触屏/点击预览真实参考样图（`/web/cover-styles/{id}.jpg`）。
3. **课程编辑页单课生成与预览确认：**
   - 支持自定义封面主标题、副标题/讲师、节数角标与画面补充词，生成临时预览图后与原封面双栏对比，满意后再点击应用覆盖正式封面。
4. **首页批量补齐/统一生成：**
   - 支持按当前视角（全部课程 / 指定媒体库）一键批量为缺失封面（`cover_path == ''`）或全部课程生成统一风格封面；后台串行队列执行、支持取消与实时进度轮询刷新。

### Non-Goals

- 不为单个课时视频（`lessons.cover_path`）或课程背景图（`backdrop_path`）生成 AI 图片（本次聚焦课程主封面）。
- 不做后端文字叠图渲染引擎（采用用户选定的纯 AI 端到端直出带字封面方案）。
- 不新增数据库 SQL 表结构或外部运行时依赖。

### Anti-Goals

- **绝不向存储源媒体目录写入文件：** 预览缓存与正式封面严格限制在 `{dataDir}/cache/cover_previews/` 与 `{dataDir}/covers/`，100% 兼容 `/media:ro`。
- **绝不静默泄露主 AI API Key：** 当配置了独立 `ai_image_base_url` 或使用 `gemini` 原生协议时，严禁把主 `ai_api_key` 转发至生图接口。
- **绝不未确认即破坏原封面：** 单课生成仅写入预览缓存，应用前不修改 `courses.cover_path`；批量任务单课失败时记录错误并保留原封面。

---

## 3. Approaches Considered & Decisions

| 方案 | 核心思路 | 决策 |
|---|---|---|
| 方案 A：AI 无字底图 + 前端 Canvas 叠字 | 避免文字乱码，文字由浏览器渲染 | 未采用 |
| **方案 B：纯 AI 端到端直出带字封面** | 将课程标题、副标题、角标与固定双栏版式写入模板 Prompt，由生图模型一次生成完整封面 | **已选定（用户选择）** |
| 方案 C：方案 A 默认 + 切换直出 | 兼顾两种模式但交互复杂 | 未采用 |

---

## 4. Detailed Architecture

- **`internal/ai/image.go`**：`ImageGenerator` 接口与 `HTTPImageGenerator` 多协议实现，内置图片魔数与尺寸校验、上游 400/422 参数兼容回退与 URL 图片安全下载。
- **`internal/cover/`**：`styles.go`（6 套模板与 Prompt 构造）、`config.go`（设置解析与防密钥泄露）、`service.go`（预览缓存管理、原子应用写入、串行批量任务与优雅停机）。
- **`internal/api/handlers_cover.go` & `handlers_ai.go` & `router.go`**：扩展 `/settings` 与新增 `/settings/ai/image/test`、`/cover/styles`、`/cover/previews/{previewId}`、`/courses/{id}/cover/generate`、`/courses/{id}/cover/apply`、`/courses/covers/batch/{status,generate,cancel}`。
- **前端**：`CoverStylePicker.tsx`（含 Portal 样图悬停/触摸预览）、`SettingsPage.tsx`、`AICoverModal.tsx` + `CourseEditorPage.tsx`、`BatchCoverModal.tsx` + `HomePage.tsx`。
