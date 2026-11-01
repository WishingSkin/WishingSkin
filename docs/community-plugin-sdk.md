# 社区插件 SDK

社区插件支持随源码编译，也支持独立可执行程序包。公开接口位于 `pkg/plugin`。最小示例位于 `examples/plugins/greeting`。插件和服务在同一个进程运行，拥有进程本身的能力。SDK 不是安全沙箱。站点维护者必须审查并信任插件源码。

## 接入

插件实现 `Manifest() plugin.Manifest` 和 `Register(plugin.Host) error`。在 `cmd/wishingskin/community.go` 导入插件，再由 `communityPlugins()` 返回实例。例如：

```go
import (
    "wishingskin/examples/plugins/greeting"
    sdk "wishingskin/pkg/plugin"
)

func communityPlugins() []sdk.Plugin {
    return []sdk.Plugin{greeting.Plugin{}}
}
```

先运行 `pnpm --dir web build`，再运行 `go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin`。两个构建必须串行。重新启动新程序后，后台插件页显示社区插件。社区插件默认停用。管理员可以保存配置并独立启停。源码插件更新需要重新编译；可执行插件包支持后台安装和热加载，见下文。

## 清单与路由

清单必须有唯一 ID、版本号和设置命名空间。ID 仅允许小写字母、数字和单个连接用的 `. _ -`，最长 64 字符。`wishingskin.` 为官方保留前缀。依赖必须先注册，启用时依赖也须启用。缺少依赖时，插件不可用。

使用 `host.Handle(plugin.Route{...})` 注册实际路由。`Path` 为以 `/` 开头的固定相对路径。宿主加上 `/api/plugins/{id}` 前缀。当前支持 GET、POST、PATCH、PUT、DELETE，不支持通配符或路径参数。清单中的 `Routes` 仅用于目录展示，不会自动注册接口。

`Access` 支持 `public`、`user`、`admin`。受保护路由由宿主检查会话与管理员权限，并传入 `Principal`。非 GET 请求检查来源；用户和管理员路由还检查 `X-CSRF-Token`。插件负责输入校验、业务权限、响应编码和自身外部请求超时。停用后路由返回 404。

## 配置

`Manifest.Settings` 最多声明 32 个字段。每项包含 `Key`、`Label`、`Type`。支持 `text`、`secret`、`boolean`、`number`，文本最长 4096 字节。后台按声明生成表单，拒绝未声明字段。

调用 `host.Settings(ctx, &target)` 读取当前配置。配置整体使用站点设置密钥加密后存入 `plugins.settings_json`。管理员读取接口会移除 secret，只返回是否已配置。secret 留空保留旧值。保存配置不改变启停状态，保存与审计在同一事务提交。请妥善备份设置密钥，禁止在日志中打印配置。

管理员接口为 `GET/PATCH /api/admin/plugin-settings/{id}`。PATCH 请求体直接使用字段键值对象。字段省略时保留原值。

## 事件与当前边界

插件在清单的 `Events` 声明事件，再用 `host.Subscribe(name, handler)` 订阅。当前宿主发布 `character.create` 和 `texture.upload`，数据只有事件名、用户 ID 和资源 ID。事件在业务提交后同步通知，停用插件不接收通知。通知没有持久队列、重试或投递保证，不能作为扣费或强一致业务的依据。处理器应及时返回，并遵守 context。

源码插件的 Pages 和 Tasks 需要实际调用 host.Page / host.Task 注册处理器；仅声明不会执行。源码插件的 Capabilities、Permissions 和 MigrationVersion 为元数据。可执行插件包会执行下文的权限和迁移检查。Host 不提供核心表或站点密钥。

## 管理员内容页面

Manifest.Pages 最多 16 项，ID 在插件内唯一，Area 当前只允许 admin.plugins。host.Page 接受相同 ID 和 Handler。处理器收到带 5 秒期限的 context 与已验证的管理员 Principal，返回 PageDocument。宿主在插件设置中生成入口并渲染标题、摘要、分段和键值列表。所有内容按纯文本显示，不执行 HTML、JavaScript 或远程前端模块。

