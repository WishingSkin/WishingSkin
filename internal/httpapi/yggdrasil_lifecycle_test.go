package httpapi

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wishingskin/internal/credential"
	"wishingskin/internal/store"
	"wishingskin/internal/testutil"
)

const ownerProfile = "11111111111141118111111111111111"
const otherOwnerProfile = "22222222222242228222222222222222"
const foreignProfile = "33333333333343338333333333333333"

type yggTestApp struct {
	t       *testing.T
	db      *store.Store
	app     *Server
	request atomic.Int64
	key     *rsa.PrivateKey
}

type tokenResponse struct {
	AccessToken, ClientToken string
	SelectedProfile          *gameProfile
	AvailableProfiles        []gameProfile
	User                     map[string]any
}

func (f *yggTestApp) call(method, path string, body any) *httptest.ResponseRecorder {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "http://example.test/api/yggdrasil"+path, bytes.NewReader(data))
	// Separate request subjects allow testing token capacity independently of IP limits.
	r.RemoteAddr = fmt.Sprintf("198.51.100.%d:12345", f.request.Add(1)%250+1)
	if strings.Contains(path, "/session/minecraft/join") {
		r.RemoteAddr = "[::ffff:192.0.2.15]:40123"
	}
	w := httptest.NewRecorder()
	f.app.ServeHTTP(w, r)
	return w
}

func yggStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
	}
	if want >= 400 {
		var failure map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil || failure["error"] == "" || failure["errorMessage"] == "" {
			t.Fatalf("missing protocol error envelope: %s", w.Body.String())
		}
	}
}

func readYggToken(t *testing.T, w *httptest.ResponseRecorder) tokenResponse {
	t.Helper()
	yggStatus(t, w, 200)
	var token tokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &token); err != nil || len(token.AccessToken) != 64 || token.ClientToken == "" {
		t.Fatalf("invalid token response: %v %s", err, w.Body.String())
	}
	return token
}

func (f *yggTestApp) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.DB.Exec(f.db.Bind(query), args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *yggTestApp) seedToken(id int64, profile string, expires int64) string {
	f.t.Helper()
	access, err := secret()
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?,?,?,?,?)", tokenHash(access), "test-client", id, profile, expires)
	return access
}

func (f *yggTestApp) validate(access string, want int) {
	f.t.Helper()
	yggStatus(f.t, f.call("POST", "/authserver/validate", map[string]any{"accessToken": access}), want)
}

func (f *yggTestApp) join(access, profile, server string, want int) {
	f.t.Helper()
	yggStatus(f.t, f.call("POST", "/sessionserver/session/minecraft/join", map[string]string{"accessToken": access, "selectedProfile": profile, "serverId": server}), want)
}

func (f *yggTestApp) hasJoined(name, server, ip string) *httptest.ResponseRecorder {
	query := url.Values{"username": {name}, "serverId": {server}}
	if ip != "" {
		query.Set("ip", ip)
	}
	return f.call("GET", "/sessionserver/session/minecraft/hasJoined?"+query.Encode(), nil)
}

