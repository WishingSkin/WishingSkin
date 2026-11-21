# 开发指南

## 环境与构建

本地开发使用与 `Dockerfile` 一致的工具版本：Go 1.26.10、Node.js 24.19.0、pnpm 11.24.0，以及 MySQL 8 或 PostgreSQL。

在仓库根目录执行构建：

```powershell
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

> [!IMPORTANT]
> 前端构建与 Go 编译、测试、`go vet` 必须**严格串行**。Vite 构建时会清空并重写 `web/dist`，若与 Go `embed` 并发运行会导致构建报错。

程序只从进程环境变量读取配置，不会自动加载 `.env`。首次初始化数据库后，在终端运行 `./bin/wishingskin.exe create-admin` 创建管理员，再执行 `./bin/wishingskin.exe serve`。完整配置见[配置与 CLI 参考](configuration.md)。

## 测试与质量检查

```powershell
pnpm --dir web test
pnpm --dir web build
go test -timeout 20m ./... -count=1
go vet ./...
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

### 双数据库集成测试

数据库测试依赖环境变量 `WS_TEST_MYSQL_DSN` 与 `WS_TEST_POSTGRES_DSN`；未配置时相关用例会自动跳过。可使用仓库自带的本地测试容器：

```powershell
docker compose -f compose.test.yaml up -d --wait
$env:WS_TEST_MYSQL_DSN='root:wishingskin-local-test-only@tcp(127.0.0.1:33316)/wishingskin_test?parseTime=true'
$env:WS_TEST_POSTGRES_DSN='postgres://wishingskin_test:wishingskin-local-test-only@127.0.0.1:35416/wishingskin_test?sslmode=disable'
```

测试会自动创建并清理随机命名空间或 schema，但切勿指向运营数据库。测试完毕后运行 `docker compose -f compose.test.yaml stop` 即可停止容器并保留数据卷。

- 改动数据库逻辑时，需同时验证 MySQL 与 PostgreSQL 两种方言。
- 改动界面代码时，需通过 `vue-tsc` 类型检查、前端单测、相关交互流程以及桌面（1440px）/ 窄屏（390px）布局检查；纯文案或文档改动无需重复整仓集成测试。

## 数据模型与测试夹具

- 系统模块与领域概念见[架构与数据模型](architecture.md)。
- Schema 定义与升级逻辑位于 `internal/store/migrate.go`。调整 schema 时，须同步更新备份表契约、升级手册与恢复校验，避免新程序直接打开或恢复不匹配的旧备份。
- `testdata/bs` 提供合成 BS 快照与 PNG 样本（不含私钥）。需要手动测试预检查时，可运行以下生成器创建本地测试夹具（目标目录须尚不存在）：

```powershell
go run ./testdata/bs/generate.go artifacts/generated-bs-fixture
```

`testdata/appearance` 用于生成外观测试图片，`testdata/resourcecheck` 提供资源体积检测工具。

## 前端规范与核心约定

- **视觉 Token（`web/src/tokens.css`）**：字号收敛为 12 / 14 / 20 px，首页主标题为 40 px；圆角收敛为控件 6 px、卡片 12 px、弹窗 16 px；阴影分为卡片与浮层两档；空间动效使用 `--ease-spring`，并在 `prefers-reduced-motion: reduce` 下退化。所有组件必须引用语义变量（二维码白底使用独立 `--qr-paper`）。
- **主题、壁纸与像素粒子**：
  - `themes.css`、`themePacks.ts`、`themeTextures.ts`、`themeFonts.ts` 与 `BiomeParticles.vue` 共同实现默认 + 13 套主题（含 8 套 MC 主题的 15 张 16:9 体素实景 WebP 壁纸、首页 `sticky` `100dvh` 双幕切换与全群系低密度原版像素粒子）及按需加载的 OFL 1.1 开源像素字体。
  - 面板半透明/毛玻璃效果由 `--surface-opacity` 与 `--surface-blur` 控制，并在 `prefers-reduced-transparency: reduce` 下回退为实色；CSS 中 `-webkit-backdrop-filter` 须写在标准 `backdrop-filter` 之前，避免压缩后丢失标准属性。
- **品牌标与图标**：默认品牌标识为自适应主题色的 `BrandMark.vue`（与 `web/public/favicon.svg`、`docs/assets/logo.svg` 保持统一）；界面线条图标由 `AppIcon` 内嵌 Heroicons outline SVG（许可见[第三方声明](../THIRD_PARTY_NOTICES.md)），`InterfaceIcon` 保留管理员短文本覆盖。加载状态复用 `LoadingSkeleton`。
- **多语言与文案**：界面同时维护简体中文与 English（`web/src/i18n.en.ts`），新增或调整中文 `t('...')` 文案时须同步补齐英文映射。
- **路由与 404 契约**：前后端共享 `internal/httpapi/page_routes.json`（含旧路径兼容映射，如 `/user/materials` → `/user/resources`），新增页面须同步更新该清单。拼写错误或不存在的子路径统一呈现 404（直接 GET 返回 HTTP 404），不静默回退到父页面。同工作区切换子页仅对 `data-panel-region` 区域播放局部过渡动画。
- **筛选与管理记录**：公开库、我的材质和衣柜共用 `ResourceFilters`。管理记录的中英文标签共用 `internal/httpapi/admin_event_labels.json`，支持关键词与 `#编号` 搜索，列表和 CSV 导出复用同一筛选逻辑。
- **社区插件**：接口与运行契约见[社区插件 SDK](community-plugin-sdk.md)，插件应只通过宿主公开接口交互，不直接读写核心业务表。

## 提交范围

版本库只提交源代码、必要合成测试夹具、用户手册、架构与接口文档。构建产物、浏览器截图、真实运营数据、凭据密钥、个人工具配置与临时验收日志留在本地并保持 Git 忽略。引入新第三方依赖或字体时，须同步核对锁文件与[第三方声明](../THIRD_PARTY_NOTICES.md)。
