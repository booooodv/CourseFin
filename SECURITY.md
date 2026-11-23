# Security Policy / 安全策略

## Supported Versions / 支持版本

| Version | Supported |
| --- | --- |
| `1.0.x` | ✅ |

## Reporting a Vulnerability / 漏洞报告

如果您在 CourseFin 中发现安全漏洞，请**不要**直接创建公开的 GitHub Issue。

请通过以下方式私密反馈：
- 在 GitHub 仓库的 **Security → Advisories → Report a vulnerability** 页面提交私密漏洞报告。

请在报告中尽量包含复现步骤、受影响的端点或组件，以及可能的影响评估。我们会在收到报告后尽快确认并发布修复。

---

## Self-Hosting Security Checklist / 自托管部署安全建议

CourseFin 定位为面向个人与内网/NAS 的自托管课程管理平台。如果计划将实例暴露到公网，请务必做好以下防护：

1. **使用反向代理并启用 HTTPS 与登录限流**
   - 请通过 Nginx、Caddy、Traefik 或 Cloudflare Tunnel 终止 TLS（HTTPS），避免在公网明文传输登录凭证与会话 Token。
   - CourseFin v1.0 为单管理员模式且未内置 IP 级登录限流（仅使用 `bcrypt` 哈希降速），若面向公网开放，强烈建议在反代层对 `/api/v1/auth/login` 与 `/Users/AuthenticateByName` 配置速率限制。
2. **妥善保护 `/data` 数据卷权限**
   - `/data/coursefin.db`（SQLite）保存了用户会话与 AI 提供商配置的 `ai_api_key`（对外查询接口已脱敏，但数据库文件内为本地调用明文存储）。
   - 请限制宿主机 `./data` 目录的文件系统权限，切勿将 `/data` 目录提交至 Git 或暴露在公开 Web 目录中。
3. **保持媒体目录只读挂载（`:ro`）**
   - CourseFin 遵循只读访问原则，推荐始终以 `-v /path/to/courses:/media:ro` 挂载课程文件目录，从容器层杜绝误改原始文件的可能。
4. **严格保管管理员账号（存储路径与 AI 端点边界）**
   - 管理员拥有添加服务器本地存储源路径以及配置外部 AI `base_url` 接口的权限（存在管理员侧 SSRF 能力），切勿将管理员账号密码分享给不可信用户。
   - 如果不希望将课程字幕片段发送给外部 AI 服务商，请在「设置 → AI 提供商」中将「最大字幕字符数」设为 `0`。
5. **图片接口匿名访问说明**
   - 为了兼容官方 Jellyfin 客户端与第三方播放器对 `<img>` 封面的加载行为，`GET /Items/{itemId}/Images/*` 允许免 Token 访问，依靠 32 位随机 UUID 隔离；请勿在课程封面上存放敏感隐私图片。