func (f *yggTestApp) signedProfile(w *httptest.ResponseRecorder, uuid, name string) {
	f.t.Helper()
	yggStatus(f.t, w, 200)
	var profile struct {
		ID, Name   string
		Properties []struct{ Name, Value, Signature string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || profile.ID != uuid || profile.Name != name || len(profile.Properties) != 1 {
		f.t.Fatalf("invalid joined profile: %v %s", err, w.Body.String())
	}
	property := profile.Properties[0]
	signature, err := base64.StdEncoding.DecodeString(property.Signature)
	if err != nil {
		f.t.Fatal(err)
	}
	sum := sha1.Sum([]byte(property.Value))
	if err := rsa.VerifyPKCS1v15(&f.key.PublicKey, crypto.SHA1, sum[:], signature); err != nil {
		f.t.Fatal("invalid hasJoined signature:", err)
	}
	data, err := base64.StdEncoding.DecodeString(property.Value)
	var payload struct {
		ProfileID, ProfileName string
		SignatureRequired      bool
	}
	if err != nil || json.Unmarshal(data, &payload) != nil || payload.ProfileID != uuid || payload.ProfileName != name || !payload.SignatureRequired {
		f.t.Fatalf("invalid signed texture payload: %s", data)
	}
}

func TestYggdrasilLifecycle(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := credential.Hash("test-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			f := &yggTestApp{t: t, db: db, key: key}
			for _, user := range []struct {
				id    int64
				email string
			}{{1, "owner@example.test"}, {2, "foreign@example.test"}} {
				f.exec("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt) VALUES(?,?,?,?,?,?,?)", user.id, user.email, user.email, "Test", hash, "ARGON2ID", "")
			}
			for index, profile := range []struct{ uuid, name string }{{ownerProfile, "FixturePlayer"}, {otherOwnerProfile, "SecondPlayer"}, {foreignProfile, "ForeignPlayer"}} {
				owner := 1
				if index == 2 {
					owner = 2
				}
				f.exec("INSERT INTO characters(id,user_id,uuid,name,name_key) VALUES(?,?,?,?,?)", index+1, owner, profile.uuid, profile.name, strings.ToLower(profile.name))
			}
			f.exec("INSERT INTO settings(setting_key,value) VALUES(?,?)", "ygg_private_key", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
			f.app, err = New(db, Config{PublicURL: "http://example.test"})
			if err != nil {
				t.Fatal(err)
			}

			t.Run("metadata_and_name_contracts", func(t *testing.T) {
				f.t = t
				w := f.call("GET", "/", nil)
				yggStatus(t, w, 200)
				var metadata struct{ Meta map[string]any }
				if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil || metadata.Meta["feature.non_email_login"] != false || metadata.Meta["feature.username_check"] != true || metadata.Meta["feature.enable_profile_key"] != false {
					t.Fatal("inaccurate supported features", w.Body.String())
				}
				for _, path := range []string{"/api/users/profiles/minecraft/fIxTuRePlAyEr", "/users/profiles/minecraft/FIXTUREPLAYER"} {
					w := f.call("GET", path, nil)
					yggStatus(t, w, 200)
					var profile gameProfile
					if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || profile.ID != ownerProfile || profile.Name != "FixturePlayer" || strings.Contains(w.Body.String(), "properties") {
						t.Fatal("incorrect single-name projection", w.Body.String())
					}
				}
				yggStatus(t, f.call("GET", "/api/users/profiles/minecraft/missing", nil), 204)
				w = f.call("POST", "/api/profiles/minecraft", []string{" fixtureplayer ", "SECONDPLAYER", "FixturePlayer", "missing"})
				yggStatus(t, w, 200)
				var profiles []gameProfile
				if err := json.Unmarshal(w.Body.Bytes(), &profiles); err != nil || len(profiles) != 2 {
					t.Fatal("batch normalization/deduplication failed", w.Body.String())
				}
				yggStatus(t, f.call("POST", "/api/profiles/minecraft", make([]string, maxProfileQueries+1)), 400)
				yggStatus(t, f.call("POST", "/api/profiles/minecraft", map[string]string{"name": "wrong shape"}), 400)
				yggStatus(t, f.call("POST", "/api/profiles/minecraft", []string{}), 200)
			})

			var bound tokenResponse
			t.Run("multirole_refresh_and_selection", func(t *testing.T) {
				f.t = t
				old := readYggToken(t, f.call("POST", "/authserver/authenticate", map[string]any{"username": "owner@example.test", "password": "test-password", "clientToken": "launcher", "requestUser": true}))
				if len(old.AvailableProfiles) != 2 || old.SelectedProfile != nil || old.User["id"] != fmt.Sprintf("%032x", 1) || len(old.User) != 2 {
					t.Fatal("unexpected user/profile exposure", old)
				}
				yggStatus(t, f.call("POST", "/authserver/refresh", map[string]any{"accessToken": old.AccessToken, "clientToken": "wrong"}), 403)
				f.validate(old.AccessToken, 204)
				yggStatus(t, f.call("POST", "/authserver/refresh", map[string]any{"accessToken": old.AccessToken, "selectedProfile": gameProfile{ID: foreignProfile, Name: "ForeignPlayer"}}), 403)
				f.validate(old.AccessToken, 204)
				yggStatus(t, f.call("POST", "/authserver/refresh", map[string]any{"accessToken": old.AccessToken, "selectedProfile": gameProfile{ID: ownerProfile, Name: "Impostor"}}), 403)
				f.validate(old.AccessToken, 204)
				selected := readYggToken(t, f.call("POST", "/authserver/refresh", map[string]any{"accessToken": old.AccessToken, "clientToken": "launcher", "requestUser": true, "selectedProfile": gameProfile{ID: ownerProfile, Name: "fixtureplayer"}}))
				if selected.SelectedProfile == nil || selected.SelectedProfile.Name != "FixturePlayer" || selected.ClientToken != old.ClientToken || selected.User["id"] != old.User["id"] {
					t.Fatal("refresh lost profile/client/user", selected)
				}
				f.validate(old.AccessToken, 403)
				yggStatus(t, f.call("POST", "/authserver/refresh", map[string]any{"accessToken": selected.AccessToken, "selectedProfile": gameProfile{ID: otherOwnerProfile, Name: "SecondPlayer"}}), 400)
				f.validate(selected.AccessToken, 204)
				f.join(selected.AccessToken, ownerProfile, "before-refresh", 204)
				bound = readYggToken(t, f.call("POST", "/authserver/refresh", map[string]any{"accessToken": selected.AccessToken}))
				if bound.SelectedProfile == nil || bound.SelectedProfile.ID != ownerProfile || bound.User != nil {
					t.Fatal("refresh without selection changed binding or exposed user", bound)
				}
				f.validate(selected.AccessToken, 403)
				yggStatus(t, f.hasJoined("FixturePlayer", "before-refresh", ""), 204)
				f.validate(bound.AccessToken, 204)
			})
			if t.Failed() {
				t.FailNow()
			}

			t.Run("concurrent_refresh_one_winner", func(t *testing.T) {
				f.t = t
				old := f.seedToken(1, "", time.Now().Add(time.Hour).Unix())
				start := make(chan struct{})
				results := make(chan *httptest.ResponseRecorder, 2)
				for range 2 {
					go func() {
						<-start
						results <- f.call("POST", "/authserver/refresh", map[string]string{"accessToken": old, "clientToken": "test-client"})
					}()
				}
				close(start)
				winners := 0
				for range 2 {
					w := <-results
					if w.Code == 200 {
						newToken := readYggToken(t, w)
						f.validate(newToken.AccessToken, 204)
						winners++
					} else {
						yggStatus(t, w, 403)
					}
				}
				if winners != 1 {
					t.Fatalf("refresh winners=%d, want 1", winners)
				}
				f.validate(old, 403)
			})

			t.Run("join_matching_signature_and_single_consumption", func(t *testing.T) {
				f.t = t
				server := "-12abA "
				f.join(bound.AccessToken, otherOwnerProfile, server, 403)
				f.join(bound.AccessToken, ownerProfile, server, 204)
				yggStatus(t, f.hasJoined("FixturePlayer", strings.TrimSpace(server), ""), 204)
				yggStatus(t, f.hasJoined("SecondPlayer", server, ""), 204)
				yggStatus(t, f.hasJoined("FixturePlayer", server, "192.0.2.16"), 204)
				yggStatus(t, f.hasJoined(" fixturePLAYER ", server, "192.0.2.15"), 204)
				yggStatus(t, f.hasJoined("fixtureplayer", server, "192.0.2.15"), 204)
				f.signedProfile(f.hasJoined("FixturePlayer", server, "192.0.2.15"), ownerProfile, "FixturePlayer")
				yggStatus(t, f.hasJoined("FixturePlayer", server, ""), 204)
				f.join(bound.AccessToken, ownerProfile, "concurrent-server", 204)
				results := make(chan *httptest.ResponseRecorder, 2)
				start := make(chan struct{})
				for range 2 {
					go func() { <-start; results <- f.hasJoined("FixturePlayer", "concurrent-server", "") }()
				}
				close(start)
				winners := 0
				for range 2 {
					w := <-results
					if w.Code == 200 {
						f.signedProfile(w, ownerProfile, "FixturePlayer")
						winners++
					} else {
						yggStatus(t, w, 204)
					}
				}
				if winners != 1 {
					t.Fatalf("hasJoined winners=%d, want 1", winners)
				}
			})

			t.Run("expired_join_and_token_rejected", func(t *testing.T) {
				f.t = t
				access := f.seedToken(1, ownerProfile, time.Now().Add(time.Hour).Unix())
				f.join(access, ownerProfile, "expiry", 204)
				var expiry int64
				if err := db.DB.QueryRow(db.Bind("SELECT expires_at FROM ygg_join_sessions WHERE token_hash=?"), tokenHash(access)).Scan(&expiry); err != nil || expiry > time.Now().Add(joinLifetime).Unix() || expiry <= time.Now().Unix() {
					t.Fatal("unbounded join expiry", expiry, err)
				}
				f.exec("UPDATE ygg_join_sessions SET expires_at=? WHERE token_hash=?", time.Now().Add(-time.Second).Unix(), tokenHash(access))
				yggStatus(t, f.hasJoined("FixturePlayer", "expiry", ""), 204)
				f.exec("UPDATE game_tokens SET expires_at=? WHERE token_hash=?", time.Now().Add(20*time.Second).Unix(), tokenHash(access))
				f.join(access, ownerProfile, "token-expiry", 204)
				var tokenExpiry int64
				if err := db.DB.QueryRow(db.Bind("SELECT expires_at FROM game_tokens WHERE token_hash=?"), tokenHash(access)).Scan(&tokenExpiry); err != nil {
					t.Fatal(err)
				}
				if err := db.DB.QueryRow(db.Bind("SELECT expires_at FROM ygg_join_sessions WHERE token_hash=?"), tokenHash(access)).Scan(&expiry); err != nil || expiry != tokenExpiry {
					t.Fatal("join outlives access token", expiry, tokenExpiry, err)
				}
				f.exec("UPDATE game_tokens SET expires_at=? WHERE token_hash=?", time.Now().Add(-time.Second).Unix(), tokenHash(access))
				f.validate(access, 403)
				yggStatus(t, f.call("POST", "/authserver/refresh", map[string]string{"accessToken": access}), 403)
				f.join(access, ownerProfile, "expired", 403)
				yggStatus(t, f.hasJoined("FixturePlayer", "token-expiry", ""), 204)
			})

			t.Run("revocation_ban_and_deleted_profile", func(t *testing.T) {
				f.t = t
				access := f.seedToken(1, ownerProfile, time.Now().Add(time.Hour).Unix())
				f.join(access, ownerProfile, "revoked", 204)
				yggStatus(t, f.call("POST", "/authserver/invalidate", map[string]string{"accessToken": access, "clientToken": "ignored-wrong-value"}), 204)
				f.validate(access, 403)
				yggStatus(t, f.hasJoined("FixturePlayer", "revoked", ""), 204)
				yggStatus(t, f.call("POST", "/authserver/invalidate", map[string]string{"accessToken": access}), 204)
				access = f.seedToken(1, ownerProfile, time.Now().Add(time.Hour).Unix())
				f.join(access, ownerProfile, "banned", 204)
				f.exec("UPDATE users SET banned=? WHERE id=?", true, 1)
				f.validate(access, 403)
				yggStatus(t, f.call("POST", "/authserver/refresh", map[string]string{"accessToken": access}), 403)
				f.join(access, ownerProfile, "banned-again", 403)
				yggStatus(t, f.hasJoined("FixturePlayer", "banned", ""), 204)
				yggStatus(t, f.call("POST", "/authserver/authenticate", map[string]string{"username": "owner@example.test", "password": "test-password"}), 403)
				yggStatus(t, f.call("GET", "/api/users/profiles/minecraft/FixturePlayer", nil), 204)
				f.exec("UPDATE users SET banned=? WHERE id=?", false, 1)
				access = f.seedToken(2, foreignProfile, time.Now().Add(time.Hour).Unix())
				f.join(access, foreignProfile, "deleted-profile", 204)
				f.exec("DELETE FROM characters WHERE uuid=?", foreignProfile)
				f.validate(access, 403)
				yggStatus(t, f.call("POST", "/authserver/refresh", map[string]string{"accessToken": access}), 403)
				f.join(access, foreignProfile, "deleted", 403)
				yggStatus(t, f.hasJoined("ForeignPlayer", "deleted-profile", ""), 204)
				yggStatus(t, f.call("GET", "/api/users/profiles/minecraft/ForeignPlayer", nil), 204)
			})

			t.Run("signout_only_revokes_own_game_tokens", func(t *testing.T) {
				f.t = t
				access := f.seedToken(1, ownerProfile, time.Now().Add(time.Hour).Unix())
				other := f.seedToken(2, "", time.Now().Add(time.Hour).Unix())
				f.join(access, ownerProfile, "signout", 204)
				web, _ := secret()
				f.exec("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", tokenHash(web), 1, time.Now().Add(time.Hour).Unix())
				yggStatus(t, f.call("POST", "/authserver/signout", map[string]string{"username": "owner@example.test", "password": "wrong"}), 403)
				f.validate(access, 204)
				yggStatus(t, f.call("POST", "/authserver/signout", map[string]string{"username": "owner@example.test", "password": "test-password"}), 204)
				f.validate(access, 403)
				f.validate(other, 204)
				yggStatus(t, f.hasJoined("FixturePlayer", "signout", ""), 204)
				var count int
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM game_tokens WHERE user_id=1").Scan(&count); err != nil || count != 0 {
					t.Fatal("signout left game credentials", count, err)
				}
				r := httptest.NewRequest("GET", "http://example.test/api/auth/me", nil)
				r.AddCookie(&http.Cookie{Name: "ws_session", Value: web})
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				yggStatus(t, w, 200)
			})

			t.Run("concurrent_token_capacity_and_join_cleanup", func(t *testing.T) {
				f.t = t
				f.exec("DELETE FROM game_tokens WHERE user_id=?", 1)
				oldest := ""
				for index := range maxGameTokens {
					access := f.seedToken(1, ownerProfile, time.Now().Add(time.Duration(index+1)*time.Minute).Unix())
					if index == 0 {
						oldest = access
					}
				}
				f.join(oldest, ownerProfile, "capacity-evicted", 204)
				start := make(chan struct{})
				results := make(chan *httptest.ResponseRecorder, 2)
				for range 2 {
					go func() {
						<-start
						results <- f.call("POST", "/authserver/authenticate", map[string]string{"username": "owner@example.test", "password": "test-password"})
					}()
				}
				close(start)
				for range 2 {
					readYggToken(t, <-results)
				}
				var count int
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM game_tokens WHERE user_id=1").Scan(&count); err != nil || count != maxGameTokens {
					t.Fatal("token capacity escaped", count, err)
				}
				f.validate(oldest, 403)
				yggStatus(t, f.hasJoined("FixturePlayer", "capacity-evicted", ""), 204)
			})
		})
	}
}

