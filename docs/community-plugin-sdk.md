# 社区插件 SDK

社区插件采用 Go 源码编译方式。公开接口位于 `pkg/plugin`。最小示例位于 `examples/plugins/greeting`。插件和服务在同一个进程运行，拥有进程本身的能力。SDK 不是安全沙箱。站点维护者必须审查并信任插件源码。

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

先运行 `pnpm --dir web build`，再运行 `go build -trimpath -o bin/wishingskin.exe ./cmd/wishingskin`。两个构建必须串行。重新启动新程序后，后台插件页显示社区插件。社区插件默认停用。管理员可以保存配置并独立启停。当前不支持后台上传、运行时下载或热加载。

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

Pages 和 Tasks 需要实际调用 host.Page / host.Task 注册处理器；仅声明不会执行。Capabilities、Permissions 和 MigrationVersion 仍是元数据，不执行数据库迁移或额外授权。受控路由以实际 Route.Access 为准。Host 不提供数据库、密钥或直接修改核心表的接口。

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

远程读超时上限 10 秒，上传上限 20 秒，SDK 最多两次尝试。远程读取会在 8 MiB 上限内缓冲并校验；默认本地读取保持流式。远程故障可能使回退多等待一次远程超时。后台现已提供旧材质同步、孤立文件回收和签名 CDN；不枚举全量远端孤立对象。
