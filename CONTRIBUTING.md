# Contributing to CourseFin / 参与贡献

欢迎向 CourseFin 提交 Issue、改进文档或发起 Pull Request！

## 开发环境要求

- **Go**: `1.26+`
- **Node.js**: `22+`
- **pnpm**: `12+`（项目在 `web/package.json` 中固定了 `pnpm@12.21.0`）
- **FFmpeg / FFprobe**: 需在系统 `PATH` 中可用（集成测试与端到端测试依赖真实 `ffmpeg` / `ffprobe`）

## 目录结构

```text
CourseFin/
├── cmd/coursefin/          # 服务入口与优雅停机
├── internal/
│   ├── config/             # 环境变量配置加载
│   ├── id/                 # 32 位小写十六进制 Jellyfin UUID
│   ├── db/                 # SQLite (WAL) 初始化与嵌入式 SQL 迁移
│   ├── auth/               # 单管理员认证、多设备会话与 Jellyfin 鉴权头解析
│   ├── storage/            # 可插拔 StorageProvider 接口与 LocalFS 驱动
│   ├── scanner/            # 增量扫描、ffprobe 探测、缩略图与外挂字幕匹配
│   ├── catalog/            # 媒体库/课程/章节/课时/标签、自然排序导入与进度同步
│   ├── media/              # DirectPlay Range 流、按需 -ss VOD HLS 转封装、字幕与图片缓存
│   ├── jellyfin/           # Jellyfin 协议兼容层与 DeviceProfile 协商
│   ├── ai/                 # OpenAI 兼容 AI 元数据整理客户端与严格校验
│   ├── api/                # CourseFin 管理端 REST API (/api/v1/*)
│   └── server/             # 统一路由装配、内嵌 SPA 与端到端测试
├── web/                    # React 19 + TypeScript + Vite + Tailwind CSS 前端
├── Dockerfile              # 多阶段单容器构建
├── docker-compose.yml      # Docker Compose 部署示例
└── Makefile                # 常用构建与测试命令
```

## 本地开发流程

1. **安装前端依赖**：
   ```bash
   pnpm --dir web install --frozen-lockfile
   ```
2. **启动后端服务**：
   ```bash
   mkdir -p data media
   COURSEFIN_PORT=8096 COURSEFIN_DATA_DIR=./data go run ./cmd/coursefin
   ```
3. **启动前端热更新（另开终端）**：
   ```bash
   pnpm --dir web dev
   ```
   Vite 开发服务器会自动将 `/api`、`/Items`、`/Videos`、`/System`、`/emby` 代理到 `http://localhost:8096`。
4. **构建并同步内嵌前端资源**：
   ```bash
   make sync-web
   ```

## 提交 PR 前的检查清单

1. **运行完整测试与静态检查**：
   ```bash
   go vet ./...
   make test
   ```
   确保所有 Go 测试（`-race` 模式）和前端 Vitest 用例全部通过。
2. **遵守核心架构原则**：
   - **绝不修改媒体存储卷中的任何原始文件**（所有只读媒体路径都必须通过 `StorageProvider` 访问并严格校验路径边界）。
   - **不破坏 Jellyfin 客户端兼容契约**（稳定 32 位小写 hex ID、PascalCase DTO、HTTP Range 与统一 Tick 进度）。
   - **AI 与扫描操作绝不静默覆盖用户已手工编辑的课程元数据**。
3. **提交信息规范**：推荐使用 Conventional Commits（如 `feat:`、`fix:`、`docs:`、`refactor:`、`test:`、`chore:`）。
4. **许可证说明**：向本仓库提交贡献即表示您同意所贡献的代码遵循本项目的 [MIT License](LICENSE)。
