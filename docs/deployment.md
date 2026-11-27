# 生产自部署手册

当前版本使用数据库 schema 29。部署前请先为数据库、材质目录、插件目录、备份目录以及 32 字节设置密钥规划好持久化存储。全部环境变量与命令行参数见[配置与 CLI 参考](configuration.md)。

## 部署前的网络与存储边界

- **反向代理与 TLS**：WishingSkin 本身不提供 HTTPS 终止。公网部署请在前方放置 Nginx / Caddy / Traefik 等反向代理，并将 `WS_PUBLIC_URL` 设为访客实际访问的完整 HTTPS origin（例如 `https://skin.example.com`）。
- **可信代理配置**：反向代理应保留原始 `Host`、覆写 `X-Forwarded-For`，并把上传请求体大小放宽至不低于产品上限（例如材质导入最大 80 MiB）。仅把受控代理的 CIDR 写入 `WS_TRUSTED_PROXIES`，切勿加入公网用户可直接到达的网段。代理缓存只能作用于明确公开且已审核的材质路径，不要缓存登录、管理接口或私有材质。
- **数据与密钥分离**：业务记录存在数据库，材质文件与默认设置密钥位于 `WS_TEXTURE_DIR`。设置密钥是首次启动生成的 32 字节随机文件，**业务备份不包含此密钥，必须单独安全备份且不能随意替换**。自动备份目录必须独立于材质目录；Docker 命名卷仍位于同一宿主机，不属于异地灾备。

## 方式一：Docker Compose（推荐）

仓库在 `deploy/` 提供 PostgreSQL 与 MySQL 两套编排文件，按需二选一，不要同时启动。

1. 复制环境模板并修改所有默认密码（`.env` 已被 Git 忽略，切勿提交）：

**Linux / macOS**：

```bash
cp .env.example .env
# 编辑 .env，至少配置 WS_PUBLIC_URL、WS_DATABASE_DSN 及数据库密码
docker compose --env-file .env -f deploy/compose.postgres.yaml config
docker compose --env-file .env -f deploy/compose.postgres.yaml build app
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d postgres
```

**Windows (PowerShell)**：

```powershell
Copy-Item .env.example .env
# 编辑 .env，至少配置 WS_PUBLIC_URL、WS_DATABASE_DSN 及数据库密码
docker compose --env-file .env -f deploy/compose.postgres.yaml config
docker compose --env-file .env -f deploy/compose.postgres.yaml build app
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d postgres
```

> [!TIP]
> 若选用 MySQL，将编排文件换为 `deploy/compose.mysql.yaml`，设置 `MYSQL_PASSWORD`、独立的 `MYSQL_ROOT_PASSWORD` 以及主机名为 `mysql` 的 DSN。两份编排均已固定数据库镜像摘要；升级数据库镜像前请先完成完整备份。

2. 服务默认监听宿主机的 `127.0.0.1:8090`，并持久化 `textures`、`backups` 与数据库命名卷。日常维护请勿运行 `docker compose down -v`，否则会清空数据卷。
3. **初始化首个管理员**（仅全新空站执行）：

```bash
docker compose --env-file .env -f deploy/compose.postgres.yaml run --rm --no-deps app create-admin
docker compose --env-file .env -f deploy/compose.postgres.yaml up -d app
```

`create-admin` 要求真实交互终端，不从命令行参数或管道读取密码。如果准备从 Blessing Skin 迁入旧数据，请跳过此步，迁入的权限 2 账号会自动保留管理员身份。

## 方式二：预编译二进制或源码构建

