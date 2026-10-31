# Blessing Skin 迁移手册

本手册用于把已核对的 Blessing Skin 6.0.2 数据迁入一套全新的 WishingSkin schema 19 实例。固定来源包括官方传统 `yggdrasil-api 5.2.1`，以及按树摘要固定的归档联合版 `5.2.2-0.2.5`，详见[来源契约](bs-source-contract.md)。其他版本、远程文件系统、未知插件和 BS 写入 API 不在本流程内。导入报告使用 `partial` 标明数据范围；切换前还需核对报告中的缺口。

## 迁移前的保护

- 迁移是新站初始化，不是向已有 WishingSkin 合并数据。目标数据库、材质目录和设置密钥路径必须独立。
- 源站必须安排停写窗口。数据库快照和材质目录副本必须来自同一窗口；不要一边上传一边复制文件。
- 导出的 snapshot 含密码哈希、站点盐、Yggdrasil 私钥和邀请码迁移明文。只放在受限目录，不提交 Git、不发到聊天、不写日志。迁移完成后按站点保留策略隔离或清理；不要删除源站数据作为流程的一部分。
- 目标不要先执行 `create-admin`。源账号和权限会被导入；不要绕过空库检查。

## 1. 核对源站

从固定版本源码和实际配置核对 BS 核心、传统 yggdrasil-api 插件、密码算法及 salt、UUID 算法、材质 hash 规则、表前缀和 `TEXTURES_DIR`。复制示例并填写已核实的值：

```powershell
New-Item -ItemType Directory -Force artifacts | Out-Null
Copy-Item deploy/bs-source.example.json artifacts/source-info.json
# 编辑 artifacts/source-info.json；不要把真实凭据放入日志。
```

创建只读 MySQL 账号，导出器只执行 SELECT。停止源站写入后，取得源材质目录的完整副本，例如 `artifacts/source-textures`。材质文件名必须为源 hash，不带扩展名；不要使用符号链接。

## 2. 导出并离线预检查

在与构建产物相同的 PowerShell 会话中设置只读源 DSN：

```powershell
$env:WS_BS_SOURCE_DSN='readonly_user:replace-me@tcp(127.0.0.1:3306)/confirmed_source?parseTime=true'
./bin/wishingskin.exe export-bs --source-info artifacts/source-info.json --output artifacts/source-snapshot.json
./bin/wishingskin.exe preflight --snapshot artifacts/source-snapshot.json --source-textures artifacts/source-textures
```

导出不会执行 SQL dump，也不会修改源库。若源库有表前缀，给 `export-bs` 增加 `--table-prefix prefix_`。输出文件必须不存在。预检查出现 blocker 时，按下一节处理来源副本，再重新导出。预检查验证数据和文件契约；迁入后的登录与角色使用在第 5 步核对。

### 预检查发现旧数据问题

失效收藏、缺失文件、重复身份和错误 UUID 等问题会阻止导入。先保留原始数据库快照和材质压缩包，记录摘要。只在独立源副本中修复已确认的问题，再从只读账号重新导出全新 snapshot；原导出文件也保留。移除失效收藏或其他记录前，先列明数量和影响并取得数据所有者确认。不要直接编辑 snapshot 或降低检查条件。合法旧高清披风按来源契约保留原字节。

## 3. 准备空目标

先创建目标数据库。推荐将 `WS_SETTINGS_KEY_FILE` 指向材质目录之外的专用持久路径，避免密钥与迁移文件混放。导入命令会在该位置首次生成 32 字节密钥；妥善备份它。使用默认密钥路径时，目标材质目录可以为空，或只含一个有效的普通 32 字节 `.settings.key`；中断恢复时，该密钥可与匹配的迁移清单共存。其他无关文件、无效密钥或不匹配的迁移清单都会阻止导入。目标应用和其他写入进程全部停止。

```powershell
$env:WS_DATABASE_DRIVER='postgres' # 或 mysql
$env:WS_DATABASE_DSN='connection-to-empty-target'
$env:WS_PUBLIC_URL='https://skin.example.com'
$env:WS_TEXTURE_DIR='data/migrated-textures'
$env:WS_SETTINGS_KEY_FILE='secrets/wishingskin.settings.key'
New-Item -ItemType Directory -Force data | Out-Null
./bin/wishingskin.exe migrate
```

目标只允许初始化默认设置。不要把目标连接到现有运营库，不要复用源材质目录或任何包含该密钥文件的目录，也不要把备份恢复和 BS 导入混在同一个目标任务中。

## 4. 导入与中断恢复

使用相同的 snapshot、源材质目录和目标目录执行导入：

```powershell
./bin/wishingskin.exe import-bs --snapshot artifacts/source-snapshot.json --source-textures artifacts/source-textures
```

CLI 会再次执行预检查、迁移数据库并建立文件 journal。文件先校验和发布，账号、角色、材质、衣柜、品牌、报告历史、邀请码和 `partial` 状态在一个业务事务中提交。硬链接不可用、文件字节变化、目标目录已有无关文件或设置不符合空目标要求时，导入会停止。

如果进程中断，保留目标目录和数据库状态。使用完全相同的 snapshot 和目标目录重试；工具会验证 journal 并复用已经完成的文件。不要删除 `migration_state`、`.wishingskin-migration.json` 或为了重试换用另一份快照。报告中的 `already_imported` 表示同一任务已经提交，不表示第二次导入了重复账号。

## 5. 启动和核验

导入报告必须记录为 `partial`，并核对账号、角色、UUID、材质 hash、衣柜数量、邀请统计、权限映射、邮箱状态和未覆盖范围。启动服务后检查 `/readyz`，登录迁移的权限 2 管理员，再验证：

- 角色 UUID 与源值一致，皮肤和披风文件 hash 一致。
- 未公开材质不会出现在公开库，但已授权的衣柜关系仍可使用。
- 源未明确 `verified=1` 的账号不会被推断为已验证。
- 旧会话、游戏令牌、邮件链接和 OIDC/OAuth2 短期状态不继承。
- BS 余额保留，新站积分仍关闭；额外插件和运营规则按报告重新配置。
- 公开 origin、SMTP、Turnstile、OIDC 回调、CDN 和对象存储在目标环境分别配置，不从 BS 快照自动继承。

不要在这一步创建新管理员覆盖迁移权限。若源权限 2 账号不可用，应停止切换并在隔离目标按管理员恢复流程处理。

## 6. 切换与回退

保留源站只读和原始快照，先完成浏览器、启动器和管理验收，再切换反向代理或 DNS。切换前记录目标数据库、材质目录和设置密钥的备份位置。切换后源站仍应保持可回看，直到新站完成观察期。

迁移后的新数据不会写回 BS，也不会自动合并到旧 WishingSkin。若切换后回退，只能切回仍未接受新写入的源站，或使用迁移前的完整 WishingSkin 备份恢复隔离实例；不能把旧快照称为包含切换后数据的无损回滚。

## 支持范围和未实现项

迁移保留账号、角色、实际 UUID、材质记录和文件、衣柜、签名私钥、纯文本品牌、只读举报历史、明确邮箱验证状态、积分余额和邀请码。未知插件、任意 BS 运营配置、PHP 插件运行、完整 BS 后台写入兼容、远程对象存储和生产切换不自动迁移。完整边界见[BS 来源契约](bs-source-contract.md)和本手册中的迁移步骤。
