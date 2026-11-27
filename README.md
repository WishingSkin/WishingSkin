<p align="center">
  <img src="docs/assets/logo.svg" alt="WishingSkin Logo" width="112" height="112" />
</p>

<h1 align="center">WishingSkin</h1>

<p align="center">
  可自部署的开源 Minecraft 皮肤站与外置登录平台
</p>

<p align="center">
  Go 单体后端 | Vue 3 前端 | MySQL 8 / PostgreSQL | Yggdrasil 外置登录 | 插件扩展 | MC 实景主题系统
</p>

<p align="center">
  <a href="https://github.com/WishingSkin/WishingSkin/releases"><img src="https://img.shields.io/github/v/release/WishingSkin/WishingSkin" alt="Release" /></a>
  <a href="https://github.com/WishingSkin/WishingSkin/actions/workflows/ci.yml"><img src="https://github.com/WishingSkin/WishingSkin/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/WishingSkin/WishingSkin" alt="License" /></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8" alt="Go 1.26" />
  <a href="https://github.com/WishingSkin/WishingSkin/pkgs/container/wishingskin"><img src="https://img.shields.io/badge/Docker-GHCR-2496ED" alt="Docker GHCR" /></a>
</p>

<p align="center">
  <a href="#快速开始">快速开始</a> |
  <a href="#核心特性">核心特性</a> |
  <a href="#官方插件生态">官方插件生态</a> |
  <a href="#文档导航">文档导航</a> |
  <a href="#社区反馈与贡献">社区反馈与贡献</a>
</p>

---

WishingSkin 是一个面向自托管服主与社区的开源 Minecraft 皮肤站。单个二进制文件即可提供网页端、皮肤与披风管理、Yggdrasil（authlib-injector）认证、CustomSkinLoader 兼容接口、官方/社区插件运行以及完整的备份恢复工具，支持 MySQL 8 与 PostgreSQL（当前数据库 schema 29）。

## 核心特性

- **角色与外观管理**：支持多角色、稳定 UUID（改名不变）、可配置的高清材质上传策略（管理员可调皮肤 `64–1024 px`、披风 `64–1024 px` 与文件大小 `1–8 MiB`，上传支持拖拽与本地预检）、静态模型缩略图与增强 3D 预览（待机/行走/奔跑动作、外层饰品开关、披风/鞘翅切换、2× 立绘导出 PNG）、标准/纤细手臂覆盖与个人衣柜；支持全站默认与单用户独立角色上限覆盖。
- **社区互动与榜单**：支持公开材质点赞、近期热门 / 最新上传 / 点赞最多 / 收藏最多榜单、公开材质独立可分享详情链接（`/library/{id}`）、首页热门材质橱窗，以及顶栏个人通知收件箱（覆盖审核、举报、下架、积分与账号变更，可选邮件通知）；支持按账号与全站配置材质存储配额（含用量进度条与后台热调整）。
- **游戏接入**：内置 Yggdrasil 外置登录（支持 `authlib-injector` 一键拖拽启动器接入链接及可选开启角色名/非邮箱登录 `feature.non_email_login`，兼容 PCL2、HMCL、BakaXL 与 Prism Launcher），同时提供 CustomSkinLoader 配置导出与传统链接模板。
- **账号与安全**：本地邮箱验证与找回、管理员审核激活、邮箱域名白黑名单、Cloudflare Turnstile、TOTP 两步验证与一次性恢复码、可切换递增或 10 位随机用户 ID；后台支持直接调整单用户积分余额、角色上限并重置密码（自动撤销所有会话与游戏令牌）；工作台内置未保存编辑离开/弹窗误关保护与统一一键复制操作。
- **主题与个性化**：内置默认 + 13 套主题（含 8 套 Minecraft 地上/地下成对群系实景壁纸与全群系原版像素粒子）、支持上传 `client.jar`/材质包服务端白名单提取、内置开源像素字体与自定义 WOFF2 / 外链 CSS 字体、半透明/毛玻璃实时调节、中英文文案覆写、多区块首页及历史公告管理。
- **插件与第三方集成**：内置邀请码（支持默认单次永久及自定义限时/限次模式、按可用状态导出）、积分与活动积分码（每日签到、扣费配置及限时/限次积分码批量生成、导出与用户兑换）、正版 Minecraft Java 验证绑定（Microsoft / Xbox Live / Minecraft 权益校验，不修改既有角色 UUID）、多 OIDC 登录、本站 OAuth2/OIDC 授权服务（支持应用权限白名单及只读资源 API Scope）、AuthMe/HTTP 外部账号连接与 S3 兼容对象存储；支持 Go SDK 源码插件和 Plugin Runtime v2 动态可执行插件（官方 Ed25519 签名、同步审核钩子、Host API、用户端工具工作区与官方静态市场源）。
- **运维与迁移**：管理后台运行概览与字段级操作审计、单二进制 CLI 批量材质导入（`import-textures`，支持 `--dry-run` 试跑、配额与审核遵循）、每日定时备份、完整性校验与隔离恢复、公开材质签名 CDN、孤立文件回收，以及从固定版本 Blessing Skin 6.0.2 迁入账号、角色 UUID、材质与衣柜数据。

