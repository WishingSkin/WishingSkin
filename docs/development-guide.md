# 开发指南

## 工具与构建

使用 Go 1.26.10、Node.js 24.19.0、pnpm 11.24.0，以及 MySQL 8 或 PostgreSQL。版本与 `Dockerfile` 保持一致。

在仓库根目录执行：

```powershell
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

前端构建与 Go 编译、测试、vet 必须串行。Vite 会重建 `web/dist`，Go 的 embed 同时读取该目录时可能失败。

程序从进程环境读取配置，不自动加载 `.env`。配置数据库后，在终端执行 `./bin/wishingskin.exe create-admin` 初始化管理员，再运行 `./bin/wishingskin.exe serve`。环境配置和持久化要求见[部署手册](deployment.md)。

## 检查

```powershell
pnpm --dir web test
pnpm --dir web build
go test -timeout 20m ./... -count=1
go vet ./...
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

数据库测试需要 `WS_TEST_MYSQL_DSN` 和 `WS_TEST_POSTGRES_DSN`。未配置时会跳过相应测试；需要数据库覆盖时，检查输出中是否出现 skip。

可使用仓库提供的本地数据库：

```powershell
docker compose -f compose.test.yaml up -d --wait
$env:WS_TEST_MYSQL_DSN='root:wishingskin-local-test-only@tcp(127.0.0.1:33316)/wishingskin_test?parseTime=true'
$env:WS_TEST_POSTGRES_DSN='postgres://wishingskin_test:wishingskin-local-test-only@127.0.0.1:35416/wishingskin_test?sslmode=disable'
```

这些是专用测试凭据。测试账号需要创建数据库或 schema 的权限。测试会创建随机命名空间，并清理自己创建的数据；不要指向运营数据库。停止容器可运行 `docker compose -f compose.test.yaml stop`，保留数据卷。

修改时运行受影响的现有测试；数据库行为同时检查两种方言。页面改动检查类型构建、相关交互和窄屏布局。无需为纯文案或链接改动重复整套集成流程。

## 数据模型与迁移

模块和领域术语见[架构说明](architecture.md)。schema 定义与升级位于 `internal/store/migrate.go`。修改 schema 时同步维护备份表契约、升级手册和恢复规则，避免让新程序直接恢复旧 schema 的备份。

`testdata/bs` 提供合成 BS 快照和 PNG，供迁移、备份及 HTTP 测试使用。仓库快照不含私钥，手动预检查前需生成独立样本：

```powershell
go run ./testdata/bs/generate.go artifacts/generated-bs-fixture
```

目标目录必须尚不存在。生成结果包含测试私钥和可选合成 SQL 种子，应留在本地。生成器不连接或修改真实 BS 站点。`testdata/appearance` 用于生成外观测试图片，`testdata/resourcecheck` 提供资源测量工具。

## 插件与前端

社区插件的注册、配置、页面和任务契约见[插件 SDK](community-plugin-sdk.md)。依赖已有宿主接口，避免直接修改核心表。

界面使用简体中文和 English 文案，个性化覆盖按语言分别保存。复用现有模态弹窗、Toast、表单和主题变量。动画需支持 `prefers-reduced-motion`。用户可配置的图片和文字规则见[界面设置](interface-customization.md)。

## 提交范围

提交源码、必要测试资源、用户手册、架构与接口说明。构建产物、浏览器记录、真实数据、凭据、个人工具配置、实施计划和详细验收流水留在本地。新增依赖时同步核对锁文件和[第三方声明](../THIRD_PARTY_NOTICES.md)。
