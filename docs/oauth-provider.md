# 本站 OAuth2 / OIDC 授权服务

在“插件管理”启用本站 OAuth2 服务，并登记客户端名称和精确回调地址。生产回调使用 HTTPS，本地 loopback 可使用 HTTP。公开客户端不使用 Secret；机密客户端通过 `client_secret_basic` 认证。Secret 只显示一次，更新后需同步到客户端。

## 端点

以下路径相对于 `WS_PUBLIC_URL`。Issuer 必须与该公开地址一致。

| 用途 | 方法与路径 |
| --- | --- |
| OAuth 发现 | `GET /.well-known/oauth-authorization-server` |
| OIDC 发现 | `GET /.well-known/openid-configuration` |
| 签名公钥 | `GET /oauth/jwks` |
| 授权 | `GET /oauth/authorize` |
| 令牌交换与续期 | `POST /oauth/token` |
| 用户资料 | `GET /oauth/userinfo` |
| 撤销令牌 | `POST /oauth/revoke` |
| 令牌内省 | `POST /oauth/introspect` |

内省仅供机密客户端查询自身 Access Token。所有令牌端点使用 `application/x-www-form-urlencoded`。UserInfo 使用 `Authorization: Bearer <access_token>`。

## 授权码

两类客户端都必须使用 PKCE S256。授权请求包含 `response_type=code`、`client_id`、`redirect_uri`、`scope`、`state`、`code_challenge` 和 `code_challenge_method=S256`。客户端生成高熵 verifier，保存 state，并在回调中校验 state。

| Scope | 内容 |
| --- | --- |
| `openid` | 返回 ID Token，并允许通过 UserInfo 读取 `sub` |
| `profile` | 用户 ID 与昵称 |
| `email` | 邮箱及验证状态 |
| `offline_access` | 同意后签发 Refresh Token |

OIDC 登录建议申请 `openid profile`，按需增加 `email`。发送随机 `nonce`，并在验证 ID Token 后核对 nonce。用户须在本站同意页确认授权；续期授权也会在该页展示。

回调返回 code 和 state。code 有效期 1 分钟，只能交换一次。交换参数为 `grant_type=authorization_code`、`code`、登记的 `redirect_uri` 和原 `code_verifier`。公开客户端还需发送 `client_id`。Access Token 有效期为 1 小时。

申请 `openid` 时返回 RS256 ID Token。客户端需通过 JWKS 验证签名，并检查 issuer、audience、exp、nonce 和 `at_hash`。`auth_time` 仅在本站有实际登录时间记录时返回；升级前的旧网页会话可能没有该值。昵称和邮箱通过 UserInfo 获取。

## 刷新与撤销

申请 `offline_access` 后，Refresh Token 族的总有效期为首次签发起 30 天。客户端向令牌端点发送 `grant_type=refresh_token` 和 `refresh_token`。每次成功续期都会返回新令牌，旧 Refresh Token 不可再次使用。请串行刷新并原子替换保存值；重用旧令牌会撤销整个令牌族及关联 Access Token。

刷新时可省略 scope，或传入原授权范围的子集。去掉 `offline_access` 后不再返回新的 Refresh Token。客户端不可通过刷新增加权限。

撤销端点接收 `token`，客户端认证方式与令牌端点一致。用户也可在账号设置撤销应用授权。改密、封禁、停用客户端、修改客户端配置或停用授权插件会使相关令牌失效。完整备份不保存这些令牌；恢复后用户重新授权。

## 接入范围

当前支持授权码、PKCE、Refresh Token、RS256 ID Token、发现、UserInfo、撤销和 Access Token 内省。`prompt=consent` 显示同意页；`prompt=none` 返回 `login_required` 或 `consent_required`，不提供静默授权。其他 prompt 值及 `max_age` 会被拒绝。

不提供 Password、Implicit、Client Credentials、动态客户端注册或单点登出协议。ID Token 签名密钥保存在数据库并用站点设置密钥加密；需保留原设置密钥以支持重启和恢复。此密钥与 Yggdrasil 签名密钥独立。
