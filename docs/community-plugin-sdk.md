# 社区插件 SDK

WishingSkin 支持两种社区插件形态：随主程序编译的 **Go 源码插件**，以及通过 ZIP 或静态市场源安装的 **动态可执行插件包**。公开 SDK 位于 `pkg/plugin`，最小参考示例见 `examples/plugins/greeting`。

> [!WARNING]
> 源码插件与宿主同进程运行，动态插件在独立子进程运行，但两者都具备宿主用户的本机权限，不提供操作系统级沙箱。站点维护者须自行审查并信任插件来源。

## Go 源码插件接入

实现 `Manifest() plugin.Manifest` 与 `Register(plugin.Host) error` 接口，然后在 `cmd/wishingskin/community.go` 中导入并注册：

```go
import (
    "wishingskin/examples/plugins/greeting"
    sdk "wishingskin/pkg/plugin"
)

func communityPlugins() []sdk.Plugin {
    return []sdk.Plugin{greeting.Plugin{}}
}
```

按顺序串行执行前端构建与 Go 编译：

```powershell
pnpm --dir web build
go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin
```

重启后，后台“插件管理”中会出现新插件（默认停用），管理员可单独保存配置并启停。源码插件更新需重新编译宿主；动态插件包支持后台直接安装与热重载。

## 清单、路由与权限

- **清单规范**：须具备唯一 `ID`、版本号与设置命名空间。`ID` 仅允许小写字母、数字及单个连接符 `. _ -`，最长 64 字符（`wishingskin.` 为官方保留前缀）。声明的依赖须先注册并处于启用状态，否则插件不可用。
- **路由注册**：通过 `host.Handle(plugin.Route{...})` 注册固定相对路径（以 `/` 开头，宿主自动挂载到 `/api/plugins/{id}` 下），支持 `GET`、`POST`、`PATCH`、`PUT`、`DELETE`，暂不支持通配符或路径参数。清单中的 `Routes` 仅用于元数据展示，不会自动绑定处理器。
- **访问级别**：`Access` 可选 `public`、`user`、`admin`。受保护路由由宿主校验登录会话、管理员权限并注入 `Principal`；非 GET 请求校验同源，用户与管理员路由同时校验 `X-CSRF-Token`。插件停用后路由统一返回 404；输入校验、业务权限与外部请求超时由插件自行负责。

## 插件配置存储

- `Manifest.Settings` 最多声明 32 个字段（包含 `Key`、`Label`、`Type`），类型支持 `text`、`secret`、`boolean`、`number`（文本上限 4096 字节）。后台自动按声明生成配置表单，拒绝未声明字段。
- 插件调用 `host.Settings(ctx, &target)` 读取配置；配置经站点 32 字节设置密钥加密后存入 `plugins.settings_json`。管理员读取接口不回显 `secret` 明文（仅返回是否已配置），更新时留空即保留原值。
- 管理员读写接口为 `GET/PATCH /api/admin/plugin-settings/{id}`（PATCH 直接传字段键值对象，省略字段保留原值）。保存配置与写入审计在同一事务提交，不改变启停状态。请勿在日志中打印配置。

## 同步事件、管理页面与定时任务（源码插件）

- **同步事件**：在清单 `Events` 声明并通过 `host.Subscribe(name, handler)` 订阅（当前发布 `character.create` 与 `texture.upload`，携带事件名、用户 ID 与资源 ID）。事件在业务提交后同步通知，无持久队列或投递保证，不适合作为扣费等强一致依据。
- **管理员内容页面**：`Manifest.Pages` 最多 16 项（`Area` 仅限 `admin.plugins`），需通过 `host.Page` 注册实际处理器（带 5 秒超时 context）。返回的 `PageDocument`（最多 32 个 section，每段 64 个 item，总 JSON 上限 64 KiB，标题 ≤ 256 B，摘要/键值 ≤ 4096 B，正文 ≤ 8192 B）全部按纯文本渲染，不执行 HTML 或脚本。停用后返回 404。
- **手动与定时任务**：`Manifest.Tasks` 最多 16 项，通过 `host.Task` 注册处理器。`Schedule` 留空为仅手动触发，或填写 `@every 1m` 至 `@every 168h`；`Timeout` 默认 1 分钟（可选 1 秒至 15 分钟），处理器须遵守 `context` 取消。
  - 状态分为 `idle` / `running` / `succeeded` / `failed`，错误与 panic 不直接回显给前端；每个任务按独立跨进程数据库锁互斥，每进程最多并发运行 2 个社区任务。
  - 定时任务每分钟巡检，不在重启时补跑全部历史错失次数；协作式任务不提供恰好一次语义，处理器应保持幂等。
  - 管理员接口：`GET /api/admin/plugin-runtime/{id}`、`GET /api/admin/plugin-runtime/{id}/pages/{page}`、`POST /api/admin/plugin-runtime/{id}/tasks/{task}`。

## 官方对象存储扩展契约

`wishingskin.object-storage` 基于 `pkg/storage.BlobStore` 提供 S3 兼容存储（可选实现 `pkg/storage.InventoryStore` 的 `List` 与 `Stat` 以支持远端孤立对象分页清理）。启用后新上传先落本地再同步远端（远端失败不提交记录），读取优先远端并校验 hash（失败或不一致自动回退本地）；远程读取超时上限 10 秒（8 MiB 内缓冲校验），上传上限 20 秒，最多重试 2 次。站点 Logo 与背景图片不转存对象存储。

