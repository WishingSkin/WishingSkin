package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/WishingSkin/WishingSkin/internal/mailer"
)

func TestMailTemplateSettingsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			for _, path := range []string{"/api/admin/email-template", "/api/admin/email-template/preview"} {
				method := "GET"
				if strings.HasSuffix(path, "/preview") {
					method = "POST"
				}
				webStatus(t, f.call(0, method, path, nil), 401)
				webStatus(t, f.call(1, method, path, nil), 403)
			}
			value := mailer.DefaultTemplate("皮肤站")
			value.AccentColor = "#8844cc"
			value.Verify.Body = "自定义验证说明 <script>alert(1)</script>"
			webStatus(t, f.call(1, "PATCH", "/api/admin/email-template", value), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/email-template", value), 200)
			w := f.call(3, "GET", "/api/admin/email-template", nil)
			var stored mailer.Template
			if json.Unmarshal(w.Body.Bytes(), &stored) != nil || stored != value {
				t.Fatal("email customization did not persist")
			}
			for _, kind := range []string{"verify", "reset", "test"} {
				w = f.call(3, "POST", "/api/admin/email-template/preview", map[string]any{"purpose": kind, "template": value})
				webStatus(t, w, 200)
				var content mailer.Content
				if json.Unmarshal(w.Body.Bytes(), &content) != nil || !strings.Contains(content.HTML, "#8844cc") || strings.Contains(content.HTML, "<script>") || strings.Contains(content.HTML, "href=") {
					t.Fatal("unsafe or missing preview")
				}
				webStatus(t, f.call(3, "POST", "/api/admin/services/test", map[string]string{"recipient": "qa@example.test", "purpose": kind}), 200)
			}
			mailbox.mu.Lock()
			for i, purpose := range []string{"preview-verify", "preview-reset", "test"} {
				if mailbox.messages[i].Purpose != purpose || mailbox.messages[i].Link != "" {
					t.Fatal("sample used account link")
				}
			}
			mailbox.mu.Unlock()
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM account_tokens").Scan(&count); err != nil || count != 0 {
				t.Fatal("sample created account tokens", err)
			}
			invalid := value
			invalid.AccentColor = "red; background:evil"
			webStatus(t, f.call(3, "PATCH", "/api/admin/email-template", invalid), 400)
			webStatus(t, f.call(3, "POST", "/api/admin/services/test", map[string]string{"purpose": "invalid"}), 400)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			value.BrandName = "unaudited"
			webStatus(t, f.call(3, "PATCH", "/api/admin/email-template", value), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			w = f.call(3, "GET", "/api/admin/email-template", nil)
			if strings.Contains(w.Body.String(), "unaudited") {
				t.Fatal("unaudited template persisted")
			}
		})
	}
}
