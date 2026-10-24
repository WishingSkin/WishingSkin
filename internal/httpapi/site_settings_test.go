package httpapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
)

func TestSiteBrandingBothDatabases(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			var initial siteBranding
			w := f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil || initial.Name != "WishingSkin" || initial.Notice != "" {
				t.Fatal("default branding", err)
			}
			body := siteBranding{Name: "合成皮肤站", Tagline: "自己的外观", Notice: "第一行\n<script>alert('test')</script>"}
			webStatus(t, f.call(0, "PATCH", "/api/admin/branding", body), 401)
			webStatus(t, f.call(1, "PATCH", "/api/admin/branding", body), 403)
			for _, invalid := range []siteBranding{{Name: ""}, {Name: strings.Repeat("😀", 65)}, {Name: "a\nb"}, {Name: "site", Tagline: strings.Repeat("字", 201)}, {Name: "site", Notice: strings.Repeat("字", 1001)}, {Name: "site", Notice: "control\x00"}} {
				webStatus(t, f.call(3, "PATCH", "/api/admin/branding", invalid), 400)
			}
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			w = f.call(0, "GET", "/api/site", nil)
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil || initial.Name != "WishingSkin" {
				t.Fatal("unaudited branding", err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 200)
			f.exec("INSERT INTO settings(setting_key,value) VALUES(?,?)", "ygg_private_key", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
			w = f.call(0, "GET", "/api/yggdrasil", nil)
			webStatus(t, w, 200)
			var metadata struct {
				Meta struct{ ServerName, ImplementationName string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil || metadata.Meta.ServerName != body.Name || metadata.Meta.ImplementationName != "WishingSkin" {
				t.Fatal("branding metadata", err, w.Body)
			}
			f.exec("INSERT INTO settings(setting_key,value) VALUES('smtp_password','synthetic-secret-not-public')")
			w = f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil || initial != body {
				t.Fatal("branding roundtrip", initial, err)
			}
			if strings.Contains(w.Body.String(), "synthetic-secret-not-public") {
				t.Fatal("settings secret exposed")
			}
			var action, target string
			if err := f.db.DB.QueryRow("SELECT action,target_id FROM admin_events WHERE action='settings.branding'").Scan(&action, &target); err != nil || target != "branding" {
				t.Fatal("branding audit", err)
			}
			// All limits use Unicode code points, and all fields save atomically.
			body = siteBranding{Name: strings.Repeat("字", 64), Tagline: strings.Repeat("字", 200), Notice: strings.Repeat("😀", 1000)}
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 200)
			body = siteBranding{Name: "WishingSkin"}
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 200)
			w = f.call(0, "GET", "/api/site", nil)
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil || initial != body {
				t.Fatal("clear branding", err)
			}
		})
	}
}
