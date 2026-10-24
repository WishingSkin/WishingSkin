# WishingSkin

面向服主自部署的开源 Minecraft 皮肤站，采用 Go 模块化单体与 Vue 3 / TypeScript / Vite，静态前端由后端提供，支持现代界面、Blessing Skin 声明契约的数据迁移及外置登录兼容能力。Kyanetwork 将作为首个使用方。

2026-10-24：本地首版及设置/资料/模型展示扩展已实现，当前 schema 11。首版全站 168 组布局、本轮设置/账号 63 组与模型相关 96 组布局、双库迁移进程中断恢复及备份恢复均有证据；真实源站、外部邮件/Turnstile 和生产切换未验收，迁移状态仍为 `partial`。先看[首版结果与限制](docs/v1-local-validation.md)及[设置与资料验证](docs/settings-profile-validation.md)，部署与迁移入口见[操作说明](docs/development.md)。

当前已实现网页登录、角色增删改名、管理员调整角色上限、PNG 材质上传与皮肤/披风绑定、本人材质资料编辑与删除、衣柜收藏和公开库、浅/深主题、双数据库升级，以及 BS 合成样本的只读导出、预检查、部分导入和中断恢复。工作台分为角色管理、我的材质、我的衣柜、公开库与账号；上传位于我的材质。角色页以穿戴实际皮肤和披风的 3D 模型展示外观，支持标准/纤细手臂、正背面、旋转与缩放；PNG 仍可查看和下载。旧密码登录后升级为 Argon2id，保留角色 UUID 与材质签名密钥。

账号页支持修改昵称、默认/上传/跟随本人角色皮肤的头像，以及修改密码；顶部头像昵称按钮进入账号页。新设置密码为 8–128 个字符且包含英文字母、数字与符号；改密后撤销该账号全部网页会话和游戏令牌。界面支持简体中文和 English，默认简体中文，切换保留当前输入与文件。

注册、邀请码、邮箱验证/找回和 Turnstile 已实现并通过本地双库与浏览器验证。新账号先验证邮箱，迁移账号仅保留源明确的已验证状态，未验证或状态未知的旧账号继续使用。新安装默认邮箱验证注册，SMTP / Turnstile 未配置时拒绝注册；升级保留原模式，BS 导入关闭注册。SMTP / Turnstile 可在后台修改并立即生效，秘密加密保存且不回显，设置密钥须单独备份；真实邮件投递与 Cloudflare 联网尚未验收。

管理后台支持用户、角色、材质与衣柜管理、品牌公告、Logo、站点默认主题/配色、举报处置和审计。名称与简介为纯文本；公告可选择 Markdown + 安全 HTML 并预览，危险标签和属性由后端过滤。普通用户不显示管理入口，后台 API 仍逐请求检查权限。积分默认关闭，启用后可配置北京时间每日签到奖励、上传费用与新建角色费用，失败操作不扣费；管理员调整费用后，旧网页表单会刷新价格并保留草稿，要求重新提交。

Yggdrasil 已实现认证、选角与令牌刷新、验证/撤销、join/hasJoined、签名资料及名称查询，并通过本地协议测试。Minecraft 26.3 原版客户端/服务端、Java 25.0.1 与 authlib-injector 1.2.8 的本地 Java 启动组合已通过人工进服、皮肤和披风验收；用户另确认 PCL2 配置认证服务器并使用 FixturePlayer 正常运行，未记录具体启动器/游戏版本及端口。其他启动器与 CustomSkinLoader 尚未验收，当前不能用于替换生产 BS 站点。迁移报告始终标记为 `partial`。BS v2 快照保留实际上传者、简短描述和衣柜收藏/别名，旧 v1 快照保持未知归属并报告缺口；不从角色绑定猜测上传者。首版范围已确认：B01–B12、E01–E05、N05–N06、M01–M04，详细边界见功能矩阵。

首版包含本地账号、邮箱验证与找回、角色与材质管理、外置登录、必要公开地址兼容及 BS 数据迁移；积分/签到可关闭。邀请码采用随程序发行的可启停官方模块，独立页面设置注册是否必填并批量生成、导出 TXT/CSV，始终保留邮箱验证；不加载第三方代码。E01–E05 按用途内建；OIDC 后续接入，其余未选条目保留为后续或可选能力。

首版同时支持 MySQL 8 与 PostgreSQL，分别验证。应用与数据库合计 2 CPU / 2 GiB 的固定负载短测已通过，两库各测量 1,795 次请求及离线导入；限流拒绝、邮件失败与成功分别统计，不据此推算最大在线人数，详见资源验证记录。

- [已确认的项目边界](docs/project-brief.md)
- [首版本地实现、验收结果与未执行项](docs/v1-local-validation.md)
- [本地开发、Docker 与迁移操作](docs/development.md)
- [第一阶段验证记录与限制](docs/phase1-validation.md)
- [第二阶段：3D 外观与认证协议验证](docs/phase2-validation.md)
- [第三阶段：角色管理与材质上传验证](docs/phase3-validation.md)
- [衣柜与公开库验证](docs/wardrobe-validation.md)
- [密码修改与双语验证](docs/account-locale-validation.md)
- [注册、邮箱与验证码验证](docs/account-access-validation.md)
- [本机管理员初始化验证](docs/bootstrap-validation.md)
- [用户管理与审计查看验证](docs/administration-validation.md)
- [整份图片下架验证](docs/moderation-validation.md)
- [品牌与公告验证](docs/branding-validation.md)
- [角色与衣柜后台验证](docs/resource-administration-validation.md)
- [管理员删除角色验证](docs/admin-character-deletion-validation.md)
- [举报与处置验证](docs/reports-validation.md)
- [旧 BS 举报历史迁移](docs/bs-source-contract.md#品牌与举报历史)
- [品牌与历史迁移验证](docs/legacy-report-migration-validation.md)
- [双库迁移进程中断恢复与脱敏核对](docs/migration-recovery-validation.md)
- [积分与签到验证](docs/points-validation.md)
- [上传窗口限流验证](docs/upload-limit-validation.md)
- [可信代理地址验证](docs/proxy-validation.md)
- [BS 上传归属与衣柜迁移验证](docs/relationship-migration-validation.md)
- [旧公开地址与游戏接入验证](docs/legacy-compatibility-validation.md)
- [本机备份与隔离恢复](docs/backup-restore.md)
- [备份恢复与积分迁移验证](docs/backup-validation.md)
- [Minecraft 原版人工验收与适用范围](docs/game-acceptance-progress.md)
- [2 核 / 2GB 资源测量与限制](docs/resource-validation.md)
- [邮箱状态与权限迁移验证](docs/verification-migration-validation.md)
- [BS 来源契约与样本说明](docs/bs-source-contract.md)
- [BS 功能对照与首版范围](docs/feature-matrix.md)
- [OpenSpec 规划变更](openspec/changes/define-wishingskin-foundation/proposal.md)
- [首版实施提案](openspec/changes/build-wishingskin-v1/proposal.md)
- [架构设计与迁移边界](openspec/changes/build-wishingskin-v1/design.md)
- [分阶段实施与验收任务](openspec/changes/build-wishingskin-v1/tasks.md)
- [MIT 许可证](LICENSE)
- [第三方依赖与许可证](THIRD_PARTY_NOTICES.md)

使用 GrillMe 收敛需求、OpenSpec 记录规划，中文文档按 Humanizer-zh 审阅。

公告弹窗、历史记录与本地 Git 产物清理见[本轮验证](docs/notices-git-validation.md)。当前代码已支持 schema 11；旧 8097 预览的程序更换被自动审批阻止，不能据此判断已经更新。
