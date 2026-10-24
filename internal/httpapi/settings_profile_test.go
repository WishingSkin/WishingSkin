package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"strings"
	"testing"
)

func TestEncryptedServicesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.app.cfg.SettingsKey = bytes.Repeat([]byte{7}, 32)
			body := map[string]any{"smtpEnabled": true, "address": "127.0.0.1:2525", "from": "mail@example.test", "username": "fixture", "password": "only-in-ciphertext", "tlsMode": "local", "testRecipient": "qa@example.test", "turnstileEnabled": true, "siteKey": "site-fixture", "secret": "never-return-this-secret"}
			w := f.call(3, "PATCH", "/api/admin/services", body)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "only-in-ciphertext") || strings.Contains(w.Body.String(), "never-return-this-secret") {
				t.Fatal("secret response", w.Body)
			}
			var raw string
			f.db.DB.QueryRow("SELECT value FROM settings WHERE setting_key='service_config'").Scan(&raw)
			if strings.Contains(raw, "only-in-ciphertext") || strings.Contains(raw, "never-return-this-secret") {
				t.Fatal("plaintext secret stored")
			}
			body["password"] = ""
			body["secret"] = ""
			body["from"] = "updated@example.test"
			webStatus(t, f.call(3, "PATCH", "/api/admin/services", body), 200)
			saved, _, err := f.app.serviceSettings(context.Background())
			if err != nil || saved.Password != "only-in-ciphertext" || saved.Secret != "never-return-this-secret" || saved.TestRecipient != "qa@example.test" {
				t.Fatal("secret preservation", err)
			}
			view := f.call(3, "GET", "/api/admin/services", nil)
			webStatus(t, view, 200)
			if !strings.Contains(view.Body.String(), `"testRecipient":"qa@example.test"`) {
				t.Fatal("SMTP test recipient was not persisted", view.Body)
			}
			reloaded, err := New(f.db, Config{PublicURL: "http://example.test", SettingsKey: f.app.cfg.SettingsKey})
			if err != nil {
				t.Fatal(err)
			}
			services, err := reloaded.services(context.Background())
			if err != nil || services.Mail == nil || services.Challenge == nil || services.SiteKey != "site-fixture" {
				t.Fatal("restart not applied", err)
			}
			body["testRecipient"] = "invalid recipient"
			webStatus(t, f.call(3, "PATCH", "/api/admin/services", body), 400)
			body["testRecipient"] = "qa@example.test"
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			body["from"] = "unaudited@example.test"
			webStatus(t, f.call(3, "PATCH", "/api/admin/services", body), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			saved, _, _ = f.app.serviceSettings(context.Background())
			if saved.From != "updated@example.test" {
				t.Fatal("unaudited services persisted")
			}
			w = f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "ciphertext") || strings.Contains(w.Body.String(), "never-return") {
				t.Fatal("public secret leak")
			}
			f.app.cfg.SettingsKey = bytes.Repeat([]byte{8}, 32)
			webStatus(t, f.call(3, "GET", "/api/admin/services", nil), 503)
			webStatus(t, f.call(0, "GET", "/api/site", nil), 503)
			webStatus(t, f.call(0, "GET", "/readyz", nil), 503)
		})
	}
}

func TestProfileAvatarOwnershipBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), nil))
			c := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "AvatarRole"}))
			webStatus(t, f.call(1, "PUT", "/api/characters/"+fmt.Sprint(c.ID)+"/textures/skin", map[string]string{"recordId": record.ID}), 204)
			body := map[string]string{"nickname": "New name", "avatarMode": "character", "characterUUID": c.UUID}
			webStatus(t, f.call(2, "PATCH", "/api/account/profile", body), 400)
			webStatus(t, f.call(1, "PATCH", "/api/account/profile", body), 200)
			w := f.call(0, "GET", "/api/avatars/1.png", nil)
			webStatus(t, w, 200)
			if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
				t.Fatal("invalid avatar", err)
			}
			f.exec("UPDATE textures SET deleted=TRUE WHERE hash=?", record.FileHash)
			fallback := f.call(0, "GET", "/api/avatars/1.png", nil)
			webStatus(t, fallback, 200)
			if bytes.Equal(w.Body.Bytes(), fallback.Body.Bytes()) {
				t.Fatal("disabled skin still rendered")
			}
			body = map[string]string{"nickname": "Upload name", "avatarMode": "upload", "avatarImage": "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG(t, 32, 32))}
			webStatus(t, f.call(1, "PATCH", "/api/account/profile", body), 200)
			w = f.call(1, "GET", "/api/account/profile", nil)
			if !strings.Contains(w.Body.String(), `"avatarMode":"upload"`) {
				t.Fatal(w.Body)
			}
		})
	}
}

func TestSettingsAndProfileBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			mailbox := &testMailbox{}
			f.app.cfg.MailSender = mailbox
			webStatus(t, f.call(3, "POST", "/api/admin/services/test", nil), 200)
			mailbox.mu.Lock()
			if len(mailbox.messages) != 1 || mailbox.messages[0].To != "3@example.test" || mailbox.messages[0].Purpose != "test" || mailbox.messages[0].Link != "" {
				mailbox.mu.Unlock()
				t.Fatal("unexpected SMTP test message", mailbox.messages)
			}
			mailbox.mu.Unlock()
			webStatus(t, f.call(3, "POST", "/api/admin/services/test", map[string]string{"recipient": "qa@example.test"}), 200)
			mailbox.mu.Lock()
			if len(mailbox.messages) != 2 || mailbox.messages[1].To != "qa@example.test" {
				mailbox.mu.Unlock()
				t.Fatal("custom SMTP recipient was not used", mailbox.messages)
			}
			mailbox.mu.Unlock()
			webStatus(t, f.call(3, "POST", "/api/admin/services/test", map[string]string{"recipient": "invalid recipient"}), 400)
			mailbox.fail = true
			webStatus(t, f.call(3, "POST", "/api/admin/services/test", nil), 503)
			for _, path := range []string{"/api/admin/services", "/api/admin/plugins/invitations"} {
				webStatus(t, f.call(0, "GET", path, nil), 401)
				webStatus(t, f.call(1, "GET", path, nil), 403)
			}
			w := f.call(3, "POST", "/api/admin/notice-preview", map[string]string{"notice": "**Hello** <b>world</b><script>alert(1)</script><a href=\"javascript:alert(1)\" onclick=\"alert(2)\">bad</a>", "format": "markdown"})
			webStatus(t, w, 200)
			var preview struct{ HTML string }
			json.Unmarshal(w.Body.Bytes(), &preview)
			if !strings.Contains(preview.HTML, "<strong>Hello</strong>") || !strings.Contains(preview.HTML, "<b>world</b>") || strings.Contains(preview.HTML, "<script") || strings.Contains(preview.HTML, "javascript:") || strings.Contains(preview.HTML, "onclick") {
				t.Fatal("unsafe or missing formatting", preview.HTML)
			}
			webStatus(t, f.call(1, "PATCH", "/api/admin/appearance", map[string]string{"accent": "violet", "defaultTheme": "system"}), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", map[string]string{"accent": "invalid", "defaultTheme": "system"}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", map[string]string{"accent": "violet", "defaultTheme": "system"}), 200)
			w = f.call(3, "POST", "/api/admin/plugins/invitations/codes", map[string]int{"count": 2, "uses": 1, "hours": 24})
			webStatus(t, w, 409)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/invitations", map[string]bool{"enabled": true, "required": true}), 200)
			w = f.call(3, "POST", "/api/admin/plugins/invitations/codes", map[string]int{"count": 2, "uses": 1, "hours": 24})
			webStatus(t, w, 201)
			var batch struct{ Codes []string }
			json.Unmarshal(w.Body.Bytes(), &batch)
			if len(batch.Codes) != 2 || batch.Codes[0] == batch.Codes[1] {
				t.Fatal("batch missing", w.Body)
			}
			var stored string
			f.db.DB.QueryRow("SELECT code_hash FROM invitation_codes LIMIT 1").Scan(&stored)
			if stored == batch.Codes[0] || stored == batch.Codes[1] {
				t.Fatal("plaintext invitation persisted")
			}
			webStatus(t, f.call(1, "PATCH", "/api/account/profile", map[string]string{"nickname": "Changed", "avatarMode": "default"}), 200)
			w = f.call(1, "GET", "/api/auth/me", nil)
			if !strings.Contains(w.Body.String(), `"nickname":"Changed"`) || !strings.Contains(w.Body.String(), `"isAdmin":false`) {
				t.Fatal("profile or permission", w.Body)
			}
			webStatus(t, f.call(1, "PATCH", "/api/account/profile", map[string]string{"nickname": " ", "avatarMode": "default"}), 400)
			webStatus(t, f.call(1, "PATCH", "/api/account/profile", map[string]string{"nickname": "Changed", "avatarMode": "character", "characterUUID": strings.Repeat("1", 32)}), 400)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/appearance", map[string]string{"accent": "green", "defaultTheme": "dark"}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			w = f.call(0, "GET", "/api/site", nil)
			if !strings.Contains(w.Body.String(), `"accent":"violet"`) {
				t.Fatal("unaudited appearance saved", w.Body)
			}
		})
	}
}

