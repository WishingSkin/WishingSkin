# 更新日志

本文件记录 WishingSkin 各版本的主要变更。版本号遵循 [Semantic Versioning 2.0.0](https://semver.org/)，详见[开发指南](docs/development-guide.md)。

## [Unreleased]

## [0.3.0] - 2026-11-27

### 新增

- Plugin Runtime v2（`protocol: 2`）：扩展 `pkg/plugin` SDK（新增 `UserFrontend`、`HookRequest` / `HookDecision`、`RuntimeResponse` 二进制响应字段与 `ServeRuntimeV2`），管理端区分展示“官方扩展”与“官方签名”徽标。
- Ed25519 官方插件签名与内置信任公钥：在 `internal/runtimeplugin` 中内置正式官方发布公钥（`BuiltInOfficialPublicKeys()`），支持通过 `WS_OFFICIAL_PLUGIN_PUBKEYS` 追加逗号分隔公钥；仅验签通过的动态包可使用 `wishingskin.` 命名空间。
- 同步合规审核钩子（`hooks`、`hooks.image`）：支持在账号改名、角色改名及材质上传落库前触发 `text.check` 与 `texture.upload.check` 进行拦截、放行或转审。
- 内部 Host API 与扩展事件：宿主仅监听 `127.0.0.1:0` 提供按插件 Bearer Token 鉴权的受控接口（支持读取站点、角色、材质字节，并按权限写入新材质、穿戴绑定与扣减积分）；事件系统新增 `character.rename`、`character.delete`、`minecraft.link`、`minecraft.unlink`。
- 用户端工具工作区与材质动作槽位：新增 `/user/tools/{pluginId}/{frontendId}` 工具箱与材质详情动作入口，提供主题/字体令牌同步、材质选择器、本地文件导出、右侧 3D 实时预览停靠栏、跨工具一键跳转与 `wishingskin:ready` 握手。
- 官方静态插件市场接入与安全重定向下载：后台插件市场支持一键填入官方市场源（`https://wishingskin.github.io/plugins/catalog.json`）；市场目录与包下载客户端支持最多 5 跳的安全重定向，拦截私网/回环/元数据 IP 与 HTTPS 降级跳转，兼容 GitHub Releases 302 下载链路。

### 改进与修复

- 优化 `PluginFrontend` 与 `UserToolsWorkspace` 跨工具上下文传递，修复响应式 `Proxy` 引发的结构化克隆异常及下载/变更通知桥接字段兼容性。
- 修复 PostgreSQL 集成测试并发执行时查询全局 `pg_indexes` 引起的偶发 OID 竞态。

### 兼容性

- 数据库维持 Schema 29，无新增表结构迁移，可从 `v0.2.0` 原地平滑升级并完全兼容既有 `protocol: 1` 社区插件。

## [0.2.0] - 2026-11-25

### 新增

- 新增正版 Minecraft Java 账号验证与绑定插件（`wishingskin.minecraft`，默认关闭）：支持管理员加密配置 Microsoft Entra 应用凭据，用户通过 Microsoft OAuth2 + PKCE S256 → Xbox Live → XSTS → Minecraft Services 校验 Java 版持有权限后显式确认绑定（支持带密码解绑与审计，不改变已有角色 UUID）。
- 新增个人通知收件箱与可选邮件投递：提供 `/api/notifications` 游标分页、未读计数与已读接口，顶栏 `NoticeCenter` 集成未读角标、个人通知与站点公告双栏切换；审核结果、举报处置、管理员下架及用户积分/角色上限/手动邮箱验证均在事务内写入通知，支持在“邮件与验证”开启个人通知邮件（`notificationEmailEnabled`）。
- 新增 OAuth2 窄范围客户端白名单与只读 API（`wishingskin.oauth2`）：新增 `characters:read`、`textures:read`、`points:read`、`minecraft:read` 四个只读资源 Scope 与对应的 `/oauth/api/{characters,textures,points,minecraft}` Bearer 端点；管理员可限制每个应用允许申请的权限范围，授权同意页按身份与只读数据清晰分组并支持浅深自适应，用户「已授权的应用」合并展示全部有效权限。
- 新增活动积分码生成、导出与兑换（`wishingskin.points`）：支持管理员生成单次永久或自定义可用次数（1–10000）与有效时长（0–8760 小时）的活动积分码、查看最近状态与一键停用、支持 TXT/CSV 重复导出（审计日志不记录明文）；用户可在积分页输入并原子兑换（同用户单码限领一次），到账记入流水并推送个人通知（可选邮件投递）。
- 新增限时/限次邀请码模式（`wishingskin.invitations` v1.2.0）：保留默认单次、永久有效生成规则，可选自定义单码可用次数（1–1000）与有效时长（0–8760 小时）；支持按可用（未用完且未过期）或全部导出，CSV 扩展 `remaining_uses` 与 `expires_at` 列。
- 新增可配置的高清材质上传策略（`content_policy`）：管理员可在「材质管理 → 上传与审核设置」配置皮肤最大宽度（`64`/`128` 默认/`256`/`512`/`1024`）、披风最大宽度（`64–1024`）及 PNG 文件上限（`1–8 MiB`），HTTP 上传与 `import-textures` CLI 在事务内统一校验，上传要求面板同步展示。
- 新增单二进制批量材质导入 CLI 子命令（`import-textures`）：支持目录/递归扫描、`--owner` 指定所有者（UID 或邮箱）、`--kind=auto|skin|cape` 自动推断、`--dry-run` 零写入预检、单用户去重跳过（`--skip-duplicates`）、上传审核策略遵循或 `--skip-review`，复用 PNG / 尺寸 / 标签 / 配额 / S3 同步逻辑并输出结构化 JSON 报告。
- 新增 Yggdrasil 角色名 / 非邮箱登录开关（`feature.non_email_login`，`PATCH /api/admin/settings` `yggNonEmailLogin`）：启用后允许玩家在 `authlib-injector` 启动器直接填写角色名与密码登录，并自动选中对应角色；同步展示于游戏接入面板。
- 新增公开材质独立可分享详情链接（`/library/{id}`，含前端一键复制分享链接按钮、公开库 `sort=newest` 按最新上传排序及展示上传日期；首页橱窗直达对应材质详情）。
- 新增管理员单用户运营控制（`GET /api/admin/users` 返回积分、角色数与独立上限，`PATCH /api/admin/users/{id}` 与用户管理弹窗支持调整积分并写入流水与审计、设置/清除单用户独立角色上限、管理员重置用户密码并立即撤销全部网页会话与游戏令牌）。

### 改进与修复

- 交互优化与未保存保护：新增编辑与一次性密钥弹窗关闭确认并修复拖选文字误关遮罩；建立全站路由、标签页、`popstate` 返回与 `beforeunload` 离开防护（保留个性化切换草稿并标示未保存圆点，支持 `Ctrl/Cmd+S`）；新增统一复制按钮 `CopyButton`、PNG 上传拖拽与本地宽高/大小预检。
- 完善 P0 安全、状态与迁移一致性：管理员写事务加锁后复核网页会话有效性；`GET /api/site` 与 `GET /api/characters` 分离全站默认与个人角色上限；Yggdrasil 角色名登录在签发令牌事务内读取一致的角色快照；Blessing Skin 迁移导出/导入保留 `textures.upload_at -> texture_records.created_at`；前端分享详情弹窗区分 HTTP 404 与临时加载失败并提供重试。

### 数据库与兼容性

- 数据库升级至 Schema 27（为 PostgreSQL 补充 `characters(user_id)` 索引 `characters_user_id_idx`，对齐 MySQL 外键索引）。
- 数据库升级至 Schema 28（新增 `user_notifications` 个人通知收件箱表及索引、`minecraft_verifications` 正版账号绑定表、`minecraft_flows` 短期授权状态表；完整备份保留前两张业务表并排除 `minecraft_flows`）。
- 数据库升级至 Schema 29（为 `oauth_clients` 增加 `allowed_scopes` 客户端权限白名单列，并新增 `point_codes` 与 `point_code_redemptions` 活动积分码及防重领表；离线备份保留 `allowed_scopes` 并比照邀请码将活动积分码与兑换表列为凭据排除）。

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