PageDocument 最多 32 个 section，每段最多 64 个 item。标题最长 256 字节，摘要/键值最长 4096 字节，段落正文最长 8192 字节，完整 JSON 最多 64 KiB。插件负责选择允许管理员查看的内容，不要返回凭据。启用状态与管理员身份由宿主检查，停用后页面返回 404。

## 手动与固定间隔任务

Manifest.Tasks 最多 16 项。Schedule 留空表示仅手动，或使用 @every 1m 到 @every 168h 的固定间隔。host.Task 接受相同 ID、Timeout 和 Handler。Timeout 默认 1 分钟，可选 1 秒到 15 分钟。处理器必须遵守 context 取消；同进程 Go 代码无法被宿主强制终止。

管理员可以查看状态和手动运行。状态为 idle/running/succeeded/failed，包含本次开始和结束时间。处理器错误和 panic 不直接回显，避免凭据泄漏。手动触发先提交审计再执行；停用插件或依赖、停止服务会取消处理器 context。固定间隔任务每分钟检查，不在服务启动时补跑全部错过次数。

每个任务使用数据库命名空间内独立的跨进程锁，不能同时执行两次。当前每进程最多同时运行两个社区任务，以保留数据库连接；任务开始前持注册设置锁复核启用和管理员权限。状态存于 settings，随完整备份保留。进程异常后的 running 状态仅在取得对应任务锁后标记 failed，不自动重试手动任务；定时任务在后续到期间隔重试。

管理员接口：GET /api/admin/plugin-runtime/{id}，GET /api/admin/plugin-runtime/{id}/pages/{page}，POST /api/admin/plugin-runtime/{id}/tasks/{task}。POST 需要同源、会话和 CSRF。示例插件展示概览页与 check-settings 手动任务。它没有加入默认服务端插件列表，需要维护者显式编译注册。

这些任务是进程内的协作式作业，不是持久消息队列，不提供恰好一次语义。任务可能在外部副作用完成后、状态写入前中断，因此处理器须自行保证重试幂等。

## 官方对象存储插件

`wishingskin.object-storage` 提供 S3 兼容材质存储，独立配置与启停。底层契约为 `pkg/storage.BlobStore`。配置使用设置密钥加密，访问凭据不回显。存储桶必须预先创建。

启用后，新上传先完成安全的本地保存，再同步远程。远程写入失败时不提交材质记录。读取优先远程，并校验材质 hash；远程失败或内容不符时回退本地。停用后使用本地文件流。既有材质不自动搬迁，也不删除本地副本。备份仍读取本地目录。站点图片和 Logo 不转存对象存储。

远程读超时上限 10 秒，上传上限 20 秒，SDK 最多两次尝试。远程读取会在 8 MiB 上限内缓冲并校验；默认本地读取保持流式。远程故障可能使回退多等待一次远程超时。后台提供旧材质同步、孤立文件回收和签名 CDN；不枚举全量远端孤立对象。

## 动态插件包与市场

管理员可在插件页上传 ZIP，或配置 HTTPS 市场源后选择包安装。本地开发允许 loopback HTTP。市场源只是一份静态 JSON，可以由任意静态站点托管；项目不依赖集中市场服务。

```json
{"packages":[{"id":"example.greeting","name":"Greeting","version":"1.0.0","url":"https://plugins.example.com/greeting.zip","sha256":"<ZIP 的 SHA-256>"}]}
```

下载不跟随重定向。安装前展示清单和权限，确认的 ZIP 摘要必须与安装时一致。升级也重新确认权限。插件以宿主用户的权限运行，必须信任其来源。进程分离、权限声明和 iframe 只约束宿主提供的接口，不限制原生程序访问本机资源。

ZIP 根目录包含 `plugin.json` 和当前平台的程序。最大压缩与展开体积均为 128 MiB，最多 256 个条目。示例：

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

