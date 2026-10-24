package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wishingskin/internal/testutil"
)

func TestSessionQueryFailureStatus(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, fault := range []struct{ name, query string }{
				{"sessions", "ALTER TABLE web_sessions RENAME COLUMN token_hash TO unavailable_token_hash"},
				{"users", "ALTER TABLE users RENAME COLUMN email TO unavailable_email"},
			} {
				t.Run(fault.name, func(t *testing.T) {
					db := testutil.Database(t, dialect)
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					if _, err := db.DB.ExecContext(ctx, db.Bind("INSERT INTO migration_state (id,source_fingerprint,status,report) VALUES (?,?,?,?)"), 1, strings.Repeat("0", 64), "completed", "{}"); err != nil {
						t.Fatal(err)
					}
					if _, err := db.DB.ExecContext(ctx, db.Bind("INSERT INTO users (id,email,email_key,nickname,password_hash,password_method,password_salt) VALUES (?,?,?,?,?,?,?)"), 1, "session@example.test", "session@example.test", "Session", "unused", "unused", ""); err != nil {
						t.Fatal(err)
					}
					token := strings.Repeat("a", 64)
					if _, err := db.DB.ExecContext(ctx, db.Bind("INSERT INTO web_sessions (token_hash,user_id,expires_at) VALUES (?,?,?)"), tokenHash(token), 1, time.Now().Add(time.Hour).Unix()); err != nil {
						t.Fatal(err)
					}
					app, err := New(db, Config{PublicURL: "http://example.test"})
					if err != nil {
						t.Fatal(err)
					}
					request := func(method, path, cookie string, want int) {
						t.Helper()
						r := httptest.NewRequest(method, "http://example.test"+path, nil).WithContext(ctx)
						if cookie != "" {
							r.AddCookie(&http.Cookie{Name: "ws_session", Value: cookie})
							r.Header.Set("X-CSRF-Token", csrf(cookie))
						}
						w := httptest.NewRecorder()
						app.ServeHTTP(w, r)
						if w.Code != want {
							t.Fatalf("%s %s: got %d, want %d; body %s", method, path, w.Code, want, w.Body.String())
						}
						if want == http.StatusServiceUnavailable && !strings.Contains(w.Body.String(), "暂时无法验证会话") {
							t.Fatalf("503 did not come from session query handling: %s", w.Body.String())
						}
					}
					endpoints := []struct{ method, path string }{
						{http.MethodGet, "/api/auth/me"},
						{http.MethodPost, "/api/auth/logout"},
						{http.MethodPost, "/api/auth/password"},
						{http.MethodGet, "/api/characters"},
						{http.MethodGet, "/api/wardrobe"},
						{http.MethodPut, "/api/wardrobe/" + strings.Repeat("a", 32)},
						{http.MethodPatch, "/api/wardrobe/" + strings.Repeat("a", 32)},
						{http.MethodDelete, "/api/wardrobe/" + strings.Repeat("a", 32)},
					}
					for _, endpoint := range endpoints {
						request(endpoint.method, endpoint.path, "", http.StatusUnauthorized)
						request(endpoint.method, endpoint.path, strings.Repeat("b", 64), http.StatusUnauthorized)
					}
					request(http.MethodGet, "/api/auth/me", token, http.StatusOK)
					request(http.MethodGet, "/api/characters", token, http.StatusOK)
					if _, err := db.DB.ExecContext(ctx, fault.query); err != nil {
						t.Fatal("inject isolated session query failure:", err)
					}
					// Prove the migration gate still succeeds instead of masking the fault.
					request(http.MethodGet, "/api/site", "", http.StatusOK)
					for _, endpoint := range endpoints {
						request(endpoint.method, endpoint.path, token, http.StatusServiceUnavailable)
						request(endpoint.method, endpoint.path, "", http.StatusUnauthorized)
					}
				})
			}
		})
	}
}
