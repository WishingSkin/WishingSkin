package httpapi

import (
	"encoding/json"
	"testing"
)

func TestSurfaceEffectsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			for _, effect := range []string{"solid", "translucent", "glass"} {
				body := map[string]string{"accent": "blue", "defaultTheme": "system", "surfaceEffect": effect}
				webStatus(t, f.call(1, "PATCH", "/api/admin/appearance", body), 403)
				webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 200)
				w := f.call(0, "GET", "/api/site", nil)
				webStatus(t, w, 200)
				var site struct{ Appearance appearance }
				if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil || site.Appearance.SurfaceEffect != effect {
					t.Fatal("surface effect did not roundtrip", effect, err)
				}
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", map[string]string{"accent": "blue", "defaultTheme": "system", "surfaceEffect": "invalid"}), 400)
			// Old clients and saved settings retain opaque surfaces by default.
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", map[string]string{"accent": "blue", "defaultTheme": "system"}), 200)
			value, err := f.app.appearance(t.Context())
			if err != nil || value.SurfaceEffect != "solid" {
				t.Fatal("missing default surface effect", err)
			}
		})
	}
}
