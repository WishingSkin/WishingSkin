package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	rp "wishingskin/internal/runtimeplugin"
	sdk "wishingskin/pkg/plugin"
)

func TestPluginPackagesAndWebhooksBothDatabases(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "echo")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", exe, "../runtimeplugin/testdata/echo")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	pkg := rp.Package{Manifest: sdk.Manifest{ID: "example.echo", Name: "Echo", Version: "1.0.0", SettingsNamespace: "echo", MigrationVersion: 1, Permissions: []string{"routes.admin", "database", "events", "pages", "tasks", "frontend", "webhooks"}, Events: []string{"character.create"}, Pages: []sdk.PageContribution{{ID: "status", Area: "admin.plugins", Label: "Status"}}, Tasks: []sdk.TaskDeclaration{{ID: "check"}}}, Protocol: 1, Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: filepath.Base(exe)}, Frontend: "index.html", Routes: []rp.Route{{Method: "POST", Path: "/data", Access: "admin"}}, Migrations: []rp.Migration{{Version: 1, MySQL: []string{"CREATE TABLE IF NOT EXISTS {{prefix}}notes (id INTEGER PRIMARY KEY)"}, Postgres: []string{"CREATE TABLE IF NOT EXISTS {{prefix}}notes (id INTEGER PRIMARY KEY)"}}}}
	manifest, _ := json.Marshal(pkg)
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"plugin.json", manifest}, {filepath.Base(exe), binary}, {"index.html", []byte("<h1>Echo</h1>")}} {
		f, e := z.Create(entry.name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(entry.data); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buffer.Bytes()
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			defer f.app.dynamic.Close()
			var signingSecret string
			var delivered bool
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				mac := hmac.New(sha256.New, []byte(signingSecret))
				fmt.Fprint(mac, r.Header.Get("X-WishingSkin-Timestamp")+"."+r.Header.Get("X-WishingSkin-Delivery")+"."+string(data))
				if r.Header.Get("X-WishingSkin-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
					w.WriteHeader(400)
					return
				}
				delivered = true
				w.WriteHeader(204)
			}))
			defer receiver.Close()
			market := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/plugin.zip" {
					w.Write(archive)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"packages": []map[string]any{{"id": pkg.Manifest.ID, "version": "1.0.0", "url": "http://" + r.Host + "/plugin.zip", "sha256": rp.Digest(archive)}}})
			}))
			defer market.Close()
			webStatus(t, f.call(1, "PUT", "/api/admin/plugin-market", map[string]string{"source": market.URL}), 403)
			webStatus(t, f.call(3, "PUT", "/api/admin/plugin-market", map[string]string{"source": market.URL}), 200)
			webStatus(t, f.call(3, "POST", "/api/admin/plugin-market/inspect", map[string]string{"id": pkg.Manifest.ID}), 200)
			install := func(grants []string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "http://example.test/api/admin/plugin-market/install", bytes.NewBufferString(`{"id":"example.echo"}`))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", "http://example.test")
				r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 3)})
				r.Header.Set("X-CSRF-Token", csrf(fmt.Sprintf("%064d", 3)))
				r.Header.Set("X-Plugin-Digest", rp.Digest(archive))
				raw, _ := json.Marshal(grants)
				r.Header.Set("X-Plugin-Permissions", string(raw))
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				return w
			}
			webStatus(t, install(nil), 409)
			webStatus(t, install(pkg.Manifest.Permissions), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/example.echo", map[string]bool{"enabled": true}), 200)
			webStatus(t, f.call(1, "POST", "/api/plugins/example.echo/data", map[string]string{}), 403)
			webStatus(t, f.call(3, "POST", "/api/plugins/example.echo/data", map[string]string{}), 200)
			var value string
			if err = f.db.DB.QueryRow(f.db.Bind("SELECT value_json FROM plugin_data WHERE plugin_id=? AND data_key=?"), pkg.Manifest.ID, "last").Scan(&value); err != nil || value != `"saved"` {
				t.Fatalf("data not persisted: %v", err)
			}
			page := f.call(3, "GET", "/api/admin/plugin-runtime/example.echo/pages/status", nil)
			webStatus(t, page, 200)
			if !bytes.Contains(page.Body.Bytes(), []byte("Echo page")) {
				t.Fatal("page missing")
			}
			webStatus(t, f.call(3, "GET", "/api/admin/plugin-packages/example.echo/frontend", nil), 200)
			sub := f.call(3, "POST", "/api/admin/webhooks", webhookSubscription{URL: receiver.URL, Events: []string{"character.create"}, Enabled: true})
			webStatus(t, sub, 200)
			var saved struct {
				ID     string `json:"id"`
				Secret string `json:"secret"`
			}
			json.Unmarshal(sub.Body.Bytes(), &saved)
			signingSecret = saved.Secret
			if err = f.app.queueExtensionEvent(context.Background(), sdk.Event{Name: "character.create"}); err != nil {
				t.Fatal(err)
			}
			f.app.runExtensionDeliveries(context.Background(), time.Now())
			if !delivered {
				t.Fatal("webhook not delivered")
			}
			var pending int
			if err = f.db.DB.QueryRow("SELECT COUNT(*) FROM plugin_event_deliveries WHERE state='delivered'").Scan(&pending); err != nil || pending != 1 {
				t.Fatalf("event not delivered %d %v", pending, err)
			}
			webStatus(t, f.call(3, "POST", "/api/admin/plugin-packages/example.echo/reload", nil), 200)
			webStatus(t, install(pkg.Manifest.Permissions), 200)
			webStatus(t, f.call(3, "DELETE", "/api/admin/plugin-packages/example.echo", nil), 200)
			webStatus(t, f.call(3, "POST", "/api/plugins/example.echo/data", map[string]string{}), 404)
		})
	}
}
