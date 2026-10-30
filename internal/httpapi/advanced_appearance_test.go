package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdvancedAppearanceValidation(t *testing.T) {
	if err := (advancedAppearance{Light: map[string]string{"heading": "#0066cc"}, Dark: map[string]string{"accent": "#aabbcc"}, Copy: map[string]string{"登录": "进入"}, Icons: map[string]string{"login": "→"}}).validate(); err != nil {
		t.Fatal(err)
	}
	for _, v := range []advancedAppearance{{Light: map[string]string{"panel": "url(https://example.test)"}}, {Dark: map[string]string{"unknown": "#ffffff"}}, {Icons: map[string]string{"login": "<svg>"}}, {Copy: map[string]string{"登录": "bad\x00"}}, {LocalizedCopy: map[string]map[string]string{"fr": {"登录": "test"}}}, {LocalizedCopy: map[string]map[string]string{"en": {"登录": "bad\x00"}}}} {
		if v.validate() == nil {
			t.Fatal("unsafe setting accepted")
		}
	}
}

func TestAdvancedAppearancePersistenceBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			body := appearance{Accent: "blue", DefaultTheme: "system", SurfaceEffect: "glass", Advanced: advancedAppearance{Light: map[string]string{"heading": "#0066cc"}, Dark: map[string]string{"heading": "#ccbbff"}, Copy: map[string]string{"登录": "进入站点", "角色管理": "我的角色"}, Icons: map[string]string{"characters": "◇"}}}
			webStatus(t, f.call(1, "PATCH", "/api/admin/appearance", body), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 200)
			w := f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			var site struct{ Appearance appearance }
			if json.Unmarshal(w.Body.Bytes(), &site) != nil || site.Appearance.Advanced.Dark["heading"] != "#ccbbff" || site.Appearance.Advanced.LocalizedCopy["zh-CN"]["登录"] != "进入站点" || site.Appearance.Advanced.LocalizedCopy["en"]["登录"] != "进入站点" || site.Appearance.Advanced.Icons["characters"] != "◇" {
				t.Fatal("advanced appearance not exposed")
			}
			body.Advanced.Copy = nil
			body.Advanced.LocalizedCopy = map[string]map[string]string{"zh-CN": {"登录": "欢迎"}, "en": {"登录": "Welcome"}}
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 200)
			w = f.call(0, "GET", "/api/site", nil)
			if json.Unmarshal(w.Body.Bytes(), &site) != nil || site.Appearance.Advanced.LocalizedCopy["zh-CN"]["登录"] != "欢迎" || site.Appearance.Advanced.LocalizedCopy["en"]["登录"] != "Welcome" || len(site.Appearance.Advanced.Copy) != 0 {
				t.Fatal("language overrides not isolated")
			}
			body.Advanced.Light["heading"] = "url(javascript:alert(1))"
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 400)
			delete(body.Advanced.Light, "heading")
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			var saved string
			if err := f.db.DB.QueryRow("SELECT value FROM settings WHERE setting_key='site_appearance'").Scan(&saved); err != nil || !strings.Contains(saved, "#0066cc") {
				t.Fatal("failed audit changed appearance")
			}
		})
	}
}
