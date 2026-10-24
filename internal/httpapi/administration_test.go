package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdministrationGuardsAndListingBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			for _, path := range []string{"/api/admin/users", "/api/admin/events"} {
				webStatus(t, f.call(0, "GET", path, nil), 401)
				webStatus(t, f.call(1, "GET", path, nil), 403)
				webStatus(t, f.call(3, "GET", path+"?page=0", nil), 400)
				webStatus(t, f.call(3, "GET", path+"?pageSize=61", nil), 400)
			}
			webStatus(t, f.call(1, "PATCH", "/api/admin/users/2", map[string]bool{"banned": true}), 403)
			for _, origin := range []string{"http://evil.test", "http://example.test"} {
				r := httptest.NewRequest("PATCH", "http://example.test/api/admin/users/1", strings.NewReader(`{"banned":true}`))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", origin)
				r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 3)})
				if origin != "http://example.test" {
					r.Header.Set("X-CSRF-Token", csrf(fmt.Sprintf("%064d", 3)))
				}
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				webStatus(t, w, 403)
			}
			f.exec("UPDATE users SET nickname='100%_literal' WHERE id=2")
			var list struct {
				Users                 []adminUser
				Total, Page, PageSize int
			}
			w := f.call(3, "GET", "/api/admin/users?pageSize=1&page=2", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Total != 3 || len(list.Users) != 1 || list.Users[0].ID != "2" || list.Page != 2 {
				t.Fatal("pagination", list, err)
			}
			w = f.call(3, "GET", "/api/admin/users?q=%25_", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Total != 1 || list.Users[0].ID != "2" {
				t.Fatal("literal search", list, err)
			}
			w = f.call(3, "GET", "/api/admin/users?q=EXAMPLE.TEST", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Total != 3 {
				t.Fatal("email search", err)
			}
			for _, secret := range []string{"password", "token", "salt", "unused"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("list leaked credentials")
				}
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/99", map[string]bool{"banned": true}), 404)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/invalid", map[string]bool{"banned": true}), 400)
		})
	}
}

func TestAdministrationRevocationAndAuditBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			c := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "AdminTarget"}))
			for _, id := range []int{1, 2} {
				f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?,?,?,?,?)", fmt.Sprintf("%064d", id), "client", id, c.UUID, time.Now().Add(time.Hour).Unix())
				f.exec("INSERT INTO account_tokens(token_hash,user_id,purpose,expires_at) VALUES(?,?,?,?)", fmt.Sprintf("%064d", id), id, "reset", time.Now().Add(time.Hour).Unix())
			}
			f.exec("INSERT INTO ygg_join_sessions(token_hash,server_id,profile_uuid,client_ip,expires_at) VALUES(?,?,?,?,?)", fmt.Sprintf("%064d", 1), "pending", c.UUID, "127.0.0.1", time.Now().Add(time.Minute).Unix())
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"banned": true}), 204)
			for _, table := range []string{"web_sessions", "game_tokens", "account_tokens"} {
				var count int
				if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE user_id=1").Scan(&count); err != nil || count != 0 {
					t.Fatal("target credentials survived", table, err)
				}
				if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE user_id=2").Scan(&count); err != nil || count != 1 {
					t.Fatal("other credentials changed", table, err)
				}
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM ygg_join_sessions").Scan(&count); err != nil || count != 0 {
				t.Fatal("join survived", err)
			}
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
			// Banning preserves content; unbanning never resurrects revoked credentials.
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM characters WHERE user_id=1").Scan(&count); err != nil || count != 1 {
				t.Fatal("content removed", err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"banned": false}), 204)
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"isAdmin": true}), 204)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"isAdmin": false}), 204)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"isAdmin": false}), 204)
			var events struct {
				Events []adminEvent
				Total  int
			}
			w := f.call(3, "GET", "/api/admin/events?pageSize=2", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil || events.Total != 4 || len(events.Events) != 2 {
				t.Fatal("audit pagination or no-op audit", events, err)
			}
			for _, e := range events.Events {
				if e.ActorID != "3" || e.TargetID != "1" || e.TargetType != "user" || e.CreatedAt <= 0 {
					t.Fatal("audit identity", e)
				}
			}
		})
	}
}

func TestAdministrationLastAvailableAndConcurrentBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			// A promoted pending registration cannot keep the site administrable.
			f.exec("UPDATE users SET permission=2 WHERE id=1")
			f.exec("INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(1,FALSE,TRUE)")
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/3", map[string]bool{"isAdmin": false}), 409)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/3", map[string]bool{"banned": true}), 409)
			f.exec("UPDATE account_states SET email_verified=TRUE WHERE user_id=1")
			var wg sync.WaitGroup
			results := make(chan int, 2)
			for _, id := range []int{1, 3} {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					results <- f.call(id, "PATCH", fmt.Sprintf("/api/admin/users/%d", id), map[string]bool{"isAdmin": false}).Code
				}(id)
			}
			wg.Wait()
			close(results)
			success := 0
			for status := range results {
				if status == 204 {
					success++
				} else if status != 409 {
					t.Fatal("unexpected concurrent status", status)
				}
			}
			if success != 1 {
				t.Fatal("last administrator race", success)
			}
			var remaining int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM users WHERE permission>=2 AND banned=FALSE").Scan(&remaining); err != nil || remaining != 1 {
				t.Fatal("last administrator lost", err)
			}
		})
	}
}

func TestAdministrationRollbackAndActorRecheckBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"banned": true, "isAdmin": true}), 503)
			webStatus(t, f.call(3, "PATCH", "/api/admin/settings", map[string]int{"characterLimit": 9}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 200)
			var banned bool
			var permission int
			if err := f.db.DB.QueryRow("SELECT banned,permission FROM users WHERE id=1").Scan(&banned, &permission); err != nil || banned || permission != 0 {
				t.Fatal("unaudited change committed", err)
			}
			limit, err := characterLimit(context.Background(), f.db.DB, f.db.Bind)
			if err != nil || limit != 1 {
				t.Fatal("unaudited setting committed", limit, err)
			}
			// Simulate an actor authorized before another request removed their role.
			f.exec("UPDATE users SET permission=0 WHERE id=3")
			w := httptest.NewRecorder()
			r := httptest.NewRequest("PATCH", "http://example.test/api/admin/users/1", nil)
			if tx, ok := f.app.beginAdminWrite(w, r, User{ID: 3, IsAdmin: true}); ok {
				tx.Rollback()
				t.Fatal("stale actor authorized")
			}
			webStatus(t, w, 403)
			f.exec("UPDATE users SET permission=2 WHERE id=3")
			webStatus(t, f.call(3, "PATCH", "/api/admin/settings", map[string]int{"characterLimit": 9}), 200)
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE action='settings.character_limit' AND actor_id=3 AND target_id='character_limit'").Scan(&count); err != nil || count != 1 {
				t.Fatal("setting audit missing", err)
			}
		})
	}
}
