# 本站 OAuth2 / OIDC 授权服务

在“管理后台 → 插件管理”启用 `wishingskin.oauth2` 后，即可将 WishingSkin 作为其他网站或工具的身份提供商。登记客户端时需填写名称和精确回调地址（生产环境要求 HTTPS，本地联调 loopback 允许 HTTP）：

- **公开客户端**：无需 Client Secret。
- **机密客户端**：通过 `client_secret_basic` 认证；Secret 仅在新建或重置时显示一次，重置后须同步更新调用方配置。

## 接口端点

以下路径均相对于站点 `WS_PUBLIC_URL`（同时作为 OIDC Issuer）：

| 用途 | 方法与路径 |
| --- | --- |
| OAuth 元数据发现 | `GET /.well-known/oauth-authorization-server` |
| OIDC 元数据发现 | `GET /.well-known/openid-configuration` |
| 签名公钥（JWKS） | `GET /oauth/jwks` |
| 授权入口 | `GET /oauth/authorize` |
| 令牌交换与续期 | `POST /oauth/token` |
| 用户资料与授权资源接口 | `GET /oauth/userinfo`、`GET /oauth/api/characters`、`GET /oauth/api/textures`、`GET /oauth/api/points`、`GET /oauth/api/minecraft` |
| 撤销令牌 | `POST /oauth/revoke` |
| 令牌内省 | `POST /oauth/introspect` |

- 所有令牌端点均使用 `application/x-www-form-urlencoded`。
- `/oauth/userinfo` 与 `/oauth/api/*` 均通过 `Authorization: Bearer <access_token>` 认证；缺少所需 Scope 时返回 HTTP `403`（`error: "insufficient_scope"`）。
- `/oauth/introspect` 仅供机密客户端查询自身签发的 Access Token。

## 授权码流程（PKCE S256）

公开与机密客户端均强制使用 PKCE S256。发起授权请求需携带 `response_type=code`、`client_id`、`redirect_uri`、`scope`、`state`、`code_challenge` 与 `code_challenge_method=S256`（客户端负责生成高熵 `code_verifier` 并在回调校验 `state`）。管理员可在客户端配置中限制每个应用允许申请的 Scope 白名单（未单独配置时默认仅允许身份类四项）。

| Scope | 返回内容与适用端点 |
| --- | --- |
| `openid` | 返回 RS256 ID Token，并允许通过 `/oauth/userinfo` 读取 `sub` |
| `profile` | 用户 ID 与昵称（`/oauth/userinfo`） |
| `email` | 邮箱地址及验证状态（`/oauth/userinfo`） |
| `offline_access` | 经用户同意后签发 Refresh Token |
| `characters:read` | 只读读取已授权用户的角色列表、UUID 与材质地址（`GET /oauth/api/characters`） |
| `textures:read` | 只读分页列出已授权用户的材质元数据（`GET /oauth/api/textures`） |
| `points:read` | 只读查询已授权用户的积分余额与流水（`GET /oauth/api/points`） |
| `minecraft:read` | 只读查询已授权用户的正版 Minecraft Java 绑定状态与角色名，不含敏感主体标识（`GET /oauth/api/minecraft`） |

- OIDC 登录推荐申请 `openid profile`（按需追加 `email`），同时发送随机 `nonce` 并在验签后核对。用户须在本站授权页手动同意，续期权限也会明确展示。
- 回调返回的授权码 `code` 有效期 **1 分钟**且只能使用一次。换取令牌时提交 `grant_type=authorization_code`、`code`、登记的 `redirect_uri` 和原 `code_verifier`（公开客户端附带 `client_id`）。
- Access Token 有效期 **1 小时**。申请 `openid` 时返回 RS256 ID Token，客户端须拉取 `/oauth/jwks` 验签并核对 `iss`、`aud`、`exp`、`nonce` 及 `at_hash`（`auth_time` 仅在存在实际登录时间记录时返回）。

## 刷新与撤销

- 申请 `offline_access` 后，同一 Refresh Token 族的总有效期为首次签发起 **30 天**。
- 使用 `grant_type=refresh_token` 和 `refresh_token` 换取新令牌时，服务端会轮换返回新的 Refresh Token，旧令牌立即作废。客户端须串行刷新并原子替换本地存储；重复使用旧令牌会直接撤销整个令牌族及关联 Access Token。
- 刷新时可省略 `scope` 或传入原范围的子集（移除 `offline_access` 后不再签发新 Refresh Token），不可借刷新扩大权限。
- 调用 `/oauth/revoke`（传入 `token`）可主动撤销令牌；用户也可在“账号”页手动取消已授权应用。修改密码、封禁账号、停用或改动客户端配置、停用授权插件均会使相关令牌立即失效。完整备份不包含此类令牌，恢复后用户重新授权即可。

## 支持范围与限制

- 支持：`prompt=consent`（展示同意页）、`prompt=none`（未登录或未授权时返回 `login_required` / `consent_required`，不静默授权）；其他 `prompt` 值和 `max_age` 会被拒绝。
- 不支持：Password、Implicit、Client Credentials 模式、动态客户端注册及单点登出（SLO）。
- ID Token 签名密钥独立于 Yggdrasil 私钥，经 32 字节站点设置密钥加密存于数据库，升级与恢复时须妥善保留原设置密钥。
