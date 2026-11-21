# 游戏、账号与存储接入

所有第三方集成都在管理员后台配置，敏感凭据由 32 字节站点设置密钥加密保存（请务必单独备份该密钥）。建议只启用实际需要的插件；更换部署域名或环境时，及时核对回调地址与访问权限。

## Minecraft 客户端与服务端接入

### Yggdrasil 外置登录（authlib-injector）

1. 先在站点内创建角色并绑定皮肤或披风。
2. 在支持 `authlib-injector` 的启动器（如 PCL2、HMCL、Prism Launcher）中添加外置登录认证服务器：
   `https://skin.example.com/api/yggdrasil/`（将域名替换为实际 `WS_PUBLIC_URL`）。
3. 使用本站邮箱和密码登录，再选择对应角色。
4. 联机服务端也需挂载 `authlib-injector` 并指向同一认证地址，在启动参数的 `-jar` 之前加入：

```text
-javaagent:/path/to/authlib-injector.jar=https://skin.example.com/api/yggdrasil/
```

注入器下载与适配说明参考 [authlib-injector](https://github.com/yushijinhun/authlib-injector)。

### CustomSkinLoader 与传统材质接口

`GET /api/integration` 会返回当前站点的全套接入地址以及可直接下载的 `CustomSkinLoader.json`。

- CustomSkinAPI 根地址：`/csl/`
- 传统材质模板：`/skin/{USERNAME}.png`、`/cape/{USERNAME}.png`
- 迁入的旧 Blessing Skin 材质编号兼容路径：`/raw/{tid}`

注意：此类材质加载接口仅负责提供外观文件，不替代游戏身份认证。

## OIDC 第三方登录

在“插件管理 → OIDC 登录”中配置身份提供商（最多 12 个，Issuer 不可重复）：

- 填写 Issuer、Client ID 与 Client Secret，并将页面显示的回调地址登记到上游提供商；只申请 `openid`、`profile`、`email` 权限。
- 生产环境必须使用 HTTPS，本地联调允许 loopback HTTP。编辑时留空 Secret 会保留原值；修改配置会使进行中的短期流程失效。
- **账号绑定规则**：
  - 已有用户须先登录本站，在“个人资料与安全”中验证本站密码后主动绑定（OIDC 邮箱可与本站邮箱不同）。
  - 绑定及后续登录都会弹出账号确认框，明确核对目标账号信息。
  - 未绑定的外部身份首次直接登录时，会引导进入本站注册流程，仍遵守本站注册开关、域名限制、邀请码、Turnstile 与邮箱验证规则，不按同邮箱自动合并。
  - OIDC 仅用于网页登录，不改变 Minecraft 游戏登录密码。

## 本站 OAuth2 / OIDC 授权服务

`wishingskin.oauth2` 插件默认关闭，启用后可将 WishingSkin 作为其他应用的登录源：

- 登记精确回调地址（生产要求 HTTPS，本地 loopback 可用 HTTP）；公开客户端无需 Secret，机密客户端使用 `client_secret_basic`（Secret 仅创建或重置时展示一次）。
- 所有客户端均强制使用授权码模式 + PKCE S256，并由客户端自行校验 `state`。

| 接口用途 | 路径 |
| --- | --- |
| OAuth / OIDC 发现 | `/.well-known/oauth-authorization-server`、`/.well-known/openid-configuration` |
| 公钥（JWKS） | `/oauth/jwks` |
| 授权 | `/oauth/authorize` |
| 令牌交换与刷新 | `/oauth/token` |
| 用户资料 | `/oauth/userinfo` |
| 令牌撤销与内省 | `/oauth/revoke`、`/oauth/introspect` |

授权码有效期 1 分钟且仅限使用一次；Access Token 有效期 1 小时。支持 `openid`（返回 RS256 ID Token）、`profile`、`email` 与 `offline_access`（签发最长 30 天、单次轮换的 Refresh Token）。不支持 Password、Implicit 或 Client Credentials 模式。完整接入参数见[本站 OAuth2 / OIDC 授权服务](oauth-provider.md)。

## 外部账号连接器（AuthMe / 通用 HTTP）

`wishingskin.connectors` 默认关闭，适用于将本站账号与既有游戏服外部账号体系建立稳定绑定。用户须先登录本站并验证本站密码，再验证外部密码完成绑定；外部密码既不落库，也不用于直接登录本站。

### AuthMe（只读 MySQL）

- 使用只读 MySQL 账号连接外部数据库，默认读取 `authme` 表的 `id`、`realname`、`password` 字段（按用户名忽略大小写查询，不写入源表）。
- 支持 AuthMe SHA256 与 bcrypt（cost 上限 14）。

### 通用 HTTP 验证

向配置的 HTTPS 端点（本地 loopback 可用 HTTP，超时 8 秒，响应上限 64 KiB，禁止重定向）发起 `POST JSON` 请求，可选配置 `Authorization: Bearer` 令牌：

```json
{"username":"example","password":"example-input"}
```

仅当返回 HTTP 200 且满足以下 JSON 结构时判定验证通过：

```json
{"authenticated":true,"subject":"stable-id","username":"display-name"}
```

`subject` 必须是外部系统的长期稳定标识，不要使用可修改的昵称。

## S3 对象存储、维护与 CDN

### S3 兼容对象存储

在“插件管理 → 对象存储”填入预先创建的私有存储桶信息（服务地址、存储桶、区域默认 `us-east-1`、独立对象前缀默认 `textures` 以及 Access Key / Secret Key），保存后启用，兼容自部署 MinIO。

- **最小权限**：需授予目标前缀的 `GetObject` 与 `PutObject`；开启远端孤立文件回收时还需 `DeleteObject`，使用远端分页列举时需授予带前缀条件的 `ListBucket`。请使用本站独占前缀，避免与其他应用冲突。
- **读写策略**：新上传先落盘本地再同步远端，远端失败则不提交材质记录；读取优先走远端并校验 hash，远端异常时自动回退本地副本。完整业务备份依然读取本地材质目录。

### 旧材质同步与孤立文件回收

- **旧材质同步**：按每批 100 条幂等同步历史材质，并逐一校验内容一致性，可重复触发。
- **本地与远端回收**：仅扫描超过 1 小时、按 hash 命名且在 `textures` 表中完全无记录的孤立对象（远端每页最多扫描 1000 条），须先预览清单并显式确认后才执行删除，期间会二次复核数据库引用，不影响逻辑删除记录或本地回退副本。

### 公开材质签名 CDN

在“站点设置 → 存储与备份”填写与本站不同域名的 CDN 地址和缓存上限（1–60 秒）即可启用。CDN 需将 `/cdn/textures/` 回源至本站，并原样转发 `expires` 与 `sig` 查询参数（可使用自建 Nginx 缓存代理）。

配置云 CDN 或缓存代理时，请核对以下规则：

- 缓存键必须包含完整查询参数，只缓存 HTTP 200 成功图片并严格遵守源站 `Cache-Control`。
- 禁止缓存 4xx/5xx 错误响应，也不要在缓存过期后继续返回旧文件。
- CDN 仅分发通过审核的公开材质；下架后新的回源会被立即拒绝，边缘已有缓存最多留存至设定的缓存上限。

上传、同步、回收与备份共享数据库命名空间级存储锁，维护期间上传最多等待 5 秒；大批量同步建议避开访问高峰。
