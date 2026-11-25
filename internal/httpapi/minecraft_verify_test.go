package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/WishingSkin/WishingSkin/internal/credential"
)

func TestMinecraftVerificationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			const path = "/api/account/minecraft"
			password := strings.Repeat("aB1!", 4)
			hash, err := credential.Hash(password)
			if err != nil {
				t.Fatal(err)
			}
			f.exec("UPDATE users SET password_hash=? WHERE id IN (1,2)", hash)
			character := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "ExistingPlayer"}))
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.minecraft", map[string]bool{"enabled": true}), 409)
			response := f.call(3, "PATCH", "/api/admin/minecraft", map[string]any{"clientId": "fixture-client", "clientSecret": "fixture-secret", "enabled": true})
			webStatus(t, response, 200)
			if strings.Contains(response.Body.String(), "fixture-secret") {
				t.Fatal("configuration response exposed client secret")
			}
			const mcID = "0123456789abcdef0123456789abcdef"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					if err := r.ParseForm(); err != nil || r.Form.Get("code_verifier") == "" || r.Form.Get("client_secret") != "fixture-secret" {
						t.Error("invalid Microsoft token exchange")
					}
					fmt.Fprintf(w, `{"access_token":%q}`, r.Form.Get("code"))
				case "/xbl":
					var request struct{ Properties struct{ RpsTicket string } }
					_ = json.NewDecoder(r.Body).Decode(&request)
					fmt.Fprintf(w, `{"Token":%q}`, strings.TrimPrefix(request.Properties.RpsTicket, "d="))
				case "/xsts":
					var request struct{ Properties struct{ UserTokens []string } }
					_ = json.NewDecoder(r.Body).Decode(&request)
					token := request.Properties.UserTokens[0]
					if token == "no_xbox" || token == "child" {
						w.WriteHeader(401)
						fmt.Fprintf(w, `{"XErr":%d}`, map[string]int64{"no_xbox": 2148916233, "child": 2148916238}[token])
						return
					}
					fmt.Fprintf(w, `{"Token":%q,"DisplayClaims":{"xui":[{"uhs":"fixture-uhs"}]}}`, token)
				case "/login":
					var request struct{ IdentityToken string }
					_ = json.NewDecoder(r.Body).Decode(&request)
					fmt.Fprintf(w, `{"access_token":%q}`, strings.TrimPrefix(request.IdentityToken, "XBL3.0 x=fixture-uhs;"))
				case "/entitlements":
					if r.Header.Get("Authorization") == "Bearer not_owned" {
						fmt.Fprint(w, `{"items":[{"name":"unrelated_game"}]}`)
					} else {
						fmt.Fprint(w, `{"items":[{"name":"product_minecraft"}]}`)
					}
				case "/profile":
					if r.Header.Get("Authorization") == "Bearer missing_profile" {
						w.WriteHeader(404)
						return
					}
					fmt.Fprintf(w, `{"id":%q,"name":"VerifiedPlayer"}`, strings.ToUpper(mcID))
				default:
					t.Error("unexpected upstream path")
					w.WriteHeader(404)
				}
			}))
			defer upstream.Close()
			f.app.minecraftEndpoints = minecraftEndpoints{upstream.URL + "/authorize", upstream.URL + "/token", upstream.URL + "/xbl", upstream.URL + "/xsts", upstream.URL + "/login", upstream.URL + "/entitlements", upstream.URL + "/profile"}
			f.app.minecraftClient = upstream.Client()
			call := func(user int, method, route string, cookie *http.Cookie, body string, csrfOK bool) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, "http://example.test"+route, bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", "http://example.test")
				token := fmt.Sprintf("%064d", user)
				req.AddCookie(&http.Cookie{Name: "ws_session", Value: token})
				if cookie != nil {
					req.AddCookie(cookie)
				}
				if csrfOK {
					req.Header.Set("X-CSRF-Token", csrf(token))
				}
				res := httptest.NewRecorder()
				f.app.ServeHTTP(res, req)
				return res
			}
			start := func(user int, code, result string) *http.Cookie {
				startRes := f.call(user, "POST", path+"/start", map[string]string{"password": password})
				webStatus(t, startRes, 200)
				var body map[string]string
				_ = json.Unmarshal(startRes.Body.Bytes(), &body)
				uri, urlErr := url.Parse(body["url"])
				if urlErr != nil || uri.Query().Get("scope") != "XboxLive.signin" || uri.Query().Get("code_challenge_method") != "S256" {
					t.Fatal("authorization URL missing scope or PKCE")
				}
				callbackPath := path + "/callback?state=" + url.QueryEscape(uri.Query().Get("state")) + "&code=" + url.QueryEscape(code)
				browser := startRes.Result().Cookies()[0]
				res := call(user, "GET", callbackPath, browser, "", false)
				webStatus(t, res, 303)
				if res.Header().Get("Location") != "/user/account/profile?"+result || res.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("invalid callback response: %v", res.Header())
				}
				for _, cookie := range res.Result().Cookies() {
					if cookie.Name == "ws_mc_confirm" {
						return cookie
					}
				}
				return nil
			}
			for _, code := range []string{"no_xbox", "child", "not_owned", "missing_profile"} {
				result := code
				if code == "missing_profile" {
					result = "not_owned"
				}
				start(1, code, "minecraftError="+result)
			}
			confirmation := start(1, "valid", "minecraftConfirm=1")
			info := call(1, "GET", path+"/confirmation", confirmation, "", false)
			webStatus(t, info, 200)
			if !strings.Contains(info.Body.String(), "01234567-89ab-cdef-0123-456789abcdef") || !strings.Contains(info.Body.String(), "1@example.test") {
				t.Fatalf("invalid confirmation: %s", info.Body)
			}
			webStatus(t, call(1, "POST", path+"/confirmation", confirmation, `{"action":"cancel"}`, false), 403)
			webStatus(t, call(1, "POST", path+"/confirmation", confirmation, `{"action":"confirm"}`, true), 200)
			start(2, "valid", "minecraftError=already_linked")
			var uuid string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT uuid FROM characters WHERE id=?"), character.ID).Scan(&uuid); err != nil || uuid != character.UUID {
				t.Fatal("binding modified an existing character UUID")
			}
			webStatus(t, f.call(1, "DELETE", path, map[string]string{"password": password}), 204)
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 200)
		})
	}
}
