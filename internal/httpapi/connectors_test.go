package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func TestAuthMeHash(t *testing.T) {
	// Format and algorithm match AuthMeReloaded Sha256.computeHash.
	password := "synthetic-pass"
	salt := "123456789abcdef0"
	hash := "$SHA$" + salt + "$" + tokenHash(tokenHash(password)+salt)
	if !verifyAuthMe(password, hash) || verifyAuthMe("wrong", hash) || verifyAuthMe(password, "$SHA$$invalid") || verifyAuthMe(password, strings.Repeat("x", 600)) {
		t.Fatal("AuthMe hash validation")
	}
}

func TestAuthMeReadOnlyQuery(t *testing.T) {
	f := newManagementApp(t, "mysql")
	f.exec("CREATE TABLE connector_authme (id BIGINT PRIMARY KEY, realname VARCHAR(255), password VARCHAR(255))")
	password, salt := "synthetic-pass", "test-salt"
	hash := "$SHA$" + salt + "$" + tokenHash(tokenHash(password)+salt)
	f.exec("INSERT INTO connector_authme VALUES(7,'Player',?)", hash)
	cfg, err := mysql.ParseDSN(os.Getenv("WS_TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal("invalid synthetic DSN")
	}
	if err = f.db.DB.QueryRow("SELECT DATABASE()").Scan(&cfg.DBName); err != nil {
		t.Fatal(err)
	}
	v := connectorConfig{ID: "authme", Name: "AuthMe", Kind: "authme", DSN: cfg.FormatDSN(), Table: "connector_authme", IDColumn: "id", NameColumn: "realname", PasswordColumn: "password", Enabled: true}
	subject, name, err := verifyConnector(context.Background(), v, "pLaYeR", password)
	if err != nil || subject != "7" || name != "Player" {
		t.Fatal("AuthMe query did not authenticate")
	}
	if _, _, err = verifyConnector(context.Background(), v, "Player", "wrong"); err == nil {
		t.Fatal("invalid password accepted")
	}
	var unchanged string
	if err = f.db.DB.QueryRow("SELECT password FROM connector_authme WHERE id=7").Scan(&unchanged); err != nil || unchanged != hash {
		t.Fatal("AuthMe source was modified")
	}
	v.Table = "connector_authme;DROP TABLE users"
	webStatus(t, f.call(3, "PATCH", "/api/admin/connectors", map[string]any{"connectors": []connectorConfig{v}}), 400)
}
func TestHTTPConnectorBindingBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			seedPassword(t, f, 1, "Local-test-123!")
			seedPassword(t, f, 2, "Local-test-123!")
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				json.NewDecoder(r.Body).Decode(&body)
				if r.Header.Get("Authorization") != "Bearer synthetic-key" || body["password"] != "external-password" {
					w.WriteHeader(401)
					return
				}
				respond(w, 200, map[string]any{"authenticated": true, "subject": "stable-123", "username": "Player"})
			}))
			defer upstream.Close()
			v := connectorConfig{ID: "test-http", Name: "HTTP", Kind: "http", Endpoint: upstream.URL, APIKey: "synthetic-key", Enabled: true}
			webStatus(t, f.call(3, "PATCH", "/api/admin/connectors", map[string]any{"connectors": []connectorConfig{v}}), 200)
			w := f.call(3, "GET", "/api/admin/connectors", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "synthetic-key") {
				t.Fatal("secret leaked")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.connectors", map[string]bool{"enabled": true}), 200)
			body := map[string]string{"currentPassword": "Local-test-123!", "username": "player", "password": "wrong"}
			webStatus(t, f.call(1, "POST", "/api/account/connectors/test-http", body), 400)
			body["password"] = "external-password"
			webStatus(t, f.call(1, "POST", "/api/account/connectors/test-http", body), 204)
			webStatus(t, f.call(2, "POST", "/api/account/connectors/test-http", body), 409)
			webStatus(t, f.call(1, "DELETE", "/api/account/connectors/test-http", body), 204)
			webStatus(t, f.call(2, "POST", "/api/account/connectors/test-http", body), 204)
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, upstream.URL, 307) }))
			defer redirect.Close()
			v.Endpoint = redirect.URL
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, _, err := verifyConnector(ctx, v, "player", "external-password"); err == nil {
				t.Fatal("redirect followed")
			}
		})
	}
}
