package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

type communityFixture struct{ events *int }

func (communityFixture) Manifest() sdk.Manifest {
	return sdk.Manifest{ID: "community.security", Version: "1.0.0", SettingsNamespace: "community.security", Events: []string{"character.create"}, Settings: []sdk.SettingField{{Key: "token", Label: "Token", Type: "secret"}, {Key: "message", Label: "Message", Type: "text"}}}
}
func (p communityFixture) Register(h sdk.Host) error {
	if err := h.Subscribe("character.create", func(context.Context, sdk.Event) error { *p.events++; return nil }); err != nil {
		return err
	}
	return h.Handle(sdk.Route{Method: "POST", Path: "/action", Access: "admin", Handler: func(w http.ResponseWriter, _ *http.Request, _ sdk.Principal) { w.WriteHeader(204) }})
}

func TestCommunitySecurityBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			events := 0
			cfg := f.app.cfg
			cfg.CommunityPlugins = []sdk.Plugin{communityFixture{events: &events}}
			var err error
			f.app, err = New(f.db, cfg)
			if err != nil {
				t.Fatal(err)
			}
			configPath := "/api/admin/plugin-settings/community.security"
			webStatus(t, f.call(3, "PATCH", configPath, map[string]string{"token": "synthetic-token", "message": "first"}), 200)
			webStatus(t, f.call(3, "PATCH", configPath, map[string]string{"token": "", "message": "next"}), 200)
			w := f.call(3, "GET", configPath, nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "synthetic-token") || !strings.Contains(w.Body.String(), `"token":true`) {
				t.Fatal("secret redaction failed")
			}
			values, err := f.app.readCommunitySettings(context.Background(), "community.security")
			if err != nil || values["token"] != "synthetic-token" {
				t.Fatal("blank secret replaced saved credential")
			}
			f.app.publishPluginEvent(context.Background(), "character.create", 1, "1")
			if events != 0 {
				t.Fatal("disabled plugin received event")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.security", map[string]bool{"enabled": true}), 200)
			f.app.publishPluginEvent(context.Background(), "character.create", 1, "1")
			if events != 1 {
				t.Fatal("enabled plugin missed event")
			}
			path := "/api/plugins/community.security/action"
			webStatus(t, f.call(1, "POST", path, nil), 403)
			r := httptest.NewRequest("POST", "http://example.test"+path, strings.NewReader(`{}`))
			r.Header.Set("Origin", "http://example.test")
			r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 3)})
			w = httptest.NewRecorder()
			f.app.ServeHTTP(w, r)
			webStatus(t, w, 403)
			webStatus(t, f.call(3, "POST", path, nil), 204)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", configPath, map[string]string{"message": "rollback"}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			f.app, err = New(f.db, cfg)
			if err != nil {
				t.Fatal(err)
			}
			webStatus(t, f.call(3, "POST", path, nil), 204)
			values, err = f.app.readCommunitySettings(context.Background(), "community.security")
			if err != nil || values["message"] != "next" {
				t.Fatal("failed audit changed configuration")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.security", map[string]bool{"enabled": false}), 200)
			f.app.publishPluginEvent(context.Background(), "character.create", 1, "1")
			if events != 1 {
				t.Fatal("disabled plugin received event after restart")
			}
		})
	}
}
