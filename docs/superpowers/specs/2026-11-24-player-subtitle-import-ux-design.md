# CourseFin 播放器、字幕、导入 AI 与进度交互优化设计文档

- **Date:** 2026-11-24
- **Status:** Ready for user approval (builder check completed)
- **Repository:** `CourseFin`

---

## 1. Intent & Why

CourseFin 目前已经具备完整的存储扫描、课程目录导入、课程编辑、AI 整理与 Web/Jellyfin 双端播放能力，但在日常使用中有 5 个集中影响学习与整理体验的痛点：

1. **导入流程缺少 AI 整理入口：** 用户在「存储与导入」点击「一键导入为课程」时只能手工修改杂乱的目录和文件名，必须先导入成课程、再跳转到课程编辑页才能调用 AI 智能整理，多走了一步来回。
2. **Web 播放器体验简陋且大量视频无法播放：** 当前 `VideoPlayer` 直接使用浏览器原生 `<video controls>` 搭配下方简陋控制条；同时后端将所有非 H.264/VP9/AV1 直放或非 H.264 封装的视频（例如常见的 HEVC/H.265、MKV 内 VP9/AV1、以及老课程常见的 AVI/MPEG4、WMV、FLV、RMVB 等）一律判为 `unsupported`，提示「当前浏览器不支持直接播放此视频编码」。
3. **外挂字幕扫描到了却在播放页无法选择：** 存储扫描报告统计了目录下所有字幕文件数量（`total_subtitles`），但当前字幕匹配逻辑（`subtitle_match.go`）只接受**严格同目录 + 严格点号分隔 + 有限语言词典**的命名；放在 `Subs/`、`Subtitles/`、`字幕/` 子目录或使用 `_`、`-`、空格、括号以及 `简体中文`、`中英双语`、`en-orig`、`ai-zh` 等常见命名的字幕全部匹配失败（`media_file_id = NULL`），导致视频播放页的字幕列表为空；同时非 UTF-8 编码（GBK/UTF-16）的中文字幕也缺乏自动转码。
4. **存储源浏览无法分辨哪些视频已入课：** 在「存储与导入」页面查看存储源、子目录与文件列表时，看不到哪些目录/视频已经加入过课程，也看不到还剩多少视频未整理入课，容易重复导入或遗漏新视频。
5. **播放页右侧目录缺少单节播放进度反馈：** 目前课程目录项前只区分「已播放（绿勾）」「当前播放（播放图标）」「未播放（空心圆）」，看不出某节课已经看到一半还是快看完了。

---

## 2. Goals, Non-Goals, and Anti-Goals

### Goals
1. **导入弹窗内置 AI 整理：** 在 `ImportCourseModal` 中直接提供「AI 智能整理」能力，生成与课程编辑页一致的对比预览与勾选应用体验，将建议回填到导入表单中供确认前微调。
2. **嵌入式现代化 Web 播放器 + 近全格式可播：**
   - 前端嵌入 **Artplayer + `hls.js`**，提供中文界面、倍速、字幕切换与样式/时间偏移调节、画中画、网页全屏/浏览器全屏、截图、底部迷你进度条与快捷键。
   - 后端实现**三层播放决策**（浏览器直放含 HEVC 探测 → H.264 零视频转码 HLS 封装 → FFmpeg 按需实时 H.264 HLS 转码兜底），并在浏览器直放解码异常时自动回退转码，彻底消除常见的「编码不支持」死胡同。
3. **字幕自动匹配、编码兼容与手动绑定兜底：**
   - 放宽并增强自动字幕匹配（支持字幕子目录/同级目录、多分隔符、常见中英/B站/yt-dlp/Udemy 命名、Unicode NFC 规范化、去噪后缀兜底）。
   - 自动识别并转换 UTF-16 / GB18030(GBK) 字幕为 UTF-8，确保 WebVTT 渲染不乱码、不解析失败。
   - 服务启动时自动对存量未绑定字幕执行一次无磁盘 I/O 的重匹配，并在课程编辑页提供手动关联/解绑字幕入口。
   - 播放页有字幕时自动开启首选轨（优先记忆偏好 → 默认轨 → 中文轨 → 首条），并记住用户的语言或关闭选择。
4. **存储源 / 子目录 / 文件入课状态全链路可视化：** 在存储源卡片、子目录列表和文件列表清晰展示「未入课数量」「已入课状态与所属课程」以及「未入课 / 已入课」筛选器，导入完成后实时刷新。
5. **课程目录环形进度指示器：** 用 SVG 环形进度条取代普通空心圆，直观反映单节观看进度（已完成为绿勾，当前播放项环内显示播放三角并实时更新进度，悬停展示百分比）。

### Non-Goals
- GPU 硬件转码调度（NVENC / VAAPI / QSV / VideoToolbox）与多码率自适应（ABR）转码档位切换（本次采用 CPU `libx264 -preset veryfast` 按需单档位转码，保证在各类 Docker/Linux/macOS 环境开箱即用）。
- 图形字幕（PGS / VOBSUB / DVB）的 OCR 识别或视频硬烧录（仅支持文本字幕 `.srt/.vtt/.ass/.ssa` 及内嵌文本字幕轨）。
- 将 Artplayer 用于第三方 Jellyfin 客户端协议（Infuse / VidHub / SenPlayer 继续走原有 Jellyfin API 与 DirectPlay/Remux 逻辑，不受影响）。
- 在导入弹窗中持久化草稿到数据库（导入弹窗的 AI 整理结果仅作用于当前前端表单状态，确认导入时才写入数据库）。

