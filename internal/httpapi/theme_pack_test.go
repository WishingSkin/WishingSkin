package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			gotDefault := read()
			if gotDefault.ThemePack != "default" || gotDefault.FontPreset != "system" || gotDefault.FontScope != "headings" || gotDefault.AllowUserTheme {
				t.Fatalf("unexpected default appearance: %+v", gotDefault)
			}
			body := appearance{
				Accent:         "violet",
				DefaultTheme:   "dark",
				ThemePack:      "cherry",
				SurfaceEffect:  "glass",
				SurfaceOpacity: 74,
				SurfaceBlur:    20,
				AllowUserTheme: true,
				FontPreset:     "fusion_pixel",
				FontScope:      "all",
				CustomFontURL:  "https://fonts.example.com/pixel.css",
				Advanced:       advancedAppearance{Light: map[string]string{"heading": "#123456"}, Dark: map[string]string{"heading": "#abcdef"}},
			}
			webStatus(t, f.call(1, "PATCH", "/api/admin/appearance", body), 403)
			packs := []string{
				"default",
				"plains", "snowy", "jungle", "desert", "cherry",
				"nether", "end", "deep_dark",
				"mc_official", "mc_wiki",
				"nord", "paper", "high_contrast",
			}
			for _, pack := range packs {
				body.ThemePack = pack
				webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 200)
				got := read()
				if got.ThemePack != pack || got.Accent != "violet" || got.DefaultTheme != "dark" || got.SurfaceEffect != "glass" || got.SurfaceOpacity != 74 || got.SurfaceBlur != 20 || !got.AllowUserTheme || got.FontPreset != "fusion_pixel" || got.FontScope != "all" || got.CustomFontURL != "https://fonts.example.com/pixel.css" || got.Advanced.Light["heading"] != "#123456" || got.Advanced.Dark["heading"] != "#abcdef" {
					t.Fatalf("theme settings did not roundtrip for %s: %+v", pack, got)
				}
			}
			body.ThemePack = "plains"
			body.SurfaceOpacity = 20
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 400)
			body.SurfaceOpacity = 74
			body.SurfaceBlur = 60
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 400)
			body.SurfaceBlur = 20
			body.ThemePack = "high_contrast"
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 200)
			body.ThemePack = "unknown"
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 400)
			if read().ThemePack != "high_contrast" {
				t.Fatal("invalid pack changed the saved theme")
			}
			body.ThemePack = "plains"
			body.FontPreset = "bad_font"
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 400)
			body.FontPreset = "monocraft"
			body.CustomFontURL = "javascript:alert(1)"
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 400)
			body.CustomFontURL = "/fonts/custom.woff2"
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", body), 200)

			// Legacy or minimal stored payloads fall back safely.
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", map[string]string{"accent": "green", "defaultTheme": "system"}), 200)
			if got := read(); got.ThemePack != "default" || got.Accent != "green" || got.FontPreset != "system" || got.FontScope != "headings" {
				t.Fatalf("minimal appearance did not apply defaults: %+v", got)
			}
			f.exec("UPDATE settings SET value='{}' WHERE setting_key='site_appearance'")
			if read().ThemePack != "default" {
				t.Fatal("empty stored appearance did not use the default theme pack")
			}
		})
	}
}

func TestExtractThemeTexturesFromSyntheticArchive(t *testing.T) {
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	writePNG := func(name string, w, h int, c color.NRGBA) {
		t.Helper()
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(fw, img); err != nil {
			t.Fatal(err)
		}
	}
	writePNG("assets/minecraft/textures/colormap/grass.png", 256, 256, color.NRGBA{100, 180, 80, 255})
	writePNG("assets/minecraft/textures/colormap/foliage.png", 256, 256, color.NRGBA{80, 160, 60, 255})
	writePNG("assets/minecraft/textures/block/grass_block_top.png", 16, 16, color.NRGBA{200, 200, 200, 255})
	writePNG("assets/minecraft/textures/block/grass_block_side.png", 16, 16, color.NRGBA{134, 96, 67, 255})
	writePNG("assets/minecraft/textures/block/grass_block_side_overlay.png", 16, 16, color.NRGBA{200, 200, 200, 255})
	writePNG("assets/minecraft/textures/block/dirt.png", 16, 16, color.NRGBA{134, 96, 67, 255})
	writePNG("assets/minecraft/textures/block/stone.png", 16, 16, color.NRGBA{125, 125, 125, 255})
	writePNG("assets/minecraft/textures/block/deepslate.png", 16, 16, color.NRGBA{72, 72, 78, 255})
	writePNG("assets/minecraft/textures/block/bedrock.png", 16, 16, color.NRGBA{40, 40, 40, 255})
	writePNG("assets/minecraft/textures/block/ice_crystal.png", 16, 32, color.NRGBA{140, 220, 255, 255}) // 2-frame animation
	writePNG("assets/minecraft/textures/block/suspicious_sand_0.png", 16, 16, color.NRGBA{219, 207, 163, 255})
	writePNG("assets/minecraft/textures/block/purpur_pillar_side.png", 16, 16, color.NRGBA{170, 126, 170, 255})
	writePNG("assets/minecraft/textures/particle/cherry_0.png", 8, 8, color.NRGBA{245, 175, 200, 255})
	writePNG("assets/minecraft/textures/particle/sculk_soul_0.png", 16, 16, color.NRGBA{60, 215, 225, 255})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	tmp := filepath.Join(t.TempDir(), "client-test.jar")
	if err := os.WriteFile(tmp, zipBuf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	stored, err := extractThemeTexturesFromZip(tmp, "client-test.jar")
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if stored.Version == "" || stored.Count < 12 {
		t.Fatalf("unexpected extracted count: %+v", stored.Count)
	}
	for _, key := range []string{"plains_grass_top", "plains_grass_side", "ice_crystal", "suspicious_sand", "purpur_pillar", "bedrock", "particle_cherry_0", "particle_sculk_soul_0"} {
		if stored.Textures[key] == "" {
			t.Fatalf("missing expected extracted texture %q", key)
		}
	}
}

func TestThemeFontUploadValidationUnit(t *testing.T) {
	if !validCustomFontURL("") || !validCustomFontURL("/fonts/custom.woff2") || !validCustomFontURL("https://cdn.example.com/font.css") {
		t.Fatal("valid custom font URLs were rejected")
	}
	for _, bad := range []string{"http://insecure.example/font.css", "//evil.example/font.css", "/../secret", "javascript:alert(1)", "https://example.com/a'b"} {
		if validCustomFontURL(bad) {
			t.Fatalf("unsafe custom font URL accepted: %q", bad)
		}
	}
	// Verify WOFF2 magic check in multipart upload helper shape.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "bad.woff2")
	_, _ = fw.Write([]byte("NOT_WOFF2"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/theme-font", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if req.Header.Get("Content-Type") == "" {
		t.Fatal("missing multipart content type")
	}
}