## 官方插件生态

WishingSkin 通过 Plugin Runtime v2 支持动态可执行插件，自 `v0.3.0` 起已内置官方 Ed25519 验签公钥。管理员在后台「插件管理 → 插件市场」点击「填入官方市场源」（或订阅 [`https://wishingskin.github.io/plugins/catalog.json`](https://wishingskin.github.io/plugins/catalog.json)）即可一键校验 SHA-256 与签名安装，插件源码与独立发布维护于 [WishingSkin/plugins](https://github.com/WishingSkin/plugins)。

| 插件标识 | 名称 | 当前版本 | 功能简介 |
| --- | --- | --- | --- |
| `wishingskin.moderation` | 合规校验与内容审核 | `0.1.0` | 敏感词、RE2 正则、空白/纯色图检测与外部审核 Webhook |
| `wishingskin.skin-overlays` | 皮肤装扮叠层 | `0.1.0` | 在现有皮肤上叠加透明装饰层，一键预览、合成与穿戴 |
| `wishingskin.skin-editor` | 在线皮肤编辑器 | `0.1.0` | 带 UV 分区标注、对称绘制与实时预览的 64×64 像素工作台 |
| `wishingskin.skin-tools` | 皮肤格式与调色工具 | `0.1.0` | 64×32 升级、Steve/Alex 手臂转换、HSL 调色与杂点清理 |
| `wishingskin.render-card` | 角色渲染与玩家名片 | `0.1.0` | 角色头像、全身渲染图与可嵌入 README / 论坛的 SVG 名片 |
| `wishingskin.server-sync` | 社群与游戏服务器联动 | `0.1.0` | 推送站内事件至 Discord / KOOK / 通用 Webhook 并提供查询接口 |
| `wishingskin.ai-skin` | AI 皮肤工坊（测试版） | `0.1.0-beta.1` | 文本 D2-Strict 像素精绘、图像 B2-v2 正交投影与外接算力节点 |

## 快速开始

### 使用预编译包或官方镜像

- **预编译二进制**：前往 [GitHub Releases](https://github.com/WishingSkin/WishingSkin/releases) 下载对应平台压缩包（提供 Linux / Windows / macOS 的 `amd64` 与 `arm64` 构建及 `SHA256SUMS.txt`）。
- **Docker 镜像**：多架构官方镜像发布在 `ghcr.io/wishingskin/wishingskin`（例如 `ghcr.io/wishingskin/wishingskin:0.3.0` 或 `:latest`），可替换 `deploy/compose.*.yaml` 中的 `build:` 直接运行。

### Docker Compose（推荐）

仓库在 `deploy/` 目录提供 PostgreSQL 与 MySQL 两套编排文件（二选一，不要同时启动）：

**Linux / macOS**：

```bash
cp .env.example .env
# 编辑 .env，设置 WS_PUBLIC_URL、WS_DATABASE_DSN 及数据库密码

docker compose --env-file .env -f deploy/compose.postgres.yaml build app
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d postgres
docker compose --env-file .env -f deploy/compose.postgres.yaml run --rm --no-deps app create-admin
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d app
```

**Windows (PowerShell)**：

```powershell
Copy-Item .env.example .env
# 编辑 .env，设置 WS_PUBLIC_URL、WS_DATABASE_DSN 及数据库密码

docker compose --env-file .env -f deploy/compose.postgres.yaml build app
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d postgres
docker compose --env-file .env -f deploy/compose.postgres.yaml run --rm --no-deps app create-admin
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d app
```

> [!NOTE]
> 如果是新站初始化，使用 `create-admin` 在终端创建首个管理员；如果准备从 Blessing Skin 迁入旧数据，请跳过 `create-admin`，直接按[迁移手册](docs/migration-runbook.md)执行。

### 从源码构建

需要 Go 1.26.11、Node.js 24.19.0、pnpm 11.24.0 以及已创建的 PostgreSQL 或 MySQL 数据库。前端构建与 Go 编译需串行执行：

**Linux / macOS**：

```bash
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -trimpath -o bin/wishingskin ./cmd/wishingskin
```

**Windows (PowerShell)**：

```powershell
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

配置好数据库连接与公开地址环境变量后，先运行 `./bin/wishingskin create-admin`（Windows 为 `.\bin\wishingskin.exe create-admin`）创建管理员，再执行 `./bin/wishingskin serve`（Windows 为 `.\bin\wishingskin.exe serve`）启动服务。

## 文档导航

### 上手与配置

- [使用指南（玩家与站长）](docs/user-guide.md)：角色管理、皮肤上传规范、启动器与服务端接入、日常后台管理
- [配置与 CLI 参考](docs/configuration.md)：全部环境变量、默认值与 9 个命令行子命令说明
- [界面与首页设置](docs/interface-customization.md)：品牌文案、多区块首页、面板效果与高级颜色覆写
- [主题包、材质与字体指南](docs/themes-and-assets.md)：14 套主题、实景壁纸、`client.jar` 材质提取与像素字体说明
- [功能与限制](docs/feature-matrix.md)：全量功能清单与明确不支持的边界

### 部署与运维

- [生产自部署手册](docs/deployment.md)：反向代理、TLS、Docker Compose 与原生二进制部署
- [升级、回退与恢复手册](docs/upgrade-runbook.md)：版本升级前检查、平滑升级与隔离回退流程
- [备份与恢复](docs/backup-restore.md)：数据保存范围、定时备份、离线 CLI 备份与空目标恢复
- [游戏、账号与存储接入](docs/integrations.md)：Yggdrasil、OIDC、AuthMe/HTTP 连接器、S3 存储与 CDN

### 扩展、迁移与开发

- [本站 OAuth2 / OIDC 授权服务](docs/oauth-provider.md)：将 WishingSkin 作为第三方应用的身份提供商
- [社区插件 SDK](docs/community-plugin-sdk.md)：源码插件、动态 ZIP/市场包、iframe 前端与 Webhook 契约
- [Blessing Skin 迁移手册](docs/migration-runbook.md)：从 BS 6.0.2 导出、预检查到空目标导入的完整步骤
- [BS 来源契约](docs/bs-source-contract.md)：冻结源码版本、字段映射与预检查校验规则
- [架构与数据模型](docs/architecture.md)：目录职责、材质分层与系统边界
- [开发指南](docs/development-guide.md)：本地开发、双库测试与前端视觉规范

## 社区反馈与贡献

- **问题反馈与需求建议**：请在 [Issues](https://github.com/WishingSkin/WishingSkin/issues) 中按表单提交 Bug 或功能建议；插件本身的问题请提交至 [WishingSkin/plugins](https://github.com/WishingSkin/plugins/issues)。
- **使用交流与提问**：欢迎前往 [GitHub Discussions](https://github.com/WishingSkin/WishingSkin/discussions) 讨论部署、主题与插件用法。
- **参与开发与版本记录**：提交代码前请阅读 [贡献指南](CONTRIBUTING.md)，版本变更记录见 [CHANGELOG.md](CHANGELOG.md)。
- **安全漏洞上报**：切勿公开披露漏洞细节，请按 [安全政策](SECURITY.md) 通过 GitHub Private Vulnerability Reporting 私密提交。

## 许可证

- 项目源码采用 [MIT 许可证](LICENSE)。
- 第三方依赖、图标与开源像素字体许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
