package plugins

import (
	"fmt"
	"regexp"
)

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

type PageContribution struct {
	ID    string `json:"id"`
	Area  string `json:"area"`
	Label string `json:"label"`
}

type TaskDeclaration struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
}

type Manifest struct {
	ID                string             `json:"id"`
	Version           string             `json:"version"`
	Dependencies      []string           `json:"dependencies"`
	SettingsNamespace string             `json:"settingsNamespace"`
	MigrationVersion  int                `json:"migrationVersion"`
	Routes            []string           `json:"routes"`
	Capabilities      []string           `json:"capabilities"`
	Events            []string           `json:"events"`
	Pages             []PageContribution `json:"pages"`
	Tasks             []TaskDeclaration  `json:"tasks"`
	Permissions       []string           `json:"permissions"`
}

func (m Manifest) Validate() error {
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("invalid plugin id %q", m.ID)
	}
	if m.Version == "" || m.SettingsNamespace == "" {
		return fmt.Errorf("plugin %q requires version and settings namespace", m.ID)
	}
	if !idPattern.MatchString(m.SettingsNamespace) {
		return fmt.Errorf("invalid settings namespace %q", m.SettingsNamespace)
	}
	return nil
}

func Builtins() []Manifest {
	return []Manifest{
		{ID: "wishingskin.oidc", Version: "1.0.0", Dependencies: []string{}, Routes: []string{"/api/auth/oidc", "/api/account/oidc", "/api/admin/oidc"}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.oidc", MigrationVersion: 15, Capabilities: []string{"oidc-sign-in", "account-linking"}, Events: []string{"oidc.link", "oidc.unlink"}, Pages: []PageContribution{{ID: "oidc", Area: "admin.plugins", Label: "OIDC 登录"}}, Permissions: []string{"user", "admin"}},
		{ID: "wishingskin.points", Version: "1.0.0", Dependencies: []string{}, Routes: []string{}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.points", MigrationVersion: 12, Capabilities: []string{"points", "checkin"}, Events: []string{"points.checkin", "points.change", "texture.upload", "character.create"}, Pages: []PageContribution{{ID: "points", Area: "admin.plugins", Label: "积分与签到"}}, Permissions: []string{"user"}},
		{ID: "wishingskin.invitations", Version: "1.1.0", Dependencies: []string{}, Routes: []string{}, Tasks: []TaskDeclaration{}, SettingsNamespace: "wishingskin.invitations", MigrationVersion: 13, Capabilities: []string{"registration-invitation", "repeatable-export"}, Events: []string{"registration"}, Pages: []PageContribution{{ID: "invitations", Area: "admin.plugins", Label: "邀请码"}}, Permissions: []string{"admin"}},
	}
}
