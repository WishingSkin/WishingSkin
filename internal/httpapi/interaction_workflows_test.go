package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminEmailVerificationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.exec("INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(1,FALSE,TRUE)")
			f.exec("INSERT INTO account_tokens(token_hash,user_id,purpose,expires_at) VALUES(?,?,?,?)", strings.Repeat("a", 64), 1, "verify", time.Now().Add(time.Hour).Unix())
			f.exec("INSERT INTO account_tokens(token_hash,user_id,purpose,expires_at) VALUES(?,?,?,?)", strings.Repeat("b", 64), 1, "reset", time.Now().Add(time.Hour).Unix())
			webStatus(t, f.call(0, "PATCH", "/api/admin/users/1", map[string]bool{"emailVerified": true}), 401)
			webStatus(t, f.call(2, "PATCH", "/api/admin/users/1", map[string]bool{"emailVerified": true}), 403)
			r := httptest.NewRequest("PATCH", "http://example.test/api/admin/users/1", strings.NewReader(`{"emailVerified":true}`))
			r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 3)})
			r.Header.Set("Origin", "http://example.test")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			f.app.ServeHTTP(w, r)
			webStatus(t, w, 403)
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"emailVerified": false}), 400)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"emailVerified": true}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
			for range 2 {
				webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"emailVerified": true}), 204)
			}
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 200)
			var verified, required bool
			if err := f.db.DB.QueryRow("SELECT email_verified,verification_required FROM account_states WHERE user_id=1").Scan(&verified, &required); err != nil || !verified || required {
				t.Fatal("verification state", err)
			}
			var tokens, events int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM account_tokens WHERE user_id=1 AND purpose='verify'").Scan(&tokens); err != nil || tokens != 0 {
				t.Fatal("verification links", err)
			}
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM account_tokens WHERE user_id=1 AND purpose='reset'").Scan(&tokens); err != nil || tokens != 1 {
				t.Fatal("password recovery changed", err)
			}
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE action='user.verify_email' AND target_id='1'").Scan(&events); err != nil || events != 1 {
				t.Fatal("idempotent audit", err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/2", map[string]bool{"emailVerified": true}), 204)
			webStatus(t, f.call(2, "GET", "/api/auth/me", nil), 200)
		})
	}
}

func TestTextureSelectionPaginationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			for _, name := range []string{"Alpha", "Beta", "Gamma"} {
				item := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": name, "isPublic": "true"}))
				webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+item.ID, nil), 204)
			}
			private := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Private"}))
			for _, c := range []struct {
				user  int
				path  string
				total int
			}{{1, "/api/textures", 4}, {2, "/api/wardrobe", 3}} {
				var seen []string
				for page := 1; page <= c.total; page++ {
					w := f.call(c.user, "GET", fmt.Sprintf("%s?page=%d&pageSize=1", c.path, page), nil)
					webStatus(t, w, 200)
					var d struct {
						Textures              []textureRecord
						Total, Page, PageSize int
					}
					if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || d.Total != c.total || d.Page != page || d.PageSize != 1 || len(d.Textures) != 1 {
						t.Fatal("page contract", err)
					}
					seen = append(seen, d.Textures[0].ID)
				}
				for i := range seen {
					for j := 0; j < i; j++ {
						if seen[i] == seen[j] {
							t.Fatal("repeated record across pages")
						}
					}
				}
				for _, bad := range []string{"?page=0", "?pageSize=61", "?page=abc"} {
					webStatus(t, f.call(c.user, "GET", c.path+bad, nil), 400)
				}
			}
			var library struct {
				Textures []libraryRecord
				Total    int
			}
			w := f.call(2, "GET", "/api/library?pageSize=1", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &library); err != nil || library.Total != 3 || len(library.Textures) != 1 || !library.Textures[0].Collected || library.Textures[0].Owned {
				t.Fatal("page collection membership", err)
			}
			w = f.call(0, "GET", "/api/library?pageSize=1", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &library); err != nil || library.Textures[0].Collected {
				t.Fatal("anonymous collection membership", err)
			}
			w = f.call(2, "GET", "/api/wardrobe?q=Beta&pageSize=1", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &library); err != nil || library.Total != 1 || library.Textures[0].Name != "Beta" {
				t.Fatal("search before pagination", err)
			}
			w = f.call(2, "GET", "/api/wardrobe?pageSize=60", nil)
			if strings.Contains(w.Body.String(), private.ID) {
				t.Fatal("private material leaked")
			}
		})
	}
}

func TestAdministratorVerificationRegistrationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.app.cfg.Challenge = testChallenge{}
			f.app.cfg.TurnstileSiteKey = "synthetic-site-key"
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "open"}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "open", "manualEmailVerification": true}), 204)
			w := f.call(0, "POST", "/api/auth/register", registrationBody("manual@example.test"))
			webStatus(t, w, 201)
			if !strings.Contains(w.Body.String(), "管理员") {
				t.Fatal("registration response must describe administrator verification")
			}
			login := map[string]string{"email": "manual@example.test", "password": "Register123!"}
			webStatus(t, f.call(0, "POST", "/api/auth/login", login), 403)
			var id int64
			if err := f.db.DB.QueryRow("SELECT id FROM users WHERE email_key='manual@example.test'").Scan(&id); err != nil {
				t.Fatal(err)
			}
			webStatus(t, f.call(3, "PATCH", fmt.Sprintf("/api/admin/users/%d", id), map[string]bool{"emailVerified": true}), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/login", login), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "open", "manualEmailVerification": false}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "closed", "manualEmailVerification": false}), 204)
		})
	}
}

func TestAdministratorVerificationSwitchRevokesEmailLinksBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "open"}), 204)
			body := registrationBody("review@example.test")
			webStatus(t, f.call(0, "POST", "/api/auth/register", body), 201)
			oldLink := mailbox.token()
			clearMailLimits(f)
			webStatus(t, mailRequest(f, body["email"], "reset"), 202)
			resetLink := mailbox.token()
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "open", "manualEmailVerification": true}), 204)
			clearMailLimits(f)
			webStatus(t, mailRequest(f, body["email"], "verify"), 202)
			if mailbox.token() != resetLink {
				t.Fatal("manual verification sent a verification email")
			}
			verification := map[string]string{"token": oldLink}
			webStatus(t, f.call(0, "POST", "/api/auth/verify", verification), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "open", "manualEmailVerification": false}), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/verify", verification), 400)
			webStatus(t, f.call(0, "POST", "/api/auth/reset", map[string]string{"token": resetLink, "newPassword": body["password"] + "1!"}), 204)
		})
	}
}
