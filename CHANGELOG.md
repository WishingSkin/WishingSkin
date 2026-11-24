# 更新日志

本文件记录 WishingSkin 各版本的主要变更。版本号遵循 [Semantic Versioning 2.0.0](https://semver.org/)，详见[开发指南](docs/development-guide.md)。

## [Unreleased]

- 数据库升级至 Schema 26（`users.character_limit`、`texture_records.created_at` 与 `texture_records_created_idx` 索引，同步纳入离线备份与恢复契约）。
- 新增 Yggdrasil 角色名 / 非邮箱登录开关（`feature.non_email_login`，`PATCH /api/admin/settings` `yggNonEmailLogin`）：启用后允许玩家在 `authlib-injector` 启动器直接填写角色名与密码登录，并自动选中对应角色；同步展示于游戏接入面板。
- 新增公开材质独立可分享详情链接（`/library/{id}`，含前端一键复制分享链接按钮、公开库 `sort=newest` 按最新上传排序及展示上传日期；首页橱窗直达对应材质详情）。
- 新增管理员单用户运营控制（`GET /api/admin/users` 返回积分、角色数与独立上限，`PATCH /api/admin/users/{id}` 与用户管理弹窗支持调整积分并写入流水与审计、设置/清除单用户独立角色上限、管理员重置用户密码并立即撤销全部网页会话与游戏令牌）。

## [0.1.1] - 2026-11-24

### 新增

- 新增后台“站点设置 → 存储与备份 → 存储配额”可视化配置（单账号与全站配额，MiB 为单位，`0` 表示不限，保存后热生效并覆盖环境变量默认值，同时纳入字段级审计记录）。
- 新增后台历史公告删除接口（`DELETE /api/admin/notices/{id}`）与品牌设置页历史公告管理列表，支持清理不再需要的历史公告（删除当前生效公告时同步撤下首页展示并写入审计日志）。

### 文档

- 同步修正 `README.md` 与多篇公开手册中的 Schema 25 与 `v0.1.0` 特性说明，补充 GitHub Releases 预编译发行包与 `ghcr.io/wishingskin/wishingskin` 镜像使用指引。

## [0.1.0] - 2026-11-21

WishingSkin 首个公开版本（数据库 Schema 25）。

### 核心能力

- **账号与角色管理**：邮箱密码注册/登录、邮箱验证与找回、角色多槽位与稳定 UUID、皮肤与披风独立绑定及按角色覆盖标准/纤细手臂模型；支持 TOTP 两步验证、一次性恢复码与管理员手动激活。
- **游戏外置登录与兼容接口**：完整 Yggdrasil / authlib-injector 认证与 RSA 签名接口、一键拖拽启动器接入链接、CustomSkinLoader 与传统材质直链兼容。
- **材质库与社区互动**：按内容 hash 去重存储、单账号/全站存储配额与用量进度条、公开材质库与个人衣柜、材质点赞、近期热门 / 点赞最多 / 收藏最多榜单、首页热门材质橱窗、上传审核与举报处置、增强 3D 预览（动作预设、外层饰品开关、披风/鞘翅切换、2× 立绘 PNG 导出）。
- **主题系统与界面定制**：内置默认 + 13 套主题（含 8 套 Minecraft 群系/维度地上地下双幕实景壁纸与全群系原版像素氛围粒子）、Minecraft `client.jar`/资源包 112 键贴图与粒子提取、3 套 SIL OFL 1.1 开源像素字体及自定义 WOFF2 / 外链 CSS 字体、面板实色/半透明/毛玻璃实时预览调节与用户本地主题偏好。
- **认证扩展与插件体系**：多 OIDC 提供商接入、内置 OAuth2/OIDC 授权服务（PKCE S256 + Refresh Token 轮换）、AuthMe/HTTP 外部账号连接器、S3 对象存储与短期签名 CDN、官方插件模块与支持热加载/受控前端/SQL 迁移/持久 Webhook 的动态可执行插件。
- **运维、备份与迁移**：MySQL 8 与 PostgreSQL 16 双库支持、管理后台运行概览与字段级审计日志、离线与每日定时完整备份及材质来源完整性核验、Blessing Skin 6.0.2 只读导出/预检/隔离迁移工具。
