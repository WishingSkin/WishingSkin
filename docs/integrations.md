# 游戏、账号与存储接入

配置在管理员后台填写。凭据以站点设置密钥加密保存，密钥需单独备份。只启用需要的插件；更换部署环境时核对服务地址、回调和权限。

## Minecraft 接入

先在本站创建角色并绑定材质。在支持 authlib-injector 的启动器中添加认证服务器 `https://skin.example.com/api/yggdrasil/`，使用本站邮箱和密码登录，再选择角色。PCL2 可在添加外置登录服务器时填写该地址。将示例域名替换为 `WS_PUBLIC_URL` 对应的站点。

服务端也需加载 authlib-injector，并使用同一个认证地址。把 `-javaagent:/path/to/authlib-injector.jar=https://skin.example.com/api/yggdrasil/` 放在 JVM 参数中、服务端 `-jar` 参数之前。注入器安装与版本说明见 [authlib-injector](https://github.com/yushijinhun/authlib-injector)。

`GET /api/integration` 返回当前站点的接入地址和 CustomSkinLoader 配置。传统材质模板为 `/skin/{USERNAME}.png`、`/cape/{USERNAME}.png`，CustomSkinAPI 根地址为 `/csl/`。`/raw/{tid}` 兼容迁入的 BS 材质编号。材质加载接口不替代游戏身份认证。

## OIDC 登录

在插件管理中配置 OIDC Web 应用，填写 Issuer、Client ID、Client Secret，并把界面显示的回调地址登记到提供商。只申请 openid、profile、email。生产使用 HTTPS，本地测试允许 loopback HTTP。Secret 留空保留原值。

支持最多 12 个提供商。默认提供商使用空 ID，新增提供商使用稳定 ID；Issuer 不得重复。修改配置会使未完成流程失效。

已有本站账号在个人资料与安全中验证本站密码后主动绑定，OIDC 邮箱可与本站邮箱不同。绑定与已绑定身份登录先通过弹窗确认账号。未绑定身份直接登录进入本站注册，并遵守本站注册、域名、邀请码、Turnstile 和邮件验证规则。OIDC 不改变 Minecraft 登录密码。

## 本站 OAuth2 服务

wishingskin.oauth2 默认关闭。登记精确 HTTPS 回调，loopback 可用 HTTP。公开客户端无需 secret；机密客户端使用 client_secret_basic，secret 仅显示一次。两类均强制授权码与 PKCE S256。客户端须校验自己的 state。

| 接口 | 路径 |
| --- | --- |
| 发现 | /.well-known/oauth-authorization-server |
| 授权 | /oauth/authorize |
| 令牌 | /oauth/token |
| 撤销 | /oauth/revoke |
| 用户资料 | /oauth/userinfo |

code 有效期 1 分钟，只能使用一次；access token 为 1 小时。仅提供 profile/email 只读 scope，不提供 refresh token、ID token、password 或 implicit grant。

## 外部账号连接器

wishingskin.connectors 默认关闭。用户先登录本站并验证本站密码，再验证外部密码，主动绑定稳定身份。外部密码不保存，不用于直接登录本站。

AuthMe 使用只读 MySQL 账号。默认表为 authme，ID、用户名和密码字段为 id、realname、password。支持 AuthMe SHA256 和 bcrypt，bcrypt cost 不超过 14。查询按用户名忽略大小写，不修改源表。

通用 HTTP 向 HTTPS endpoint POST JSON：

```json
{"username":"example","password":"example-input"}
```

成功响应必须为 HTTP 200：

```json
{"authenticated":true,"subject":"stable-id","username":"display-name"}
```

subject 必须稳定，不应使用可更名的昵称。可选 API key 通过 Authorization: Bearer 发送。loopback 可用 HTTP，超时 8 秒，响应最多 64 KiB，不跟随重定向。非 200 或 authenticated=false 表示验证失败。

## 对象存储与维护

### S3 兼容存储

预先创建私有存储桶，在“插件管理 → 对象存储”填写服务地址、存储桶、区域、对象前缀及 Access Key / Secret Key，保存后启用。默认区域为 `us-east-1`，前缀为 `textures`；以实际服务配置为准。可使用自部署的 MinIO，无需购买云存储。插件保留本地副本；实现与扩展契约见[插件 SDK](community-plugin-sdk.md)。

凭据需要指定前缀的 `GetObject`、`PutObject` 权限；同时回收远端孤立副本时还需 `DeleteObject`。使用独立前缀，避免与其他应用共用对象名。更换凭据或地址后上传一份材质并核对远端对象即可检查接入。远端不可读时回退本地，上传失败则不提交材质记录。

### 同步与回收

旧材质每批同步 100 条，校验本地与远端内容，可重复执行。回收只处理超过一小时、hash 命名且 textures 表完全无记录的本地普通文件，先预览，再显式确认清单。不回收逻辑删除记录或本地回退副本，不枚举全量远端孤立对象。

### CDN

在“站点设置 → 存储与备份”填写 CDN 地址和 1–60 秒缓存上限，再启用公开材质 CDN。CDN 使用与本站不同的域名，将 `/cdn/textures/` 回源本站，并转发 `expires`、`sig` 查询参数。可以使用自部署的 nginx 缓存代理。

缓存键必须包含完整查询参数，只缓存成功图片，遵守源站 `Cache-Control`，禁止缓存错误或在过期后继续返回旧内容。CDN 只处理已审核公开文件。下架后新回源拒绝访问，已有缓存最多保留到配置上限。使用云 CDN 时需确认边缘节点实际遵守这些规则。

上传、同步、回收和备份使用数据库命名空间级存储锁。维护期间上传等待最多 5 秒；大批同步应避开高峰。自动备份与独立恢复见[备份手册](backup-restore.md)。
