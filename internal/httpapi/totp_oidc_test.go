package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"wishingskin/internal/credential"
	"wishingskin/internal/securebox"
)

// Secret-bearing responses must never be included in failure output.
func totpStatusOnly(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d want %d", w.Code, want)
	}
}

func TestOIDCTOTPBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			p := newOIDCProviderFixture(t)
			hash, err := credential.Hash("Local123!")
			if err != nil {
				t.Fatal(err)
			}
			f.exec("UPDATE users SET password_hash=?", hash)
			webStatus(t, f.call(3, "PATCH", "/api/admin/oidc/providers", map[string]any{"providers": []map[string]any{{"id": "", "enabled": false}, {"id": "secondary", "enabled": true, "name": "OIDC", "issuer": p.server.URL, "clientId": "test-client", "clientSecret": "synthetic-client-secret"}}}), 200)
			u, browser := startOIDCTest(t, f, p, 1, "secondary")
			w := callbackOIDCTest(f, u, browser, 1)
			if w.Code != 303 {
				t.Fatal("link failed")
			}
			secret, err := newTOTPSecret()
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := securebox.Seal(f.app.cfg.SettingsKey, secret, totpPurpose)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			f.app.totpNow = func() time.Time { return now }
			f.exec("INSERT INTO user_totp(user_id,secret_encrypted,enabled,created_at,last_used_step) VALUES(?,?,TRUE,?,-1)", 1, sealed, now.Unix())
			challenge := func() *http.Cookie {
				t.Helper()
				u, browser := startOIDCTest(t, f, p, 0, "secondary")
				w := callbackOIDCTest(f, u, browser, 0)
				if w.Header().Get("Location") != "http://example.test/login?totp=1" {
					t.Fatal("missing TOTP handoff")
				}
				var cookie *http.Cookie
				for _, c := range w.Result().Cookies() {
					if c.Name == "ws_session" {
						t.Fatal("session issued before TOTP")
					}
					if c.Name == "ws_totp" {
						cookie = c
					}
				}
				if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
					t.Fatal("missing protected challenge cookie")
				}
				return cookie
			}
			complete := func(c *http.Cookie, code, origin string) *httptest.ResponseRecorder {
				body, _ := json.Marshal(map[string]string{"code": code})
				r := httptest.NewRequest("POST", "http://example.test/api/auth/totp", bytes.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", origin)
				if c != nil {
					r.AddCookie(c)
				}
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				return w
			}
			c := challenge()
			code := totpCode(secret, now.Unix()/30)
			totpStatusOnly(t, complete(nil, code, "http://example.test"), 401)
			totpStatusOnly(t, complete(c, code, "https://other.test"), 403)
			totpStatusOnly(t, complete(c, "invalid", "http://example.test"), 401)
			var wg sync.WaitGroup
			results := make(chan *httptest.ResponseRecorder, 2)
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); results <- complete(c, code, "http://example.test") }()
			}
			wg.Wait()
			close(results)
			statuses := map[int]int{}
			for w := range results {
				statuses[w.Code]++
			}
			if statuses[200] != 1 || statuses[401] != 1 {
				t.Fatalf("challenge consumed more than once: %v", statuses)
			}
			totpStatusOnly(t, complete(c, code, "http://example.test"), 401)
			c = challenge()
			f.exec("UPDATE oidc_flows SET expires_at=0 WHERE flow_hash=?", tokenHash(c.Value))
			totpStatusOnly(t, complete(c, code, "http://example.test"), 401)
			c = challenge()
			f.exec("UPDATE users SET password_hash='changed' WHERE id=1")
			totpStatusOnly(t, complete(c, code, "http://example.test"), 401)
			f.exec("UPDATE users SET password_hash=? WHERE id=1", hash)
			c = challenge()
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oidc", map[string]bool{"enabled": false}), 200)
			totpStatusOnly(t, complete(c, code, "http://example.test"), 401)
		})
	}
}
