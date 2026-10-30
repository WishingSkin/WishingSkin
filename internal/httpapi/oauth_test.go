package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOAuthPKCEBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			webStatus(t, f.call(1, "POST", "/api/admin/oauth/clients", oauthClient{}), 403)
			w := f.call(3, "POST", "/api/admin/oauth/clients", oauthClient{Name: "Example", Redirects: []string{"https://app.example/callback"}, Public: true, Enabled: true})
			webStatus(t, w, 200)
			var created struct{ Client oauthClient }
			if json.Unmarshal(w.Body.Bytes(), &created) != nil {
				t.Fatal("invalid client")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oauth2", map[string]bool{"enabled": true}), 200)
			verifier := strings.Repeat("a", 64)
			hash := sha256.Sum256([]byte(verifier))
			challenge := base64.RawURLEncoding.EncodeToString(hash[:])
			q := url.Values{"client_id": {created.Client.ID}, "redirect_uri": {"https://app.example/callback"}, "response_type": {"code"}, "scope": {"profile"}, "code_challenge_method": {"S256"}, "code_challenge": {challenge}, "state": {"state-check"}}
			r := httptest.NewRequest("GET", "http://example.test/oauth/authorize?"+q.Encode(), nil)
			r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 1)})
			r.AddCookie(&http.Cookie{Name: "ws_locale", Value: "en"})
			consent := httptest.NewRecorder()
			f.app.ServeHTTP(consent, r)
			if consent.Code != 200 || !strings.Contains(consent.Body.String(), "Authorize Example") || !strings.Contains(consent.Body.String(), "lang=\"en\"") {
				t.Fatal("English consent did not render")
			}
			webStatus(t, f.call(1, "GET", "/oauth/authorize?"+q.Encode()+"&redirect_uri=https://evil.example", nil), 400) // Reject ambiguous repeated parameters.
			submit := func(path string, form url.Values, account int) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "http://example.test"+path, strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Origin", "http://example.test")
				if account > 0 {
					r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", account)})
				}
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				return w
			}
			webStatus(t, submit("/oauth/authorize?"+q.Encode(), url.Values{"decision": {"allow"}}, 1), 403)
			w = submit("/oauth/authorize?"+q.Encode(), url.Values{"decision": {"allow"}, "csrf": {csrf(fmt.Sprintf("%064d", 1))}}, 1)
			webStatus(t, w, 303)
			location, _ := url.Parse(w.Header().Get("Location"))
			code := location.Query().Get("code")
			if code == "" || location.Query().Get("state") != "state-check" {
				t.Fatal("missing code/state")
			}
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {created.Client.ID}, "redirect_uri": {"https://app.example/callback"}, "code": {code}, "code_verifier": {strings.Repeat("b", 64)}}
			webStatus(t, submit("/oauth/token", form, 0), 400)
			form.Set("code_verifier", verifier)
			var wg sync.WaitGroup
			statuses := make(chan int, 2)
			tokens := make(chan string, 2)
			for range 2 {
				wg.Go(func() {
					w := submit("/oauth/token", form, 0)
					statuses <- w.Code
					if w.Code == 200 {
						var d map[string]any
						json.Unmarshal(w.Body.Bytes(), &d)
						tokens <- d["access_token"].(string)
					}
				})
			}
			wg.Wait()
			close(statuses)
			counts := map[int]int{}
			for status := range statuses {
				counts[status]++
			}
			if counts[200] != 1 || counts[400] != 1 {
				t.Fatalf("code reused: %v", counts)
			}
			access := <-tokens
			info := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "http://example.test/oauth/userinfo", nil)
				r.Header.Set("Authorization", "Bearer "+access)
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				return w
			}
			w = info()
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "email") {
				t.Fatal("scope leak")
			}
			webStatus(t, f.call(1, "DELETE", "/api/account/oauth/"+created.Client.ID, nil), 204)
			webStatus(t, info(), 401)
			q.Set("redirect_uri", "https://evil.example")
			webStatus(t, f.call(1, "GET", "/oauth/authorize?"+q.Encode(), nil), 400)
		})
	}
}

func TestOAuthConfidentialAndRevocationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			w := f.call(3, "POST", "/api/admin/oauth/clients", oauthClient{Name: "Confidential", Redirects: []string{"https://app.example/callback"}, Enabled: true})
			totpStatusOnly(t, w, 200)
			var created struct {
				Client       oauthClient
				ClientSecret string
			}
			if json.Unmarshal(w.Body.Bytes(), &created) != nil || created.ClientSecret == "" {
				t.Fatal("missing client secret")
			}
			if strings.Contains(f.call(3, "GET", "/api/admin/oauth/clients", nil).Body.String(), created.ClientSecret) {
				t.Fatal("secret leaked")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oauth2", map[string]bool{"enabled": true}), 200)
			verifier := strings.Repeat("v", 64)
			hash := sha256.Sum256([]byte(verifier))
			q := url.Values{"client_id": {created.Client.ID}, "redirect_uri": {created.Client.Redirects[0]}, "response_type": {"code"}, "scope": {"profile email"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}}
			submit := func(path string, form url.Values, clientSecret string, account bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "http://example.test"+path, strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Origin", "http://example.test")
				if account {
					r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 1)})
				}
				if clientSecret != "" {
					r.SetBasicAuth(created.Client.ID, clientSecret)
				}
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				return w
			}
			code := func() string {
				t.Helper()
				w := submit("/oauth/authorize?"+q.Encode(), url.Values{"decision": {"allow"}, "csrf": {csrf(fmt.Sprintf("%064d", 1))}}, "", true)
				totpStatusOnly(t, w, 303)
				u, _ := url.Parse(w.Header().Get("Location"))
				if u.Query().Get("code") == "" {
					t.Fatal("missing code")
				}
				return u.Query().Get("code")
			}
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {created.Client.ID}, "redirect_uri": {created.Client.Redirects[0]}, "code_verifier": {verifier}, "code": {code()}}
			totpStatusOnly(t, submit("/oauth/token", form, "", false), 401)
			totpStatusOnly(t, submit("/oauth/token", form, "wrong", false), 401)
			f.exec("UPDATE oauth_codes SET expires_at=?", time.Now().Add(-time.Minute).Unix())
			totpStatusOnly(t, submit("/oauth/token", form, created.ClientSecret, false), 400)
			issue := func() string {
				t.Helper()
				form.Set("code", code())
				w := submit("/oauth/token", form, created.ClientSecret, false)
				totpStatusOnly(t, w, 200)
				var d struct {
					Access string `json:"access_token"`
				}
				if json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Access == "" {
					t.Fatal("missing access token")
				}
				return d.Access
			}
			info := func(access string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "http://example.test/oauth/userinfo", nil)
				r.Header.Set("Authorization", "Bearer "+access)
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				return w
			}
			access := issue()
			w = info(access)
			totpStatusOnly(t, w, 200)
			if !strings.Contains(w.Body.String(), "email_verified") {
				t.Fatal("email scope missing")
			}
			f.exec("UPDATE oauth_tokens SET expires_at=0")
			totpStatusOnly(t, info(access), 401)
			access = issue()
			f.exec("UPDATE users SET password_hash='changed' WHERE id=1")
			totpStatusOnly(t, info(access), 401)
			access = issue()
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oauth2", map[string]bool{"enabled": false}), 200)
			totpStatusOnly(t, info(access), 401)
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM oauth_tokens").Scan(&count); err != nil || count != 0 {
				t.Fatal("plugin disable retained tokens")
			}
		})
	}
}
