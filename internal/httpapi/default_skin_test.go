package httpapi

import (
	"bytes"
	"context"
	"image/png"
	"testing"
)

func TestDefaultSkinAssetsAndProfileMetadataBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			standard := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "DefaultSkin"}))
			slim := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "SlimSkin"}))
			f.exec("UPDATE characters SET model=? WHERE id=?", "slim", slim.ID)

			for _, check := range []struct {
				character Character
				model     string
				slim      bool
			}{
				{character: standard, model: "default"},
				{character: slim, model: "slim", slim: true},
			} {
				profile, err := f.app.profileResponse(context.Background(), f.db.DB, check.character.UUID, false)
				if err != nil {
					t.Fatal(err)
				}
				textures, ok := profile["textures"].(map[string]any)
				if !ok {
					t.Fatalf("missing textures: %#v", profile["textures"])
				}
				skin, ok := textures["SKIN"].(map[string]any)
				if !ok || skin["url"] != "http://example.test/defaults/skins/"+check.model {
					t.Fatalf("default skin for %s: %#v", check.model, textures["SKIN"])
				}
				metadata, hasMetadata := skin["metadata"].(map[string]string)
				if check.slim && (!hasMetadata || metadata["model"] != "slim") {
					t.Fatalf("slim metadata missing: %#v", skin)
				}
				if !check.slim && hasMetadata {
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
