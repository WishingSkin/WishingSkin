# BS 来源契约与本地迁移样本

当前迁入用户（含余额及权限）、角色、材质及 Yggdrasil 签名私钥；新导出的 v2 快照还保留材质上传者、简短描述、衣柜关系/别名、明确的邮箱验证状态、纯文本品牌及独立只读举报历史。成功导入的报告仍为 `partial`，不能据此判断整个 BS 站点已迁完，也不能据此切换真实站点。OAuth、其他运营配置及额外插件数据不自动迁入；旧 Web 会话和游戏令牌明确不继承，用户需要重新登录。收藏数由衣柜关系计算，不复制可能已失真的 `likes` 缓存。当前本地验收见[手动验收清单](manual-acceptance.md)。

2026-10-23 补充积分迁移决定：保留余额，新站积分关闭，不自动转换 BS 的随机奖励、存储费用或退款。新 v2 快照新增 `score_rules`，仅记录固定核心 14 项非秘密积分配置，以核心默认值叠加实际数据库覆盖值；核对报告保存原规则，管理员按新规则重配。旧快照无该字段时明确报告原规则不可用，原指纹不变。完整新站备份与 BS 快照用途不同，操作见[备份恢复说明](backup-restore.md)。

## 冻结的官方源码

2026-10-22 通过 GitHub API 读取标签及 `master`，固定以下源码。插件来自官方 monorepo 的 `plugins/yggdrasil-api`，不是旧的独立 `bs-community/yggdrasil-api` 仓库。

| 来源 | 版本 | 固定 commit |
| --- | --- | --- |
| `bs-community/blessing-skin-server` | `6.0.2` | `b737a65f5f5fce1f897f8603db74fd970437b351` |
| `bs-community/blessing-skin-plugins/plugins/yggdrasil-api` | `5.2.1`，要求 BS `^6` | `de016f4cbb43df7bd6d46193d5625693f8c79b3f` |

核心 `6.0.2` 是 annotated tag，标签对象 `b37c2aa753d9cfa53019aaeb9cc6a08de546c406` 解引用到上表中的 commit。对应 API 证据：