### Anti-Goals
- **绝不修改磁盘上的原始视频或字幕文件：** 所有转码、字幕转码和绑定均只在 `/data/cache` 与 SQLite 元数据层完成，保持对 `:ro` 只读媒体卷的完全兼容。
- **绝不破坏已有 Jellyfin API 与现有测试契约：** 原有 `CanRemuxVideoCodec` / `CanWebRemuxVideoCodec` 语义保持不变；`PlaybackInfo` 仍优先给 Infuse/VidHub/SenPlayer 返回 DirectPlay；已有存储源/课程 API 返回字段向后兼容（仅做增量字段扩展）。
- **绝不让转码 FFmpeg 进程泄漏或常驻占用 CPU：** 转码会话必须完全接入现有空闲超时回收（5 分钟）、切集/卸载 `DELETE /Videos/ActiveEncodings` 清理以及服务关闭清理链路。
- **绝不覆盖用户手动绑定的字幕：** 二次扫描存储源时，对用户手动绑定的字幕记录必须保留其绑定关系。

---

## 3. Constraints

- **单容器与依赖边界：**
  - 前端（`web/package.json`）新增 1 个纯 JS 依赖 `artplayer`（无 React 19 peer dependency 冲突），需同步更新 `web/pnpm-lock.yaml` 满足 Dockerfile 的 `pnpm install --frozen-lockfile`。
  - 后端（`go.mod`）新增官方纯 Go 模块 `golang.org/x/text`（用于 Unicode NFC 规范化及 UTF-16 / GB18030 编码转 UTF-8），保持 `CGO_ENABLED=0` 静态编译。
  - 运行时依赖 `ffmpeg`/`ffprobe`（Dockerfile 已内置；本机部署未安装 FFmpeg 时，无法直放的视频仍优雅回退到 `mode: "unsupported"` 提示使用原生客户端）。
- **双主题与样式系统：** 所有新增 UI（导入 AI 预览、存储页徽章/筛选器、目录环形进度条）必须使用 `web/src/index.css` 中的语义 token（`bg-card`, `bg-subtle`, `text-ink-primary`, `bg-accent`, `text-status-success-ink` 等），禁止硬编码 `slate/indigo/emerald/rose` 色板；Artplayer 内部皮肤主题色通过读取计算后的 `--accent` 注入。
- **测试环境兼容性：** Vitest 运行在 `jsdom` 中（无真实媒体解码器与完整 Canvas/Fullscreen API）。`VideoPlayer` 对 Artplayer 的封装必须优雅降级/支持干净销毁，确保前端单测稳定可测。

---

## 4. Approaches Considered & Decisions

| 领域 | 备选方案 | 权衡对比 | 决策 |
|---|---|---|---|
| **1. Web 播放器前端** | **A. 嵌入 `Artplayer` + `hls.js`** vs. B. 基于 `<video>` + `hls.js` 自研 React 播放器 | A 开箱即用提供中文 UI、字幕样式与时间偏移、画中画、网页全屏、截图、迷你进度条和手势，纯 JS 无 React 19 冲突，仅增 1 个依赖；B 零新增依赖但缺少字幕偏移/截图等进阶能力且自研控件维护成本高。 | **方案 A（用户已选）** |
| **2. 编码兼容策略** | **A. 三层播放（浏览器直放含 HEVC 探测 → H.264 零转码 HLS 封装 → FFmpeg 按需 H.264 HLS 转码）** vs. B. 仅放开 HEVC 直放（不做服务端转码） | A 兼顾低 CPU（支持直放/remux 的绝不转码）与近全格式可播（解决 HEVC MKV、MPEG4/WMV/FLV/10-bit 等所有「编码不支持」问题）；B 无法解决非 MP4 HEVC 和老编码课程。 | **方案 A（用户已选）** |
| **3. 字幕默认开启策略** | **A. 自动开启首选轨（记忆偏好 → default → 中文 → 首条）** vs. B. 仅开启带 `default` 标记的字幕 | A 让绝大多数无 `.default.` 命名标记的课程字幕开播即见，同时尊重用户手动关闭或切换语言的选择；B 仍需每节课手动打开字幕。 | **方案 A（用户已选）** |
| **4. 目录进度圆圈样式** | **A. SVG 环形进度条（外圈弧度表示进度，当前播放项环内带 ▶，已完成为 ✓）** vs. B. 实心扇形饼图 | A 视觉更轻盈，与现有 Lucide 线性图标体系及「当前播放」状态天然融合，清晰区分播放中/半看/未看/已看；B 信息密度高但难以同时叠放播放状态图标。 | **方案 A（用户已选）** |

---

## 5. Detailed Design

### 5.1 导入弹窗 AI 智能整理

