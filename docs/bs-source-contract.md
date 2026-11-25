# BS 来源契约

本文档定义从 Blessing Skin `6.0.2` 迁入 WishingSkin 时冻结的源码版本、字段映射规则与预检查边界。实际操作步骤见[Blessing Skin 迁移手册](migration-runbook.md)；WishingSkin 自身的全量备份恢复见[备份与恢复](backup-restore.md)。

## 迁入范围概览

- **迁入数据**：用户（含余额、原权限值与明确的邮箱验证状态）、角色与实际 UUID、材质文件与记录（v2 快照保留上传者、简短描述与有效的材质上传时间）、衣柜收藏与别名、Yggdrasil 签名私钥、纯文本站点品牌/简介/公告、邀请码，以及独立的只读旧举报历史。
- **不自动迁入**：OAuth 与其他运营配置、额外第三方插件数据与 PHP 运行逻辑；旧网页会话和游戏令牌明确不继承（用户需重新登录）。收藏数由衣柜关系实时计算，不照搬可能失真的源站 `likes` 缓存。
- **积分说明**：保留用户积分余额，新站积分默认保持关闭，不自动换算 BS 的随机奖励、存储费或退款规则。v2 快照的 `score_rules` 会记录 14 项核心非敏感积分配置供管理员在核对报告中参考重配。
- **导入报告**：成功导入后的状态固定标记为 `partial`，明确标示迁入边界与缺口。

## 冻结的官方源码基准

官方契约固定以下版本与提交（插件来自官方 monorepo 的 `plugins/yggdrasil-api`，非旧独立仓库）：

| 来源 | 版本 | 固定 commit |
| --- | --- | --- |
| `bs-community/blessing-skin-server` | `6.0.2` | `b737a65f5f5fce1f897f8603db74fd970437b351` |
| `bs-community/blessing-skin-plugins/plugins/yggdrasil-api` | `5.2.1`（要求 BS `^6`） | `de016f4cbb43df7bd6d46193d5625693f8c79b3f` |

