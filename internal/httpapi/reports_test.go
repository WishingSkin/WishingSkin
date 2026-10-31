package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func submittedReport(t *testing.T, f *managementApp, user int, record, reason string) string {
	t.Helper()
	w := f.call(user, "POST", "/api/reports", map[string]string{"recordId": record, "reason": reason})
	webStatus(t, w, 201)
	var result struct{ ID string }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.ID) != 32 {
		t.Fatal("report identity", w.Body, err)
	}
	return result.ID
}

func TestReportsAccessAndDuplicateBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Reported", "isPublic": "true"}))
			private := uploadedRecord(t, f.upload(1, testPNG(t, 64, 32), map[string]string{}))
			webStatus(t, f.call(0, "GET", "/api/reports", nil), 401)
			webStatus(t, f.call(0, "POST", "/api/reports", map[string]string{}), 401)
			webStatus(t, f.call(1, "GET", "/api/admin/reports", nil), 403)
			webStatus(t, f.call(2, "POST", "/api/reports", map[string]string{"recordId": private.ID, "reason": "private"}), 404)
			for _, reason := range []string{" ", strings.Repeat("字", 1001)} {
				webStatus(t, f.call(2, "POST", "/api/reports", map[string]string{"recordId": record.ID, "reason": reason}), 400)
			}
			r := httptest.NewRequest("POST", "http://example.test/api/reports", strings.NewReader(`{"recordId":"`+record.ID+`","reason":"csrf"}`))
			r.Header.Set("Origin", "http://example.test")
			r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 2)})
			w := httptest.NewRecorder()
			f.app.ServeHTTP(w, r)
			webStatus(t, w, 403)
			var wg sync.WaitGroup
			statuses := make(chan int, 2)
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					statuses <- f.call(2, "POST", "/api/reports", map[string]string{"recordId": record.ID, "reason": "private reason"}).Code
				}()
			}
			wg.Wait()
			close(statuses)
			counts := map[int]int{}
			for code := range statuses {
				counts[code]++
			}
			if counts[201] != 1 || counts[409] != 1 {
				t.Fatal("duplicate reports", counts)
			}
			w = f.call(1, "GET", "/api/reports", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "private reason") || !strings.Contains(w.Body.String(), `"total":0`) {
				t.Fatal("other reporter leaked", w.Body)
			}
			w = f.call(2, "GET", "/api/reports", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "private reason") || strings.Contains(w.Body.String(), "@") || strings.Contains(w.Body.String(), "ownerId") {
				t.Fatal("own report projection", w.Body)
			}
			for _, search := range []struct {
				user  int
				path  string
				total string
			}{{2, "/api/reports", `"total":0`}, {3, "/api/admin/reports", `"total":1`}} {
				w = f.call(search.user, "GET", search.path+"?q="+record.FileHash, nil)
				webStatus(t, w, 200)
				if !strings.Contains(w.Body.String(), search.total) {
					t.Fatal("report hash search is reserved for administrators", w.Body)
				}
			}
			webStatus(t, f.call(3, "GET", "/api/admin/reports?status=invalid", nil), 400)
			webStatus(t, f.call(2, "GET", "/api/reports?page=0", nil), 400)
			// A retained collection grants access after its record becomes unlisted.
			webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+record.ID, nil), 204)
			webStatus(t, f.call(1, "PATCH", "/api/textures/"+record.ID, map[string]any{"name": "Changed", "description": "", "isPublic": false}), 204)
			f.exec("DELETE FROM reports")
			submittedReport(t, f, 2, record.ID, strings.Repeat("字", 1000))
			w = f.call(3, "GET", "/api/admin/reports?status=pending&pageSize=1", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "2@example.test") || !strings.Contains(w.Body.String(), `"total":1`) {
				t.Fatal("admin listing", w.Body)
			}
		})
	}
}

func TestReportResolutionAtomicAndConcurrentBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"isPublic": "true"}))
			id := submittedReport(t, f, 2, record.ID, "reason not for audit")
			second := submittedReport(t, f, 1, record.ID, "other reason")
			path := "/api/admin/reports/" + id
			webStatus(t, f.call(1, "PATCH", path, map[string]string{"action": "disable"}), 403)
			for _, body := range []map[string]string{{"action": "invalid"}, {"action": "disable", "reply": strings.Repeat("x", 1001)}} {
				webStatus(t, f.call(3, "PATCH", path, body), 400)
			}
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", path, map[string]string{"action": "disable", "reply": "reply"}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			webStatus(t, f.call(0, "GET", "/textures/"+record.FileHash, nil), 200)
			var status string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT status FROM reports WHERE report_id=?"), id).Scan(&status); err != nil || status != "pending" {
				t.Fatal("report rollback", status, err)
			}
			var wg sync.WaitGroup
			results := make(chan int, 2)
			for _, action := range []string{"disable", "dismiss"} {
				wg.Add(1)
				go func(action string) {
					defer wg.Done()
					results <- f.call(3, "PATCH", path, map[string]string{"action": action, "reply": "public reply"}).Code
				}(action)
			}
			wg.Wait()
			close(results)
			counts := map[int]int{}
			for code := range results {
				counts[code]++
			}
			if counts[204] != 1 || counts[409] != 1 {
				t.Fatal("resolution race", counts)
			}
			w := f.call(2, "GET", "/api/reports", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "public reply") || strings.Contains(w.Body.String(), `"status":"pending"`) {
				t.Fatal("report tracking", w.Body)
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE target_type='report'").Scan(&count); err != nil || count != 1 {
				t.Fatal("resolution audit", count, err)
			}
			w = f.call(3, "GET", "/api/admin/events", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "reason not for audit") || strings.Contains(w.Body.String(), "public reply") {
				t.Fatal("audit includes report text")
			}
			// A distinct report can still disable an image, including after the first was dismissed.
			webStatus(t, f.call(3, "PATCH", "/api/admin/reports/"+second, map[string]string{"action": "disable"}), 204)
			webStatus(t, f.call(0, "GET", "/textures/"+record.FileHash, nil), 404)
		})
	}
}

func TestReportBanGuardsAndCredentialsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"isPublic": "true"}))
			id := submittedReport(t, f, 2, record.ID, "ban")
			f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?, 'client',1,'',?)", strings.Repeat("c", 64), time.Now().Add(time.Hour).Unix())
			f.exec("INSERT INTO account_tokens(user_id,purpose,token_hash,expires_at) VALUES(1,'reset',?,?)", strings.Repeat("d", 64), time.Now().Add(time.Hour).Unix())
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/reports/"+id, map[string]string{"action": "ban"}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/reports/"+id, map[string]string{"action": "ban"}), 204)
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
			for _, table := range []string{"game_tokens", "account_tokens", "web_sessions"} {
				var count int
				if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE user_id=1").Scan(&count); err != nil || count != 0 {
					t.Fatal("credentials retained", table, count, err)
				}
			}
			// Never infer an owner for legacy records, and preserve the last usable administrator.
			adminRecord := uploadedRecord(t, f.upload(3, testPNG(t, 64, 32), map[string]string{"isPublic": "true"}))
			adminID := submittedReport(t, f, 2, adminRecord.ID, "admin")
			webStatus(t, f.call(3, "PATCH", "/api/admin/reports/"+adminID, map[string]string{"action": "ban"}), 409)
			f.exec("UPDATE texture_records SET owner_id=NULL WHERE record_id=?", adminRecord.ID)
			webStatus(t, f.call(3, "PATCH", "/api/admin/reports/"+adminID, map[string]string{"action": "ban"}), 409)
			webStatus(t, f.call(3, "PATCH", "/api/admin/reports/"+adminID, map[string]string{"action": "dismiss", "reply": "No uploader known"}), 204)
		})
	}
}