func TestTurnstileAdminDiagnosticDoesNotTouchLoginFailuresBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.app.cfg.Challenge = testChallenge{}
			f.app.cfg.TurnstileSiteKey = "synthetic-site-key"
			before := len(f.app.loginFailures.entries)
			webStatus(t, f.call(3, "POST", "/api/admin/services/turnstile-test", map[string]string{"turnstileToken": "valid"}), 200)
			webStatus(t, f.call(3, "POST", "/api/admin/services/turnstile-test", map[string]string{"turnstileToken": "invalid"}), 403)
			if len(f.app.loginFailures.entries) != before {
				t.Fatal("Turnstile diagnostic changed login failure state")
			}
		})
	}
}

func TestInvitationPluginEmailGateBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "open"}), 204)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/invitations", map[string]bool{"enabled": true, "required": true}), 200)
			body := registrationBody("required@example.test")
			webStatus(t, f.call(0, "POST", "/api/auth/register", body), 400)
			clearMailLimits(f)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/invitations", map[string]bool{"enabled": true, "required": false}), 200)
			webStatus(t, f.call(0, "POST", "/api/auth/register", body), 201)
			token := mailbox.token()
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": body["email"], "password": body["password"]}), 403)
			webStatus(t, f.call(0, "POST", "/api/auth/verify", map[string]string{"token": token}), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": body["email"], "password": body["password"]}), 200)
			// Disable then enable legacy invitation mode: it must restore the required gate.
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/invitations", map[string]bool{"enabled": false, "required": false}), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "invitation"}), 204)
			var options invitationOptions
			response := f.call(3, "GET", "/api/admin/plugins/invitations", nil)
			webStatus(t, response, 200)
			if json.Unmarshal(response.Body.Bytes(), &options) != nil || !options.Enabled || !options.Required {
				t.Fatal("legacy mode lost required plugin", response.Body)
			}
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/invitations", map[string]bool{"enabled": false, "required": false}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			response = f.call(3, "GET", "/api/admin/plugins/invitations", nil)
			if json.Unmarshal(response.Body.Bytes(), &options) != nil || !options.Enabled || !options.Required {
				t.Fatal("unaudited plugin update survived", response.Body)
			}
		})
	}
}

func TestImageAndNoticeBoundaries(t *testing.T) {
	for _, raw := range []string{"https://example.test/image.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,bm90LWFuLWltYWdl"} {
		if _, err := normalizedImage(raw); err == nil {
			t.Fatal("invalid image accepted", raw)
		}
	}
	raw := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG(t, 31, 45))
	normalized, err := normalizedImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(normalized, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 64 || cfg.Height != 64 {
		t.Fatal("image not normalized", cfg, err)
	}
	logo, err := normalizedLogoImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG(t, 160, 80)))
	if err != nil {
		t.Fatal(err)
	}
	logoData, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(logo, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	logoCfg, err := png.DecodeConfig(bytes.NewReader(logoData))
	if err != nil || logoCfg.Width != 160 || logoCfg.Height != 160 {
		t.Fatalf("logo not normalized to a larger square: %dx%d %v", logoCfg.Width, logoCfg.Height, err)
	}
	literal := renderNotice("<b>literal</b>\nsecond", "plain")
	if strings.Contains(literal, "<b>") || !strings.Contains(literal, "&lt;b&gt;") || !strings.Contains(literal, "<br>") {
		t.Fatal(literal)
	}
}
