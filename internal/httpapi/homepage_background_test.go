package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestHomepageBackgroundNormalization(t *testing.T) {
	for _, size := range [][2]int{{32, 16}, {2048, 1024}} {
		value, err := normalizeHomepageBackground("data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG(t, size[0], size[1])))
		if err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "data:image/jpeg;base64,"))
		config, format, decodeErr := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || decodeErr != nil || format != "jpeg" || config.Width > 1920 || config.Height > 1920 || config.Width != config.Height*2 {
			t.Fatalf("bad normalized background: %v %v %+v", err, decodeErr, config)
		}
	}
	var large bytes.Buffer
	_ = png.Encode(&large, image.NewGray(image.Rect(0, 0, 4097, 1)))
	for _, value := range []string{"https://example.test/image.png", "data:image/svg+xml;base64,PHN2Zy8+", "data:image/png;base64,bm90IGEgcG5n", "data:image/png;base64," + base64.StdEncoding.EncodeToString(large.Bytes()), "data:image/png;base64," + strings.Repeat("A", 2800000)} {
		if _, err := normalizeHomepageBackground(value); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
}

func TestHomepageBackgroundBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			body := defaultHomepage()
			body.Background = homepageBackground{Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG(t, 64, 32)), Overlay: 65}
			body.Blocks[1].Features[0].Icon = "◇"
			webStatus(t, f.call(1, "PATCH", "/api/admin/homepage", body), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", body), 200)
			w := f.call(0, "GET", "/api/site", nil)
			var site struct {
				Homepage homepageSettings `json:"homepage"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil || site.Homepage.Background.Image != "" || !strings.HasPrefix(site.Homepage.Background.ImageURL, "/api/homepage/background?v=") {
				t.Fatal("background leaked or missing URL", err)
			}
			imageResponse := f.call(0, "GET", site.Homepage.Background.ImageURL, nil)
			webStatus(t, imageResponse, 200)
			if imageResponse.Header().Get("Content-Type") != "image/jpeg" || imageResponse.Header().Get("ETag") == "" {
				t.Fatal("missing image headers")
			}
			w = f.call(3, "GET", "/api/admin/homepage", nil)
			var saved homepageSettings
			if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || !strings.HasPrefix(saved.Background.Image, "data:image/jpeg;base64,") || saved.Blocks[1].Features[0].Icon != "◇" {
				t.Fatal("background roundtrip", err)
			}
			body.Background.Overlay = 1
			webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", body), 400)
			body.Background = homepageBackground{Overlay: 65}
			webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", body), 200)
			webStatus(t, f.call(0, "GET", "/api/homepage/background", nil), 404)
		})
	}
}
