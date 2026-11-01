# 生产自部署手册

当前程序使用 schema 21。部署前请为数据库、材质目录、插件目录、备份目录和设置密钥安排持久化存储。

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

原生构建使用 Go 1.26.10、Node.js 24.19.0 和 pnpm 11.24.0，与 Dockerfile 保持一致。运行时需要已创建的 PostgreSQL 或 MySQL 数据库和独立材质目录。先构建 `web/dist`，再编译 Go 二进制：

```powershell
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

通过服务管理器注入 `WS_DATABASE_DRIVER`、`WS_DATABASE_DSN`、`WS_PUBLIC_URL` 和 `WS_TEXTURE_DIR`。程序不会自动读取 `.env`。首次运行 `serve` 或 `migrate` 会执行 schema 迁移；数据库本身必须提前创建，运行账号需要 DDL 权限。新安装还需在终端执行 `./bin/wishingskin.exe create-admin`，再启动 `./bin/wishingskin.exe serve`。BS 迁移按迁移手册初始化。

## 部署检查

部署后从反向代理和本机分别检查：

```powershell
Invoke-WebRequest https://skin.example.com/healthz
Invoke-WebRequest https://skin.example.com/readyz
```

`healthz` 表示进程可响应；`readyz` 还会检查数据库、迁移状态和加密设置是否可解密。两者都应返回 HTTP 200。公开 origin 由环境变量 `WS_PUBLIC_URL` 设置。登录管理员后配置 SMTP、Turnstile、注册规则和需要启用的插件。

只检查已启用服务在新环境中的配置：邮件能否送达、Turnstile 是否允许当前域名、身份提供商是否登记正确回调、外部账号服务及存储是否可达。使用云 CDN 时，还需确认缓存键保留查询参数、错误响应不缓存、过期时间符合配置。接入步骤见[账号与存储接入](integrations.md)。这些是部署配置检查，无需重复执行整套功能验收。

反向代理至少应保持原始 Host、HTTPS 访问和长请求的正常转发，并把上传请求体上限设置为不低于产品要求。代理缓存只应放在明确公开且已审核的材质路径，不能缓存登录、管理接口或私有材质。

## 日常维护

后台“存储与备份”可按时区和每日固定时间启用完整备份，目标目录必须已存在并独立于材质目录。保留份数默认是 0，即全部保留。设置大于 0 的份数后，每次备份成功会清理超出份数且校验完整的程序备份；手动 CLI 命名的备份和不完整目录保留。手动 CLI 备份、恢复和 BS 导入必须停应用写入，具体步骤见[备份与恢复](backup-restore.md)和[迁移手册](migration-runbook.md)。升级前先阅读[升级手册](upgrade-runbook.md)。

## 无 SMTP 的注册方式

在后台“站点设置 → 注册”选择“管理员验证”。配置 Turnstile 后可开放注册。新账号注册后处于待验证状态，管理员在“用户管理”中点击“完成邮箱验证”并确认，账号随后可以登录。此操作记录在管理日志中。默认仍使用邮件验证。

## 动态插件目录

默认目录为 `<WS_TEXTURE_DIR>/.plugins`，使用现有材质卷。原生部署可用 `WS_PLUGIN_DIR` 指定其他目录；该目录需要可读写和可执行权限，并纳入持久化存储。默认 Compose 无需新增卷。若使用自定义目录，需同时修改 Compose 环境变量与卷挂载。

插件包必须包含运行平台对应的程序。默认容器使用 Alpine，插件应使用静态构建或与 musl 兼容的 Linux 程序。源码插件仍需重新构建宿主；动态包可在后台安装、升级、重新加载和卸载。多实例部署应停写后统一更新插件目录，并逐个重启其他实例以同步内存中的插件清单。

完整备份包含受支持的动态包和插件数据，但不会保存插件自行写入的任意外部文件。此类文件按插件手册另行备份。接入方法见[插件 SDK](community-plugin-sdk.md)。
