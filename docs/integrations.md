# 账号与存储接入

配置在管理员后台填写。凭据以站点设置密钥加密保存；不要写入源码、文档或日志。设置密钥需单独备份。各项启用前按[手动验收清单](manual-acceptance.md)验证。

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

S3 兼容存储桶须预先创建。对象存储为独立官方插件，保留本地副本；实现与扩展契约见[插件 SDK](community-plugin-sdk.md)。既有材质通过后台手动同步，不自动搬迁。

旧材质每批同步 100 条，校验本地与远端内容，可重复执行。回收只处理超过一小时、hash 命名且 textures 表完全无记录的本地普通文件，先预览，再显式确认清单。不回收逻辑删除记录或本地回退副本，不枚举全量远端孤立对象。

CDN 默认关闭，只处理已审核公开文件。将 /cdn/textures/ 回源本站，转发 expires/sig 参数，缓存键保留查询参数，并遵守源站 1–60 秒上限。CDN 不得使用本站相同域名。下架后新回源拒绝访问，旧缓存可能保留到上限。

上传、同步、回收和备份使用数据库命名空间级存储锁。维护期间上传等待最多 5 秒；大批同步应避开高峰。自动备份与独立恢复见[备份手册](backup-restore.md)。
