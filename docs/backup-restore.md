# 备份与恢复

WishingSkin 提供应用层的完整离线备份与恢复工具（不执行 SQL dump，也不能替代 BS 迁移快照）。当前备份格式为 `1`，要求目标数据库与当前程序 schema 版本完全一致（当前为 schema 29），不支持跨版本自动恢复或 schema 降级。旧 schema 备份必须先用对应旧版本恢复到独立空目标，再由新程序执行 `migrate` 升级。

## 备份包含与排除的内容

### 会保存在备份中的数据

备份目录由完成清单 `manifest.json`、各业务表 JSONL、`textures/` 以及动态插件 `plugins/` 组成，完整保留：

- 账号、密码哈希与盐、邮箱验证状态、加密的 TOTP 密钥与恢复码摘要、OIDC / 外部身份绑定、正版 Minecraft 验证绑定（`minecraft_verifications`）、OAuth2 客户端与权限范围（`allowed_scopes`）、个人资料与头像设置。
- 角色 UUID 与外观绑定、材质记录与物理文件（含已下架文件）、审核元数据、个人衣柜与别名、个人通知收件箱记录（`user_notifications`），以及材质点赞记录（`texture_likes`）。
- 站点设置（含品牌、Logo、主题包、导入的方块/粒子贴图、自定义字体、首页区块与背景、公告格式、官方模块配置与 Yggdrasil 签名私钥）、迁移状态、管理审计及字段级变更详情、举报记录、积分余额与流水，以及从 BS 迁入的只读 `legacy_reports` 历史。
- 大整数统一以十进制字符串编码，防止精度丢失；`NULL` 原样保留。每张表记录行数和 SHA-256，每份受引用材质记录大小和独立 SHA-256（孤立文件、临时文件与旧迁移清单不复制）。

### 不保存在备份中的数据

- **短期会话与凭据**：网页会话、游戏令牌、进服记录、邮件验证/找回链接、邀请码、活动积分码与兑换记录（`point_codes`、`point_code_redemptions`）、OIDC/OAuth2/正版验证短期流程状态（`minecraft_flows`）及待投递扩展事件均不备份，恢复后全部失效（用户重新登录或重新发放激活码即可，原库凭据不会被导出撤销）。
- **环境配置与设置密钥**：`WS_PUBLIC_URL`、数据库连接和代理配置来自环境变量，需另行记录。**32 字节设置密钥**（默认位于 `<WS_TEXTURE_DIR>/.settings.key`，或由 `WS_SETTINGS_KEY_FILE` 指定）**不会进入业务备份，必须单独安全备份并严格控制访问权限**。
- **迁移状态约束**：存在 `pending` 迁移任务时会阻止备份；`partial` 状态原样保留。

> [!CAUTION]
> 备份内含签名私钥和业务密文，且备份格式本身不加密，请存放在受控目录中（Linux / macOS 建议设置目录权限 `0700`、密钥文件 `0600`；Windows 的 `0600` 权限不能代替 NTFS ACL）。恢复完成后、启动服务前，须将原 32 字节设置密钥放回目标配置路径（若使用默认路径，请等 `restore` 创建完材质目录后再放入）。缺少或放错密钥会导致程序拒绝启动或 `/readyz` 返回 503，丢失密钥意味着已加密的服务凭据无法解密。

## Docker Compose 持久化说明

PostgreSQL 与 MySQL 编排文件均把数据库、材质和 `/data/backups` 放在独立命名卷中。后台定时备份可直接写入 `/data/backups`；命名卷在容器重建时保留，但仍位于同一宿主机，请按灾备要求定期复制到异地或离线介质，且不要执行 `docker compose down -v`。

## 动态插件备份边界

- 已安装包与保留旧包的 ZIP、权限确认、启停配置、KV 数据和受管理前缀的插件 SQL 表（二进制列以 Base64 编码）会一并备份；暂存但未安装的升级包不会在恢复时执行。
- 恢复过程不会启动插件进程。无 SQL 状态的纯核心数据可在 MySQL 与 PostgreSQL 间跨引擎恢复；含插件 SQL 表时须恢复到相同数据库引擎。
- 含生成列、循环外键依赖或缺少重建包的插件 SQL 表会导致导出失败，此类插件需自行提供备份流程；插件写入宿主磁盘的其他外部文件也需单独备份。
- 自定义 `WS_PLUGIN_DIR` 时，恢复前请将其指向新的空目录（与源备份互不包含）。注意 MySQL 插件 DDL 失败无法整体回滚，如遇错误请换用全新空数据库重试。

