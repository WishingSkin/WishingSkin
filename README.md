# WishingSkin

可自部署的开源 Minecraft 皮肤站，支持角色与皮肤管理、外置登录、插件扩展和界面个性化。

采用 Go 模块化单体与 Vue 3 / TypeScript / Vite，前端静态资源由后端提供，支持 MySQL 8 与 PostgreSQL。

当前程序使用 schema 19。支持本地账号、邮箱验证与找回、TOTP、角色与材质管理、衣柜和公开库、标签分类、可选上传审核、Yggdrasil 外置登录、浅深主题和个性化设置。新账号默认使用递增 ID，可切换为 10 位随机数字；已有 ID 保持不变。

新皮肤接受 64×64、64×32、128×128、128×64 PNG，单个文件最大 8 MiB。披风使用独立尺寸规则，既有和迁入材质不按新上传规则重新校验。

官方插件提供邀请码、积分、多 OIDC、本站 OAuth2 授权、AuthMe/HTTP 外部账号连接及 S3 对象存储。社区插件通过 Go SDK 随源码编译，可独立启停和配置，支持路由、事件、内容页及任务。

支持从固定版本的 Blessing Skin 6.0.2 迁入账号、角色实际 UUID、材质及关系。支持的插件版本和数据范围见迁移手册。报告使用 partial 标明迁移范围，原 PHP 插件需要重新实现。

运行时只需程序、一个选定数据库、材质目录和设置密钥。前端构建需要 Node.js 与 pnpm，完整构建步骤见部署手册。新安装没有默认管理员，通过本机 create-admin 初始化；从 BS 迁入时不要先创建管理员。生产使用 HTTPS 反向代理并持久化数据库、材质、备份目录和设置密钥。

- [部署手册](docs/deployment.md)
- [升级与回退](docs/upgrade-runbook.md)
- [备份与恢复](docs/backup-restore.md)
- [Blessing Skin 迁移](docs/migration-runbook.md)
- [BS 来源契约](docs/bs-source-contract.md)
- [游戏、账号与存储接入](docs/integrations.md)
- [社区插件 SDK](docs/community-plugin-sdk.md)
- [界面与首页设置](docs/interface-customization.md)
- [功能与限制](docs/feature-matrix.md)
- [架构与数据模型](docs/architecture.md)
- [开发指南](docs/development-guide.md)
- [MIT 许可证](LICENSE)
- [第三方依赖与许可证](THIRD_PARTY_NOTICES.md)