func TestYggdrasilMalformedAndOversizedRequests(t *testing.T) {
	for _, body := range []string{"{} trailing", strings.Repeat("x", 8193)} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var value any
		if ydecode(w, r, &value) {
			t.Fatal("accepted malformed or oversized protocol body")
		}
		yggStatus(t, w, 400)
	}
}

func TestYggdrasilAccountRateLimit(t *testing.T) {
	hash, err := credential.Hash("test-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			f := &yggTestApp{t: t, db: db}
			for id, email := range []string{"owner@example.test", "other@example.test"} {
				f.exec("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt) VALUES(?,?,?,?,?,?,?)", id+1, email, email, "Rate Test", hash, "ARGON2ID", "")
			}
			var credentialFailure string
			t.Run("normalized_account_across_ips_and_password_endpoints", func(t *testing.T) {
				f.t = t
				f.app, err = New(db, Config{PublicURL: "http://example.test"})
				if err != nil {
					t.Fatal(err)
				}
				for index := range 10 {
					username := " OWNER@EXAMPLE.TEST "
					endpoint := "/authserver/authenticate"
					if index%2 == 1 {
						username = "owner@example.test"
						endpoint = "/authserver/signout"
					}
					w := f.call("POST", endpoint, map[string]string{"username": username, "password": "wrong"})
					yggStatus(t, w, 403)
					if credentialFailure == "" {
						credentialFailure = w.Body.String()
					} else if w.Body.String() != credentialFailure {
						t.Fatal("password endpoints expose different credential failure responses", w.Body.String())
					}
				}
				for _, endpoint := range []string{"/authserver/authenticate", "/authserver/signout"} {
					yggStatus(t, f.call("POST", endpoint, map[string]string{"username": " Owner@Example.Test ", "password": "test-password"}), 429)
				}
				// A different account retains its quota even after the first is exhausted.
				readYggToken(t, f.call("POST", "/authserver/authenticate", map[string]string{"username": "other@example.test", "password": "test-password"}))
				var count int
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM game_tokens WHERE user_id=1").Scan(&count); err != nil || count != 0 {
					t.Fatal("rate-limited request created a game token", count, err)
				}
				f.app.loginLimit.mu.Lock()
				defer f.app.loginLimit.mu.Unlock()
				if _, exists := f.app.loginLimit.entries["account:"+tokenHash("owner@example.test")]; !exists {
					t.Fatal("normalized account did not share one hashed quota")
				}
				for key := range f.app.loginLimit.entries {
					if strings.Contains(key, "@") || strings.Contains(key, "owner") || strings.Contains(key, "other") {
						t.Fatal("limiter retained a plaintext account identifier")
					}
				}
			})
			t.Run("unknown_account_has_same_failure_and_quota", func(t *testing.T) {
				f.t = t
				f.app, err = New(db, Config{PublicURL: "http://example.test"})
				if err != nil {
					t.Fatal(err)
				}
				for index := range 10 {
					username := "missing@example.test"
					if index%2 == 0 {
						username = " MISSING@EXAMPLE.TEST "
					}
					w := f.call("POST", "/authserver/authenticate", map[string]string{"username": username, "password": "wrong"})
					yggStatus(t, w, 403)
					if w.Body.String() != credentialFailure {
						t.Fatal("unknown account response differs from known-account password failure", w.Body.String())
					}
				}
				yggStatus(t, f.call("POST", "/authserver/authenticate", map[string]string{"username": "missing@example.test", "password": "wrong"}), 429)
			})
		})
	}
}
