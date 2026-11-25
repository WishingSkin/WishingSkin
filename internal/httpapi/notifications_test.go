package httpapi

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestNotificationInboxBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			const inbox = "/api/notifications"
			webStatus(t, f.call(0, "GET", inbox, nil), 401)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"score": 15, "scoreReason": "感谢参与", "characterLimit": 12, "emailVerified": true}), 204)
			skin := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Test skin"}))
			webStatus(t, f.call(3, "PATCH", "/api/admin/review/"+skin.ID, map[string]string{"state": "rejected", "reason": "测试拒绝理由"}), 204)
			webStatus(t, f.call(3, "POST", "/api/admin/textures/"+skin.FileHash+"/disable", nil), 204)
			webStatus(t, f.call(3, "POST", "/api/admin/textures/"+skin.FileHash+"/disable", nil), 204)
			f.exec("UPDATE user_notifications SET created_at=42 WHERE user_id=1")
			type inboxPage struct {
				Notifications []struct{ ID, Body, Link string }
				UnreadCount   int
				NextCursor    string
			}
			list := func(path string) inboxPage {
				res := f.call(1, "GET", path, nil)
				webStatus(t, res, 200)
				var page inboxPage
				if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				return page
			}
			first := list(inbox + "?limit=2")
			if first.UnreadCount != 5 || len(first.Notifications) != 2 || !strings.HasPrefix(first.NextCursor, "42:") {
				t.Fatalf("first page: %+v", first)
			}
			second := list(inbox + "?limit=3&cursor=" + url.QueryEscape(first.NextCursor))
			if len(second.Notifications) != 3 || second.NextCursor != "" || first.Notifications[1].ID <= second.Notifications[0].ID {
				t.Fatalf("second page: %+v", second)
			}
			id := first.Notifications[0].ID
			webStatus(t, f.call(2, "POST", inbox+"/"+id+"/read", nil), 404)
			webStatus(t, f.call(1, "POST", inbox+"/"+id+"/read", nil), 200)
			if page := list(inbox + "?unreadOnly=true"); page.UnreadCount != 4 || len(page.Notifications) != 4 {
				t.Fatalf("unread page: %+v", page)
			}
			webStatus(t, f.call(1, "POST", inbox+"/read-all", nil), 200)
			if page := list(inbox + "?unreadOnly=true"); page.UnreadCount != 0 || len(page.Notifications) != 0 {
				t.Fatalf("read all: %+v", page)
			}
			reported := uploadedRecord(t, f.upload(1, testPNG(t, 128, 128), map[string]string{"name": "Reported skin", "isPublic": "true"}))
			created := f.call(2, "POST", "/api/reports", map[string]string{"recordId": reported.ID, "reason": "测试举报原因"})
			webStatus(t, created, 201)
			var report reportItem
			if err := json.Unmarshal(created.Body.Bytes(), &report); err != nil || report.ID == "" {
				t.Fatalf("report response: %s", created.Body)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/reports/"+report.ID, map[string]string{"action": "disable", "reply": "已调查"}), 204)
			reporter := f.call(2, "GET", inbox, nil)
			webStatus(t, reporter, 200)
			if !strings.Contains(reporter.Body.String(), "已调查") || !strings.Contains(reporter.Body.String(), "/user/account/reports") {
				t.Fatalf("reporter notification: %s", reporter.Body)
			}
			if page := list(inbox + "?unreadOnly=true"); page.UnreadCount != 1 || page.Notifications[0].Link != "/user/resources" {
				t.Fatalf("owner notification: %+v", page)
			}
			f.app.WaitBackground()
			address, messages := accountSMTPFixture(t)
			config := map[string]any{"smtpEnabled": true, "notificationEmailEnabled": true, "address": address, "from": "site@example.test", "tlsMode": "local"}
			webStatus(t, f.call(3, "PATCH", "/api/admin/services", config), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/2", map[string]int{"score": 12}), 204)
			f.app.WaitBackground()
			select {
			case <-messages:
				t.Fatal("notification email was sent to an unverified address")
			default:
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/2", map[string]bool{"emailVerified": true}), 204)
			f.app.WaitBackground()
			text, html := accountMailParts(t, messages)
			if !strings.Contains(text, "管理员已手动验证你的邮箱") || !strings.Contains(html, "http://example.test/user/account/profile") {
				t.Fatal("notification email missing account update or link")
			}
		})
	}
}
