# 配置与 CLI 参考

WishingSkin 从进程环境变量读取启动配置，不会自动加载 `.env` 文件（Docker Compose 需通过 `--env-file .env` 传入）。

## 环境变量

### 核心运行配置

| 变量名 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `WS_DATABASE_DRIVER` | 否 | `postgres` | 数据库类型，可选 `postgres` 或 `mysql` |
| `WS_DATABASE_DSN` | 是 | 无 | 数据库连接串；目标数据库须提前创建，账号须具备 DDL 权限 |
| `WS_LISTEN` | 否 | `127.0.0.1:8090` | HTTP 监听地址；公网访问请置于 TLS 反向代理之后 |
| `WS_PUBLIC_URL` | 建议填写 | `http://localhost:8090` | 对外公开访问的完整 origin（如 `https://skin.example.com`），用于生成 Yggdrasil、OIDC、OAuth2 回调和材质链接 |
| `WS_TEXTURE_DIR` | 否 | `data/textures` | 材质文件持久化目录；默认同时存放 `.settings.key` 与 `.plugins` |
| `WS_PLUGIN_DIR` | 否 | `<WS_TEXTURE_DIR>/.plugins` | 动态插件包存放目录，需具备读写与执行权限 |
| `WS_SETTINGS_KEY_FILE` | 否 | `<WS_TEXTURE_DIR>/.settings.key` | 32 字节站点设置加密密钥文件路径；生产环境建议放在材质目录之外的受限位置并单独备份 |
| `WS_TRUSTED_PROXIES` | 否 | 空 | 允许信任 `X-Forwarded-For` 的反向代理网段，逗号分隔的 IPv4/IPv6 CIDR（如 `127.0.0.1/32,172.18.0.0/16`）；切勿填入公网用户可达网段 |

### 初始邮件与人机验证配置（可选）

以下环境变量仅作为站点初始值。在后台“站点设置 → 邮件与验证”保存过配置后，数据库中的加密 `service_config` 会优先生效。

| 变量名 | 说明 |
| --- | --- |
| `WS_SMTP_ADDRESS` | SMTP 服务器地址（`host:port`）；若配置发信，此项必填 |
| `WS_SMTP_FROM` | 发件人邮箱地址 |
| `WS_SMTP_USERNAME` | SMTP 登录用户名 |
| `WS_SMTP_PASSWORD` | SMTP 登录密码 |
| `WS_SMTP_TLS` | 加密方式，默认 `starttls` |
| `WS_TURNSTILE_SITE_KEY` | Cloudflare Turnstile Site Key（需与 Secret 成对配置） |
| `WS_TURNSTILE_SECRET` | Cloudflare Turnstile Secret Key（需与 Site Key 成对配置） |

### 迁移专用配置

| 变量名 | 说明 |
| --- | --- |
| `WS_BS_SOURCE_DSN` | 仅 `export-bs` 子命令使用，指向 Blessing Skin 6.0.2 源库的只读 MySQL DSN |

## CLI 子命令

不带参数直接执行二进制时，默认运行 `serve`。

```text
wishingskin [command] [flags]
```

| 子命令 | 用途 | 常用参数与要求 |
| --- | --- | --- |
| `serve` | 执行 schema 迁移并启动 HTTP 服务与后台任务 | 默认命令；若数据库存在加密配置但缺失密钥文件，会拒绝启动 |
| `migrate` | 仅初始化或升级数据库 schema 后退出 | 常用于新站或空恢复目标的预处理 |
| `create-admin` | 在空账号库交互式创建首个管理员 | 要求真实交互终端，不从参数或管道读取密码；从 BS 迁入时不要执行 |
| `backup` | 停写后导出完整离线业务备份 | `--offline --directory <新目录路径>`；须先停止所有写入实例 |
| `restore` | 将离线备份恢复到全新空数据库与空材质目录 | `--offline --directory <备份目录路径>`；要求先用同版本执行 `migrate`，且目标目录尚不存在 |
| `export-bs` | 只读导出 Blessing Skin 6.0.2 快照 JSON | `--source-info <元数据 JSON> --output <新快照路径> [--table-prefix <前缀>]`，需设置 `WS_BS_SOURCE_DSN` |
| `preflight` | 离线校验 BS 快照与源材质目录，不连接数据库 | `--snapshot <快照路径> --source-textures <源材质目录>` |
| `import-bs` | 将通过预检查的 BS 快照导入全新空 WishingSkin 目标 | `--snapshot <快照路径> --source-textures <源材质目录>` |

详细操作顺序与回退边界请分别参考[生产自部署手册](deployment.md)、[备份与恢复](backup-restore.md)和[Blessing Skin 迁移手册](migration-runbook.md)。
