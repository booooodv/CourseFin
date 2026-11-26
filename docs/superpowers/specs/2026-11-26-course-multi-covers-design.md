# CourseFin 课程多关联封面设计文档

- **Date:** 2026-11-26
- **Status:** Approved
- **Repository:** `CourseFin`

---

## 1. Intent & Why

CourseFin 现有的课程封面功能（包括 AI 单课/批量生成、本地上传与课时帧选取）均采用固定文件名覆盖方式，只保存单张 `courses.cover_path`。当用户对同一门课程尝试多种 AI 风格模板或多次重新生成时，旧封面会被直接替换销毁，无法找回或对比历史生成结果。

本设计将课程封面升级为**每门课程支持多个关联封面（`course_covers`）**：每次新增（包括 AI 生成、批量生成、手动上传、课时帧选取、目录导入）都会保留历史封面并自动将最新封面设为当前展示封面，同时用户可随时浏览、切换或删除任意历史封面。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals

1. **多关联封面统一留存与默认激活最新：**
   - 新增 `course_covers` 表记录每门课程关联的所有封面，支持 `ai`、`ai_batch`、`upload`、`frame`、`import`、`legacy` 六种来源。
   - 任意来源新增封面时均落盘为不可变唯一文件 `{courseID}_cover_{coverID}{ext}` 并自动激活为当前封面（同步更新 `courses.cover_path` 与 `courses.updated_at`）。
   - 不设单课程封面数量上限，支持在前端浏览、切回任意历史封面以及带二次确认的删除。
2. **既有数据与外围客户端无缝兼容：**
   - 迁移 SQL 自动将既有 `courses.cover_path` 回填至 `course_covers`（`source = 'legacy'`）。
   - `courses.cover_path` 始终与当前激活封面保持一致，首页、课程详情页、`media.Service` 与 Jellyfin 客户端（`ImageTags`）零破坏升级。
3. **AI 生成流程简化与历史联动：**
   - 单课 AI 生成接口改为直接生成落盘、入库并激活（移除临时预览缓存及两步应用流程），弹窗内即可直接对比历史封面并随手切换或删除。
   - 批量 AI 生成的「全部课程」模式改为新增封面并设为当前，保留既有历史封面。

### Non-Goals

- 不为单个课时（`lessons.cover_path`）或课程背景横幅（`backdrop_path`）引入多封面管理。
- 不向任何只读存储源目录（如 `/media`）写入封面文件。

### Anti-Goals

- **绝不在封面与课程表之间引入冗余激活列导致状态漂移：** 始终以 `course_covers.file_path == courses.cover_path` 作为唯一激活判定真相源。
- **绝不在删课或强制删库时遗留孤儿封面文件：** 删课及强制删库必须在提交后清理所有关联 `course_covers` 磁盘文件。
- **绝不在编辑页封面操作时冲掉用户未保存的课程元数据草稿：** 上传、生成、切换、删除封面仅同步课程封面状态与 `updated_at`，不重置表单草稿。

---

## 3. Approaches Considered & Decisions

| 方案 | 核心思路 | 决策 |
|---|---|---|
| **方案 A：`course_covers` 独立表 + 编辑页内嵌列表 + AI 弹窗联动** | 独立数据表持久化封面元数据与来源，编辑页直接呈现网格且弹窗实时联动 | **已选定（用户选择）** |
| 方案 B：`course_covers` 独立表 + 集中式封面管理弹窗 | 编辑页仅保留入口按钮，在独立弹窗中管理全部封面 | 未采用 |
| 方案 C：纯文件系统扫描 | 不新建表，靠扫描磁盘文件时间识别封面 | 未采用 |

---

## 4. Detailed Architecture

### Data Model & Migration (`004_course_covers.sql`)

- 新增 `course_covers` 表：
  - `id` TEXT PRIMARY KEY（32 位 hex）
  - `course_id` TEXT NOT NULL REFERENCES `courses(id)` ON DELETE CASCADE
  - `file_path` TEXT NOT NULL（同 `courses.cover_path` 相对文件名）
  - `source` TEXT NOT NULL DEFAULT `'legacy'`
  - `style_id`, `style_name`, `prompt` TEXT NOT NULL DEFAULT `''`
  - `width`, `height` INTEGER NOT NULL DEFAULT 0
  - `created_at` TEXT NOT NULL（定长 9 位小数 UTC RFC3339，保证 SQLite 字典序与时间序严格一致）
  - `UNIQUE(course_id, file_path)` 与索引 `idx_course_covers_course_created(course_id, created_at DESC, id DESC)`
- 迁移执行时自动把所有非空 `courses.cover_path` 插入 `course_covers`（`source = 'legacy'`）。

### Backend Services & API

- **`internal/catalog/covers.go` & `service.go` & `importer.go`**：
  - 提供 `ListCourseCovers`、`GetCourseCover`、`AddCourseCover`、`ActivateCourseCover`、`DeleteCourseCover`；删除当前激活封面时自动激活次新封面（若无剩余则清空 `courses.cover_path`）。
  - `CourseDetail` 新增 `covers` 字段；`DeleteCourse`、`DeleteLibraryWithOptions(Force: true)` 清理全部关联文件；`CommitImport` 导入封面同步写入 `course_covers`。
- **`internal/media/image.go`**：新增 `ServeCoverFile` 复用缩放与 SHA-256 ETag 缓存服务单张历史封面。
- **`internal/cover/service.go`**：移除临时预览缓存，`GenerateCourseCover` 与批量任务均调用 `catalog.AddCourseCover` 直接追加保存并设为当前。
- **`internal/api/router.go` & `handlers_cover.go` & `handlers_catalog.go`**：
  - 新增 `GET /api/v1/courses/{id}/covers`、`GET /api/v1/courses/{id}/covers/{coverId}/image`、`POST /api/v1/courses/{id}/covers/{coverId}/activate`、`DELETE /api/v1/courses/{id}/covers/{coverId}`。
  - 更新 `POST /api/v1/courses/{id}/cover/generate`（返回 `{ cover, course }`）及 `POST /api/v1/courses/{id}/cover`（上传与帧选取均追加不覆盖）。
  - 移除 `/api/v1/cover/previews/{previewId}` 与 `/api/v1/courses/{id}/cover/apply`。

### Frontend UX

- **`web/src/components/CourseCoverGallery.tsx`**：通用语义化多封面网格组件，高亮当前激活封面、标注来源/风格与尺寸，支持点击切换和内联二次确认删除。
- **`web/src/pages/CourseEditorPage.tsx`**：在封面按钮区下方内嵌「关联封面 (N)」网格；封面变更不覆盖未保存的表单草稿。
- **`web/src/components/AICoverModal.tsx`**：单步生成即保存并设为当前，主区展示当前封面与 Prompt，下方联动显示并可操作全量关联封面历史。
- **`web/src/components/BatchCoverModal.tsx`**：调整「全部课程」模式说明为保留历史封面并新增。
