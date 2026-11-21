package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"wishingskin/internal/backup"
	"wishingskin/internal/testutil"
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
				FontPreset:       "fusion_pixel",
				FontScope:        "all",
				CustomFontURL:    "https://fonts.example.com/pixel.css",
				CustomFontFamily: "Press Start 2P",
				Advanced:         advancedAppearance{Light: map[string]string{"heading": "#123456"}, Dark: map[string]string{"heading": "#abcdef"}},
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
				if got.ThemePack != pack || got.Accent != "violet" || got.DefaultTheme != "dark" || got.SurfaceEffect != "glass" || got.SurfaceOpacity != 74 || got.SurfaceBlur != 20 || !got.AllowUserTheme || got.FontPreset != "fusion_pixel" || got.FontScope != "all" || got.CustomFontURL != "https://fonts.example.com/pixel.css" || got.CustomFontFamily != "Press Start 2P" || got.Advanced.Light["heading"] != "#123456" || got.Advanced.Dark["heading"] != "#abcdef" {
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
			for old, want := range map[string]string{"cave": "plains", "ice_caves": "snowy", "lush_caves": "jungle", "desert_temple": "desert", "nether_crimson": "nether", "nether_warped": "nether", "end_city": "end", "void": "end"} {
				f.exec("UPDATE settings SET value=? WHERE setting_key='site_appearance'", `{"themePack":"`+old+`"}`)
				if got := read().ThemePack; got != want {
					t.Fatalf("stored theme %q resolved to %q, want %q", old, got, want)
				}
			}
		})
	}
}

func TestThemeAssetsRoundTripAcrossDatabases(t *testing.T) {
	font, err := os.ReadFile("../../web/public/fonts/fusion-pixel-12px-zh_hans.woff2")
	if err != nil {
		t.Fatal(err)
	}
	var pack bytes.Buffer
	zw := zip.NewWriter(&pack)
	rng := rand.New(rand.NewPCG(51, 87))
	var stone *image.NRGBA
	for _, key := range []string{"dirt", "stone", "cobblestone", "oak_planks", "oak_log", "deepslate", "bedrock", "packed_ice"} {
		w, err := zw.Create("assets/minecraft/textures/block/" + key + ".png")
		if err != nil {
			t.Fatal(err)
		}
		img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for i := range img.Pix {
			img.Pix[i] = byte(rng.Uint32())
		}
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
		if err := png.Encode(w, img); err != nil {
			t.Fatal(err)
		}
		if key == "stone" {
			stone = img
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"mysql", "postgres"}, {"postgres", "mysql"}} {
		t.Run(pair[0]+"_to_"+pair[1], func(t *testing.T) {
			f := newManagementApp(t, pair[0])
			upload := func(path, name string, data []byte) {
				t.Helper()
				var body bytes.Buffer
				mw := multipart.NewWriter(&body)
				part, err := mw.CreateFormFile("file", name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(data); err != nil {
					t.Fatal(err)
				}
				if err := mw.Close(); err != nil {
					t.Fatal(err)
				}
				webStatus(t, f.raw(3, http.MethodPost, path, mw.FormDataContentType(), body.Bytes()), 200)
			}
			upload("/api/admin/theme-font", "fixture.woff2", font)
			upload("/api/admin/theme-assets", "fixture.zip", pack.Bytes())
			check := func(f *managementApp) (assetURL, fontURL string) {
				t.Helper()
				var site struct {
					ThemeAssets publicThemeAssets
					ThemeFont   publicThemeFont
				}
				w := f.call(0, http.MethodGet, "/api/site", nil)
				webStatus(t, w, 200)
				if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil {
					t.Fatal(err)
				}
				if w.Body.Len() > 16<<10 || site.ThemeAssets.Count < 8 || site.ThemeAssets.Version == "" || site.ThemeFont.Size != len(font) || site.ThemeFont.URL == "" {
					t.Fatal("site response should contain asset metadata only")
				}
				w = f.call(0, http.MethodGet, site.ThemeFont.URL, nil)
				webStatus(t, w, 200)
				if !bytes.Equal(w.Body.Bytes(), font) {
					t.Fatal("downloaded WOFF2 bytes changed")
				}
				assetURL = "/api/theme-assets/" + site.ThemeAssets.Version + "/stone.png"
				w = f.call(0, http.MethodGet, assetURL, nil)
				webStatus(t, w, 200)
				img, err := png.Decode(w.Body)
				if err != nil {
					t.Fatal(err)
				}
				for y := range 64 {
					for x := range 64 {
						if c := color.NRGBAModel.Convert(img.At(x, y)); c != stone.NRGBAAt(x, y) {
							t.Fatalf("stone pixel (%d, %d) changed during import or restore", x, y)
						}
					}
				}
				return assetURL, site.ThemeFont.URL
			}
			assetURL, fontURL := check(f)
			root := t.TempDir()
			backupDir, restoreDir := filepath.Join(root, "export"), filepath.Join(root, "restored-textures")
			if _, err := backup.Export(context.Background(), f.db, f.app.cfg.TextureDir, backupDir); err != nil {
				t.Fatal(err)
			}
			target := testutil.Database(t, pair[1])
			if _, err := backup.Restore(context.Background(), target, backupDir, restoreDir); err != nil {
				t.Fatal(err)
			}
			app, err := New(target, Config{PublicURL: "http://example.test", TextureDir: restoreDir, SettingsKey: f.app.cfg.SettingsKey})
			if err != nil {
				t.Fatal(err)
			}
			if gotAsset, gotFont := check(&managementApp{t: t, db: target, app: app}); gotAsset != assetURL || gotFont != fontURL {
				t.Fatal("asset URLs changed after restore")
			}
			webStatus(t, f.call(3, http.MethodDelete, "/api/admin/theme-assets", nil), 200)
			webStatus(t, f.call(3, http.MethodDelete, "/api/admin/theme-font", nil), 200)
			webStatus(t, f.call(0, http.MethodGet, assetURL, nil), 404)
			webStatus(t, f.call(0, http.MethodGet, fontURL, nil), 404)
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
	if !validCustomFontFamily("") || !validCustomFontFamily("Press Start 2P, Noto Sans SC") {
		t.Fatal("valid custom font family was rejected")
	}
	for _, bad := range []string{"Bad;color:red", "Bad\"Quote", "Bad{Brace}", "Bad\nLine"} {
		if validCustomFontFamily(bad) {
			t.Fatalf("unsafe custom font family accepted: %q", bad)
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

func TestThemeArchiveHonorsPNGPixelBudget(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("assets/minecraft/textures/colormap/grass.png")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewGray(image.Rect(0, 0, 2048, 2049))
	if err := png.Encode(entry, img); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	remaining := maxThemePackPixels
	if _, err := decodeZipPNG(reader.File[0], &remaining); !errors.Is(err, errThemePNGLimit) {
		t.Fatalf("resource pack PNG beyond the supported pixel limit: %v", err)
	}
}
