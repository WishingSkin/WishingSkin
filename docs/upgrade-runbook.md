# 升级、回退与恢复手册

当前版本使用数据库 schema 29。数据库迁移是单向演进的，`migrate` 不提供降级命令。每次升级前，务必用**当前正在运行的版本**做好数据库、材质目录、插件目录与 32 字节设置密钥的完整备份，并确认备份目录内已生成 `manifest.json`。

## schema 20–29 关键变化

- **schema 20**：新增动态插件数据与迁移记录、Webhook 订阅与投递队列、动态插件事件队列，以及 OAuth 授权元数据与刷新令牌表。
- **schema 21**：新增授权会话时间、刷新令牌族及 Access Token 关联字段，并扩展凭据摘要。
- **schema 22**：移除待验证账号原先建立的 OIDC 绑定，账号完成验证后可重新绑定。
- **schema 23**：分开保存主题资源及其元数据，保留原有资源地址。
- **schema 24**：新增材质摘要记录，用于核验上传文件与备份源文件。
- **schema 25**：新增 `texture_likes` 表与热门排序索引，支持公开材质点赞、近期热门与点赞榜单，并同步纳入完整备份与恢复。
- **schema 26**：新增 `users.character_limit`、`texture_records.created_at` 与 `texture_records_created_idx` 索引，支持单用户独立角色上限及公开库按上传时间最新排序，并同步纳入完整备份与恢复。
- **schema 27**：为 PostgreSQL `characters(user_id)` 补充索引 `characters_user_id_idx`（与 MySQL 外键索引对齐）。
- **schema 28**：新增 `user_notifications`（个人通知收件箱及未读/分页索引）、`minecraft_verifications`（正版 Minecraft Java 验证绑定）与 `minecraft_flows`（短期正版授权状态），前两张业务表纳入完整备份与恢复。
- **schema 29**：为 `oauth_clients` 增加 `allowed_scopes` 权限白名单列，新增 `point_codes` 活动积分码表与 `point_code_redemptions` 防重领记录表（`allowed_scopes` 随客户端配置纳入完整备份，活动积分码与兑换表按凭据策略排除）。
- 现有账号与插件配置平滑保留；动态插件默认存放在 `<WS_TEXTURE_DIR>/.plugins`（使用自定义 `WS_PLUGIN_DIR` 时须单独持久化）；OAuth 签名密钥自动生成并用原站点设置密钥加密保存。
- 低于 schema 29 的备份须先用对应旧版本恢复到独立空目标，再用新版程序升级至 schema 29。升级后不要让旧程序再连接该数据库。

## 升级前检查清单

1. 记录当前二进制或镜像版本、数据库驱动、公开 `WS_PUBLIC_URL`、材质目录、插件目录、设置密钥路径与反向代理配置。
2. 确认 `/readyz` 返回 HTTP 200，无 `pending` 状态的 BS 迁移，且无正在执行的上传、同步、回收或备份任务。
3. 停止应用写入，用当前版本执行完整离线备份，并单独复制保管 32 字节设置密钥。
4. 在隔离的空数据库与空材质目录上用同一旧版本演练一次 `restore`，核验账号、角色、材质和管理登录；未验证备份可用前不要覆盖生产程序。

**Linux / macOS**：

```bash
# 原生部署：停止服务后在当前版本目录执行
mkdir -p backups
./bin/wishingskin backup --offline --directory backups/pre-upgrade-20261029
```

**Windows (PowerShell)**：

```powershell
# 原生部署：停止服务后在当前版本目录执行
New-Item -ItemType Directory -Force backups | Out-Null
.\bin\wishingskin.exe backup --offline --directory backups/pre-upgrade-20261029
```

```bash
# Docker Compose：先停 app（保留数据库容器），再通过一次性容器执行备份
docker compose --env-file .env -f deploy/compose.postgres.yaml stop app
docker compose --env-file .env -f deploy/compose.postgres.yaml run --rm --no-deps app backup --offline --directory /data/backups/pre-upgrade-20261029
```

## 执行升级

在维护窗口内构建或拉取新版本并启动应用。服务启动会自动按序完成未执行的 schema 迁移；切勿同时运行多个不同版本的实例。

**Linux / macOS**：

```bash
docker compose --env-file .env -f deploy/compose.postgres.yaml build app
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d app
docker compose --env-file .env -f deploy/compose.postgres.yaml logs --tail=100 app
curl -i http://127.0.0.1:8090/readyz
```

**Windows (PowerShell)**：

```powershell
docker compose --env-file .env -f deploy/compose.postgres.yaml build app
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d app
docker compose --env-file .env -f deploy/compose.postgres.yaml logs --tail=100 app
Invoke-WebRequest http://127.0.0.1:8090/readyz
```

MySQL 部署将编排文件改为 `deploy/compose.mysql.yaml`。原生部署请先确认新进程迁移完成且 `/readyz` 返回 200，再放行反向代理流量。

升级完成后抽检：管理员与普通用户登录、首页与公开库加载、材质读写、Yggdrasil 角色资料、已启用外部服务的配置读取以及定时备份计划，并记录最新 schema 版本。

## 失败处理与隔离回退

若新程序在迁移或启动阶段报错，请保留日志、数据库与材质目录现场，不要删除迁移表、备份或设置密钥。一旦迁移事务已提交，旧版本无法安全读写新 schema，切勿直接把旧程序指回原数据库。

建议采用隔离回退流程：

1. 停止新版本应用，保留原现场用于排查。
2. 使用升级前的旧程序、升级前完整备份及原设置密钥，在全新的空数据库和空材质目录上执行 `restore`。
3. 启动旧版本并完成登录、材质与游戏接口验证。
4. 将反向代理流量切向该回退实例。若新站曾短暂接收过写入，新增数据不会出现在旧备份中，需单独记录处理。

## 恢复后的密钥与会话说明

- 必须先执行 `restore`，在启动服务前再把原 32 字节设置密钥放回 `WS_SETTINGS_KEY_FILE` 指定路径（默认 `<WS_TEXTURE_DIR>/.settings.key`）。不要提前创建默认材质目录放密钥，否则 `restore` 会因目标目录已存在而拒绝执行。
- 缺少或放错密钥会导致程序拒绝启动或 `/readyz` 返回 503。
- 恢复完成后，原有网页会话、游戏令牌、邮件验证链接及 OIDC/OAuth2 短期凭据全部失效，用户重新登录即可。
