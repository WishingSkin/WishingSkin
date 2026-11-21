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

	"github.com/WishingSkin/WishingSkin/internal/credential"
)

func seedPassword(t *testing.T, f *managementApp, id int, password string) {
	t.Helper()
	hash, err := credential.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE users SET password_hash=? WHERE id=?", hash, id)
}

func TestPasswordChangeBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			seedPassword(t, f, 1, "old") // Migrated weak passwords remain valid for verification.
			seedPassword(t, f, 2, "other")
			c := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "PasswordTest"}))
			f.exec("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", tokenHash(strings.Repeat("a", 64)), 1, time.Now().Add(time.Hour).Unix())
			for _, id := range []int{1, 2} {
				f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?,?,?,?,?)", fmt.Sprintf("%064d", id), "client", id, c.UUID, time.Now().Add(time.Hour).Unix())
			}
			f.exec("INSERT INTO ygg_join_sessions(token_hash,server_id,profile_uuid,client_ip,expires_at) VALUES(?,?,?,?,?)", fmt.Sprintf("%064d", 1), "pending", c.UUID, "127.0.0.1", time.Now().Add(time.Minute).Unix())
			body := map[string]string{"currentPassword": "old", "newPassword": "abc123!x"}
			webStatus(t, f.call(0, "POST", "/api/auth/password", body), 401)
			for _, invalid := range []string{"abc123!", "abcdef12", "abc123  ", strings.Repeat("a", 129) + "1!"} {
				webStatus(t, f.call(1, "POST", "/api/auth/password", map[string]string{"currentPassword": "old", "newPassword": invalid}), 400)
			}
			webStatus(t, f.call(1, "POST", "/api/auth/password", map[string]string{"currentPassword": "wrong", "newPassword": "abc123!x"}), 400)
			w := f.call(1, "POST", "/api/auth/password", body)
			webStatus(t, w, 204)
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].MaxAge != -1 || !cookies[0].HttpOnly {
				t.Fatal("session cookie not cleared")
			}
			for _, query := range []string{"SELECT COUNT(*) FROM web_sessions WHERE user_id=1", "SELECT COUNT(*) FROM game_tokens WHERE user_id=1", "SELECT COUNT(*) FROM ygg_join_sessions"} {
				var count int
				if err := f.db.DB.QueryRow(query).Scan(&count); err != nil || count != 0 {
					t.Fatal("revocation incomplete", count, err)
				}
			}
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
			webStatus(t, f.call(2, "GET", "/api/auth/me", nil), 200)
			var other int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM game_tokens WHERE user_id=2").Scan(&other); err != nil || other != 1 {
				t.Fatal("other account token revoked", other, err)
			}
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "old"}), 401)
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "abc123!x"}), 200)
			restarted, err := New(f.db, f.app.cfg)
			if err != nil {
				t.Fatal(err)
			}
			f.app = restarted
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
		})
	}
}

func TestPasswordChangeRollbackAndGuardsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			seedPassword(t, f, 1, "old")
			for _, guard := range []string{"csrf", "origin", "cross-site"} {
				r := httptest.NewRequest("POST", "http://example.test/api/auth/password", strings.NewReader(`{"currentPassword":"old","newPassword":"abc123!x"}`))
				token := fmt.Sprintf("%064d", 1)
				r.AddCookie(&http.Cookie{Name: "ws_session", Value: token})
				r.Header.Set("Origin", "http://example.test")
				r.Header.Set("X-CSRF-Token", csrf(token))
				switch guard {
				case "csrf":
					r.Header.Del("X-CSRF-Token")
				case "origin":
					r.Header.Set("Origin", "https://other.test")
				case "cross-site":
					r.Header.Set("Sec-Fetch-Site", "cross-site")
				}
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				webStatus(t, w, 403)
			}
			f.exec("ALTER TABLE game_tokens RENAME COLUMN user_id TO unavailable_user_id")
			webStatus(t, f.call(1, "POST", "/api/auth/password", map[string]string{"currentPassword": "old", "newPassword": "abc123!x"}), 503)
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 200)
			var hash string
			if err := f.db.DB.QueryRow("SELECT password_hash FROM users WHERE id=1").Scan(&hash); err != nil {
				t.Fatal(err)
			}
			if ok, err := credential.Verify("ARGON2ID", hash, "", "old"); err != nil || !ok {
				t.Fatal("failed change modified password", err)
			}
		})
	}
}

func TestConcurrentPasswordChangeBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			seedPassword(t, f, 1, "old")
			verified, err := f.app.authenticate(context.Background(), "1@example.test", "old")
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			responses := make(chan *httptest.ResponseRecorder, 2)
			for _, password := range []string{"abc123!x", "xyz789!x"} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					responses <- f.call(1, "POST", "/api/auth/password", map[string]string{"currentPassword": "old", "newPassword": password})
				}()
			}
			wg.Wait()
			close(responses)
			success := 0
			for w := range responses {
				if w.Code == 204 {
					success++
				} else if w.Code != 401 && w.Code != 400 {
					t.Fatalf("concurrent change: %d %s", w.Code, w.Body)
				}
			}
			if success != 1 {
				t.Fatal("concurrent changes succeeded", success)
			}
			tx, err := f.db.DB.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := f.app.lockAuthenticatedUser(context.Background(), tx, verified); err != errCredentials {
				t.Fatal("stale verified password can issue credentials", err)
			}
		})
	}
}

func TestPasswordChangeRateLimitBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			seedPassword(t, f, 1, "old")
			for range 10 {
				webStatus(t, f.call(1, "POST", "/api/auth/password", map[string]string{"currentPassword": "wrong", "newPassword": "abc123!x"}), 400)
			}
			w := f.call(1, "POST", "/api/auth/password", map[string]string{"currentPassword": "old", "newPassword": "abc123!x"})
			webStatus(t, w, 429)
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("missing retry advice")
			}
			var payload map[string]any
			if json.Unmarshal(w.Body.Bytes(), &payload) != nil {
				t.Fatal("invalid error response")
			}
		})
	}
}