#### 后端接口：`POST /api/v1/import/ai/organize`
- **路由与鉴权：** 注册在 `/api/v1` 受 `requireAuth` 保护路由组下（`internal/api/router.go` + `internal/api/handlers_ai.go`）。
- **请求体：**
  ```json
  {
    "source_id": "32位hex",
    "dir_rel_path": "Go实战课程",
    "title": "Go实战课程",
    "overview": "",
    "chapters": [
      {
        "title": "第一章",
        "lessons": [
          { "media_file_id": "mf1", "title": "01_intro" }
        ]
      }
    ]
  }
  ```
- **服务端处理逻辑：**
  1. 校验 AI 配置（`ai_base_url` 和 `ai_model` 非空，否则返回 `400` 与现有 `handleAIOrganize` 一致的报错信息）。
  2. 校验 `source_id` 存在且 `chapters` 中至少包含 1 个有效 `media_file_id`（去重校验；所有 `media_file_id` 必须属于该 `source_id`，否则返回 `400`/`404`）。
  3. 抽出公共辅助函数 `loadMediaAndSubtitlesByIDs(ctx, mediaFileIDs, includeSubtitles)`，供 `handleAIOrganize` 与 `handleImportAIOrganize` 复用：批量读取 `media_files.file_name`、`relative_path` 以及对应已匹配的 `subtitle_files` 摘录（遵守全局 `ai_max_subtitle_chars` 与单节 `DefaultPerLessonSubtitleChars` 预算）。
  4. 构造 `ai.CourseOrganizeInput`：将每个待导入课时的 `media_file_id` 填入 `OrganizeLessonInput.LessonID`，调用现有 `OpenAICompatibleProvider.OrganizeCourse(ctx, input)`（沿用同一套严格 JSON Schema 与 100% ID 覆盖率校验）。
  5. **响应体（`200 OK`）：** 返回标准 `CourseOrganizeProposal` 结构，为了前端语义清晰，同时在每个 lesson 对象中映射出 `media_file_id`（与 `lesson_id` 等值，或专用结构体 `ImportAIProposal` 含 `title`, `overview`, `tags`, `chapters[].{title, lessons[].{media_file_id, title}}`）——建议定义专用响应结构体 `ImportOrganizeProposal`，前端无需把 `lesson_id` 当 `media_file_id` 猜着转换。*(Our call)*

#### 前端交互：`ImportCourseModal.tsx`
- 在弹窗头部操作区（或表单上方操作栏）新增 **「AI 智能整理」** 按钮（`Sparkles` 图标 + Tooltip `"调用 AI 自动整理课程标题、简介、标签及章节课时结构"`），仅在 `preview` 加载完成且存在至少 1 个视频时可点击。
- **预览与勾选应用面板：**
  - 点击「AI 智能整理」后，调用 `api.aiOrganizeImport(...)`（传入当前弹窗中的 `source_id`, `dir_rel_path`, `title`, `overview`, `chapters`）。
  - 复用/抽离 `AIOrganizeModal` 的对比预览体验（可抽为通用预览内容组件或二级弹窗 `AIImportOrganizeModal`）：展示加载态、错误态（附「前往系统设置」链接与「重新生成」按钮）、以及「当前草稿 vs AI 建议」的 4 项分块对比与复选框：
    - 应用建议标题（默认勾选）
    - 应用建议简介（默认勾选）
    - 应用建议标签（默认勾选）
    - 应用建议章节与课时结构（默认勾选）
  - 点击「应用到导入表单」后，**只更新 `ImportCourseModal` 的本地 state**（不写入数据库），关闭预览层并提示 Toast（`"已将 AI 建议填入导入表单，可继续微调后确认导入"`）。
  - 提供轻量的 **「恢复初始识别」** 按钮（仅在应用过 AI 建议后显示），一键还原到刚打开弹窗时的 `preview` 初始值。
  - **关键修复：** `ImportCourseModal` 中课时时长的查找从当前脆弱的 `preview.chapters[chIdx]?.lessons[lIdx]` 改为按 `media_file_id` 建立 `Map<string, PreviewLesson>`，确保 AI 重组章节或移动课时顺序后，每个课时的时长显示依然准确。

---

### 5.2 嵌入式 Web 播放器（Artplayer）与三层编码播放策略

#### 后端三层播放决策：`GET /api/v1/lessons/{id}/playback`
- **新增可选查询参数（全部向后兼容）：**
  - `codecs`：逗号分隔的浏览器探测结果，例如 `h264,hevc,vp9,av1`；缺省时按 `h264,vp9,av1` 处理（保持旧调用方与现有测试默认行为一致）。
  - `force_transcode`：`1` 或 `true` 时跳过 DirectPlay 与 `-c:v copy` 封装，强制走 FFmpeg H.264 转码 HLS（用于前端直放解码失败时的自动容错回退）。
