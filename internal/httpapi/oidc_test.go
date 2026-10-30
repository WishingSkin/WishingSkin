package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
	"wishingskin/internal/credential"
)

type oidcProviderFixture struct {
	server                                                  *httptest.Server
	nonce, challenge, subject, email, name, username, fault string
	pkceChecked                                             bool
	afterToken                                              func()
}

func newOIDCProviderFixture(t *testing.T) *oidcProviderFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &oidcProviderFixture{subject: "local-subject", email: "new@example.test", name: "Local Player", username: "local-player"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/jwks", "userinfo_endpoint": f.server.URL + "/me", "id_token_signing_alg_values_supported": []string{"ES384"}})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "ES384", Use: "sig"}}})
		case "/token":
			_ = r.ParseForm()
			f.pkceChecked = oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) == f.challenge
			if !f.pkceChecked || r.Form.Get("code") != "good" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			if f.afterToken != nil {
				f.afterToken()
			}
			claims := map[string]any{"iss": f.server.URL, "sub": f.subject, "aud": "test-client", "nonce": f.nonce, "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix()}
			switch f.fault {
			case "nonce":
				claims["nonce"] = "wrong"
			case "audience":
				claims["aud"] = "other"
			case "issuer":
				claims["iss"] = "https://other.test"
			case "expired":
				claims["exp"] = time.Now().Add(-time.Minute).Unix()
			}
			signingKey := key
			if f.fault == "signature" {
				signingKey, _ = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			}
			signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES384, Key: signingKey}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
			payload, _ := json.Marshal(claims)
			signed, _ := signer.Sign(payload)
			token, _ := signed.CompactSerialize()
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic-access", "token_type": "Bearer", "expires_in": 60, "id_token": token})
		case "/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"sub": f.subject, "name": f.name, "preferred_username": f.username, "email": f.email, "email_verified": true})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func startOIDCTest(t *testing.T, f *managementApp, p *oidcProviderFixture, user int, provider ...string) (*url.URL, *http.Cookie) {
	t.Helper()
	// Independent authorization scenarios must not consume each other's IP quota.
	f.app.loginLimit.entries = map[string]bucket{}
	id := ""
	if len(provider) > 0 {
		id = provider[0]
	}
	w := f.call(user, "POST", "/api/auth/oidc/start", map[string]any{"provider": id, "link": user > 0, "password": "Local123!", "next": "/user/materials"})
	webStatus(t, w, 200)
	var result struct{ URL string }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(result.URL)
	if err != nil {
		t.Fatal(err)
	}
	p.nonce = u.Query().Get("nonce")
	p.challenge = u.Query().Get("code_challenge")
	if u.Query().Get("scope") != "openid profile email" || u.Query().Get("code_challenge_method") != "S256" || p.nonce == "" {
		t.Fatal("OIDC request omitted security parameters")
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "ws_oidc_browser" {
			return u, cookie
		}
	}
	t.Fatal("missing browser cookie")
	return nil, nil
}

func callbackOIDCTest(f *managementApp, u *url.URL, browser *http.Cookie, user int) *httptest.ResponseRecorder {
	w := callbackOIDCRawTest(f, u, browser, user)
	if !strings.Contains(w.Header().Get("Location"), "oidcConfirm=1") {
		return w
	}
	var confirmation *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "ws_oidc_confirm" {
			confirmation = cookie
		}
	}
	if confirmation == nil {
		f.t.Fatal("missing OIDC confirmation cookie")
	}
	confirmed := oidcConfirmationRequest(f, user, confirmation, "POST", `{"action":"confirm"}`)
	webStatus(f.t, confirmed, 200)
	var result struct {
		Next string `json:"next"`
	}
	if err := json.Unmarshal(confirmed.Body.Bytes(), &result); err != nil || result.Next == "" {
		f.t.Fatal("OIDC confirmation omitted its next path", err)
	}
	final := httptest.NewRecorder()
	for _, response := range []*httptest.ResponseRecorder{w, confirmed} {
		for _, cookie := range response.Result().Cookies() {
			final.Header().Add("Set-Cookie", cookie.String())
		}
	}
	final.Code = http.StatusSeeOther
	final.Header().Set("Location", "http://example.test"+result.Next)
	return final
}