核心 `6.0.2` 为 annotated tag（标签对象 `b37c2aa753d9cfa53019aaeb9cc6a08de546c406` 指向上述 commit）：
- [核心标签](https://api.github.com/repos/bs-community/blessing-skin-server/git/ref/tags/6.0.2)、[标签对象](https://api.github.com/repos/bs-community/blessing-skin-server/git/tags/b37c2aa753d9cfa53019aaeb9cc6a08de546c406)
- [插件固定 commit](https://api.github.com/repos/bs-community/blessing-skin-plugins/commits/de016f4cbb43df7bd6d46193d5625693f8c79b3f)、[插件 package.json](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/package.json)

操作人员须人工核对实际源文件版本后填写来源元数据；导出器不从库表猜测版本，也不会把任意 BS 6.x 视作 `6.0.2`。

## 字段映射与校验规则

| 源表 / 配置 | 固定版本字段 | WishingSkin 目标行为 |
| --- | --- | --- |
| `users` | `uid,email,nickname,password,score,permission,verified` | 保留 ID、展示邮箱、昵称、密码哈希、积分与原权限值；仅 `permission=2` 映射为管理员，`1` 作为普通用户由服主另行授权，`-1` 为封禁。邮箱去空白转小写去重，冲突即阻止；仅保留明确的 `verified=0/1`，不凭旧登录推断 |
| `players` | `pid,uid,name,tid_skin,tid_cape` | 保留角色 ID 与归属；角色名小写判重且须符合 `[A-Za-z0-9_]{1,16}`，冲突或超限即阻止 |
| `uuid` | `name,uuid`（按角色原名匹配） | 保留实际 UUID（去除连字符并转小写），不重算；缺失、非法或重复即阻止 |
| `textures` | `tid,name,type,hash,public,uploader,upload_at` | `steve→skin/default`、`alex→skin/slim`、`cape→cape/default`；保留 hash 与公开状态；v2 保留上传者（`0` 记为未知，指向不存在账号即阻止）；有效的 `upload_at` 写入 `texture_records.created_at`，缺失、零值或不可解析时间回退为 `0` 并在报告中统计 |
| `user_closet` | `user_uid,texture_tid,item_name` | v2 保留收藏与别名（含未公开材质的既有收藏授权）；`NULL` 别名回退原材质名，收藏时间填 `0` 表示未知；重复、断链或别名超过 255 码点即阻止 |
| `textures_description` | 可选 `tid,description` | 按纯文本保存；超过 200 Unicode 码点阻止而不截断；重复或孤立描述拒绝导出 |
| `options`（认证） | `ygg_private_key,ygg_uuid_algorithm` | 原 RSA 私钥（须为 ≥ 4096 位未加密 PKCS#1 或 PKCS#8）写入 `settings.setting_key=ygg_private_key`；UUID 算法仅记录来源，不重算已有 UUID |
| `options`（品牌） | `site_name,site_description,announcement` | 叠加核心默认值，以纯文本写入品牌、简介与公告（不执行 HTML、不展开 `{version}` 占位符）；超长（名称 1–64、简介 ≤ 200、公告 ≤ 1000 码点）或名称/简介换行即阻止 |
| `reports` | `id,tid,uploader,reporter,reason,status,report_at` | v2 完整存入独立只读表 `legacy_reports`（无外键，保留长文本原因与原 DATETIME 文本）；不进入新处置队列，待处理项需管理员另行复核 |
| `PWD_METHOD` / `SALT` | 人工核对配置 | 保留原哈希、算法与盐，用户首次登录成功后由认证模块升级哈希 |

核心源码依据：[User](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/User.php)、[Player](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/Player.php)、[Texture](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/Texture.php)、[CreateCloset](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/database/migrations/2019_03_14_174727_create_closet.php)、[Report](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/Report.php)、[secure.php](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/config/secure.php)、[HasPassword.php](https://github.com/bs-community/blessing-skin-server/blob/b737a65f5f5fce1f897f8603db74fd970437b351/app/Models/Concerns/HasPassword.php) 及插件 [callbacks.php](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/callbacks.php)、[Profile.php](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Models/Profile.php)、[ConfigController.php](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Controllers/ConfigController.php)、[texture-description](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/texture-description/callbacks.php)。

### 密码算法支持范围

支持 `BCRYPT`、`PHP_PASSWORD_HASH`、`ARGON2I`、`ARGON2ID`、`MD5`、`SHA256`、`SHA512`（并校验编码与工作因子）。其中 `MD5`/`SHA256`/`SHA512` 仅支持空站点盐；`SALTED2MD5`、`SALTED2SHA256`、`SALTED2SHA512` 及自定义 `verify_password`/`user_password` 过滤器尚未适配，不可靠改名绕过校验。

## 固定支持的归档联合版

除官方 5.2.1 外，另明确支持一套固定的 BS `6.0.2` / `yggdrasil-api 5.2.2-0.2.5` 归档来源（无公开 Git commit，联合服务、远端身份、额外 OAuth2 签名配置及 PHP 行为不迁移）。该来源的 `core_commit` 与 `plugin_commit` 须留空，并填写以下经实际归档核验的源码树摘要：

| 字段 | 固定值 |
| --- | --- |
| `core_version` | `6.0.2` |
| `plugin_version` | `5.2.2-0.2.5` |
| `core_tree_sha256` | `1eaa6d052fa42c22ae89a34bf6b9af7d113c6ed5b32e560b08b1c827f7863d16` |
| `plugin_tree_sha256` | `60c056460e1e3bea2ce151b43c78b2a961fe4027d8d36b5689ffd66d4d4833ed` |

树摘要计算规则：核心枚举 `app/`、`config/`、`database/`、`routes/` 下全部普通文件，插件枚举 `plugins/yggdrasil-api/` 下全部普通文件，按相对 POSIX 路径排序后依次向 SHA-256 写入 `UTF-8 路径 + NUL + 文件 SHA-256 原始 32 字节`（不含时间、权限、目录与 `.env`）。其他未备案树摘要会被拒绝。

## 材质文件与 hash 模式

源站默认从本地 `TEXTURES_DIR`（通常为 `storage/textures`）复制一致的材质副本，远程 `FS_DRIVER` 尚未适配：

- `hash_mode=sha256`：每个文件实际 SHA-256 必须与源 hash 完全一致。
- `hash_mode=preserve-custom`：须填写 `hash_explanation` 说明来源过滤器，保留原 hash 并额外以 SHA-256 核对复制字节。
- 文件名只允许 32–128 位十六进制（无扩展名、无符号链接），单文件上限 8 MiB 且完整解码 PNG：皮肤宽 64–1024（64 倍数，高为宽或宽的一半）；旧高清披风允许宽至 4096（高为宽的一半，解码像素 ≤ 8,388,608），保留原字节且不放宽新站上传规则。多条记录共用同一 hash 时物理文件去重、记录分别保留；存在大小写冲突的 hash 会阻止导入。

## 快照版本、导入事务与中断恢复

- 新导出使用 `format_version=2`（结构定义在 `internal/bsmigration/snapshot.go`）；旧 v1 快照仍可按原语义兼容导入（`owner_id=NULL`、无描述/衣柜，保持原任务指纹不变）。`Load` 拒绝未知字段、尾随数据及超过 64 MiB 的快照。
- 导出器同步记录源库 `information_schema.COLUMNS` 与未处理表行数（`-1` 表示未计数），脱敏 `Report` 可安全用于核对日志，不要直接打印 snapshot 原文。
- 导入要求初始化后的空目标库与独立目标目录，按相同 snapshot 的 SHA-256 指纹建立断点续跑：
  1. 在 `migration_state` 写入并锁定 `pending` 任务行。
  2. 目标目录建立 `.wishingskin-migration.json` 记录指纹与校验值（遇无关文件或不匹配清单即拒绝）。
  3. 材质先写入 `.copy-<hash>` 临时文件，校验后通过不覆盖已有文件的硬链接发布（目标文件系统须支持硬链接）。
  4. 用户、角色、材质、衣柜、品牌、旧举报、邀请码与 `partial` 状态在同一数据库事务提交；数据库失败则业务行回滚、已发布文件留待同快照重试复用。
  5. 重复运行已完成任务只核验现存副本并返回 `already_imported=true`。

开发用合成夹具与生成器位于 `testdata/bs`，使用方法见[开发指南](development-guide.md)。
