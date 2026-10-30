package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginImageBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			read := func() string {
				t.Helper()
				w := f.call(3, "GET", "/api/admin/login-image", nil)
				webStatus(t, w, 200)
				var saved struct{ Image string }
				if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
					t.Fatal(err)
				}
				return saved.Image
			}
			if read() != "" {
				t.Fatal("default login image is not empty")
			}
			webStatus(t, f.call(0, "GET", "/api/login/image", nil), 404)
			transparent := image.NewNRGBA(image.Rect(0, 0, 2048, 1024))
			transparent.SetNRGBA(0, 0, color.NRGBA{R: 20, G: 40, B: 80, A: 64})
			var transparentData bytes.Buffer
			if err := png.Encode(&transparentData, transparent); err != nil {
				t.Fatal(err)
			}
			body := map[string]string{"image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(transparentData.Bytes())}
			for _, user := range []int{0, 1} {
				status := 403
				if user == 0 {
					status = 401
				}
				webStatus(t, f.call(user, "GET", "/api/admin/login-image", nil), status)
				webStatus(t, f.call(user, "PATCH", "/api/admin/login-image", body), status)
			}
			payload, _ := json.Marshal(body)
			r := httptest.NewRequest("PATCH", "http://example.test/api/admin/login-image", bytes.NewReader(payload))
			r.Header.Set("Origin", "http://example.test")
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(&http.Cookie{Name: "ws_session", Value: strings.Repeat("0", 63) + "3"})
			w := httptest.NewRecorder()
			f.app.ServeHTTP(w, r)
			webStatus(t, w, 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/login-image", body), 200)
			saved := read()
			if !strings.HasPrefix(saved, "data:image/png;base64,") {
				t.Fatal("transparent PNG was not preserved")
			}
			w = f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			var site struct{ LoginImageURL string }
			if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil || !strings.HasPrefix(site.LoginImageURL, "/api/login/image?v=") || strings.Contains(w.Body.String(), "data:image/") {
				t.Fatal("public image URL missing or bytes leaked", err)
			}
			w = f.call(0, "GET", site.LoginImageURL, nil)
			webStatus(t, w, 200)
			cfg, format, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
			if err != nil || format != "png" || cfg.Width != 1920 || cfg.Height != 960 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "public, max-age=0, must-revalidate" {
				t.Fatal("invalid public image or headers", err, cfg)
			}
			decoded, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, alpha := decoded.At(0, 0).RGBA()
			if alpha == 65535 {
				t.Fatal("transparent pixel became opaque")
			}
			etag := w.Header().Get("ETag")
			if etag == "" {
				t.Fatal("missing ETag")
			}
			r = httptest.NewRequest("GET", "http://example.test"+site.LoginImageURL, nil)
			r.Header.Set("If-None-Match", etag)
			w = httptest.NewRecorder()
			f.app.ServeHTTP(w, r)
			webStatus(t, w, 304)
			if w.Body.Len() != 0 {
				t.Fatal("304 returned an image body")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/login-image", map[string]string{"image": saved}), 200)
			if read() != saved {
				t.Fatal("unchanged JPEG was encoded again")
			}
			var oversized bytes.Buffer
			_ = png.Encode(&oversized, image.NewGray(image.Rect(0, 0, 4097, 1)))
			for _, invalid := range []string{"https://example.test/image.png", "data:image/svg+xml;base64,PHN2Zy8+", "data:image/png;base64,bm90IGEgcG5n", "data:image/png;base64," + base64.StdEncoding.EncodeToString(oversized.Bytes()), "data:image/png;base64," + strings.Repeat("A", 2800000)} {
				webStatus(t, f.call(3, "PATCH", "/api/admin/login-image", map[string]string{"image": invalid}), 400)
				if read() != saved {
					t.Fatal("invalid upload changed saved image")
				}
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE action='settings.login_image' AND actor_id=3").Scan(&count); err != nil || count != 2 {
				t.Fatal("missing image audit", count, err)
			}
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/login-image", map[string]string{"image": ""}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			if read() != saved {
				t.Fatal("unaudited image removal committed")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/login-image", map[string]string{"image": ""}), 200)
			if read() != "" {
				t.Fatal("image was not removed")
			}
			webStatus(t, f.call(0, "GET", site.LoginImageURL, nil), 404)
			w = f.call(0, "GET", "/api/site", nil)
			if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil || site.LoginImageURL != "" {
				t.Fatal("removed image URL still exposed", err)
			}
		})
	}
}