- **四档决策顺序：**
  1. **DirectPlay (`mode: "direct"`, `transcoding: false`)：**
     - `!forceTranscode`
     - `isBrowserDirectContainer(container)`（`mp4`, `m4v`, `webm`, `mov`）
     - `isBrowserDirectAudioCodec(acodec)`（`""`, `aac`, `mp3`, `opus`）
     - `isBrowserDirectVideoCodec(vcodec, clientCodecs)`：
       - `h264`/`avc`/`avc1`、`vp9`、`av1`：当 `clientCodecs` 包含对应归一化编码（或 `codecs` 未传）时允许；
       - `hevc`/`h265`/`hvc1`/`hev1`：仅当 `clientCodecs` 明确包含 `hevc` 且视频非高色深（若 `streams_json` 中存在 `pix_fmt` 且明显为浏览器难解格式如 `yuv444p` 则不直放）时允许。*(Our call)*
  2. **轻量 HLS 封装 (`mode: "hls"`, `transcoding: false`)：**
     - `!forceTranscode` 且 `h.media.HasFFmpeg()` 且 `(vcodec == "" || h.media.CanWebRemuxVideoCodec(vcodec))`
     - `stream_url`: `/Videos/{lessonID}/master.m3u8?PlaySessionId=...&MediaSourceId=...&api_key=...`（沿用现有零视频转码 `-c:v copy` 逻辑）。
  3. **FFmpeg 按需 H.264 HLS 转码 (`mode: "hls"`, `transcoding: true`)：**
     - `h.media.HasFFmpeg()` 为真（涵盖 HEVC MKV、不支持 HEVC 的浏览器、VP9/AV1 MKV、MPEG4/AVI、WMV3、FLV1、RMVB、VC-1、MPEG-2 以及 `force_transcode=1`）。
     - `stream_url`: `/Videos/{lessonID}/master.m3u8?PlaySessionId=...&MediaSourceId=...&TranscodeVideo=1&api_key=...`
  4. **不支持 (`mode: "unsupported"`, `transcoding: false`)：**
     - 仅当服务端未安装可用 FFmpeg（`!h.media.HasFFmpeg()`）且无法 DirectPlay 时返回，保留 Infuse / VidHub / SenPlayer 推荐页。
- **响应新增字段：** `"transcoding": true|false`（表示当前流是否启用了服务端视频转码，供前端展示轻量状态标识）。

#### 后端按需可拖拽 H.264 转码管线 (`internal/media/streamer.go`, `internal/media/remux.go`)
- **`HasFFmpeg() bool`：** 在 `media.Service` 初始化时通过 `exec.LookPath` / `os.Stat` 判断 `ffmpegBin` 是否可执行，对外暴露 `HasFFmpeg() bool`（测试中传入不存在的自定义路径即可轻松验证降级为 `unsupported`）。
- **保持原有接口不变：** `CanRemuxVideoCodec`、`CanWebRemuxVideoCodec`、`NeedsAudioTranscode` 保持完全一致的判断逻辑。
- **HLS 查询参数透传：** `propagateHLSQuery` 增加透传 `TranscodeVideo=1`（从 `master.m3u8` → `main.m3u8` → `hls1/main/{idx}.ts`）。
- **编码放行：** 当请求携带 `TranscodeVideo=1` 时，`ServeMasterPlaylist`、`ServeMediaPlaylist`、`ServeHLSSegment` 不再因 `!CanRemuxVideoCodec(rec.VideoCodec)` 返回 `415`，而是进入转码分片生成。
- **FFmpeg 转码命令（`launchFFmpegRemuxLocked` 扩展 `transcodeVideo bool`）：**
  - 保持输入前 `-ss <startSec>` 与 `-output_ts_offset <startSec>` 机制，确保拖拽到任意时间点仍可立即从目标分片开始生成；
  - 当 `transcodeVideo == true` 时：
    - 视频参数：`-c:v libx264 -preset veryfast -crf 23 -pix_fmt yuv420p -force_key_frames expr:gte(t,n_forced*6) -sc_threshold 0`（强制 6 秒对齐关键帧与 8-bit `yuv420p`，兼容 10-bit HEVC/H.264 及所有浏览器 MSE）；
    - 音频参数：若 `NeedsAudioTranscode(audioCodec)` 为真则 `-c:a aac -ac 2 -b:a 192k`，否则 `-c:a copy`（注：MPEG-TS 容器对 Opus 兼容性不佳，`NeedsAudioTranscode("opus")` 已返回 `true`，会自动转 AAC）；
    - 若源视频无音频流，`-map 0:a:0?` 仍安全忽略。
  - **等待超时与回收：** 转码首片可能比 copy 略慢，保持 `remuxWaitTimeout = 30s`（足够 `veryfast` 编码单个 6s 分片）；沿用现有 5 分钟空闲清理、seek 重启旧进程杀掉、以及 `DELETE /Videos/ActiveEncodings` 立即终止进程并清理 `/data/cache/remux/{key}`。

#### 前端浏览器编码探测与 Artplayer 封装 (`web/src/components/VideoPlayer.tsx`)
- **浏览器能力探测工具（`web/src/utils/mediaCapabilities.ts`）：**
  - 通过临时 `<video>` 元素的 `canPlayType` 检测本机浏览器支持：
    - `h264`: `video/mp4; codecs="avc1.42E01E"`
    - `hevc`: `video/mp4; codecs="hvc1.1.6.L93.B0"` 或 `video/mp4; codecs="hev1.1.6.L93.B0"`（需返回 `'probably'`，避免某些浏览器的 `'maybe'` 误报；再配运行时错误回退双保险）*(Our call)*
    - `vp9`: `video/webm; codecs="vp9"`
    - `av1`: `video/mp4; codecs="av01.0.04M.08"`
  - `CourseDetailPage` 调用 `api.getLessonPlayback(lessonId, { codecs, forceTranscode })` 时自动附带探测结果。
