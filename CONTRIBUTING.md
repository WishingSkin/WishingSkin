# 贡献指南 / Contributing to WishingSkin

感谢你关注并参与 WishingSkin！无论是报告问题、改进文档、完善界面体验，还是提交后端与扩展功能，都欢迎通过 Issue 或 Pull Request 与我们协作。

## 仓库分工

- [WishingSkin/WishingSkin](https://github.com/WishingSkin/WishingSkin)：宿主服务端、Vue 前端工作台、皮肤站核心 API、Yggdrasil、备份迁移工具与系统手册。
- [WishingSkin/plugins](https://github.com/WishingSkin/plugins)：官方 Plugin Runtime v2 动态插件、打包验签工具 `tools/pack` 以及静态市场索引 `catalog.json`。

## 开发环境要求

- **Go**：`1.26.11`（见 `go.mod`）
- **Node.js / pnpm**：Node.js `24.19.0`、pnpm `11.24.0`
- **数据库（运行集成测试时可选其一或双库）**：PostgreSQL 与 MySQL 8

更多本地调试与界面视觉规范请参考 [开发指南](docs/development-guide.md) 与 [集成测试说明](docs/integration-testing.md)。

## 本地构建与验证流程

> [!IMPORTANT]
> 前端构建（`pnpm --dir web build`）会先删除旧的哈希资源再输出至嵌入式目录，因此必须**串行执行**前端构建与 `go build` / `go test` / `go vet`，避免 Go `embed` 读取到清理中的中间状态。

```powershell
# 1. 前端依赖、单元测试与生产构建
pnpm --dir web install --frozen-lockfile
pnpm --dir web test
pnpm --dir web build

# 2. Go 静态检查与无库单元测试
go vet ./...
go test ./...
```

如需运行完整 MySQL 8 与 PostgreSQL 双库集成测试，请设置对应测试 DSN 后再执行 `go test ./...`。

## 提交与 Pull Request 规范

1. **保持改动聚焦**：一次 PR 只解决一个明确问题或功能点，避免顺手重构无关代码或调整未涉及文件的排版。
2. **契约与兼容性**：修改配置项、数据库迁移、API 字段或 Yggdrasil 响应时，须同步检查离线备份恢复（`internal/backup`）并更新对应文档。
3. **验证透明**：在 PR 描述中如实填写已运行的测试命令、数据库环境与验证范围；涉及前端界面或样式调整时，建议附上桌面端与移动端截图。
4. **文档与界面文案**：使用清晰直接的中文表达，不随意新增依赖或过度抽象；英文映射文件见 `web/src/i18n.en.ts`。

## 反馈渠道

- **Bug 报告 / 功能建议**：使用 [Issue 表单](https://github.com/WishingSkin/WishingSkin/issues/new/choose) 提交结构化信息。
- **部署与使用答疑**：前往 [GitHub Discussions](https://github.com/WishingSkin/WishingSkin/discussions)。
- **安全漏洞**：请勿在公开 Issue 或 PR 中披露，务必通过 [GitHub Private Vulnerability Reporting](https://github.com/WishingSkin/WishingSkin/security/advisories/new) 私密报告，详见 [SECURITY.md](SECURITY.md)。
