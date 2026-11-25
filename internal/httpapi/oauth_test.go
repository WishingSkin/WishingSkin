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
			if consent.Header().Get("Referrer-Policy") != "same-origin" {
				t.Fatal("consent must preserve the origin for same-origin form submissions")
			}
			if !strings.Contains(consent.Header().Get("Content-Security-Policy"), "form-action 'self' https://app.example;") {
				t.Fatal("consent must allow the registered callback origin after form submission")
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

func TestOAuthReadOnlyScopesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			const callback = "https://app.example/callback"
			clientBody := map[string]any{"name": "Scoped", "redirectUris": []string{callback}, "public": true, "enabled": true}
			clientBody["allowedScopes"] = []string{}
			webStatus(t, f.call(3, "POST", "/api/admin/oauth/clients", clientBody), 400)
			clientBody["allowedScopes"] = []string{"points:read", " characters:read ", "textures:read", "minecraft:read", "offline_access", "points:read"}
			created := f.call(3, "POST", "/api/admin/oauth/clients", clientBody)
			webStatus(t, created, 200)
			var client struct{ Client struct{ ID string } }
			if err := json.Unmarshal(created.Body.Bytes(), &client); err != nil || client.Client.ID == "" {
				t.Fatal("missing client ID", err)
			}
			id := client.Client.ID
			delete(clientBody, "allowedScopes")
			webStatus(t, f.call(3, "PATCH", "/api/admin/oauth/clients/"+id, clientBody), 200)
			listing := f.call(3, "GET", "/api/admin/oauth/clients", nil)
			webStatus(t, listing, 200)
			if !strings.Contains(listing.Body.String(), `"availableScopes"`) || !strings.Contains(listing.Body.String(), `"allowedScopes"`) {
				t.Fatal("scope metadata missing")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oauth2", map[string]bool{"enabled": true}), 200)
			configurePoints(t, f, true, 7, 0, 0)
			webStatus(t, f.call(2, "POST", "/api/points/checkin", nil), 204)
			webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "OwnPlayer"}))
			webCharacter(t, f.call(2, "POST", "/api/characters", map[string]string{"name": "OtherPlayer"}))
			f.exec("UPDATE users SET character_limit=5 WHERE id=1")
			uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "OwnTexture"}))
			uploadedRecord(t, f.upload(2, testPNG(t, 64, 32), map[string]string{"name": "OtherTexture"}))
			f.exec("INSERT INTO minecraft_verifications(user_id,mc_uuid,mc_name,ms_subject,verified_at) VALUES(1,?,'OwnMC','private-subject',42)", strings.Repeat("c", 32))
			verifier := strings.Repeat("s", 64)
			challenge := sha256.Sum256([]byte(verifier))
			query := url.Values{"client_id": {id}, "redirect_uri": {callback}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "ui_locales": {"en"}}
			request := func(method, path string, form url.Values, account bool, bearer string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Origin", "http://example.test")
				if account {
					r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 1)})
				}
				if bearer != "" {
					r.Header.Set("Authorization", "Bearer "+bearer)
				}
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				return w
			}
			consentForm := url.Values{"decision": {"allow"}, "csrf": {csrf(fmt.Sprintf("%064d", 1))}}
			query.Set("scope", "profile points:read")
			for _, method := range []string{"GET", "POST"} {
				webStatus(t, request(method, "/oauth/authorize?"+query.Encode(), consentForm, true, ""), 400)
			}
			query.Set("scope", "points:read offline_access")
			consent := request("GET", "/oauth/authorize?"+query.Encode(), nil, true, "")
			webStatus(t, consent, 200)
			if body := consent.Body.String(); !strings.Contains(body, "Read your user ID") || !strings.Contains(body, "Read-only data") || !strings.Contains(body, "app.example/callback") || strings.Contains(body, "nickname") {
				t.Fatal("incorrect consent permissions", body)
			}
			issue := func(scope string) map[string]string {
				t.Helper()
				query.Set("scope", scope)
				approved := request("POST", "/oauth/authorize?"+query.Encode(), consentForm, true, "")
				webStatus(t, approved, 303)
				location, err := url.Parse(approved.Header().Get("Location"))
				if err != nil || location.Query().Get("code") == "" {
					t.Fatal("missing authorization code", err)
				}
				token := request("POST", "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {callback}, "code": {location.Query().Get("code")}, "code_verifier": {verifier}}, false, "")
				webStatus(t, token, 200)
				var response struct {
					Access  string `json:"access_token"`
					Refresh string `json:"refresh_token"`
				}
				if err := json.Unmarshal(token.Body.Bytes(), &response); err != nil || response.Access == "" {
					t.Fatal("missing access token", err)
				}
				return map[string]string{"access": response.Access, "refresh": response.Refresh}
			}
			points := issue("points:read offline_access")
			data := issue("characters:read textures:read minecraft:read")
			reject := func(scope string) {
				t.Helper()
				response := request("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {points["refresh"]}, "scope": {scope}}, false, "")
				webStatus(t, response, 400)
				if !strings.Contains(response.Body.String(), `"invalid_scope"`) {
					t.Fatal("incorrect refresh rejection", response.Body)
				}
			}
			reject("characters:read")
			f.exec("UPDATE oauth_clients SET allowed_scopes='offline_access' WHERE client_id=?", id)
			reject("points:read")
			f.exec("UPDATE oauth_clients SET allowed_scopes='characters:read textures:read minecraft:read points:read offline_access' WHERE client_id=?", id)
			grants := f.call(1, "GET", "/api/account/oauth", nil)
			webStatus(t, grants, 200)
			if !strings.Contains(grants.Body.String(), `"scope":"characters:read minecraft:read offline_access points:read textures:read"`) {
				t.Fatal("active scopes were not merged", grants.Body)
			}
			for _, test := range []struct{ path, token, want, forbidden string }{
				{"characters", data["access"], `"characterLimit":5`, "OtherPlayer"},
				{"textures?pageSize=1", data["access"], "OwnTexture", "OtherTexture"},
				{"points", points["access"], `"balance":"0"`, `"kind":"checkin"`},
				{"minecraft", data["access"], "OwnMC", "private-subject"},
			} {
				w := request("GET", "/oauth/api/"+test.path, nil, false, test.token)
				webStatus(t, w, 200)
				if body := w.Body.String(); !strings.Contains(body, test.want) || strings.Contains(body, test.forbidden) || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("incorrect scoped response for %s: %s", test.path, body)
				}
			}
			missing := request("GET", "/oauth/api/characters", nil, false, points["access"])
			webStatus(t, missing, 403)
			if missing.Header().Get("WWW-Authenticate") != `Bearer error="insufficient_scope", scope="characters:read"` {
				t.Fatal("missing scope challenge")
			}
			info := request("GET", "/oauth/userinfo", nil, false, points["access"])
			webStatus(t, info, 200)
			if strings.Contains(info.Body.String(), `"name"`) || strings.Contains(info.Body.String(), `"email"`) {
				t.Fatal("identity fields leaked", info.Body)
			}
			webStatus(t, request("GET", "/oauth/api/characters", nil, false, strings.Repeat("a", 64)), 401)
			webStatus(t, request("POST", "/api/characters", nil, false, data["access"]), 401)
			webStatus(t, request("GET", "/api/admin/oauth/clients", nil, false, data["access"]), 401)
			webStatus(t, f.call(1, "DELETE", "/api/account/oauth/"+id, nil), 204)
			webStatus(t, request("GET", "/oauth/api/characters", nil, false, data["access"]), 401)
			data = issue("characters:read")
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oauth2", map[string]bool{"enabled": false}), 200)
			webStatus(t, request("GET", "/oauth/api/characters", nil, false, data["access"]), 401)
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