## 后台定时备份与校验

在后台“站点设置 → 存储与备份”中可配置每日定时备份：

- 按 24 小时制和 IANA 时区名称（如 `Asia/Shanghai`、`UTC`）执行，默认模板为北京时间每日 `03:00`，保存并启用后生效；调度器每分钟检查一次。
- 每个本地日期最多自动尝试一次（手动备份不消耗当日定时名额）；服务在当日设定时间之后启动会补做一次，不补跑历史日期。遇夏令时跳时会在跳时后补做，重复时刻只执行一次。若从旧版“间隔小时”升级，需重新设定时间、时区并启用。
- **保留份数**：设为 `0` 表示全部保留。后台生成的新备份须同时通过完整性与来源核验后，才会清理超出份数的旧后台备份；手动 CLI 备份、损坏或未完成的备份不参与自动清理。
- **来源核验**：若源材质文件与记录的摘要不一致，备份会直接中止；无法追溯来源摘要的旧材质仍可正常恢复，但包含此类材质的备份不计入自动保留份数，也不会触发旧备份清理。
- 后台“备份列表”支持在线触发完整性校验并查看恢复指引，实际恢复始终通过停站 CLI 完成，不在线覆盖运行中的站点。

## 离线 CLI 备份与恢复步骤

执行前请停止所有 WishingSkin 实例、迁移器及其他写入进程（`--offline` 为人工停写确认，程序不会远程停机）。目标父目录必须已存在，备份与恢复路径均须为全新目录，且与源材质目录互不包含。

### 原生部署示例

**Linux / macOS**：

```bash
# 1. 停止所有写入后，在当前版本环境导出备份
mkdir -p backups
./bin/wishingskin backup --offline --directory backups/site-20261029

# 2. 在全新空数据库和尚不存在的材质目录上恢复
export WS_DATABASE_DSN='connection-to-isolated-empty-target'
export WS_TEXTURE_DIR='data/restored-textures'
mkdir -p data
./bin/wishingskin migrate
./bin/wishingskin restore --offline --directory backups/site-20261029
```

**Windows (PowerShell)**：

```powershell
# 1. 停止所有写入后，在当前版本环境导出备份
New-Item -ItemType Directory -Force backups | Out-Null
.\bin\wishingskin.exe backup --offline --directory backups/site-20261029

# 2. 在全新空数据库和尚不存在的材质目录上恢复
$env:WS_DATABASE_DSN='connection-to-isolated-empty-target'
$env:WS_TEXTURE_DIR='data/restored-textures'
New-Item -ItemType Directory -Force data | Out-Null
.\bin\wishingskin.exe migrate
.\bin\wishingskin.exe restore --offline --directory backups/site-20261029
```

### Docker Compose 停写备份示例

```bash
docker compose --env-file .env -f deploy/compose.postgres.yaml stop app
docker compose --env-file .env -f deploy/compose.postgres.yaml run --rm --no-deps app backup --offline --directory /data/backups/site-20261029
```

MySQL 部署将编排文件替换为 `deploy/compose.mysql.yaml`，备份完成后再重新启动 `app`。

### 恢复校验与空目标要求

- `backup` 与 `restore` 均不自动升级数据库，恢复前须先用匹配版本运行 `migrate`。
- 目标库只允许存在初始化默认设置（角色上限 1、默认注册模式、关闭全零积分规则、默认首页区块），存在任何业务数据或非默认设置都会阻止恢复；目标材质目录即使为空也不可提前创建。
- 恢复时先校验清单版本、表结构、行数、摘要与材质清单，再复制文件并执行匹配的插件 SQL 迁移，随后在同一业务事务中写入核心行与插件数据（插件 DDL 在事务前执行）。

## 中断处理与切换

- 导出最后才写入 `manifest.json`，缺少或损坏清单的备份无法恢复；导出失败后保留现场，排查后换新目录重跑。
- 恢复开始会写入 `.restore-incomplete.json`，数据库提交成功后改名为 `.restore-complete.json`。发生错误或提交结果不确定时，先核对数据库持久状态，不要盲目删标记或复用原目录。
- 体积限制：单条 JSONL 行上限 64 MiB，清单上限 32 MiB（BS 快照整体上限 64 MiB），超出即中止不截断。
- 核对恢复实例无误后再切换流量；切换后产生的新数据不会回到旧备份，如需回退请做好停写与增量数据处理。