- **Artplayer 实例化与核心能力：**
  - 依赖：`artplayer`（配合已有 `hls.js`）。
  - 主题联动：读取 `getComputedStyle(document.documentElement).getPropertyValue('--accent')` 传入 Artplayer `theme`，并在主题切换时同步更新。
  - 开启的核心内置能力：`lang: 'zh-cn'`、`playbackRate: true`、`aspectRatio: true`、`setting: true`、`pip: true`、`fullscreen: true`、`fullscreenWeb: true`、`miniProgressBar: true`、`screenshot: true`、`subtitleOffset: true`、`hotkey: true`、`fastForward: true`、`mutex: true`、`playsInline: true`、`autoPlayback: false`（断点续播统一由 CourseFin 服务端 `position_ticks` 接管）。
  - **HLS 挂载（`customType.m3u8`）：**
    - 支持 `Hls.isSupported()` 时创建 `new Hls(resumeSec > 0 ? { startPosition: resumeSec } : undefined)`，挂载到 `video`，监听 `Hls.Events.ERROR`（fatal 时给出清晰错误提示），销毁 Artplayer 时同步 `hls.destroy()` 并调用 `api.stopActiveEncoding(playback.play_session_id)`。
    - Safari 原生支持 `application/vnd.apple.mpegurl` 时直接赋给 `video.src`。
  - **字幕集成与偏好记忆：**
    - 首选字幕解析规则（`pickInitialSubtitle(textSubtitles, savedPref)`）：
      1. 读取 `localStorage['coursefin_subtitle_pref']`：若为 `'off'`，默认关闭；若为具体语言码（如 `'zh'`、`'en'`），优先匹配同语言轨道；
      2. 否则选 `is_default === true` 的轨道；
      3. 否则优先选中文轨道（`language === 'zh'` 或标题含 `中文`/`简体`/`繁體`/`双语`）；
      4. 否则选第一条可用文本字幕轨。
    - 在 Artplayer 中通过 `subtitle` 配置初始轨道（`type: 'vtt'`, `encoding: 'utf-8'`, `escape: false`），并注入清晰的「字幕」菜单（既放在底部快捷控件/设置面板中，也确保全屏和网页全屏下均可随时切换，选项为 `关闭` + 所有文本字幕轨），同时提供字幕字号快捷调节（小 / 中 / 大）和内置字幕时间偏移。
    - 用户手动切换或关闭字幕时，即时写入 `localStorage['coursefin_subtitle_pref']`（关闭存 `'off'`，选中轨道存其 `language` 或 `'on'`）。
  - **DirectPlay 解码失败自动降级转码：**
    - 若 `playback.mode === 'direct'` 且在未成功播放前触发 `<video>` 的 `error` 事件，自动调用 `onRequestTranscodeFallback?.()`，由 `CourseDetailPage` 以 `forceTranscode: true` 重新请求一次播放地址（每节最多自动重试 1 次，避免死循环）并弹出轻量 Toast（`"浏览器无法直接解码该视频，已自动切换为兼容转码模式"`）。
  - **进度上报与实时回调：**
    - 保留完整断点续播（`!played && position_ticks > 0`）、10 秒定时同步、暂停即同步、播放完成即同步、切课/卸载 `keepalive` 防重复刷写逻辑。
    - 新增 `onTimeProgress?: (lessonId: string, currentSec: number, durationSec: number) => void` 回调（按 1% 进度变化或 1 秒节流触发），驱动右侧目录当前播放课时的环形进度条实时填充。
  - **转码状态轻量提示：** 播放器底部状态角标或信息条在 `playback.transcoding === true` 时显示轻量徽章（如 `兼容转码播放`），不干扰观影画面；当 `mode === 'unsupported'` 时保留现有原生 Jellyfin 播放器推荐卡片。

---

### 5.3 字幕匹配、编码转换、存量重匹配与手动绑定

#### 1. 自动匹配规则升级 (`internal/scanner/subtitle_match.go`, `internal/scanner/scanner.go`)
- **Unicode 规范化：** 匹配前对视频路径与字幕路径统一做 `norm.NFC.String(...)`，消除 macOS NFD 与 Linux/Windows NFC 差异导致的匹配失败。
- **跨目录范围（`syncSubtitlesTx` & `MatchSubtitlesForVideo`）：**
  - 定义公认字幕目录名集合 `subtitleDirNames`（大小写不敏感）：`subs`, `subtitles`, `subtitle`, `sub`, `字幕`, `中文字幕`, `英文字幕`, `srt`, `vtt`, `ass`。
  - 对每个字幕文件，候选视频查找优先级：
    1. **同目录视频**：`path.Dir(video) == path.Dir(sub)`；
    2. **父目录视频**（字幕位于视频目录下的字幕子目录中）：若 `path.Base(path.Dir(sub))` 属于 `subtitleDirNames`，则在 `path.Dir(path.Dir(sub))` 查找候选视频（如 `Course/Ch1/Subs/01.srt` → `Course/Ch1/01.mp4`）；
    3. **兄弟目录视频**（字幕位于同级字幕目录中）：若 `path.Base(path.Dir(sub))` 属于 `subtitleDirNames` 且同父目录下存在视频目录（如 `Course/Subtitles/01.srt` 与 `Course/Videos/01.mp4`），则在同父目录下的兄弟目录中按唯一 stem 匹配（仅当恰好命中唯一视频时绑定，避免跨章节同名文件误绑）。*(Our call)*
  - 非字幕目录（例如 `Course/Chapter2/01.srt`）绝不跨目录匹配 `Course/Chapter1/01.mp4`。