func callbackOIDCRawTest(f *managementApp, u *url.URL, browser *http.Cookie, user int) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://example.test/api/auth/oidc/callback?state="+url.QueryEscape(u.Query().Get("state"))+"&code=good", nil)
	if browser != nil {
		r.AddCookie(browser)
	}
	if user > 0 {
		r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", user)})
	}
	w := httptest.NewRecorder()
	f.app.ServeHTTP(w, r)
	return w
}

func oidcConfirmationRequest(f *managementApp, user int, confirmation *http.Cookie, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://example.test/api/auth/oidc/confirmation", strings.NewReader(body))
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(confirmation)
	if user > 0 {
		session := fmt.Sprintf("%064d", user)
		r.AddCookie(&http.Cookie{Name: "ws_session", Value: session})
		r.Header.Set("X-CSRF-Token", csrf(session))
	}
	w := httptest.NewRecorder()
	f.app.ServeHTTP(w, r)
	return w
}

func TestMultipleOIDCProvidersBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			first, second := newOIDCProviderFixture(t), newOIDCProviderFixture(t)
			hash, err := credential.Hash("Local123!")
			if err != nil {
				t.Fatal(err)
			}
			f.exec("UPDATE users SET password_hash=?", hash)
			providers := []map[string]any{
				{"id": "", "enabled": true, "name": "First", "issuer": first.server.URL, "clientId": "test-client", "clientSecret": "synthetic-first"},
				{"id": "second", "enabled": true, "name": "Second", "issuer": second.server.URL, "clientId": "test-client", "clientSecret": "synthetic-second"},
			}
			w := f.call(3, "PATCH", "/api/admin/oidc/providers", map[string]any{"providers": providers})
			totpStatusOnly(t, w, 200)
			if strings.Contains(w.Body.String(), "synthetic-") {
				t.Fatal("provider secret leaked")
			}
			for _, scenario := range []struct {
				p    *oidcProviderFixture
				id   string
				user int
			}{{first, "", 1}, {second, "second", 2}} {
				u, cookie := startOIDCTest(t, f, scenario.p, scenario.user, scenario.id)
				w = callbackOIDCTest(f, u, cookie, scenario.user)
				if !strings.Contains(w.Header().Get("Location"), "oidcLinked=1") {
					t.Fatal("provider link failed")
				}
				u, cookie = startOIDCTest(t, f, scenario.p, 0, scenario.id)
				w = callbackOIDCTest(f, u, cookie, 0)
				if w.Header().Get("Location") != "http://example.test/user/materials" {
					t.Fatal("provider login failed")
				}
				for _, c := range w.Result().Cookies() {
					if c.Name == "ws_session" {
						var uid int
						err = f.db.DB.QueryRow(f.db.Bind("SELECT user_id FROM web_sessions WHERE token_hash=?"), tokenHash(c.Value)).Scan(&uid)
						if err != nil || uid != scenario.user {
							t.Fatal("provider identity crossed")
						}
					}
				}
			}
			u, cookie := startOIDCTest(t, f, second, 0, "second")
			providers[1]["enabled"] = false
			totpStatusOnly(t, f.call(3, "PATCH", "/api/admin/oidc/providers", map[string]any{"providers": providers}), 200)
			w = callbackOIDCTest(f, u, cookie, 0)
			if !strings.Contains(w.Header().Get("Location"), "oidcError=") {
				t.Fatal("disabled provider flow survived")
			}
		})
	}
}

func TestOIDCConfirmationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			p := newOIDCProviderFixture(t)
			hash, err := credential.Hash("Local123!")
			if err != nil {
				t.Fatal(err)
			}
			f.exec("UPDATE users SET password_hash=?", hash)
			webStatus(t, f.call(3, "PATCH", "/api/admin/oidc", map[string]any{"enabled": true, "name": "Test Identity", "issuer": p.server.URL, "clientId": "test-client", "clientSecret": "synthetic"}), 200)
			p.email = "provider@example.test"

			u, browser := startOIDCTest(t, f, p, 3)
			w := callbackOIDCRawTest(f, u, browser, 3)
			if !strings.Contains(w.Header().Get("Location"), "oidcConfirm=1") {
				t.Fatal("link callback skipped the confirmation page", w.Header().Get("Location"))
			}
			var confirmation *http.Cookie
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == "ws_oidc_confirm" {
					confirmation = cookie
				}
			}
			if confirmation == nil {
				t.Fatal("missing link confirmation cookie")
			}
			var linked int
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM oidc_identities").Scan(&linked)
			if linked != 0 {
				t.Fatal("identity was linked before confirmation")
			}
			info := oidcConfirmationRequest(f, 3, confirmation, "GET", "")
			webStatus(t, info, 200)
			if strings.Contains(info.Body.String(), p.subject) || !strings.Contains(info.Body.String(), p.email) || !strings.Contains(info.Body.String(), p.name) || !strings.Contains(info.Body.String(), p.username) || !strings.Contains(info.Body.String(), "3@example.test") {
				t.Fatal("confirmation did not expose only the human-readable provider account details", info.Body.String())
			}
			confirmed := oidcConfirmationRequest(f, 3, confirmation, "POST", `{"action":"confirm"}`)
			webStatus(t, confirmed, 200)
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM oidc_identities WHERE user_id=3").Scan(&linked)
			if linked != 1 {
				t.Fatal("confirmed identity was not linked")
			}
			webStatus(t, oidcConfirmationRequest(f, 3, confirmation, "POST", `{"action":"confirm"}`), 401)

			u, browser = startOIDCTest(t, f, p, 0)
			w = callbackOIDCRawTest(f, u, browser, 0)
			if !strings.Contains(w.Header().Get("Location"), "oidcConfirm=1") {
				t.Fatal("login callback skipped the confirmation page", w.Header().Get("Location"))
			}
			confirmation = nil
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == "ws_oidc_confirm" {
					confirmation = cookie
				}
				if cookie.Name == "ws_session" && cookie.MaxAge > 0 {
					t.Fatal("local session was created before confirmation")
				}
			}
			if confirmation == nil {
				t.Fatal("missing login confirmation cookie")
			}
			info = oidcConfirmationRequest(f, 0, confirmation, "GET", "")
			webStatus(t, info, 200)
			if !strings.Contains(info.Body.String(), `"purpose":"login"`) || !strings.Contains(info.Body.String(), p.email) {
				t.Fatal("login confirmation omitted provider account details", info.Body.String())
			}
			confirmed = oidcConfirmationRequest(f, 0, confirmation, "POST", `{"action":"confirm"}`)
			webStatus(t, confirmed, 200)
			var result struct {
				Next string `json:"next"`
			}
			if err := json.Unmarshal(confirmed.Body.Bytes(), &result); err != nil || result.Next != "/user/materials" {
				t.Fatal("confirmed login returned the wrong destination", result.Next, err)
			}
			var sessionCreated bool
			for _, cookie := range confirmed.Result().Cookies() {
				if cookie.Name == "ws_session" && cookie.MaxAge > 0 {
					sessionCreated = true
				}
			}
			if !sessionCreated {
				t.Fatal("confirmed login did not create a local session")
			}

			u, browser = startOIDCTest(t, f, p, 0)
			w = callbackOIDCRawTest(f, u, browser, 0)
			confirmation = nil
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == "ws_oidc_confirm" {
					confirmation = cookie
				}
			}
			if confirmation == nil {
				t.Fatal("missing cancel confirmation cookie")
			}
			cancelled := oidcConfirmationRequest(f, 0, confirmation, "POST", `{"action":"cancel"}`)
			webStatus(t, cancelled, 200)
			if !strings.Contains(cancelled.Body.String(), `"next":"/login"`) {
				t.Fatal("cancel did not return to local sign-in", cancelled.Body.String())
			}
			for _, cookie := range cancelled.Result().Cookies() {
				if cookie.Name == "ws_session" && cookie.MaxAge > 0 {
					t.Fatal("cancelled OIDC login created a local session")
				}
			}
			webStatus(t, oidcConfirmationRequest(f, 0, confirmation, "POST", `{"action":"confirm"}`), 401)
		})
	}
}

func TestOIDCBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			p := newOIDCProviderFixture(t)
			hash, err := credential.Hash("Local123!")
			if err != nil {
				t.Fatal(err)
			}
			f.exec("UPDATE users SET password_hash=?", hash)
			config := map[string]any{"enabled": true, "name": "Local Logto", "issuer": p.server.URL, "clientId": "test-client", "clientSecret": "synthetic-client-secret"}
			webStatus(t, f.call(0, "PATCH", "/api/admin/oidc", config), 401)
			webStatus(t, f.call(1, "PATCH", "/api/admin/oidc", config), 403)
			w := f.call(3, "PATCH", "/api/admin/oidc", config)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "synthetic-client-secret") {
				t.Fatal("secret returned")
			}
			var stored string
			_ = f.db.DB.QueryRow("SELECT value FROM settings WHERE setting_key='oidc_config'").Scan(&stored)
			if strings.Contains(stored, "synthetic-client-secret") {
				t.Fatal("secret stored unencrypted")
			}
			for _, fault := range []string{"nonce", "audience", "issuer", "expired", "signature"} {
				p.fault = fault
				u, cookie := startOIDCTest(t, f, p, 0)
				w = callbackOIDCTest(f, u, cookie, 0)
				if !strings.HasSuffix(w.Header().Get("Location"), "oidcError=failed") {
					t.Fatalf("accepted invalid %s", fault)
				}
			}
			p.fault = ""
			u, cookie := startOIDCTest(t, f, p, 3)
			if w = callbackOIDCTest(f, u, nil, 3); !strings.HasSuffix(w.Header().Get("Location"), "oidcError=expired") {
				t.Fatal("missing browser binding accepted")
			}
			// Linking is explicit from the current local session. The upstream
			// email may differ from the local account email.
			p.email = "logto@example.test"
			w = callbackOIDCTest(f, u, cookie, 3)
			if !strings.Contains(w.Header().Get("Location"), "oidcLinked=1") || !p.pkceChecked {
				t.Fatal("link failed", w.Header().Get("Location"))
			}
			var linkedEmailCount int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM oidc_identities WHERE user_id=3").Scan(&linkedEmailCount); err != nil || linkedEmailCount != 1 {
				t.Fatal("different-email OIDC identity was not stored on the local account", err)
			}
			if w = callbackOIDCTest(f, u, cookie, 3); !strings.HasSuffix(w.Header().Get("Location"), "oidcError=expired") {
				t.Fatal("state replay accepted")
			}
			u, cookie = startOIDCTest(t, f, p, 0)
			w = callbackOIDCTest(f, u, cookie, 0)
			if w.Header().Get("Location") != "http://example.test/user/materials" {
				t.Fatal("bound login failed", w.Header())
			}
			var session *http.Cookie
			for _, c := range w.Result().Cookies() {
				if c.Name == "ws_session" {
					session = c
				}
			}
			if session == nil {
				t.Fatal("missing local session")
			}
			u, cookie = startOIDCTest(t, f, p, 1)
			w = callbackOIDCTest(f, u, cookie, 1)
			if !strings.HasSuffix(w.Header().Get("Location"), "oidcError=linked") {
				t.Fatal("same identity bound to a second local account")
			}
			f.exec("UPDATE users SET banned=TRUE WHERE id=3")
			u, cookie = startOIDCTest(t, f, p, 0)
			w = callbackOIDCTest(f, u, cookie, 0)
			if !strings.HasSuffix(w.Header().Get("Location"), "oidcError=account") {
				t.Fatal("banned OIDC login accepted")
			}
			f.exec("UPDATE users SET banned=FALSE WHERE id=3")
			p.subject = "unlinked-same-email"
			p.email = "1@example.test"
			u, cookie = startOIDCTest(t, f, p, 0)
			w = callbackOIDCTest(f, u, cookie, 0)
			if !strings.HasSuffix(w.Header().Get("Location"), "oidcError=registration") {
				t.Fatal("closed registration bypassed")
			}
			f.exec("UPDATE settings SET value='open' WHERE setting_key='registration_mode'")
			u, cookie = startOIDCTest(t, f, p, 0)
			w = callbackOIDCTest(f, u, cookie, 0)
			if !strings.Contains(w.Header().Get("Location"), "mode=register&oidc=1") {
				t.Fatal("missing signup handoff")
			}
			var count int
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM oidc_identities").Scan(&count)
			if count != 1 {
				t.Fatal("same email was auto linked")
			}
		})
	}
}

func TestOIDCRegistrationRulesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			f.app.cfg.OIDC = OIDCSettings{Enabled: true, Name: "Test", Issuer: "https://issuer.example.test", ClientID: "client", ClientSecret: "synthetic"}
			provider := f.app.cfg.OIDC
			provider.ID = "secondary"
			sealed, err := f.app.sealOIDC([]OIDCSettings{provider}, "providers")
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(sealed)
			f.exec("INSERT INTO settings(setting_key,value) VALUES('oidc_providers',?)", string(encoded))
			pending := strings.Repeat("f", 64)
			flow := oidcFlow{ProviderID: provider.ID, Purpose: "signup", ConfigHash: oidcFingerprint(provider), Issuer: provider.Issuer, Subject: "new-subject"}
			if err := f.app.saveOIDCFlow(context.Background(), pending, flow); err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"oidc": true, "email": "new@example.test", "password": "Register123!", "turnstileToken": "valid"}
			register := func() *httptest.ResponseRecorder {
				clearMailLimits(f)
				// Each call exercises a separate rule, independent of the IP quota.
				f.app.registrationLimit.entries = map[string]bucket{}
				data, _ := json.Marshal(body)
				r := httptest.NewRequest("POST", "http://example.test/api/auth/register", strings.NewReader(string(data)))
				r.Header.Set("Origin", "http://example.test")
				r.Header.Set("Content-Type", "application/json")
				r.AddCookie(&http.Cookie{Name: "ws_oidc_signup", Value: pending})
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				return w
			}
			webStatus(t, register(), 403) // Closed registration.
			f.exec("UPDATE settings SET value='open' WHERE setting_key='registration_mode'")
			body["password"] = "weak"
			webStatus(t, register(), 400)
			body["password"] = "Register123!"
			body["turnstileToken"] = "invalid"
			webStatus(t, register(), 403)
			body["turnstileToken"] = "valid"
			f.exec("INSERT INTO settings(setting_key,value) VALUES('registration_email_domains',?)", `["allowed.test"]`)
			webStatus(t, register(), 400)
			f.exec("UPDATE settings SET value='[]' WHERE setting_key='registration_email_domains'")
			f.exec("INSERT INTO settings(setting_key,value) VALUES('registration_blocked_domains',?)", `["example.test"]`)
			webStatus(t, register(), 400)
			f.exec("UPDATE settings SET value='[]' WHERE setting_key='registration_blocked_domains'")
			f.exec("UPDATE settings SET value='invitation' WHERE setting_key='registration_mode'")
			webStatus(t, register(), 400)
			invite := strings.Repeat("i", 64)
			f.exec("INSERT INTO invitation_codes(code_hash,remaining_uses,expires_at) VALUES(?,1,0)", tokenHash(invite))
			body["invitation"] = invite
			body["email"] = "1@example.test"
			webStatus(t, register(), 409)
			body["email"] = "new@example.test"
			mailbox.fail = true
			webStatus(t, register(), 503)
			var count, uses int
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM oidc_identities").Scan(&count)
			_ = f.db.DB.QueryRow("SELECT remaining_uses FROM invitation_codes").Scan(&uses)
			if count != 0 || uses != 1 {
				t.Fatal("failed registration did not roll back")
			}
			mailbox.fail = false
			webStatus(t, register(), 201)
			webStatus(t, register(), 400) // Pending flow is single-use.
			var uid int64
			if err := f.db.DB.QueryRow("SELECT user_id FROM oidc_identities").Scan(&uid); err != nil {
				t.Fatal(err)
			}
			if _, err := f.app.oidcUser(context.Background(), uid); err == nil {
				t.Fatal("upstream verification bypassed local email verification")
			}
			webStatus(t, f.call(0, "POST", "/api/auth/verify", map[string]string{"token": mailbox.token()}), 204)
			if _, err := f.app.oidcUser(context.Background(), uid); err != nil {
				t.Fatal(err)
			}
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "new@example.test", "password": "Register123!"}), 200)
		})
	}
}

func TestOIDCConfigurationAndRevocationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			p := newOIDCProviderFixture(t)
			hash, _ := credential.Hash("Local123!")
			f.exec("UPDATE users SET password_hash=?", hash)
			config := map[string]any{"enabled": true, "issuer": p.server.URL, "clientId": "test-client", "clientSecret": "synthetic"}
			webStatus(t, f.call(3, "PATCH", "/api/admin/oidc", config), 200)
			u, c := startOIDCTest(t, f, p, 3)
			config["name"] = "Changed"
			delete(config, "clientSecret")
			webStatus(t, f.call(3, "PATCH", "/api/admin/oidc", config), 200)
			w := callbackOIDCTest(f, u, c, 3)
			if !strings.Contains(w.Header().Get("Location"), "/user/account/profile?oidcError=expired") {
				t.Fatal("config change accepted or lost profile error")
			}
			u, c = startOIDCTest(t, f, p, 3)
			f.exec("UPDATE oidc_flows SET expires_at=0")
			w = callbackOIDCTest(f, u, c, 3)
			if !strings.HasSuffix(w.Header().Get("Location"), "oidcError=expired") {
				t.Fatal("expired flow accepted")
			}
			u, c = startOIDCTest(t, f, p, 3)
			f.exec("DELETE FROM web_sessions WHERE user_id=3")
			w = callbackOIDCTest(f, u, c, 3)
			if !strings.HasSuffix(w.Header().Get("Location"), "oidcError=session") {
				t.Fatal("revoked link session accepted")
			}
			f.exec("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,3,?)", tokenHash(fmt.Sprintf("%064d", 3)), time.Now().Add(time.Hour).Unix())
			u, c = startOIDCTest(t, f, p, 3)
			w = callbackOIDCTest(f, u, c, 3)
			if !strings.HasSuffix(w.Header().Get("Location"), "oidcLinked=1") {
				t.Fatal("link failed")
			}
			webStatus(t, f.call(3, "DELETE", "/api/account/oidc", map[string]string{"password": "wrong"}), 401)
			webStatus(t, f.call(3, "DELETE", "/api/account/oidc", map[string]string{"password": "Local123!"}), 204)
			var count int
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM oidc_identities").Scan(&count)
			if count != 0 {
				t.Fatal("unlink kept identity")
			}
			webStatus(t, f.call(3, "GET", "/api/auth/me", nil), 401)
			f.app.cfg.SettingsKey = make([]byte, 32)
			webStatus(t, f.call(0, "GET", "/readyz", nil), 503)
		})
	}
}

func TestOIDCIssuerTransport(t *testing.T) {
	for _, value := range []string{"http://example.com/oidc", "https://user:secret@example.com", "javascript:alert(1)", "https://example.com/oidc?x=1", "https://example.com/#x"} {
		if validOIDCIssuer(value) {
			t.Fatal("invalid issuer accepted", value)
		}
	}
	for _, value := range []string{"http://localhost:3001/oidc", "http://127.0.0.1:3001/oidc", "https://accounts.example.com/oidc"} {
		if !validOIDCIssuer(value) {
			t.Fatal("valid issuer rejected", value)
		}
	}
}
