package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"wishingskin/examples/plugins/greeting"
	sdk "wishingskin/pkg/plugin"
)

func TestCommunityPluginsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			cfg := f.app.cfg
			cfg.CommunityPlugins = []sdk.Plugin{greeting.Plugin{}}
			a, err := New(f.db, cfg)
			if err != nil {
				t.Fatal(err)
			}
			f.app = a
			path := "/api/plugins/community.greeting/hello"
			webStatus(t, f.call(1, "GET", path, nil), 404)
			webStatus(t, f.call(1, "PATCH", "/api/admin/plugins/community.greeting", map[string]bool{"enabled": true}), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugin-settings/community.greeting", map[string]string{"message": "SDK works"}), 200)
			webStatus(t, f.call(1, "GET", path, nil), 404)
			var raw string
			if err := f.db.DB.QueryRow("SELECT settings_json FROM plugins WHERE plugin_id='community.greeting'").Scan(&raw); err != nil || strings.Contains(raw, "SDK works") {
				t.Fatal("community settings not encrypted")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.greeting", map[string]bool{"enabled": true}), 200)
			webStatus(t, f.call(0, "GET", path, nil), 401)
			w := f.call(1, "GET", path, nil)
			webStatus(t, w, 200)
			var result map[string]any
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || result["message"] != "SDK works" {
				t.Fatal("source plugin settings did not reach handler")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugin-settings/community.greeting", map[string]string{"unknown": "no"}), 400)
			w = f.call(3, "GET", "/api/admin/plugins", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"origin":"community"`) {
				t.Fatal("community plugin missing catalog entry")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.greeting", map[string]bool{"enabled": false}), 200)
			webStatus(t, f.call(1, "GET", path, nil), 404)
		})
	}
}