- [核心标签](https://api.github.com/repos/bs-community/blessing-skin-server/git/ref/tags/6.0.2)、[标签对象](https://api.github.com/repos/bs-community/blessing-skin-server/git/tags/b37c2aa753d9cfa53019aaeb9cc6a08de546c406)。
- [插件固定 commit](https://api.github.com/repos/bs-community/blessing-skin-plugins/commits/de016f4cbb43df7bd6d46193d5625693f8c79b3f)、[插件 package.json](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/package.json)。

源码契约已核对；当前样本是按该契约构造的本地数据，尚未安装并运行原 PHP 站点来验证样本。源码版本需要操作人员核对实际源文件后提供，导出器不会根据数据库字段推断已安装版本，也不会把任意 BS 6.x 标成 `6.0.2`。

## 已核对的字段与行为

| 官方来源 | 固定版本源码 | 目标行为 |
| --- | --- | --- |
| `users` | `uid,email,nickname,password,score,permission,verified` | 保留 ID、展示邮箱、昵称、哈希、积分和原权限值；仅 `permission=2` 获得新站管理员权限，`1` 为普通用户、待服主重新授权，`-1` 为封禁；邮箱唯一键为去除首尾空白后转小写，冲突时阻止导入；只保留明确的 `verified=0/1`，不根据邮箱存在或旧登录推断 |
| `players` | `pid,uid,name,tid_skin,tid_cape` | 保留角色 ID 和归属；角色名使用小写比较键，名字必须符合目标 `[A-Za-z0-9_]{1,16}` 规则，冲突或超范围时阻止导入 |
| `uuid` | 按角色原名读取 `name,uuid` | 保留实际 UUID；只去除标准连字符并转小写，不按名字重新生成；缺失、非法或重复 UUID 会阻止导入 |
| `textures` | `tid,name,type,hash,public,uploader` | `steve→skin/default`，`alex→skin/slim`，`cape→cape/default`；保留 hash 和公开状态；源 tid 用于稳定生成目标记录标识和角色引用映射；v2 保留实际上传者，0 表示未知并报告，其他不存在的上传者阻止导入 |
| `user_closet` | `user_uid,texture_tid,item_name` | v2 保留收藏和别名，包括未公开材质的已有收藏授权；NULL 别名使用原材质名，原表没有收藏时间，目标以 0 明确表示未知 |
| `textures_description` | 可选 `tid,description` | 固定官方插件表，按纯文本保存原值；超出 200 个 Unicode 码点阻止导入，不截断；重复或孤立描述拒绝导出 |
| `options` | `ygg_private_key,ygg_uuid_algorithm` | 原 RSA 私钥写入目标 `settings.setting_key=ygg_private_key`；UUID 算法仅记录来源，不用于重算已有 UUID |
| `options` | `site_name,site_description,announcement` | 叠加冻结核心默认值，原样保存为纯文本品牌、简介、公告；不执行 HTML、不展开占位符，超过目标长度则阻止导入，不截断 |
| `reports` | `id,tid,uploader,reporter,reason,status,report_at` | v2 完整保存为独立只读历史；保留孤立引用、长原因和原时间文本，不加入新站处置队列、不关联当前账号或材质 |
| `PWD_METHOD` / `SALT` | 操作人员提供已核对配置 | 原哈希、方法与站点盐保留；成功登录后的升级由认证模块负责；迁移器不会试改密码 |

核心字段依据：[User](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/User.php)、[Player](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/Player.php)、[Texture](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/Texture.php)、[初始迁移](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/database/migrations/2016_11_18_133939_create_all_tables.php)、[players 重命名迁移](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/database/migrations/2019_03_13_130311_rename_players_table_columns.php)。

插件的 [callbacks.php](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/callbacks.php) 创建 `uuid(id,name,uuid)` 表及相关选项；[Profile.php](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Models/Profile.php) 优先使用已存映射，且用 `ygg_private_key` 签名属性。插件 [ConfigController.php](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Controllers/ConfigController.php) 拒绝小于 4096 位的 RSA 密钥，预检查遵守该限制，只接收未加密 PKCS#1 或 PKCS#8 RSA 私钥。

密码配置依据 [secure.php](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/config/secure.php) 与 [HasPassword.php](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/Concerns/HasPassword.php)。`credential` 支持 BCRYPT、PHP_PASSWORD_HASH、ARGON2I、ARGON2ID、MD5、SHA256、SHA512，并限制可验证编码和工作因子；超出实现范围的编码或参数同样会阻止导入。MD5、SHA256、SHA512 只支持站点盐为空的已核实形式，非空盐会阻止导入；BCRYPT 还检查前缀、编码和尾位。BS 的 SALTED2MD5、SALTED2SHA256、SALTED2SHA512 及 `verify_password` / `user_password` 过滤器尚未适配，不能仅把方法名改成另一算法后导入。

## 已核对的归档联合版

另支持一个明确固定的 BS 6.0.2 / `yggdrasil-api 5.2.2-0.2.5` 归档来源。它不是官方 5.2.1，也没有可核实的 Git commit。账号、密码、角色、材质、上传校验与默认配置关键文件和上述固定核心一致。插件继续读取实际 `uuid` 映射和 `ygg_private_key`；额外改动包括角色名大小写修正、联合服务配置及额外 OAuth2 签名配置。联合服务、远端身份、额外签名配置及 PHP 运行行为不迁移。

该来源的 `core_commit` / `plugin_commit` 均留空，填写以下树摘要；不能只改版本号，不能把官方 commit 填到归档来源：

| 字段 | 固定值 |
| --- | --- |
| `core_version` | `6.0.2` |
| `plugin_version` | `5.2.2-0.2.5` |
| `core_tree_sha256` | `1eaa6d052fa42c22ae89a34bf6b9af7d113c6ed5b32e560b08b1c827f7863d16` |
| `plugin_tree_sha256` | `60c056460e1e3bea2ce151b43c78b2a961fe4027d8d36b5689ffd66d4d4833ed` |

摘要计算方式：核心枚举 `app/`、`config/`、`database/`、`routes/` 下全部普通文件，插件枚举 `plugins/yggdrasil-api/` 下全部普通文件。按相对于 BS 根目录的 POSIX 路径排序，依次向 SHA-256 写入 `UTF-8 路径 + NUL + 文件内容 SHA-256 的 32 字节原值`。不包含时间、权限、目录项或 `.env`。操作人员必须从实际归档计算并比对；导出器只能验证声明值和数据库内容，不能远程验证 PHP 文件。其他树摘要仍被拒绝。

## 材质文件与 hash 模式

核心 [SkinlibController.php](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Http/Controllers/SkinlibController.php) 默认计算文件 SHA-256，再应用 `uploaded_texture_hash` 过滤器；[filesystems.php](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/config/filesystems.php) 的材质目录是 `TEXTURES_DIR`，默认 `storage/textures`。导出时必须另行取得一致的本地材质副本；远程 FS_DRIVER 尚未适配。

- `hash_mode=sha256`：每个源文件的 SHA-256 必须与原 hash 完全相同。
- `hash_mode=preserve-custom`：必须填写 `hash_explanation`，说明源过滤器或自定义标识的来源。保留源 hash，并使用单独 SHA-256 核对复制前后字节；报告明确声明未验证源过滤器算法。
- 两种模式都只接受 32–128 位十六进制文件名，不接受路径、扩展名、目录或符号链接。文件限 8 MiB，并完整解码 PNG；皮肤宽度 64–1024、64 的倍数，高为宽或宽的一半；旧披风宽度允许到 4096、高为宽的一半，解码最多 8,388,608 像素。保留原文件与 hash，不重采样；此兼容范围不改变新上传规则。
- 目标按文件 hash 保存材质字节，并用独立材质记录保存名称、类型、模型和公开状态。因此多个 BS 记录共用同一 hash 时，只保留一份文件字节，各记录仍分别导入；源记录 ID 只用于生成目标记录标识和角色引用映射。hash 大小写冲突仍会被阻止，以免不同系统的文件名规则产生不同结果。

旧 `format_version=1` snapshot 没有材质上传者、描述和衣柜。旧快照继续按原语义导入，`owner_id=NULL`、描述为空，并报告缺口；不会把角色所属账号视为上传者，也不会把新数据合入已导入的旧任务。v1 的序列化指纹保持不变，可继续原任务恢复。新导出器生成 `format_version=2`，必须从源字段读取上传者和衣柜；描述表不存在时为空，旧 `textures_desc` 或其他未支持表仍列为缺口。

衣柜的重复 `(user_uid,texture_tid)`、断开的账号/材质引用，以及超过 255 个 Unicode 码点的别名均阻止导入，不合并或截断。相同图片的不同记录保留独立上传者、名称、描述和收藏关系；目标只去重图片文件。升级格式不会重算 UUID、恢复旧会话或自动标记邮箱已验证。

迁移权限规则：BS 普通管理员（原权限 1）在新站作为普通用户使用，由服主逐个重新授权；超级管理员（2）保留管理员权限。原数值继续保存，不把 1 自动提升为 2。报告的 `permissions` 只包含映射规则及四类账号数量，不列出邮箱、昵称或凭据。

v2 用户新增可选 `verified`。新只读导出直接读取固定核心 `users.verified`，不是 0/1 或缺失列时拒绝；旧 v1/v2 快照缺字段时报告 `unknown`，不修改旧任务指纹。只有明确已验证的账号才写入 `account_states(email_verified=true, verification_required=false)`；未验证或未知的旧账号继续按原迁移兼容方式登录，不设置新的验证门槛。报告分别计数已验证、未验证和未知，不包含邮箱。此字段不能补入已提交的旧任务；需要重新导出并迁入独立空目标。

衣柜字段依据固定核心的 [CreateCloset](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/database/migrations/2019_03_14_174727_create_closet.php) 和 [User::closet](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/User.php)；描述字段依据固定插件仓库的 [texture-description callbacks](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/texture-description/callbacks.php)。验证与限制见[手动验收清单](manual-acceptance.md)。

## 品牌与举报历史

v2 的可选 `branding` 固定包含 `site_name`、`site_description`、`announcement` 三项。只读导出读取固定核心默认值并叠加数据库覆盖值，分别写入新站 `site_name`、`site_tagline`、`site_notice`。名称 1–64、简介至多 200、公告至多 1000 个 Unicode 码点；名称和简介不允许换行，无法无损表示时阻止导入。HTML 与 `{version}` 等占位符均作为字面文字，不自动翻译。注册模式、邮件凭据、公开地址、自定义 CSS/JS 和其他运营配置不应用；积分只保留核对清单，由管理员重新配置。旧快照缺少品牌时报告不可用。

旧 BS 举报作为独立只读历史完整保留。依据固定核心的 [Report 模型](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/Report.php) 和 [举报建表迁移](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/database/migrations/2019_03_23_171728_create_report_table.php)，源表只有上述七字段，状态为 0 待处理、1 已处置、2 已驳回，没有处理人、动作及处理时间。原因使用 `LONGTEXT`，迁移不套用新举报的 1000 字限制。

v2 可选 `legacy_reports.items` 保存所有记录。缺少包装字段表示历史不可用；显式空数组表示已检查且为零。源表不存在时另报告未检查，不当成零。`report_at` 通过 MySQL `CAST(... AS CHAR)` 读取，保留日期时间文本（含零日期和小数秒）或 NULL，不经驱动日期解析、不赋予时区。无效状态、重复/非正举报 ID、无效 UTF-8、NUL 或不符合源 DATETIME 文本形状的值阻止导入；源引用编号不要求现存对象。旧快照指纹保持不变，不把历史补入已提交的旧任务。

schema 9 的 `legacy_reports` 没有指向当前对象的外键。管理员通过 `GET /api/admin/legacy-reports` 和举报页的独立历史区域只读检索/分页；API 将全部编号投影为十进制字符串，避免大于 2^53 的精度损失。源待处理项需要管理员在新后台另行核查处置，导入不回放下架、封禁或积分动作。核对报告只记录策略、是否包含及各状态数量，不复制原因、编号或时间。本站备份包含该表，详见[备份说明](backup-restore.md)。

## 导出、预检查和导入

`ExportMySQL(ctx, db, ExportOptions)` 从操作人员指定的源 MySQL 数据库读取，使用只读、可重复读事务和显式 SELECT，不解析或执行 SQL dump，也不调用原站接口。`ExportOptions.Source` 必须填写上表固定版本、源实例标识和经核对的密码/hash 配置；表前缀只允许字母、数字和下划线。

导出器读取核心用户/角色/材质、实际 UUID、必要选项，同时记录源库 `information_schema.COLUMNS`。其他表逐项记录行数，其他字段记录名称，额外站点选项和孤立 UUID 映射记录数量；未处理的数据不写入目标。导出器不会自动迁入未知插件内容，报告会列出缺口。行数为 `-1` 表示未计数，不能解释成零。

规范化 snapshot JSON 的结构定义在 `internal/bsmigration/snapshot.go`，读取 v1/v2，新导出使用 v2。`Load` 拒绝未知 JSON 字段、尾随内容和大于 64 MiB 的文件；离线 `Preflight` 不连接数据库，只读 snapshot 和材质目录。缺少必要身份、错误引用、规范化冲突、未知密码方法/编码、无效密钥及文件问题都会写入 `blockers` 并返回错误。

snapshot 含密码哈希、站点盐和签名私钥，属于敏感备份。只把脱敏 `Report` 用于日志；不要打印 snapshot。报告中的 `signature=true` 表示私钥通过格式及强度预检查，`files` 表示通过离线校验的文件数，二者均不能代替原站运行或 Minecraft 客户端验收。

`Import` 要求已初始化的空业务目标库，以及与源目录互不包含的独立目标目录。用户、角色、材质、衣柜、审计、举报、积分流水及会话/令牌表任一存在业务数据时，不允许开启新迁移。预置 `character_limit=1`、`registration_mode=closed` 和完全匹配默认关闭全零的积分规则属于初始化状态；其他设置或非默认配置仍会阻止导入。导入和恢复期间必须关闭应用业务写入；工具不协调原站停写或分布式双写。

恢复使用相同 snapshot 的 SHA-256 指纹：

1. 首次在 `migration_state` 持久写入 `pending`，再锁住该任务行执行导入。
2. 空目标目录建立 `.wishingskin-migration.json`，记录指纹和文件校验值。目录已有无关文件、清单不匹配或原文件字节不同，均拒绝覆盖。
3. 材质先写本任务 `.copy-<hash>` 临时文件，再用不覆盖已有文件的硬链接发布。目标文件系统需要支持硬链接；失败会清理本次临时文件。
4. 用户、材质、衣柜、角色、签名和 `partial` 状态在同一数据库事务提交。数据库失败时业务行回滚，已完成的本任务材质保留供重试；重试会校验并复用这些文件。
5. 相同任务再次运行只验证现存副本并返回 `already_imported=true`，不重复插入。其他快照不能借用该任务恢复。

当前故障测试覆盖文件阶段实际终止导入进程、材质发布后及业务事务末段入库失败后的重试；两库大样本恢复结果与普通导入逐表/逐文件摘要相同，见[手动验收清单](manual-acceptance.md)。检查点用于文件恢复，业务单事务提交，不是多批数据库提交。断电导致文件系统或数据库自身损坏、目录清单首次写入时的撕裂、外部进程篡改目标和跨主机恢复尚未演练；损坏的清单会停止导入，工具不会猜测归属后清除目录。业务写入开放后的数据合并、反向迁移和无损回滚也不在迁移支持范围。

## 可重复的本地样本

`testdata/bs/snapshot.json` 与无扩展名 PNG 为合成样本，没有生产数据。样本账号 `Fixture@Example.invalid` 的密码是 `password`，角色 `FixturePlayer` 的实际 UUID 是 `12345678123442348234123456789abc`，特意不同于按名字推导的离线 UUID；材质为 64×64 slim 皮肤，未公开，积分 42。

仓库中的 snapshot 不含私钥，直接预检查会因缺钥阻止导入。测试运行时动态生成 4096 位 RSA；要手动试验，可从仓库根目录生成独立的临时样本：

```powershell
go run ./testdata/bs/generate.go <不存在的本地临时目录>
```

输出包含带测试私钥的 `snapshot.json`、PNG 和 `source.sql`。`source.sql` 只是可选的本地合成源库种子，只有测试场景需要的字段；它不是官方完整 schema，也不是生产备份。仅可由操作人员导入新建的空本地源库，再使用只读导出器读取。迁移器本身从不执行该 SQL 文件。

单元测试运行：

```powershell
go test ./internal/bsmigration -count=1
```

双库集成测试使用 `WS_TEST_MYSQL_DSN` / `WS_TEST_POSTGRES_DSN`；未配置时明确跳过对应测试。配置后，测试会在该本地服务器创建随机 `ws_bs_*` MySQL 库或 PostgreSQL schema，并且只删除自己创建的空间，不清理其他测试或运营数据。

2026-10-22 本地运行结果：MySQL 8 与 PostgreSQL 的普通导入、注入失败后恢复、同任务重复导入、不同任务拒绝，以及两库结果一致均通过；MySQL 只读源导出、未知插件登记和源数据未改也通过。单元测试覆盖重复 UUID、邮箱规范化冲突、缺文件、未知密码、路径、错误引用、PNG、签名、目标目录保护及敏感信息不进入报告。

以上 2026-10-22 结果对应当时的最小链路；后续衣柜、权限/邮箱、积分核对、品牌与历史已补齐声明契约的本地测试，证据见[手动验收清单](manual-acceptance.md)。原 PHP 站点运行、真实完整 BS 数据、额外插件、真实站点恢复和生产切换仍未验收，整体状态保持 `partial`。