- **文件名匹配优先级（同目录内候选视频仍按 stem 长度降序排列，防止短前缀抢占长视频名）：**
  1. **精确同名（忽略大小写 + NFC）：** `01_intro.srt` ↔ `01_intro.mp4`。
  2. **视频 stem + 分隔符 + 后缀：** 允许的分隔符扩展为 `.`、`_`、`-`、空格 `' '`、`[`、`(`、`【`、`（`。
     - 扩充 `langAliases` 覆盖真实课程常见标签：`简体中文`、`繁体中文`、`繁體中文`、`中英`、`中英双语`、`双语`、`中文字幕`、`英文字幕`、`双语字幕`、`chinese`、`simplified`、`traditional`、`zh-hans`、`zh_hans`、`zh-hant`、`zh_hant`、`zh-chs`、`zh-cht`、`zh-sg`、`en-orig`、`en-au`、`en-ca`、`ai-zh`、`ai-en`、`bilingual`、`chs&eng`、`cht&eng`、`zh&en`、`chs_eng` 等。
     - 扩充 `subtitleModifiers`：`default`, `forced`, `sdh`, `cc`, `hi`, `auto`, `orig`, `ai`, `asr`, `official`, `translated`, `machine`。
     - **兜底不丢弃原则：** 只要字幕名以 `<videoStem>` + 允许分隔符开头，且未被更长 stem 的视频抢先匹配，即使后缀包含词典外词（如 `.CourseName`、`[机翻]`、`（双语校对）`），**仍然匹配该视频**：从后缀词元中尽可能提取已识别语言码与 `is_default`，提取不到时 `language = "und"`，并将清理后的后缀文本作为可读 `title`（如 `中英双语`），绝不再默默丢弃！
     - 防误匹配保护：若后缀去掉空白和括号后为**纯数字**（例如视频为 `01.mp4`，字幕为 `01_2.srt` 或 `01-02.srt`），且不是通过 `.` 紧接语言码，则不走模糊前缀兜底，防止分集编号误绑。*(Our call)*
  3. **去清晰度/编码噪声标签兜底：** 若前两轮均未匹配，去除视频 stem 末尾常见的清晰度/编码标签（如 `[1080p]`、`(720p)`、`.1080p`、`_1080P`、`[HEVC]`、`[x264]`、`[4K]`）后，重试精确同名及分隔符前缀匹配（仅在候选唯一时绑定）。

#### 2. 字幕编码自动转换 (`internal/media/subtitle.go`)
- 在 `serveExternalSubtitle` 中读取外部字幕原始字节后，先经 `decodeSubtitleToUTF8(raw []byte) string` 处理：
  1. UTF-8 BOM (`0xEF 0xBB 0xBF`)：去 BOM 按 UTF-8 返回；
  2. UTF-16 LE (`0xFF 0xFE`) / UTF-16 BE (`0xFE 0xFF`)：使用 `golang.org/x/text/encoding/unicode` 解码为 UTF-8；
  3. 合法 UTF-8（`utf8.Valid(raw)`）：直接按 UTF-8 返回；
  4. 非法 UTF-8 字节流：使用 `simplifiedchinese.GB18030.NewDecoder()` 解码为 UTF-8（GB18030 向下完全兼容 GBK 与 GB2312）。
- 转换后的 UTF-8 文本再送入 `ConvertSRTToVTT` 或 FFmpeg（`.ass/.ssa -> webvtt`）以及 `.vtt` 直出（若 `.vtt` 缺少 `WEBVTT` 头也做容错补齐）。
- 同样复用该解码函数于 `handlers_ai.go` 的 `readSubtitleExcerpt`，确保非 UTF-8 中文字幕喂给 AI 整理时也不乱码。

#### 3. 数据库迁移、存量自动重匹配与手动绑定
- **新增迁移 `internal/db/migrations/002_subtitle_manual_bind.sql`：**
  ```sql
  ALTER TABLE subtitle_files ADD COLUMN manually_bound INTEGER NOT NULL DEFAULT 0;
  ```
