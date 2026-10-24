package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNoticeHistoryBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			type response struct {
				Notices []struct {
					ID, HTML  string
					CreatedAt int64
				}
				CurrentID string
			}
			read := func() response {
				t.Helper()
				w := f.call(0, "GET", "/api/notices", nil)
				webStatus(t, w, 200)
				var result response
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			if result := read(); len(result.Notices) != 0 || result.CurrentID != "" {
				t.Fatal(result)
			}
			f.exec("INSERT INTO settings(setting_key,value) VALUES('site_notice','迁移公告')")
			legacy := read()
			if len(legacy.Notices) != 1 || legacy.CurrentID == "" || legacy.Notices[0].CreatedAt != 0 {
				t.Fatal(legacy)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", siteBranding{Name: "Name only", Notice: "迁移公告", NoticeFormat: "plain"}), 200)
			if result := read(); len(result.Notices) != 1 || result.CurrentID != legacy.CurrentID {
				t.Fatal("legacy plain format published a duplicate", result)
			}
			body := siteBranding{Name: "Test", Notice: "**新公告** <script>alert(1)</script>", NoticeFormat: "markdown"}
			webStatus(t, f.call(1, "PATCH", "/api/admin/branding", body), 403)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			if result := read(); len(result.Notices) != 1 || result.CurrentID != legacy.CurrentID {
				t.Fatal("history escaped rollback", result)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 200)
			first := read()
			if len(first.Notices) != 2 || first.CurrentID == legacy.CurrentID || !strings.Contains(first.Notices[0].HTML, "<strong>新公告</strong>") || strings.Contains(first.Notices[0].HTML, "script") || first.Notices[1].ID != legacy.CurrentID {
				t.Fatal(first)
			}
			body.Name = "Rename only"
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 200)
			if result := read(); len(result.Notices) != 2 || result.CurrentID != first.CurrentID {
				t.Fatal("duplicate history", result)
			}
			body.Notice = ""
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 200)
			if result := read(); len(result.Notices) != 2 || result.CurrentID != "" {
				t.Fatal("clear removed history", result)
			}
			body.Notice = "迁移公告"
			body.NoticeFormat = ""
			webStatus(t, f.call(3, "PATCH", "/api/admin/branding", body), 200)
			if result := read(); len(result.Notices) != 3 || result.CurrentID == legacy.CurrentID {
				t.Fatal("republish did not create a new notice", result)
			}
		})
	}
}