## 动态可执行插件包与静态市场

管理员可在插件页直接上传 ZIP，或配置 HTTPS 市场源（本地开发允许 loopback HTTP）。市场源是一份静态 JSON，可由任意静态托管提供：

```json
{"packages":[{"id":"example.greeting","name":"Greeting","version":"1.0.0","url":"https://plugins.example.com/greeting.zip","sha256":"<ZIP 的 SHA-256>"}]}
```

下载不跟随重定向；安装与升级前都会展示清单与权限，并核验 ZIP 的 SHA-256 摘要一致。ZIP 压缩与展开上限均为 128 MiB、最多 256 个条目，根目录包含 `plugin.json` 及目标平台可执行文件：

```json
{
  "manifest": {
    "id":"example.greeting", "name":"Greeting", "version":"1.0.0",
    "settingsNamespace":"greeting", "migrationVersion":0,
    "permissions":["routes.admin", "frontend"],
    "dependencies":[], "events":[], "pages":[], "tasks":[]
  },
  "protocol":1,
  "executables":{"linux/amd64":"bin/greeting", "windows/amd64":"bin/greeting.exe"},
  "routes":[{"method":"POST", "path":"/hello", "access":"admin"}],
  "frontend":"index.html"
}
```

- **运行时协议**：使用 `plugin.ServeRuntime` 持续接收 JSON lines 请求并输出结果（日志写 `stderr`）。`health` 由 SDK 自动处理，其余按 `RuntimeRequest.Kind` 分派并返回 `RuntimeResponse`（`status` 200–599，路由不允许 3xx；任务与事件应返回 2xx）。单次调用超时 30 秒（事件 5 秒），超时会终止子进程并在下次调用时拉起。
- **权限声明**：可选 `routes.public`、`routes.user`、`routes.admin`、`events`、`pages`、`tasks`、`frontend`、`database`、`webhooks`，实际提供的能力须与声明匹配。依赖不能成环。
- **目录与热重载**：包存放在 `WS_PLUGIN_DIR`（默认 `<WS_TEXTURE_DIR>/.plugins`）。支持后台免重启重载、升级与卸载（卸载保留数据与已应用迁移，升级保留旧包）。多实例部署应共享包目录与数据库，变更包时先保留单个实例操作，完成后再重启其余实例。

### 动态插件 KV 数据与 SQL 迁移

- **KV 存储**：声明 `database` 权限后，请求会注入 `data` 键值集合，响应通过 `writes` 提交更新（置 `null` 删除）。单值 ≤ 64 KiB、单次 ≤ 128 项、单插件总计 ≤ 1 MiB 且 ≤ 1024 项，由数据库锁串行化事务写入。
- **SQL 迁移**：包内 `migrations` 为版本连续数组，每项同时提供 `mysql` 与 `postgres` SQL 数组（长度与 `migrationVersion` 一致），表名用 `{{prefix}}` 占位由宿主替换为稳定前缀。已执行 SQL 不可修改或删除；由于 MySQL DDL 不可整体回滚，建表语句应保持可重入（如 `CREATE TABLE IF NOT EXISTS`）。

### 动态前端模块（iframe）

`frontend` 指向包内单个 HTML 文件（≤ 1 MiB），在后台插件页的独立 opaque origin `iframe` 中运行（禁止直接发起外部网络请求，脚本与样式需内联，图片可用 data URL）。页面通过 `parent.postMessage` 调用清单内固定路由（最多 4 并发，请求体 ≤ 64 KiB，宿主自动附加会话与 CSRF）：

```js
parent.postMessage({type:'wishingskin:request', id:'hello-1', method:'POST', path:'/hello', body:{name:'Alex'}}, '*')
window.addEventListener('message', event => {
  if (event.source !== parent || event.data?.type !== 'wishingskin:response') return
  // event.data 为 {type, id, status, body}，按 id 关联请求
})
```

## Webhook 与持久事件队列

在后台可创建 Webhook 订阅 `character.create`、`texture.upload`（关联插件时须具备 `webhooks` 权限）。签名密钥新建或轮换时仅展示一次，配置加密保存；后台保留最近 50 条投递并支持失败手动重试。请求头与验签规则：

```text
X-WishingSkin-Delivery: <投递 ID>
X-WishingSkin-Timestamp: <Unix 秒>
X-WishingSkin-Signature: sha256=<HMAC-SHA256 十六进制>
签名输入 = timestamp + "." + deliveryId + "." + 原始请求体
```

动态插件事件与 Webhook 均走持久队列，每分钟巡检、每批最多 4 条、失败最多重试 5 次、完成记录保留 30 天。接收端应校验时间窗口并按投递 ID 做幂等去重。

## 插件备份与恢复边界

完整备份自动包含安装索引引用的 ZIP、保留旧包、权限确认、配置、KV 数据与受管理前缀内的 SQL 表（请勿手动删旧包）。纯 KV 数据可随核心备份在 MySQL 与 PostgreSQL 间跨引擎恢复；含插件 SQL 表时须恢复到相同数据库引擎。自动表备份支持普通列、NULL、二进制列、自增主键及无环外键（表名与列名限 64 字符内字母/数字/下划线）；含生成列或循环外键的表会拒绝导出，需由插件另行提供备份方案。
