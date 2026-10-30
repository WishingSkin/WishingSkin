package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestHomepageIconNormalization(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	img.Set(16, 8, color.NRGBA{R: 80, G: 120, B: 200, A: 90})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	value, err := normalizeHomepageIcon("data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 64 {
		t.Fatal("icon dimensions not normalized")
	}
	_, _, _, alpha := decoded.At(32, 32).RGBA()
	if alpha == 0 || alpha == 65535 {
		t.Fatal("icon alpha lost")
	}
	for _, value := range []string{"data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,bad", "/%2fexample.test/icon.png", "/%5cexample.test/icon.png", "/icon\n.png"} {
		if _, err := normalizeHomepageIcon(value); err == nil {
			t.Fatal("unsafe icon accepted")
		}
	}
}

func TestHomepageSettingsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			var initial struct {
				Homepage homepageSettings `json:"homepage"`
			}
			webStatus(t, f.call(0, "GET", "/api/site", nil), 200)
			w := f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil {
				t.Fatal(err)
			}
			if len(initial.Homepage.Blocks) != 3 || initial.Homepage.Blocks[0].ID != "intro" {
				t.Fatalf("default homepage: %+v", initial.Homepage)
			}

			body := homepageSettings{Blocks: []homepageBlock{
				{ID: "intro", Kind: "intro", Enabled: true, Title: "欢迎", Body: "安全内容", ButtonLabel: "进入", ButtonHref: "/login"},
				{ID: "cards", Kind: "features", Enabled: true, Title: "功能", Features: []homepageFeature{{Title: "公开库", Body: "浏览材质", Href: "/library"}}},
			}}
			webStatus(t, f.call(0, "PATCH", "/api/admin/homepage", body), 401)
			webStatus(t, f.call(1, "PATCH", "/api/admin/homepage", body), 403)
			for _, invalid := range []homepageSettings{
				{Blocks: []homepageBlock{{ID: "same", Kind: "cta"}, {ID: "same", Kind: "notice"}}},
				{Blocks: []homepageBlock{{ID: "bad", Kind: "cta", ButtonHref: "javascript:alert(1)"}}},
				{Blocks: []homepageBlock{{ID: "bad", Kind: "cta", ButtonHref: "http://example.test"}}},
				{Blocks: []homepageBlock{{ID: "bad", Kind: "cta", Title: strings.Repeat("字", 81)}}},
			} {
				webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", invalid), 400)
			}

			w = f.call(3, "PATCH", "/api/admin/homepage", body)
			webStatus(t, w, 200)
			var saved homepageSettings
			if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || len(saved.Blocks) != 2 || saved.Blocks[1].Features[0].Href != "/library" {
				t.Fatalf("saved homepage: %v %+v", err, saved)
			}
			w = f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil || initial.Homepage.Blocks[0].Title != "欢迎" {
				t.Fatalf("roundtrip homepage: %v %+v", err, initial.Homepage)
			}
			var action, target string
			if err := f.db.DB.QueryRow("SELECT action,target_id FROM admin_events WHERE action='settings.homepage'").Scan(&action, &target); err != nil || target != "homepage" {
				t.Fatalf("homepage audit: %v %s %s", err, action, target)
			}
		})
	}
}

func TestHomepageIconAddressesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			for _, address := range []string{"javascript:alert(1)", "//example.test/icon.png", `C:\icons\logo.png`, "/\\example.test/icon.png", "http://example.test/icon.png", "https://user:secret@example.test/icon.png"} {
				body := map[string]any{"blocks": []any{map[string]any{"id": "intro", "kind": "intro", "iconImage": address}}}
				webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", body), 400)
			}
			body := map[string]any{"blocks": []any{map[string]any{"id": "intro", "kind": "intro", "enabled": true, "iconImage": "https://example.test/brand.png"}, map[string]any{"id": "cards", "kind": "features", "enabled": true, "features": []any{map[string]any{"title": "Card", "iconImage": "/assets/card.png"}}}}}
			webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", body), 200)
			w := f.call(0, "GET", "/api/site", nil)
			if !strings.Contains(w.Body.String(), "https://example.test/brand.png") || !strings.Contains(w.Body.String(), "/assets/card.png") {
				t.Fatal("homepage icon addresses not retained")
			}
		})
	}
}
