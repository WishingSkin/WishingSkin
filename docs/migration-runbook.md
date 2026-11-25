# Blessing Skin 迁移手册

本手册用于将已核对的 Blessing Skin `6.0.2` 数据迁入一套全新的 WishingSkin（schema 29）实例。受支持的固定插件来源包括官方传统 `yggdrasil-api 5.2.1`，以及按源码树摘要固定的归档联合版 `5.2.2-0.2.5`，完整规则见[BS 来源契约](bs-source-contract.md)。

其他 BS 版本、远程对象存储文件系统、未知插件及 BS 写入 API 不在本迁移范围内。导入报告会标记为 `partial`，用于明确标示迁入边界，切换流量前请务必核对报告中的缺口项。

## 迁移前注意事项

- **面向全新空站**：迁移不是向已有 WishingSkin 合并数据，目标数据库、材质目录和设置密钥必须完全独立。不要在目标库预先执行 `create-admin`（源站权限 2 管理员会直接迁入）。
- **保持源站一致窗口**：停写源站后再导出数据库快照并复制源材质目录，不要边上传边拷贝。
- **妥善保管敏感快照**：导出的 snapshot 包含密码哈希、站点盐、Yggdrasil 私钥和邀请码迁移数据，仅限存放在受限目录，切勿提交 Git、发送至聊天窗口或打印到日志。不要在迁移流程中删除源站数据。

## 步骤 1：核对源站信息

对照源站实际文件确认 BS 版本、传统 `yggdrasil-api` 插件版本、密码算法与 `SALT`、UUID 算法、材质 hash 规则、表前缀以及 `TEXTURES_DIR`，复制模板并填写核实值：

```powershell
New-Item -ItemType Directory -Force artifacts | Out-Null
Copy-Item deploy/bs-source.example.json artifacts/source-info.json
# 编辑 artifacts/source-info.json，不要将真实凭据写入日志
```

为源 MySQL 准备一个**只读账号**（导出器只执行 `SELECT`）。源站停写后，复制源材质目录的完整本地副本（例如 `artifacts/source-textures`），文件名须为无扩展名的源 hash，且不得包含符号链接。

## 步骤 2：只读导出与离线预检查

```powershell
$env:WS_BS_SOURCE_DSN='readonly_user:replace-me@tcp(127.0.0.1:3306)/confirmed_source?parseTime=true'
./bin/wishingskin.exe export-bs --source-info artifacts/source-info.json --output artifacts/source-snapshot.json
./bin/wishingskin.exe preflight --snapshot artifacts/source-snapshot.json --source-textures artifacts/source-textures
```

- 导出不执行 SQL dump，也不修改源库；如源库有表前缀，给 `export-bs` 添加 `--table-prefix prefix_`。输出路径必须尚不存在。
- 若 `preflight` 报告 `blockers`（如失效收藏、缺失文件、重复身份或非法 UUID），先保留原始数据库备份与材质归档并记录摘要，仅在**独立的源库副本**上经数据所有者确认后修复问题，再重新导出新快照；切勿手工篡改 snapshot JSON 或放宽校验。合法旧高清披风按来源契约原样保留。

## 步骤 3：初始化空目标环境

提前创建空的 PostgreSQL 或 MySQL 目标数据库，确保无任何业务进程写入。建议将 `WS_SETTINGS_KEY_FILE` 指定到材质目录之外的独立持久路径（导入时会自动生成 32 字节密钥，请立即备份）；若使用默认密钥路径，目标材质目录须为空，或仅包含一个有效的 32 字节 `.settings.key`。

```powershell
$env:WS_DATABASE_DRIVER='postgres' # 或 mysql
$env:WS_DATABASE_DSN='connection-to-empty-target'
$env:WS_PUBLIC_URL='https://skin.example.com'
$env:WS_TEXTURE_DIR='data/migrated-textures'
$env:WS_SETTINGS_KEY_FILE='secrets/wishingskin.settings.key'
New-Item -ItemType Directory -Force data | Out-Null
./bin/wishingskin.exe migrate
```

切勿将目标指向现有运营库、源材质目录，也不要把备份恢复与 BS 导入混在同一目标任务中。

## 步骤 4：执行导入与中断续跑

```powershell
./bin/wishingskin.exe import-bs --snapshot artifacts/source-snapshot.json --source-textures artifacts/source-textures
```

导入器会再次执行预检查并写入文件 journal：先校验并通过硬链接发布材质文件，再在同一数据库事务中提交账号、角色、材质、衣柜、品牌、只读举报历史、邀请码及 `partial` 迁移状态。

若中途中断，请保留目标目录与数据库状态，使用**完全相同的 snapshot 和目标目录**重试即可（程序会自动校验并复用已发布文件）。不要删除 `migration_state` 表或 `.wishingskin-migration.json`，也不要更换另一份快照。再次运行已完成任务时返回 `already_imported=true`，不会重复写入。

## 步骤 5：启动与核验

核对导入报告中的 `partial` 标记、账号/角色/UUID/材质 hash/衣柜统计、权限映射、邮箱验证状态与缺口清单。启动服务并确认 `/readyz` 返回 200 后，用迁入的权限 2 管理员登录并抽查：

- 角色 UUID、皮肤和披风文件 hash 与源站完全一致。
- 未公开材质隐藏于公开库，但原已授权的衣柜收藏仍可正常穿戴。
- 源站未明确标记 `verified=1` 的账号不会被推断为已验证；旧网页会话、游戏令牌与短期链接不继承。
- 积分余额保留，新站积分默认关闭；公开地址、SMTP、Turnstile、OIDC、CDN 和对象存储按新环境单独配置。
- 若源权限 2 账号异常不可用，请暂缓切换并在隔离环境中排查，不要贸然覆盖权限。

## 步骤 6：切换流量与回退说明

保持源站只读状态与原始快照完整，完成浏览器、游戏启动器和后台验收后再切换反向代理或 DNS，并留足观察期。

迁入后产生的新数据不会回写至 BS。若切换后需要回退，只能切回未写入新数据的源站，或用迁移前的 WishingSkin 完整备份恢复隔离实例；切换后新增的数据无法无损回滚至旧快照。

## 迁移范围速览

- **迁入**：账号与余额、权限映射（仅 `permission=2` 映射为管理员）、角色与实际 UUID、材质记录（含有效上传时间）与物理文件、衣柜收藏与别名、Yggdrasil 签名私钥、纯文本品牌/公告、只读历史举报、明确的邮箱验证状态和邀请码。
- **不自动迁入**：未知第三方插件、任意 BS 运营配置、PHP 插件运行逻辑、远程对象存储文件及 BS 后台写入接口。详见[BS 来源契约](bs-source-contract.md)。