推荐直接从 [GitHub Releases](https://github.com/WishingSkin/WishingSkin/releases) 下载对应平台（`linux` / `windows` / `darwin` 的 `amd64` / `arm64`）压缩包并核对 `SHA256SUMS.txt`；若使用预构建容器镜像，也可将编排文件中的 `build:` 替换为官方多架构镜像 `ghcr.io/wishingskin/wishingskin:<版本>`。

**Linux / macOS**：

```bash
# 以 Linux amd64 为例（macOS 可替换为 darwin_arm64 并改用 shasum -a 256）
sha256sum wishingskin_<版本>_linux_amd64.tar.gz
tar -xzf wishingskin_<版本>_linux_amd64.tar.gz
```

**Windows (PowerShell)**：

```powershell
Get-FileHash wishingskin_<版本>_windows_amd64.zip -Algorithm SHA256
Expand-Archive wishingskin_<版本>_windows_amd64.zip -DestinationPath .
```

如需自行从源码构建，环境需与 `Dockerfile` 保持一致（Go 1.26.11、Node.js 24.19.0、pnpm 11.24.0），先构建前端 `web/dist`，再编译 Go 程序：

**Linux / macOS**：

```bash
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -trimpath -o bin/wishingskin ./cmd/wishingskin
```

**Windows (PowerShell)**：

```powershell
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

部署步骤：

1. 提前创建空的 PostgreSQL 或 MySQL 数据库（运行账号需具备 DDL 权限）以及独立的材质目录。
2. 通过 systemd、Docker 或服务管理器注入 `WS_DATABASE_DRIVER`、`WS_DATABASE_DSN`、`WS_PUBLIC_URL` 和 `WS_TEXTURE_DIR`（程序不会自动加载 `.env`）。
3. 首次运行 `serve` 或 `migrate` 会自动执行 schema 迁移。新站先在终端运行 `./wishingskin create-admin`，再启动 `./wishingskin serve`（源码构建产物位于 `bin/wishingskin`，Windows 对应为 `.\wishingskin.exe` 或 `.\bin\wishingskin.exe`）；若从 BS 迁入，请改按[迁移手册](migration-runbook.md)执行。

**Linux / macOS**：

```bash
export WS_DATABASE_DRIVER='postgres' # 或 mysql
export WS_DATABASE_DSN='postgres://wishingskin:replace-me@127.0.0.1:5432/wishingskin?sslmode=disable'
export WS_PUBLIC_URL='https://skin.example.com'
export WS_TEXTURE_DIR='data/textures'
./wishingskin create-admin
./wishingskin serve
```

**Windows (PowerShell)**：

```powershell
$env:WS_DATABASE_DRIVER='postgres' # 或 mysql
$env:WS_DATABASE_DSN='postgres://wishingskin:replace-me@127.0.0.1:5432/wishingskin?sslmode=disable'
$env:WS_PUBLIC_URL='https://skin.example.com'
$env:WS_TEXTURE_DIR='data/textures'
.\wishingskin.exe create-admin
.\wishingskin.exe serve
```

## 上线后核验

分别从本机和反向代理外网入口检查健康接口：

**Linux / macOS**：

```bash
curl -i https://skin.example.com/healthz
curl -i https://skin.example.com/readyz
```

**Windows (PowerShell)**：

```powershell
Invoke-WebRequest https://skin.example.com/healthz
Invoke-WebRequest https://skin.example.com/readyz
```

- `/healthz` 返回 HTTP 200 表示 HTTP 进程存活。
- `/readyz` 返回 HTTP 200 表示数据库连接、schema 迁移以及加密设置解密全部正常。

随后登录管理员后台，按实际需要配置 SMTP 发信、Turnstile、注册策略、主题外观与插件，并逐项确认已启用外部服务的连通性（邮件投递、Turnstile 域名白名单、OIDC 回调地址、S3 读写以及 CDN 缓存键是否保留查询参数且不缓存错误响应），接入细节见[游戏、账号与存储接入](integrations.md)。

## 无 SMTP 环境的注册方案

若暂未配置发信服务，可在“站点设置 → 注册”中将邮箱验证方式切换为“管理员验证”，配合 Turnstile 即可开放注册。新用户提交后处于待激活状态，管理员在“用户管理”中点击“完成邮箱验证”确认放行，操作会记入管理审计日志。

## 动态插件目录与多实例说明

- 动态插件包默认存放在 `<WS_TEXTURE_DIR>/.plugins`（复用现有材质卷）；原生部署也可通过 `WS_PLUGIN_DIR` 指定独立路径，需具备读写和执行权限并纳入持久化。
- 插件包内的可执行程序须匹配运行平台；默认容器基于 Alpine，Linux 插件应采用静态构建或兼容 musl。
- 源码插件更新需重新编译主程序；动态包支持在后台直接安装、升级、热重载和卸载。多实例部署更新插件时，请先保留单实例完成操作，再逐个重启其余实例以同步内存中的插件清单。
- 完整备份会自动包含受支持的动态插件包与插件数据，但不覆盖插件自行写入宿主磁盘的外部文件。

## 日常维护

- 在后台“存储与备份”可按指定时区每日定时备份（目标目录须已存在且独立于材质目录）。保留份数和自动清理规则见[备份与恢复](backup-restore.md)。
- 手动 CLI 备份、隔离恢复和版本升级必须先停写，操作步骤见[备份与恢复](backup-restore.md)与[升级、回退与恢复手册](upgrade-runbook.md)。
