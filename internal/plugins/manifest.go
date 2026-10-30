package plugins

import sdk "wishingskin/pkg/plugin"

type Manifest = sdk.Manifest
type PageContribution = sdk.PageContribution
type TaskDeclaration = sdk.TaskDeclaration

func Builtins() []Manifest {
	return []Manifest{
		{ID: "wishingskin.oauth2", Name: "OAuth2 授权服务", Description: "让其他应用通过用户同意读取本站基本资料。", Version: "1.0.0", Dependencies: []string{}, Routes: []string{"/user/oauth/authorize", "/oauth", "/api/admin/oauth"}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.oauth2", MigrationVersion: 17, Capabilities: []string{"oauth2-authorization-server"}, Events: []string{}, Pages: []PageContribution{{ID: "oauth2", Area: "admin.plugins", Label: "OAuth2 授权服务"}}, Permissions: []string{"admin", "user"}},
		{ID: "wishingskin.connectors", Name: "外部账号连接器", Description: "验证并绑定 AuthMe 或 HTTP 服务中的外部账号。", Version: "1.0.0", Dependencies: []string{}, Routes: []string{"/api/account/connectors", "/api/admin/connectors"}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.connectors", MigrationVersion: 17, Capabilities: []string{"external-account-linking"}, Events: []string{}, Pages: []PageContribution{{ID: "connectors", Area: "admin.plugins", Label: "外部账号连接器"}}, Permissions: []string{"admin", "user"}},
		{ID: "wishingskin.oidc", Version: "1.0.0", Dependencies: []string{}, Routes: []string{"/api/auth/oidc", "/api/account/oidc", "/api/admin/oidc"}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.oidc", MigrationVersion: 15, Capabilities: []string{"oidc-sign-in", "account-linking"}, Events: []string{"oidc.link", "oidc.unlink"}, Pages: []PageContribution{{ID: "oidc", Area: "admin.plugins", Label: "OIDC 登录"}}, Permissions: []string{"user", "admin"}},
		{ID: "wishingskin.object-storage", Version: "1.0.0", Dependencies: []string{}, Routes: []string{}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.object-storage", MigrationVersion: 16, Capabilities: []string{"blob-storage", "texture-files"}, Events: []string{"texture.upload", "texture.delete"}, Pages: []PageContribution{{ID: "object-storage", Area: "admin.plugins", Label: "对象存储"}}, Permissions: []string{"admin"}},
		{ID: "wishingskin.points", Version: "1.0.0", Dependencies: []string{}, Routes: []string{}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.points", MigrationVersion: 12, Capabilities: []string{"points", "checkin"}, Events: []string{"points.checkin", "points.change", "texture.upload", "character.create"}, Pages: []PageContribution{{ID: "points", Area: "admin.plugins", Label: "积分与签到"}}, Permissions: []string{"user"}},
		{ID: "wishingskin.invitations", Version: "1.1.0", Dependencies: []string{}, Routes: []string{}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.invitations", MigrationVersion: 13, Capabilities: []string{"registration-invitation", "repeatable-export"}, Events: []string{"registration"}, Pages: []PageContribution{{ID: "invitations", Area: "admin.plugins", Label: "邀请码"}}, Permissions: []string{"admin"}},
	}
}
