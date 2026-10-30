# 生产自部署手册

本文面向第一次部署 WishingSkin 的运维人员。当前程序使用 schema 18。示例不包含真实密码、token、Client Secret 或 SMTP 凭据。部署前请为数据库、材质目录、备份目录和设置密钥安排持久化存储。

## 运行边界

WishingSkin 原生 HTTP 服务不终止 TLS。公网部署需要在它前面放置反向代理，并把 `WS_PUBLIC_URL` 设置为用户实际访问的 HTTPS origin，例如 `https://skin.example.com`。代理必须把请求转发到应用监听地址，覆盖 `X-Forwarded-For`，并只在应用的 `WS_TRUSTED_PROXIES` 中登记受控代理地址。不要把用户可直接访问的网段加入可信代理列表。

应用只使用数据库保存业务数据，材质文件和设置密钥在 `WS_TEXTURE_DIR`。设置密钥是 32 字节随机文件，首次启动时创建，不能用新文件替换。业务备份不包含它，必须单独备份。自动备份目录必须独立于材质目录；Docker 部署可使用 `/data/backups`，但命名卷仍只属于同一主机，不是异地灾备。

## Docker Compose（推荐）

1. 准备 Docker Compose、反向代理和一个 DNS/TLS 入口。PostgreSQL 与 MySQL 只能选择一套 Compose 文件，不要同时启动。
2. 复制环境模板并替换所有示例密码。`.env` 已被 Git 忽略，不能提交。

```powershell
Copy-Item .env.example .env
# 编辑 .env：至少设置 WS_PUBLIC_URL、WS_DATABASE_DSN、数据库密码。
docker compose --env-file .env -f deploy/compose.postgres.yaml config
docker compose --env-file .env -f deploy/compose.postgres.yaml build app
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d postgres
```

MySQL 使用 `deploy/compose.mysql.yaml`，并设置 `MYSQL_PASSWORD`、独立的 `MYSQL_ROOT_PASSWORD` 和指向 `mysql` 主机名的 DSN。Compose 已固定数据库镜像摘要；变更镜像前应先做备份并记录回退镜像。

应用暴露到宿主机的默认地址是 `127.0.0.1:8090`。两份 Compose 都持久化 `textures`、`backups` 和数据库卷。不要使用 `docker compose down -v`，这会删除卷中的数据。

首次安装只在空账号库执行交互式管理员初始化：

```powershell
docker compose --env-file .env -f deploy/compose.postgres.yaml run --rm --no-deps app create-admin
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d app
```

该命令要求真实终端，密码不会从参数或管道读取。导入 BS 数据时不要执行它，迁移来的权限 2 账号会保留管理员权限。

## 原生二进制

原生部署需要 Go 构建产物、已创建的 PostgreSQL 或 MySQL 数据库和独立材质目录。前端修改后必须先构建 `web/dist`，再编译 Go 二进制：

```powershell
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

通过服务管理器注入 `WS_DATABASE_DRIVER`、`WS_DATABASE_DSN`、`WS_PUBLIC_URL` 和 `WS_TEXTURE_DIR`。程序不会自动读取 `.env`。首次运行 `serve` 或 `migrate` 会执行 schema 迁移；数据库本身必须提前创建，运行账号需要 DDL 权限。

## 首次验收

部署后从反向代理和本机分别检查：

```powershell
Invoke-WebRequest https://skin.example.com/healthz
Invoke-WebRequest https://skin.example.com/readyz
```

`healthz` 只表示进程可响应；`readyz` 还会检查数据库、迁移状态和加密设置是否可解密。两者都应返回 HTTP 200。然后登录管理员，配置站点 origin、SMTP、Turnstile、注册规则和需要启用的插件；真实 SMTP 投递、Turnstile 联网校验、OIDC 回调、AuthMe/HTTP 连接器、对象存储和 CDN 必须在实际环境逐项验收。

反向代理至少应保持原始 Host、HTTPS 访问和长请求的正常转发，并把上传请求体上限设置为不低于产品要求。代理缓存只应放在明确公开且已审核的材质路径，不能缓存登录、管理接口或私有材质。

## 日常维护

后台“存储与备份”可按时区和每日固定时间启用完整备份，目标目录必须已存在并独立于材质目录。默认不自动删除旧备份；需要保留策略时由运维在独立备份系统中实施。手动 CLI 备份、恢复和 BS 导入必须停应用写入，具体步骤见[备份与恢复](backup-restore.md)和[迁移手册](migration-runbook.md)。升级前先阅读[升级手册](upgrade-runbook.md)。

## 无 SMTP 的注册方式

在后台“站点设置 → 注册”选择“管理员验证”。配置 Turnstile 后可开放注册。新账号注册后处于待验证状态，管理员在“用户管理”中点击“完成邮箱验证”并确认，账号随后可以登录。此操作记录在管理日志中。默认仍使用邮件验证。