- **扫描报告增强（`scanner.ScanReport`）：** 新增 `MatchedSubtitles int`（JSON `matched_subtitles`），记录本次扫描中成功关联到视频的字幕数；`StoragePage` 扫描结果条展示 `字幕：{total_subtitles}（已匹配 {matched_subtitles}）`（若存在未匹配字幕，以提示色标出便于排查）。
- **`syncSubtitlesTx` 保留手动绑定/解绑决策：** 对 `manually_bound = 1` 的记录，无论是绑定到有效 `media_file_id` 还是用户显式解绑（`media_file_id IS NULL`），扫描更新时均只刷新 `status = 'active'`，不覆盖用户的 `media_file_id` / `language` / `title` / `is_default`（避免用户手动解绑误匹配字幕后被每小时定时扫描重新绑回）。*(Our call)*
- **启动时存量自动重匹配（`scanner.Service.RematchUnboundSubtitles(ctx)`）：**
  - 在 `server.New` 初始化完成并打开 DB 后执行一次轻量同步 SQLite 重匹配：查询所有 `status = 'active' AND media_file_id IS NULL AND manually_bound = 0` 的字幕记录，按 `source_id` 分组与对应 `media_files` 运行新匹配算法并回写 `media_file_id`、`language`、`title`、`is_default`。
  - 纯数据库操作、零磁盘 I/O，老用户升级重启后无需重新扫描即可直接看到字幕生效。
- **手动绑定接口与编辑页入口：**
  - `LessonDetail` 增补来自 `media_files.source_id` 的 `source_id` 字段，确保手工组装的跨存储源课程也能定位该课时所属存储源的字幕列表。*(Our call)*
  - `GET /api/v1/sources/{id}/subtitles`：支持查询参数 `unmatched=true|false`、`prefix`、`search`，返回存储源下已索引的字幕文件（含 `id`, `relative_path`, `file_name`, `media_file_id`, `language`, `title`, `format`, `is_default`, `manually_bound`, `status`）。
  - `PUT /api/v1/subtitles/{id}/bind`：请求体 `{ "media_file_id": string | null, "title"?: string, "language"?: string, "is_default"?: boolean, "reset_auto"?: boolean }`；传有效 `media_file_id` 时绑定并置 `manually_bound = 1`（校验目标视频存在且归属合理）；传 `""` / `null` 时解绑并**保持 `manually_bound = 1`**（永久保持未绑定，不被后续扫描覆盖）；仅当 `reset_auto: true` 时清空 `media_file_id` 并重置 `manually_bound = 0` 交回自动匹配池。*(Our call)*
  - 在 `CourseEditorPage.tsx` 每个课时行的字幕区旁提供一个轻量的「关联字幕」按钮，打开弹窗展示当前课时已绑定的字幕（支持解绑）以及同存储源下的未匹配/全部字幕列表（支持搜索和一键关联）。

---

### 5.4 存储与导入页面的课程关联状态标识

#### 后端数据结构扩展 (`internal/catalog/service.go`, `internal/api/handlers_storage.go`)
- 新增轻量引用结构：
  ```go
  type CourseRef struct {
      ID    string `json:"id"`
      Title string `json:"title"`
  }
  ```
- **`StorageSource` 新增字段：**
  - `AssignedCount int` (`assigned_count`)：该存储源中状态为 `active` 且至少被一个 `lessons` 记录引用的去重视频数。
  - `UnassignedCount int` (`unassigned_count`)：`MediaCount - AssignedCount`（不小于 0）。
  - 在 `ListSources` 与 `GetSource` 的 SQL 中通过 `COUNT(DISTINCT CASE WHEN EXISTS (SELECT 1 FROM lessons l WHERE l.media_file_id = mf.id) THEN mf.id END)` 一次性计算，避免 N+1 查询。
- **`IndexedDir` 新增字段：**
  - `AssignedCount int` (`assigned_count`)：该子目录树下已入课的 `active` 视频数。
  - `UnassignedCount int` (`unassigned_count`)：该子目录树下未入课的 `active` 视频数。
  - `Courses []CourseRef` (`courses`)：引用了该子目录树下视频的去重课程列表（按标题排序）。
  - 在 `ListIndexedDirs` 中通过一次查询拉取该 `source_id` 下所有 `active` 文件及其关联的 `(course_id, course_title)` 映射后在内存中聚合，时间复杂度 $O(N)$。
- **`MediaFile` 新增字段：**
  - `Assigned bool` (`assigned`)：是否已被至少一门课程引用。
  - `Courses []CourseRef` (`courses`)：已加入的课程列表（未入课时为空数组 `[]`）。
  - 在 `ListMediaFiles` 中对返回的 `media_file_id` 批量查询 `lessons JOIN courses` 填充 `Assigned` 与 `Courses`。
  - `GET /api/v1/sources/{id}/files` 新增可选查询参数 `assigned`（`"true"` 仅看已入课，`"false"` 仅看未入课，空值看全部），与现有 `prefix` / `status` / `search` 组合生效。

#### 前端展示与交互 (`web/src/pages/StoragePage.tsx`)
- **存储源卡片：**
  - 元信息行展示：`已索引 {src.media_count} 个视频 · 未入课 {src.unassigned_count} 个`。
  - 当 `src.media_count > 0` 时，在卡片显著位置附状态徽章：
    - `src.unassigned_count === 0`：绿色徽章 `全部已入课`；
    - `src.unassigned_count > 0`：强调色徽章 `未入课 {src.unassigned_count} 个`。
