# 安全政策

## 报告安全漏洞

如果你在 WishingSkin 中发现了安全问题，请**不要**在公开 Issue、Discussion 或 Pull Request 中直接披露细节。

请通过以下私密渠道提交报告：

- 在本仓库的 **Security → Advisories** 页面使用 [GitHub Private Vulnerability Reporting](https://github.com/WishingSkin/WishingSkin/security/advisories/new) 提交漏洞报告。

提交时请尽量附带：

1. 受影响的 WishingSkin 版本（或 commit SHA）及部署方式（MySQL / PostgreSQL、反向代理与主要配置）。
2. 问题复现步骤或最小 PoC，以及潜在影响范围。
3. 如有修复建议，欢迎一并附上。

我们会尽快确认接收并评估修复方案，在补丁发布后再协调公开披露。

## 安全边界说明

以下事项属于文档明确声明的设计边界，部署时请务必按手册配置：

- WishingSkin 本身不提供 TLS 终止，公网部署须配合 HTTPS 反向代理并正确配置 `WS_PUBLIC_URL` 与 `WS_TRUSTED_PROXIES`（见 [生产自部署手册](docs/deployment.md)）。
- 32 字节设置密钥（默认 `<WS_TEXTURE_DIR>/.settings.key`）用于加密数据库中的凭据与密钥，须独立妥善保管，避免泄露或丢失（见 [备份与恢复](docs/backup-restore.md)）。
- 动态可执行插件与 Go 源码插件按可信本机代码管理，仅做进程/接口隔离，不提供操作系统级沙箱（见 [社区插件 SDK](docs/community-plugin-sdk.md)）。
