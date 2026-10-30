# BS 功能对照与首版范围

更新日期：2026-10-30。用途：记录 WishingSkin 功能范围与扩展能力。本文记录范围与来源，不逐项表示实现状态；生产操作见[部署、升级与迁移手册](deployment.md)。行为规格和首版验收任务见[首版实施提案](../openspec/changes/build-wishingskin-v1/proposal.md)。

归属以 BS 核心 `dev` 分支和官方插件仓库 `master` 分支的当前源码为准，对应版本和来源配置需按迁移契约核对。“扩展候选”按用途选择，不表示已核实插件使用热度。第三方插件不在本次完整盘点范围内。

B01–B12、E01–E05、N05–N06 和 M01–M04 属于当前功能范围，按用途实现，不要求复刻 BS 的内部实现或页面。其余条目保留为后续或可选能力，按部署需求选择。

## BS 核心能力

| 编号 | 功能 | BS 对照 | WishingSkin 建议 | 来源 |
| --- | --- | --- | --- | --- |
| B01 | 账号注册、登录、退出、密码修改和找回 | 核心账号流程 | 使用本地账号，包含找回 | S1 |
| B02 | 邮箱验证与发送邮件 | 核心支持注册邮箱验证 | 默认邮件验证；无 SMTP 可选管理员验证，并记录管理操作 | S1 |
| B03 | 多角色、角色改名、删除、材质绑定 | 核心支持一账号多角色及角色名规则 | 新站默认每账号 1 个角色、管理员可调整；迁入角色保留；应用皮肤时可为当前角色选择标准或纤细手臂 | S1、S2 |
| B04 | 皮肤/披风上传、公开/私有、名称、模型类型 | 核心材质管理 | 单个 PNG 最大 8 MiB；新皮肤仅允许 64×64、64×32、128×128、128×64；披风沿用原尺寸规则，默认未公开；相同文件可有独立材质记录 | S3 |
| B05 | 衣柜收藏、重命名、移除、应用到角色 | 核心衣柜；`likes` 与加入/移出衣柜关联 | 独立点赞系统暂不另做 | S4 |
| B06 | 公开皮肤库、详情、搜索、过滤、排序 | 核心有名称关键词、类型/上传者过滤和排序 | 不把标签搜索冒充核心已有功能 | S3 |
| B07 | 积分与签到 | 核心；可配置签到奖励、业务成本与奖励 | 默认关闭，管理员配置北京时间每日奖励、上传费用与新建角色费用，无其他隐式费用 | S1、S4、S5 |
| B08 | 举报、处理进度、后台处置 | 核心；管理员可驳回、删除材质、封禁上传者 | 这是发布后治理，上传前审核另见 N02 | S6 |
| B09 | 用户、角色、材质、衣柜和权限管理 | 核心管理后台 | 普通用户/管理员两级，高影响操作有确认及审计，保护最后可用管理员 | S1 |
| B10 | 站点信息、通知、外观配置与多语言 | 核心提供站点配置、自定义和国际化 | 支持品牌、多语言文案、浅深配色、面板效果和可命名的多块图文首页；简体中文与 English，默认简体中文 | S1、S7 |
| B11 | 验证码与请求防滥用 | 核心有图形验证码，积分用于限制滥用 | 登录/注册/上传限流，Turnstile 用于每次注册及网页登录累计失败后 | S1、S7 |
| B12 | 业务 API | 核心提供用户、角色、衣柜及管理 API，部分受 OAuth scope 保护 | 提供 WishingSkin 自身 API，不默认兼容 BS 全套后台或写入 API | S8 |

B04 的现代预览、B11 的具体限流方式属于新站设计建议，不表示 BS 对应实现完全相同。

## 官方扩展与生态接入

