package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"testing"
)

func TestDefaultSkinAssetsAndProfileMetadataBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			standard := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "DefaultSkin"}))
			slim := webCharacter(t, f.call(2, "POST", "/api/characters", map[string]string{"name": "SlimSkin"}))
			f.exec("UPDATE characters SET model=? WHERE id=?", "slim", slim.ID)

			for _, check := range []struct {
				character Character
				model     string
				slim      bool
			}{
				{character: standard, model: "default"},
				{character: slim, model: "slim", slim: true},
			} {
				w := f.call(0, "GET", "/api/yggdrasil/sessionserver/session/minecraft/profile/"+check.character.UUID, nil)
				webStatus(t, w, 200)
				var profile struct {
					Properties []struct{ Name, Value string }
				}
				if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || len(profile.Properties) != 1 || profile.Properties[0].Name != "textures" {
					t.Fatalf("invalid profile: %v %s", err, w.Body.String())
				}
				data, err := base64.StdEncoding.DecodeString(profile.Properties[0].Value)
				if err != nil {
					t.Fatal(err)
				}
				var payload struct {
					Textures map[string]struct {
						URL      string
						Metadata map[string]string
					}
				}
				if err := json.Unmarshal(data, &payload); err != nil {
					t.Fatal(err)
				}
				skin, ok := payload.Textures["SKIN"]
				if !ok || skin.URL != "http://example.test/defaults/skins/"+check.model {
					t.Fatalf("default skin for %s: %#v", check.model, skin)
				}
				if check.slim && skin.Metadata["model"] != "slim" {
					t.Fatalf("slim metadata missing: %#v", skin)
				}
				if !check.slim && skin.Metadata != nil {
					t.Fatalf("standard skin has metadata: %#v", skin)
				}
			}

			for _, method := range []string{"GET", "HEAD"} {
				w := f.raw(0, method, "/defaults/skins/default", "", nil)
				webStatus(t, w, 200)
				if w.Header().Get("Content-Type") != "image/png" {
					t.Fatalf("%s content type: %q", method, w.Header().Get("Content-Type"))
				}
				if method == "GET" {
					cfg, err := png.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
					if err != nil || cfg.Width != 64 || cfg.Height != 64 {
						t.Fatalf("default skin PNG: %dx%d %v", cfg.Width, cfg.Height, err)
					}
				}
			}
			w := f.raw(0, "GET", "/defaults/skins/slim", "", nil)
			webStatus(t, w, 200)
			if _, err := png.DecodeConfig(bytes.NewReader(w.Body.Bytes())); err != nil {
				t.Fatal("slim skin is not a PNG:", err)
			}
			webStatus(t, f.raw(0, "GET", "/defaults/skins/unknown", "", nil), 404)
		})
	}
}
