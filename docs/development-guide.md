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

视觉 token 位于 `web/src/tokens.css`。字号使用 12、14、20px，首页主标题使用 40px；控件、卡片和弹窗圆角分别为 6、12、16px。阴影分为卡片和浮层两档。`themes.css` 只覆盖配色。组件引用语义颜色和尺寸变量，二维码使用独立白底变量。

默认图标通过 `AppIcon` 使用 Heroicons outline SVG，许可见[第三方声明](../THIRD_PARTY_NOTICES.md)。`InterfaceIcon` 保留管理员自定义覆盖。加载界面复用 `LoadingSkeleton`；卡片骨架沿用材质列表网格。

页面地址由 `web/src/workspaceRoutes.ts` 统一处理。材质浏览和上传分别使用 `/user/resources`、`/user/resources/upload`；旧 `/user/materials?tab=…` 地址会转换到对应页面。后台使用 `/admin/users`、`/admin/resources`、`/admin/events` 等子页，设置分类和插件配置也有独立地址。子页支持刷新、浏览器前进和后退。

同一工作区切换子页时，只对 `data-panel-region` 标记的内容区播放动画，保留标题和导航。公开库、我的材质和衣柜复用 `ResourceFilters`，使用相同的搜索栏和类型筛选。

管理记录的动作、对象和设置名称共用 `internal/httpapi/admin_event_labels.json`。搜索支持中英文名称及带 `#` 的编号，列表和 CSV 导出使用相同筛选。普通用户按材质名称、描述或收藏名搜索；哈希搜索保留在管理页面。

## 提交范围

提交源码、必要测试资源、用户手册、架构与接口说明。构建产物、浏览器记录、真实数据、凭据、个人工具配置、实施计划和详细验收流水留在本地。新增依赖时同步核对锁文件和[第三方声明](../THIRD_PARTY_NOTICES.md)。