| 编号 | 功能 | BS 对照 | WishingSkin 建议 | 来源 |
| --- | --- | --- | --- | --- |
| E01 | Yggdrasil / authlib-injector 外置登录 | `yggdrasil-api`；另有 `yggdrasil-connect` | 验收登录、令牌生命周期、进服验证、资料与材质签名 | S9、S10 |
| E02 | 旧材质地址、角色公开查询和加载配置 | `legacy-api`、`config-generator`；核心也有材质公开接口 | 按需兼容；列出实际 URL、响应格式和客户端再确定支持矩阵 | S1、S11 |
| E03 | 注册邀请码 | `invitation-codes` | 随程序发行的可启停官方模块；独立插件页设置是否必填、使用次数/有效期、批量生成及 TXT/CSV 导出，仍须验证邮箱 | S12 |
| E04 | 开放/关闭注册、每账号角色数量限制 | `disable-registration`、`single-player-limit` | 内建；作为站点设置，不需要管理员另装插件 | S12 |
| E05 | 材质描述 | `texture-description` | 内建；提供简短描述，富文本不作为默认需求 | S12 |
| E06 | 注册邮箱域名白/黑名单 | `restricted-email-domains` | 已实现完整域名白/黑名单，黑名单优先；普通与 OIDC 注册提交前复核 | S12 |
| E07 | 第三方网页登录 | `oauth` 客户端核心及 GitHub、Google、LittleSkin、Microsoft Live 登录插件 | OIDC 官方插件支持最多 12 个提供商登录、绑定和保留本站注册规则的新账号创建；插件启停与外观更新见 [手动验收清单](manual-acceptance.md) | S12 |
| E08 | 把皮肤站作为其他应用的 OAuth2 授权服务 | 核心已有 OAuth2 应用管理及受 scope 保护的 API | 已实现独立 OAuth2 插件：授权码 + PKCE S256、profile/email 只读 scope、同意与撤销；无 refresh token 或 ID token | S1、S8 |
| E09 | Minecraft 正版验证及绑定 | `mojang-verification` | 建议后续；不能用普通 Microsoft 登录成功代替正版持有验证 | S13 |
| E10 | AuthMe、论坛等既有账号系统对接 | 官方 README 列为插件能力，具体连接器需按源站盘点 | 已实现 AuthMe 只读 MySQL 与通用 HTTP 主动绑定；不提供外部密码直接登录 | S7、S12 |
| E11 | 插件扩展能力 | BS 运行 PHP 插件，调用 Laravel 容器、路由和事件等 | Go SDK 支持编译接入、启停/配置、路由、事件、管理员结构化内容页及手动/固定间隔任务；SQL 迁移执行与动态安装未实现，原 PHP 插件不能原样运行 | S12、S14 |

E01 的两个 BS 插件存在迁移差异：Connect 的 UUID 关联方式及令牌实现与传统插件不同，不能仅凭 BS 主版本决定导入规则。Connect 配合 Janus 的 OAuth/OIDC 游戏授权也不等于“核心自带通用 OIDC”。

## 新站增强与运维候选

以下用于选择新项目功能，不能据此声称 BS 核心或所有插件都没有相应能力。本次没有核实的功能会明确说明。

| 编号 | 功能 | 归属与边界 | WishingSkin 建议 |
| --- | --- | --- | --- |
| N01 | 标签、分类与更丰富的检索 | 本次未在所查核心/官方插件中核实标签系统 | 已实现记录级分类、标签和公开库过滤；使用现有数据库 |
| N02 | 上传前审核队列 | 不等于已核实的事后举报处理 | 已实现审核队列，默认关闭；开启后上传和资料修改需审核，通过前不能下载与穿戴 |
| N03 | TOTP、Passkey 等额外登录保护 | 本次未在所查核心/官方插件中核实对应能力 | TOTP、恢复码和本地二维码已实现，Passkey 尚未实现 |
| N04 | 对象存储、CDN 与高级存储后端 | BS 官方生态存在存储/CDN 扩展；具体适配器另行核实 | 官方 S3 插件保留本地副本；已实现旧材质幂等同步、本地孤立文件回收和公开文件签名 CDN；不枚举全部远端孤立对象 |
| N05 | 审计记录、备份恢复、健康检查、运行日志 | 新站运维验收建议，不作 BS 功能有无判断 | 管理记录包含字段前后值与 CSV 导出；支持备份恢复、每日固定时区备份，默认关闭；不自动清理旧备份 |
| N06 | 手机适配、键盘操作、加载/空/错误状态、减少动态效果 | 新站 UI/UX 质量目标 | 浏览列表使用人物模型静态缩略图，详情加载可交互模型；统一弹窗、Toast、公告 Popover 和固定导航，支持减少动态效果 |

