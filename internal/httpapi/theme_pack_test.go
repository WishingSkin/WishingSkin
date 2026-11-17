package httpapi

import (
	"encoding/json"
	"testing"
)

func TestThemePacksBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			read := func() appearance {
				t.Helper()
				w := f.call(0, "GET", "/api/site", nil)
				webStatus(t, w, 200)
				var site struct{ Appearance appearance }
				if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil {
					t.Fatal(err)
				}
				return site.Appearance
			}
			if read().ThemePack != "default" {
				t.Fatal("new site did not use the default theme pack")
			}
			body := appearance{Accent: "violet", DefaultTheme: "dark", ThemePack: "cherry", SurfaceEffect: "glass", Advanced: advancedAppearance{Light: map[string]string{"heading": "#123456"}, Dark: map[string]string{"heading": "#abcdef"}}}
			webStatus(t, f.call(1, "PATCH", "/api/admin/appearance", body), 403)
			for _, pack := range []string{"default", "plains", "snowy", "jungle", "desert", "cave", "ice_caves", "lush_caves", "desert_temple", "cherry", "nether_crimson", "nether_warped", "deep_dark", "end_city", "void", "mc_official", "mc_wiki"} {
				body.ThemePack = pack
				webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 200)
				got := read()
				if got.ThemePack != pack || got.Accent != "violet" || got.DefaultTheme != "dark" || got.SurfaceEffect != "glass" || got.Advanced.Light["heading"] != "#123456" || got.Advanced.Dark["heading"] != "#abcdef" {
					t.Fatalf("theme settings did not roundtrip for %s", pack)
				}
			}
			body.ThemePack = "unknown"
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 400)
			if read().ThemePack != "mc_wiki" {
				t.Fatal("invalid pack changed the saved theme")
			}
			// Settings saved before theme packs existed retain their original accent.
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", map[string]string{"accent": "green", "defaultTheme": "system"}), 200)
			if got := read(); got.ThemePack != "default" || got.Accent != "green" {
				t.Fatal("legacy appearance did not retain the default pack and accent")
			}
			f.exec("UPDATE settings SET value='{}' WHERE setting_key='site_appearance'")
			if read().ThemePack != "default" {
				t.Fatal("legacy stored appearance did not use the default theme pack")
			}
		})
	}
}
