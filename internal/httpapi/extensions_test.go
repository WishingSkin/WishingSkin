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
	"strings"
	"sync/atomic"
	"testing"
	"time"
	rp "github.com/WishingSkin/WishingSkin/internal/runtimeplugin"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
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
	pkg := rp.Package{Manifest: sdk.Manifest{ID: "example.echo", Name: "Echo", Version: "1.0.0", SettingsNamespace: "echo", MigrationVersion: 1, Permissions: []string{"routes.admin", "database", "events", "pages", "tasks", "frontend", "webhooks"}, Events: []string{"character.create"}, Pages: []sdk.PageContribution{{ID: "status", Area: "admin.plugins", Label: "Status"}}, Tasks: []sdk.TaskDeclaration{{ID: "check"}}}, Protocol: 1, Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: filepath.Base(exe)}, Frontend: "index.html", Routes: []rp.Route{{Method: "POST", Path: "/data", Access: "admin"}}, Migrations: []rp.Migration{{Version: 1, MySQL: []string{"CREATE TABLE IF NOT EXISTS {{prefix}}notes (id BIGINT AUTO_INCREMENT PRIMARY KEY, content BLOB, label VARCHAR(100))"}, Postgres: []string{"CREATE TABLE IF NOT EXISTS {{prefix}}notes (id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, content BYTEA, label VARCHAR(100))"}}}}
	archive := pluginTestArchive(t, pkg, filepath.Base(exe), binary)
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			defer f.app.dynamic.Close()
			var signingSecret string
			var delivered atomic.Bool
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				mac := hmac.New(sha256.New, []byte(signingSecret))
				fmt.Fprint(mac, r.Header.Get("X-WishingSkin-Timestamp")+"."+r.Header.Get("X-WishingSkin-Delivery")+"."+string(data))
				if r.Header.Get("X-WishingSkin-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
					w.WriteHeader(400)
					return
				}
				delivered.Store(true)
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
				return postPluginTestPackage(f, "/api/admin/plugin-market/install", strings.NewReader(`{"id":"example.echo"}`), rp.Digest(archive), grants)
			}
			webStatus(t, install(nil), 409)
			webStatus(t, install(pkg.Manifest.Permissions), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/example.echo", map[string]bool{"enabled": true}), 200)
			webStatus(t, f.call(1, "POST", "/api/plugins/example.echo/data", map[string]string{}), 403)
			webStatus(t, f.call(3, "GET", "/api/plugins/example.echo/data", nil), 404)
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
			frontend := f.call(3, "GET", "/api/admin/plugin-packages/example.echo/frontend", nil)
			webStatus(t, frontend, 200)
			var bridge struct{ Digest string }
			if err = json.Unmarshal(frontend.Body.Bytes(), &bridge); err != nil || bridge.Digest != rp.Digest(archive) {
				t.Fatal("frontend package version missing", err)
			}
			maxOpen := f.db.DB.Stats().MaxOpenConnections
			f.db.DB.SetMaxOpenConns(1)
			bridgeResponse := postPluginTestPackage(f, "/api/plugins/example.echo/data", strings.NewReader(`{}`), bridge.Digest, nil)
			f.db.DB.SetMaxOpenConns(maxOpen)
			webStatus(t, bridgeResponse, 200)
			webStatus(t, postPluginTestPackage(f, "/api/plugins/example.echo/data", strings.NewReader(`{}`), rp.Digest([]byte("previous package")), nil), 409)
			sub := f.call(3, "POST", "/api/admin/webhooks", webhookSubscription{URL: receiver.URL, Events: []string{"character.create"}, Enabled: true})
			webStatus(t, sub, 200)
			var saved struct {
				ID     string `json:"id"`
				Secret string `json:"secret"`
			}
			json.Unmarshal(sub.Body.Bytes(), &saved)
			signingSecret = saved.Secret
			f.db.DB.SetMaxOpenConns(1)
			enqueueCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = f.app.queueExtensionEvent(enqueueCtx, sdk.Event{Name: "character.create"})
			cancel()
			f.db.DB.SetMaxOpenConns(maxOpen)
			if err != nil {
				t.Fatal(err)
			}
			f.app.runExtensionDeliveries(context.Background(), time.Now())
			if !delivered.Load() {
				t.Fatal("webhook not delivered")
			}
			var pending int
			if err = f.db.DB.QueryRow("SELECT COUNT(*) FROM plugin_event_deliveries WHERE state='delivered'").Scan(&pending); err != nil || pending != 1 {
				t.Fatalf("event not delivered %d %v", pending, err)
			}
			webStatus(t, f.call(3, "POST", "/api/admin/plugin-packages/example.echo/reload", nil), 200)
			webStatus(t, install(pkg.Manifest.Permissions), 200)

			incompatible := pkg
			incompatible.Manifest.Version = "1.1.0"
			incompatible.Migrations = []rp.Migration{{Version: 1, MySQL: []string{"CREATE TABLE {{prefix}}different (id INTEGER)"}, Postgres: []string{"CREATE TABLE {{prefix}}different (id INTEGER)"}}}
			candidate := pluginTestArchive(t, incompatible, filepath.Base(exe), binary)
			webStatus(t, postPluginTestPackage(f, "/api/admin/plugin-packages/install", bytes.NewReader(candidate), rp.Digest(candidate), pkg.Manifest.Permissions), 409)
			webStatus(t, f.call(3, "POST", "/api/plugins/example.echo/data", nil), 200)

			upgrade := pkg
			upgrade.Manifest.Version = "1.1.0"
			upgrade.Manifest.MigrationVersion = 2
			upgrade.Migrations = append(append([]rp.Migration(nil), pkg.Migrations...), rp.Migration{
				Version:  2,
				MySQL:    []string{"CREATE INDEX {{prefix}}notes_label ON {{prefix}}notes (label)"},
				Postgres: []string{"CREATE INDEX IF NOT EXISTS {{prefix}}notes_label ON {{prefix}}notes (label)"},
			})
			upgradeArchive := pluginTestArchive(t, upgrade, filepath.Base(exe), binary)
			installArchive := func(data []byte) *httptest.ResponseRecorder {
				return postPluginTestPackage(f, "/api/admin/plugin-packages/install", bytes.NewReader(data), rp.Digest(data), pkg.Manifest.Permissions)
			}
			// Make the staged index unwritable until the administrator retries the upgrade.
			blockedIndex := filepath.Join(f.app.cfg.TextureDir, ".plugins", "installed.json.new")
			if err := os.Mkdir(blockedIndex, 0700); err != nil {
				t.Fatal(err)
			}
			failed := installArchive(upgradeArchive)
			if err := os.Remove(blockedIndex); err != nil {
				t.Fatal(err)
			}
			webStatus(t, failed, 409)
			var recovery struct{ RecoveryRequired, RetrySamePackage bool }
			if err := json.Unmarshal(failed.Body.Bytes(), &recovery); err != nil || !recovery.RecoveryRequired || !recovery.RetrySamePackage {
				t.Fatal("failed upgrade did not report recovery", err)
			}
			webStatus(t, f.call(3, "POST", "/api/plugins/example.echo/data", nil), 404)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/example.echo", map[string]bool{"enabled": true}), 409)
			webStatus(t, installArchive(archive), 409)
			webStatus(t, installArchive(upgradeArchive), 200)
			webStatus(t, f.call(3, "POST", "/api/plugins/example.echo/data", nil), 200)

			verifyDynamicBackup(t, f, upgrade, upgradeArchive, true)
			webStatus(t, f.call(3, "DELETE", "/api/admin/plugin-packages/example.echo", nil), 200)
			verifyDynamicBackup(t, f, upgrade, upgradeArchive, false)
			webStatus(t, f.call(3, "POST", "/api/plugins/example.echo/data", map[string]string{}), 404)
		})
	}
}

