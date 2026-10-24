package httpapi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"wishingskin/internal/bsmigration"
	"wishingskin/internal/testutil"
)

func TestLimiterExpiryAndAddressBoundary(t *testing.T) {
	now := time.Now()
	l := limiter{entries: map[string]bucket{}, now: func() time.Time { return now }}
	r := httptest.NewRequest("POST", "/api/auth/login", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	for i := 0; i < 10; i++ {
		if !l.allow(r) {
			t.Fatalf("early limit at %d", i)
		}
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	if l.allow(r) {
		t.Fatal("untrusted proxy header bypassed limit")
	}
	now = now.Add(time.Minute)
	if !l.allow(r) {
		t.Fatal("window did not recover")
	}
}

func TestMalformedProtocolBody(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{}garbage`))
	var v any
	if ydecode(w, r, &v) || w.Code != 400 {
		t.Fatal("accepted malformed trailing JSON")
	}
}

func TestPendingMigrationClosesBusinessEndpoints(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			app, err := New(db, Config{PublicURL: "http://localhost:8090", TextureDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB.Exec("INSERT INTO migration_state(id,source_fingerprint,status,report) VALUES(1,'fixture','pending','{}')"); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/readyz", "/api/site", "/api/auth/me", "/api/characters", "/api/yggdrasil/", "/textures/fixture"} {
				w := httptest.NewRecorder()
				app.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if w.Code != 503 {
					t.Fatalf("pending migration allowed %s: %d", path, w.Code)
				}
			}
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
			if w.Code != 200 {
				t.Fatal("migration gate must preserve liveness")
			}
			if _, err := db.DB.Exec("UPDATE migration_state SET status='partial' WHERE id=1"); err != nil {
				t.Fatal(err)
			}
			w = httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
			if w.Code != 200 {
				t.Fatal("completed phase import did not restore readiness")
			}
		})
	}
}

func TestStaticAssetsAndAPIRouting(t *testing.T) {
	db := testutil.Database(t, "postgres")
	app, err := New(db, Config{PublicURL: "http://localhost:8090", Assets: fstest.MapFS{
		"index.html":     {Data: []byte("<!doctype html><title>WishingSkin</title>")},
		"assets/main.js": {Data: []byte("console.log('ready')")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200}, {"HEAD", "/", 200}, {"GET", "/assets/main.js", 200},
		{"POST", "/", 405}, {"GET", "/api/unknown", 404}, {"POST", "/api/unknown", 404},
		{"GET", "/api/site", 200}, {"GET", "/api/auth/me", 401},
	} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != tt.status {
			t.Fatalf("%s %s got %d want %d", tt.method, tt.path, w.Code, tt.status)
		}
	}
}

func TestImportedAccountAndProfile(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			ctx := context.Background()
			snap, err := bsmigration.Load(filepath.Join("..", "..", "testdata", "bs", "snapshot.json"))
			if err != nil {
				t.Fatal(err)
			}
			key, err := rsa.GenerateKey(rand.Reader, 4096)
			if err != nil {
				t.Fatal(err)
			}
			snap.SignaturePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
			target := filepath.Join(t.TempDir(), "textures")
			report, err := bsmigration.Import(ctx, db, snap, filepath.Join("..", "..", "testdata", "bs"), target)
			if err != nil || !report.Imported || report.Status != "partial" {
				t.Fatalf("import: %v %+v", err, report)
			}
			ts := httptest.NewUnstartedServer(nil)
			origin := "http://" + ts.Listener.Addr().String()
			app, err := New(db, Config{PublicURL: origin, TextureDir: target})
			if err != nil {
				t.Fatal(err)
			}
			ts.Config.Handler = app
			ts.Start()
			defer ts.Close()
			jar, _ := cookiejar.New(nil)
			client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
			request := func(method, path string, body any, csrfValue string) *http.Response {
				t.Helper()
				var raw io.Reader
				if body != nil {
					b, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					raw = bytes.NewReader(b)
				}
				r, err := http.NewRequest(method, ts.URL+path, raw)
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Content-Type", "application/json")
				if csrfValue != "" {
					r.Header.Set("X-CSRF-Token", csrfValue)
				}
				resp, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				return resp
			}
			check := func(resp *http.Response, status int, v any) {
				t.Helper()
				defer resp.Body.Close()
				if resp.StatusCode != status {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("status %d wanted %d: %s", resp.StatusCode, status, body)
				}
				if v != nil {
					if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
						t.Fatal(err)
					}
				}
			}
			check(request("GET", "/api/characters", nil, ""), 401, nil)
			check(request("POST", "/api/auth/login", map[string]string{"email": "fixture@example.invalid", "password": "wrong"}, ""), 401, nil)
			var method string
			if err := db.DB.QueryRow("SELECT password_method FROM users WHERE id=7").Scan(&method); err != nil || method != "BCRYPT" {
				t.Fatal("wrong password changed source hash", method, err)
			}
			var login struct {
				User User   `json:"user"`
				CSRF string `json:"csrfToken"`
			}
			check(request("POST", "/api/auth/login", map[string]string{"email": " fixture@example.invalid ", "password": "password"}, ""), 200, &login)
			if login.User.ID != 7 || login.CSRF == "" {
				t.Fatal("incorrect imported account")
			}
			if err := db.DB.QueryRow("SELECT password_method FROM users WHERE id=7").Scan(&method); err != nil || method != "ARGON2ID" {
				t.Fatal("old hash not upgraded", method, err)
			}
			var chars struct {
				Characters []Character `json:"characters"`
			}
			check(request("GET", "/api/characters", nil, ""), 200, &chars)
			if len(chars.Characters) != 1 || chars.Characters[0].UUID != "12345678123442348234123456789abc" {
				t.Fatal("UUID lost", chars)
			}
			check(request("POST", "/api/auth/logout", nil, "wrong"), 403, nil)
			check(request("POST", "/api/auth/logout", nil, login.CSRF), 204, nil)
			check(request("GET", "/api/auth/me", nil, ""), 401, nil)
			var profile struct {
				ID         string                              `json:"id"`
				Properties []struct{ Value, Signature string } `json:"properties"`
			}
			check(request("GET", "/api/yggdrasil/sessionserver/session/minecraft/profile/"+chars.Characters[0].UUID+"?unsigned=false", nil, ""), 200, &profile)
			if profile.ID != chars.Characters[0].UUID || len(profile.Properties) != 1 {
				t.Fatal("wrong signed profile")
			}
			p := profile.Properties[0]
			signature, err := base64.StdEncoding.DecodeString(p.Signature)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha1.Sum([]byte(p.Value))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA1, sum[:], signature); err != nil {
				t.Fatal("signature invalid", err)
			}
			skin := chars.Characters[0].SkinHash
			res := request("GET", "/textures/"+skin, nil, "")
			check(res, 200, nil)
			check(request("POST", "/api/yggdrasil/authserver/validate", map[string]string{"accessToken": "old-bs-token"}, ""), 403, nil)
			var auth struct{ AccessToken, ClientToken string }
			check(request("POST", "/api/yggdrasil/authserver/authenticate", map[string]string{"username": "fixture@example.invalid", "password": "password", "clientToken": "test-launcher"}, ""), 200, &auth)
			check(request("POST", "/api/yggdrasil/authserver/validate", auth, ""), 204, nil)
			check(request("POST", "/api/yggdrasil/authserver/invalidate", auth, ""), 204, nil)
			check(request("POST", "/api/yggdrasil/authserver/validate", auth, ""), 403, nil)
			if _, err := db.DB.ExecContext(ctx, db.Bind("UPDATE textures SET deleted=? WHERE hash=?"), true, skin); err != nil {
				t.Fatal(err)
			}
			r, err := http.NewRequest("GET", ts.URL+"/textures/"+skin, nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("If-None-Match", `"`+skin+`"`)
			res, err = client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			check(res, 404, nil)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			check(request("GET", "/healthz", nil, ""), 200, nil)
			check(request("GET", "/readyz", nil, ""), 503, nil)
		})
	}
}
