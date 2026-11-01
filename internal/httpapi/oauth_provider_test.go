package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

func TestOAuthProviderAndRefreshBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			host := httptest.NewServer(f.app)
			defer host.Close()
			transport := host.Client().Transport.(*http.Transport).Clone()
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, host.Listener.Addr().String())
			}
			ctx := oidc.ClientContext(context.Background(), &http.Client{Transport: transport})
			w := f.call(3, "POST", "/api/admin/oauth/clients", oauthClient{Name: "Extension client", Redirects: []string{"https://app.example/callback"}, Enabled: true})
			webStatus(t, w, 200)
			var created struct {
				Client       oauthClient
				ClientSecret string
			}
			if json.Unmarshal(w.Body.Bytes(), &created) != nil {
				t.Fatal("client response")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oauth2", map[string]bool{"enabled": true}), 200)
			requestNumber := 0
			submit := func(path string, form url.Values, account bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "http://example.test"+path, strings.NewReader(form.Encode()))
				requestNumber++
				r.RemoteAddr = fmt.Sprintf("192.0.2.%d:12000", requestNumber)
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Origin", "http://example.test")
				if account {
					r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 1)})
				} else {
					r.SetBasicAuth(created.Client.ID, created.ClientSecret)
				}
				out := httptest.NewRecorder()
				f.app.ServeHTTP(out, r)
				return out
			}
			verifier := strings.Repeat("v", 64)
			hash := sha256.Sum256([]byte(verifier))
			q := url.Values{"client_id": {created.Client.ID}, "redirect_uri": {created.Client.Redirects[0]}, "response_type": {"code"}, "scope": {"openid profile email offline_access"}, "nonce": {"nonce-roundtrip"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}}
			type tokens struct {
				Access  string `json:"access_token"`
				Refresh string `json:"refresh_token"`
				ID      string `json:"id_token"`
			}
			issue := func() tokens {
				t.Helper()
				w := submit("/oauth/authorize?"+q.Encode(), url.Values{"decision": {"allow"}, "csrf": {csrf(fmt.Sprintf("%064d", 1))}}, true)
				webStatus(t, w, 303)
				location, _ := url.Parse(w.Header().Get("Location"))
				w = submit("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {location.Query().Get("code")}, "code_verifier": {verifier}, "redirect_uri": {created.Client.Redirects[0]}}, false)
				webStatus(t, w, 200)
				var v tokens
				if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Refresh == "" || v.ID == "" {
					t.Fatal("missing tokens")
				}
				return v
			}
			refresh := func(old string) *httptest.ResponseRecorder {
				return submit("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old}}, false)
			}
			introspect := func(access string) bool {
				w := submit("/oauth/introspect", url.Values{"token": {access}}, false)
				webStatus(t, w, 200)
				var d struct{ Active bool }
				json.Unmarshal(w.Body.Bytes(), &d)
				return d.Active
			}
			first := issue()
			provider, err := oidc.NewProvider(ctx, f.app.cfg.PublicURL)
			if err != nil {
				t.Fatal("discovery", err)
			}
			id, err := provider.Verifier(&oidc.Config{ClientID: created.Client.ID}).Verify(ctx, first.ID)
			if err != nil {
				t.Fatal("ID token", err)
			}
			var claims struct {
				Nonce      string `json:"nonce"`
				AccessHash string `json:"at_hash"`
			}
			if err = id.Claims(&claims); err != nil || claims.Nonce != "nonce-roundtrip" || id.Subject != "1" {
				t.Fatal("identity claims")
			}
			if err = id.VerifyAccessToken(first.Access); err != nil {
				t.Fatal("at_hash", err)
			}
			w = refresh(first.Refresh)
			webStatus(t, w, 200)
			var second tokens
			json.Unmarshal(w.Body.Bytes(), &second)
			if second.Refresh == "" || second.Refresh == first.Refresh || !introspect(second.Access) {
				t.Fatal("rotation")
			}
			webStatus(t, refresh(first.Refresh), 400)
			webStatus(t, refresh(second.Refresh), 400)
			if introspect(second.Access) || introspect(first.Access) {
				t.Fatal("replay must revoke family access tokens")
			}
			for _, action := range []string{"revoke", "account", "password", "client", "plugin", "ban"} {
				current := issue()
				switch action {
				case "revoke":
					webStatus(t, submit("/oauth/revoke", url.Values{"token": {current.Refresh}}, false), 200)
				case "account":
					webStatus(t, f.call(1, "DELETE", "/api/account/oauth/"+created.Client.ID, nil), 204)
				case "password":
					f.exec("UPDATE users SET password_hash='changed' WHERE id=1")
				case "ban":
					webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]bool{"banned": true}), 204)
				case "client":
					webStatus(t, f.call(3, "PATCH", "/api/admin/oauth/clients/"+created.Client.ID, created.Client), 200)
				case "plugin":
					webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oauth2", map[string]bool{"enabled": false}), 200)
				}
				if w = refresh(current.Refresh); w.Code == 200 {
					t.Fatal(action, "kept refresh authorization")
				}
				if action == "plugin" {
					webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oauth2", map[string]bool{"enabled": true}), 200)
				}
			}
		})
	}
}
