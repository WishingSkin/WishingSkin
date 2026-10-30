# 升级、回退与恢复手册

当前程序理解 schema 18。迁移是单向前进的；`migrate` 没有降级命令。升级前必须保留同一版本的数据库、材质目录和 32 字节设置密钥备份，并确认备份目录有 `manifest.json`。

## 升级前检查

1. 记录当前二进制或镜像版本、数据库驱动、公开 origin、材质目录、设置密钥路径和反向代理配置。
2. 检查 `/readyz` 返回 200，确认没有 BS migration `pending`，没有运行中的上传、同步、回收或备份任务。
3. 停止应用写入，在当前版本执行完整备份。备份目录必须是新路径，设置密钥另行复制并限制访问。
4. 在隔离数据库和材质目录用相同版本执行一次 `restore`，检查账号、角色、材质、公开地址和管理登录。未完成这一步前不要覆盖生产程序。

```powershell
# 停止服务后，在当前版本目录执行；目标父目录必须存在。
New-Item -ItemType Directory -Force backups | Out-Null
./bin/wishingskin.exe backup --offline --directory backups/pre-upgrade-20261029
```

Compose 部署应先停止 `app`，保留数据库容器，再用当前镜像运行同一 CLI：

```powershell
docker compose --env-file .env -f deploy/compose.postgres.yaml stop app
docker compose --env-file .env -f deploy/compose.postgres.yaml run --rm --no-deps app backup --offline --directory /data/backups/pre-upgrade-20261029
```

## 执行升级

先在维护窗口构建或拉取新版本，再只启动应用。应用启动时会按顺序执行未完成的 schema migration；不要并行启动多个不同版本的应用。

```powershell
docker compose --env-file .env -f deploy/compose.postgres.yaml build app
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d app
docker compose --env-file .env -f deploy/compose.postgres.yaml logs --tail=100 app
Invoke-WebRequest http://127.0.0.1:8090/readyz
```

MySQL 只需替换 Compose 文件名。原生部署则替换服务管理器指向的新二进制并启动；不要在数据库迁移完成前接入代理流量。

验收至少包括管理员登录、普通账号登录、首页和公开库、公开与私有材质、Yggdrasil profile、SMTP/Turnstile 配置读取，以及启用功能对应的 OIDC、TOTP、审核和备份状态。记录 HTTP 状态、数据库 migration 版本和关键页面结果。

## 失败处理与回退

如果新程序在迁移或启动阶段失败，先保留日志、数据库和材质目录，不删除 migration 表、备份目录或设置密钥。若迁移事务已提交，旧程序可能无法理解新 schema；不要直接把旧 EXE 指回原目录。

推荐回退流程：

1. 停止新应用并保留当前现场。
2. 使用升级前的匹配版本、升级前数据库备份、原材质备份和原设置密钥，在新的隔离数据库和材质目录执行 `restore`。
3. 启动旧版本，完成登录、材质和游戏接口验收。
4. 将反向代理或服务配置切换到隔离回退实例。切换后产生的新数据不在旧备份中，需单独记录和处理；不能宣称无损回滚。

若必须原地恢复，先确认目标数据库和材质目录满足恢复工具的空目标要求。恢复过程中出现提交结果不确定或完成标记缺失时，先核对数据库状态再决定下一步，不要直接重试或清理目录。

## 恢复后的密钥与会话

恢复前把原设置密钥放回 `WS_SETTINGS_KEY_FILE` 指定路径；未指定时放在 `<WS_TEXTURE_DIR>/.settings.key`。缺少或错误密钥时，程序会拒绝启动或让 `/readyz` 返回 503，不会静默生成替代密钥。恢复会使网页会话、游戏令牌、邮件验证链接和 OIDC/OAuth2 短期凭据失效，用户需要重新登录或重新发起流程。