- **子目录列表行：**
  - 副标题展示：`共 {dir.file_count} 个视频 · 直属 {dir.direct_files} 个 · 未入课 {dir.unassigned_count} 个`。
  - 目录名称右侧增加状态标签：
    - `dir.unassigned_count === 0 && dir.file_count > 0`：绿色 `已全部入课` + 所属课程快捷链接徽章（最多显示 2 个，超出显示 `+N` 并用 Tooltip 展示全部）；
    - `dir.assigned_count > 0 && dir.unassigned_count > 0`：琥珀色 `部分已入课` + 所属课程标签；
    - `dir.assigned_count === 0`：浅灰 `未入课`。
  - 已全部入课的目录仍保留「一键导入为课程」按钮（允许将同一素材组建成另一门课程），但视觉降权为次级样式并补充 Tooltip 提示 `"该目录下视频均已入课，仍可再次导入为新课程"`。*(Our call)*
- **媒体文件列表：**
  - 在现有文件状态筛选（`全部 | 正常 | 缺失`）旁新增入课状态筛选（`全部 | 未入课 | 已入课`），同步调用后端 `assigned` 参数。
  - 每个文件行右侧除 `正常 / 缺失` 徽章外，增加入课状态标识：
    - 已入课：显示 `已入课` + 可点击跳转的课程名徽章（如 `已入课 · Go 微服务实战`，多门课程时展示首门 `+N` 并通过 Tooltip 列出全部）；
    - 未入课：显示浅色 `未入课` 徽章。
- **导入后自动刷新：** `ImportCourseModal` 触发 `onImported` 后，自动重新拉取 `api.listSources()` 与当前路径的 `loadBrowseData(...)`，让所有入课计数和徽章即时更新。

---

### 5.5 播放页右侧课程目录环形进度指示器

#### 独立组件：`web/src/components/LessonProgressIcon.tsx`
- **Props：**
  - `played: boolean`
  - `progressRatio: number`（范围 `0` 到 `1`）
  - `isActive: boolean`
- **视觉与几何规范（`18px × 18px` SVG，`viewBox="0 0 20 20"`）：**
  - **已完成 (`played === true`)：** 渲染绿色 `CheckCircle2`（`text-status-success`），Tooltip `"已学完"`，`aria-label="已学完"`。
  - **未完成 (`played === false`)：**
    - 背景轨道圆：圆心 `(10, 10)`，半径 `7.5`，线宽 `2`，颜色 `text-ink-muted/30`（空心圆环）。
    - 进度弧线：同圆心与半径，周长 $C = 2\pi \times 7.5 \approx 47.124$，通过 `transform="rotate(-90 10 10)"` 从 12 点钟方向顺时针覆盖，`strokeDasharray={C}`、`strokeDashoffset={C * (1 - effectiveRatio)}`、`strokeLinecap="round"`，颜色 `text-accent`。
    - 当 `progressRatio > 0` 时，下限夹紧到 `0.04`（保证刚开始看几秒也能看到清晰进度起点），上限夹紧到 `0.99`（未标记完成前不与 100% 混淆）。
    - **当前播放项 (`isActive === true`)：** 在圆环正中额外绘制居中微缩播放三角（`polygon` 实心 `text-accent`），同时呈现「正在播放」与「当前播放进度」双重信息；Tooltip 为 `"正在播放 · 已观看 XX%"`（0% 时为 `"正在播放"`）。
    - **非当前项 (`isActive === false`)：**
      - `progressRatio > 0`：仅显示轨道圆 + 进度弧，Tooltip `"已观看 XX%"`；
      - `progressRatio === 0`：仅显示空心灰环，Tooltip `"未播放"`。
  - 节点附加 `data-progress-percent={pct}` 和 `aria-label`，便于屏幕阅读器与单测断言。

#### 实时进度同步 (`web/src/pages/CourseDetailPage.tsx`)
- 对列表中每节课，默认进度比由 `lesson.duration_ms > 0 ? lesson.position_ticks / (lesson.duration_ms * 10_000) : 0` 计算。
- 对当前正在播放的 `activeLessonId`，在 `CourseDetailPage` 维护轻量状态 `activeLiveProgress: { lessonId: string; ratio: number } | null`，由 `VideoPlayer` 的 `onTimeProgress`（按至少 1% 变化节流）驱动更新，让圆环随着播放平滑推进；当服务端通过 `onProgressSaved` 标记 `played = true`、用户手动切换已看/未看、或切换课时时同步清空/重置该临时状态。

---

## 6. Left to the Builder

- `Artplayer` 的具体控制栏按钮排布、字幕样式默认 CSS（字号档位具体像素值、背景阴影透明度）以及 `jsdom` 测试中的 Mock/封装细节。
- `ImportCourseModal` 内 AI 对比预览采用二级覆盖弹窗还是抽屉/内嵌切换视图（需保证与 `AIOrganizeModal` 视觉一致且键盘 `Escape` 仅关闭最顶层弹层）。
- 后端 `ffmpeg` 转码参数细节微调（例如 `-threads`、是否对极小或超长视频设置额外保护）与单元测试、集成测试组织方式。
- 构建同步嵌入前端静态资源（`make sync-web` / `make build`）并跑通全套 Go + Vitest 回归测试。