func pluginTestArchive(t *testing.T, pkg rp.Package, executable string, binary []byte) []byte {
	t.Helper()
	manifest, _ := json.Marshal(pkg)
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"plugin.json", manifest}, {executable, binary}, {"index.html", []byte("<h1>Echo</h1>")}} {
		f, e := z.Create(entry.name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(entry.data); e != nil {
			t.Fatal(e)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func postPluginTestPackage(f *managementApp, path string, body io.Reader, digest string, grants []string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://example.test"+path, body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://example.test")
	r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 3)})
	r.Header.Set("X-CSRF-Token", csrf(fmt.Sprintf("%064d", 3)))
	r.Header.Set("X-Plugin-Digest", digest)
	raw, _ := json.Marshal(grants)
	r.Header.Set("X-Plugin-Permissions", string(raw))
	w := httptest.NewRecorder()
	f.app.ServeHTTP(w, r)
	return w
}

func TestPausedSubscriptionDoesNotBlockWebhookDeliveryBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			var pausedCalls, activeCalls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/paused" {
					pausedCalls.Add(1)
				} else {
					activeCalls.Add(1)
				}
				w.WriteHeader(204)
			}))
			defer receiver.Close()
			paused := webhookSubscription{URL: receiver.URL + "/paused", Events: []string{"character.create"}, Enabled: true}
			sub := f.call(3, "POST", "/api/admin/webhooks", paused)
			webStatus(t, sub, 200)
			var saved struct{ ID string }
			if err := json.Unmarshal(sub.Body.Bytes(), &saved); err != nil || saved.ID == "" {
				t.Fatal("subscription id missing", err)
			}
			enqueue := func() {
				t.Helper()
				if err := f.app.queueExtensionEvent(context.Background(), sdk.Event{Name: "character.create"}); err != nil {
					t.Fatal(err)
				}
			}
			for range 4 {
				enqueue()
			}
			f.exec("UPDATE webhook_deliveries SET created_at=created_at-10 WHERE subscription_id=?", saved.ID)
			path := "/api/admin/webhooks/" + saved.ID
			paused.Enabled = false
			webStatus(t, f.call(3, "PUT", path, paused), 200)
			webStatus(t, f.call(3, "POST", "/api/admin/webhooks", webhookSubscription{URL: receiver.URL + "/active", Events: paused.Events, Enabled: true}), 200)
			enqueue()
			f.app.runExtensionDeliveries(context.Background(), time.Now())
			if pausedCalls.Load() != 0 || activeCalls.Load() != 1 {
				t.Fatal("the paused backlog occupied the delivery window")
			}
			paused.Enabled = true
			webStatus(t, f.call(3, "PUT", path, paused), 200)
			f.app.runExtensionDeliveries(context.Background(), time.Now())
			if pausedCalls.Load() != 4 {
				t.Fatal("paused deliveries did not resume")
			}
			enqueue()
			webStatus(t, f.call(3, "DELETE", path, nil), 200)
			f.app.runExtensionDeliveries(context.Background(), time.Now())
			if pausedCalls.Load() != 4 || activeCalls.Load() != 2 {
				t.Fatal("deleted subscription still received queued events")
			}
		})
	}
}