使用 `plugin.ServeRuntime` 接收 JSON lines 请求并输出结果。程序必须保持运行，日志写 stderr。`health` 请求由 SDK 处理；其他请求按 `RuntimeRequest.Kind` 分派。处理器返回 `RuntimeResponse`，其中 `status` 为 200–599，`body` 为 JSON。路由不允许 3xx，失败响应不提交数据。任务与事件应返回 2xx。单次调用最长 30 秒；事件最多 5 秒。超时会终止插件进程，下次调用重新启动。

权限列表为 `routes.public`、`routes.user`、`routes.admin`、`events`、`pages`、`tasks`、`frontend`、`database`、`webhooks`。清单中的实际贡献必须声明对应权限。依赖不接受环，运行时还会检查依赖启用状态。包存于 `WS_PLUGIN_DIR`；默认是材质目录下 `.plugins`。多实例部署必须使用相同的包目录与数据库配置。

管理界面可以重新加载、升级和卸载包，无需重启主站。卸载保留插件数据及已执行迁移。已安装 ZIP 与解压文件在打开索引、复用包及启动程序时校验。升级保留旧包文件，备份应包含当前启用版本的原包。程序不得修改自身包目录。

## 动态数据与迁移

`database` 权限会向请求提供 `data` 键值集合，响应 `writes` 提交更新，值为 `null` 时删除。每值最多 64 KiB，每次最多 128 项，插件数据合计最多 1 MiB、1024 项。宿主用数据库锁串行执行插件调用，成功后事务写入数据。

包的 `migrations` 是版本连续的数组，每项含 `version`、`mysql` 和 `postgres` SQL 数组。两种数据库都必须提供，清单 `migrationVersion` 等于数组长度。表名使用 `{{prefix}}`，宿主替换成该插件稳定的表名前缀。迁移不向可执行程序提供数据库连接；运行时数据使用上述键值接口。

安装先停止插件，再逐条执行并记录 SQL 摘要；失败时保持停用。已执行的 SQL 不允许修改或移除。MySQL DDL 不能整体回滚，SQL 应可重入，例如使用 `CREATE TABLE IF NOT EXISTS`。迁移 SQL 由维护者审查，前缀校验不构成 SQL 沙箱。

## 动态前端模块

`frontend` 指定包内单个 HTML 文件，最多 1 MiB。它在管理员插件页的 iframe 中运行，允许脚本，使用独立的 opaque origin，不允许直接网络请求。脚本和样式需内联，图片可使用 data URL。

子页面通过 `parent.postMessage` 请求宿主，只能调用清单中的固定路由。例如：

```js
parent.postMessage({type:'wishingskin:request', id:'hello-1', method:'POST', path:'/hello', body:{name:'Alex'}}, '*')
window.addEventListener('message', event => {
  if (event.source !== parent || event.data?.type !== 'wishingskin:response') return
  // event.data 为 {type, id, status, body}；按 id 关联请求。
})
```

宿主只接受当前 iframe 的消息，自动附上会话和 CSRF。最多 4 个并发请求，请求体最多 64 KiB。主站设置、DOM、凭据不直接提供给 iframe。

## Webhook 与持久事件

后台可新增 Webhook，选择 `character.create`、`texture.upload`。关联插件时，该插件必须声明 `webhooks` 权限。新建或轮换时只显示一次签名密钥，配置加密保存。停用订阅或关联插件会暂停投递。后台显示最近 50 次投递，失败后可手动重试。

请求体包含稳定的 `id`、`createdAt` 和 `event`。使用下列头验证签名：

```text
X-WishingSkin-Delivery: <投递 ID>
X-WishingSkin-Timestamp: <Unix 秒>
X-WishingSkin-Signature: sha256=<HMAC-SHA256 的十六进制结果>
签名输入 = timestamp + "." + deliveryId + "." + 原始请求体
```

接收端应校验时间窗口并按投递 ID 去重。动态插件事件及 Webhook 持久排队，每分钟检查，每批最多各 4 条，失败最多尝试 5 次。完成记录保留 30 天。投递可能重复，处理器须幂等。事件在业务提交后入队，入队失败不会回滚已提交业务；不提供事务性投递或恰好一次语义。源码插件继续使用同步 EventBus。