## 迁移与兼容目标

| 编号 | 目标 | 后续验收要点 |
| --- | --- | --- |
| M01 | 新数据结构，完整迁移历史数据 | 按源版本和插件建立字段/关系清单；预检查和核对报告；未知数据不静默丢弃 |
| M02 | 旧账号与角色继续使用 | 识别原密码算法及必要配置，保留实际 UUID 和关联；不强制重新生成 UUID |
| M03 | 材质文件及引用完整 | 保留源 hash、原文件、模型类型、角色与衣柜绑定；读取实际存储配置 |
| M04 | 外置登录及必要公开地址兼容 | 签名密钥处理、协议用例、启动器与服务端实测；旧会话保留与否单独决定 |

这些是迁移目标，不是当前已有能力。迁移支持范围以固定来源契约为准。上传 hash 可被插件改写，材质目录也可配置，不能在导入工具中强制假定为固定目录或统一 SHA-256 文件名。

## 功能组合

B01–B12、E01–E05、N05–N06 和 M01–M04 属于当前功能范围。其中 B07 的积分/签到允许关闭，E03 的邀请码现采用随程序发行的官方模块，新安装默认邮箱验证注册。首版包含本地账号、邮箱验证与找回；OIDC 已实现，仍保留本站注册、验证码、邀请码和邮件验证规则。其他能力按部署需求选择。2026-10-23 新增站点设置/资料及模型展示范围见[设置扩展](../openspec/changes/add-settings-and-profiles/proposal.md)。

官方插件已包括邀请码、积分、OIDC、OAuth2、外部账号连接器和对象存储。社区插件采用 Go SDK，支持宿主渲染内容页和手动/固定间隔任务，随源码编译。Webhook、SQL 迁移执行、任意前端模块、动态安装和插件市场仍为后续能力。

技术栈为 Go 模块化单体与 Vue 3 / TypeScript / Vite，静态前端由后端提供。首版同时支持 MySQL 8 与 PostgreSQL，分别验证；2 CPU / 2 GiB 固定数据短测已通过，实测负载、镜像和限制见[手动验收清单](manual-acceptance.md)，不推算最大在线人数。

## 来源

- S1：[核心 Web 路由](https://github.com/bs-community/blessing-skin-server/blob/dev/routes/web.php)
- S2：[角色名规则](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Rules/PlayerName.php)
- S3：[皮肤库与材质实现](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Http/Controllers/SkinlibController.php)
- S4：[衣柜与收藏计数实现](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Http/Controllers/ClosetController.php)
- S5：[用户与签到实现](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Http/Controllers/UserController.php)
- S6：[举报处理实现](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Http/Controllers/ReportController.php)
- S7：[核心中文 README](https://github.com/bs-community/blessing-skin-server/blob/dev/README-zh.md)
- S8：[核心 API 路由](https://github.com/bs-community/blessing-skin-server/blob/dev/routes/api.php)
- S9：[传统 Yggdrasil 插件路由](https://github.com/bs-community/blessing-skin-plugins/blob/master/plugins/yggdrasil-api/routes.php)
- S10：[Yggdrasil Connect 说明](https://github.com/bs-community/blessing-skin-plugins/blob/master/plugins/yggdrasil-connect/README.md)
- S11：[传统加载插件](https://github.com/bs-community/blessing-skin-plugins/tree/master/plugins/legacy-api)、[配置生成插件](https://github.com/bs-community/blessing-skin-plugins/tree/master/plugins/config-generator)
- S12：[官方插件目录](https://github.com/bs-community/blessing-skin-plugins/tree/master/plugins)；本文对应目录：`invitation-codes`、`disable-registration`、`single-player-limit`、`texture-description`、`restricted-email-domains`、`oauth`、`oauth-github`、`oauth-google`、`oauth-littleskin`、`oauth-microsoft-live`。
- S13：[正版验证插件](https://github.com/bs-community/blessing-skin-plugins/blob/master/plugins/mojang-verification/README.md)
- S14：[核心插件加载机制](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Services/PluginManager.php)
- 材质配置补充：[文件存储配置](https://github.com/bs-community/blessing-skin-server/blob/dev/config/filesystems.php)。
