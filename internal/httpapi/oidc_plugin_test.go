package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOIDCPluginBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			p := newOIDCProviderFixture(t)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oidc", map[string]bool{"enabled": true}), 409)
			// Existing environment configuration must work before a plugin row exists.
			f.app.cfg.OIDC = OIDCSettings{Enabled: true, Name: "OIDC", Issuer: p.server.URL, ClientID: "test-client", ClientSecret: "synthetic-client-secret"}
			w := f.call(3, "GET", "/api/admin/plugins", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "wishingskin.oidc") {
				t.Fatal("OIDC missing from official plugins")
			}
			u, cookie := startOIDCTest(t, f, p, 0)
			webStatus(t, f.call(1, "PATCH", "/api/admin/plugins/wishingskin.oidc", map[string]bool{"enabled": false}), 403)
			// A failed audit must preserve the enabled state and pending flow.
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oidc", map[string]bool{"enabled": false}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			active, err := f.app.oidcSettings(t.Context())
			if err != nil || !active.Enabled {
				t.Fatal("unaudited plugin toggle took effect", err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oidc", map[string]bool{"enabled": false}), 200)
			w = f.call(0, "GET", "/api/site", nil)
			var site struct{ OIDC struct{ Enabled bool } }
			if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil || site.OIDC.Enabled {
				t.Fatal("disabled plugin exposed login", err)
			}
			// Disabled OIDC retains the existing service-unavailable response contract.
			webStatus(t, f.call(0, "POST", "/api/auth/oidc/start", map[string]any{}), 503)
			var flows int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM oidc_flows").Scan(&flows); err != nil || flows != 0 {
				t.Fatal("disable retained pending flows", flows, err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oidc", map[string]bool{"enabled": true}), 200)
			w = callbackOIDCTest(f, u, cookie, 0)
			if !strings.Contains(w.Header().Get("Location"), "oidcError=expired") {
				t.Fatal("re-enable resurrected a cancelled flow")
			}
			active, err = f.app.oidcSettings(t.Context())
			if err != nil || !active.Enabled || active.ClientSecret != "synthetic-client-secret" {
				t.Fatal("re-enable lost provider configuration", err)
			}
			// A new server reads persisted plugin state without relying on registry memory.
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oidc", map[string]bool{"enabled": false}), 200)
			restarted, err := New(f.db, f.app.cfg)
			if err != nil {
				t.Fatal(err)
			}
			active, err = restarted.oidcSettings(t.Context())
			if err != nil || active.Enabled {
				t.Fatal("stored disable did not override environment", err)
			}
		})
	}
}

func TestOIDCPluginDisableDuringTokenExchangeBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			p := newOIDCProviderFixture(t)
			f.app.cfg.OIDC = OIDCSettings{Enabled: true, Name: "OIDC", Issuer: p.server.URL, ClientID: "test-client", ClientSecret: "synthetic"}
			f.exec("INSERT INTO oidc_identities(identity_key,issuer_key,user_id,issuer,subject,created_at) VALUES(?,?,1,?,?,0)", oidcIdentityKey(p.server.URL, p.subject), tokenHash(p.server.URL), p.server.URL, p.subject)
			u, cookie := startOIDCTest(t, f, p, 0)
			disabled := make(chan int, 1)
			p.afterToken = func() {
				disabled <- f.call(3, "PATCH", "/api/admin/plugins/wishingskin.oidc", map[string]bool{"enabled": false}).Code
			}
			w := callbackOIDCTest(f, u, cookie, 0)
			select {
			case code := <-disabled:
				if code != 200 {
					t.Fatal("disable during exchange failed", code)
				}
			default:
				t.Fatal("token exchange was not reached")
			}
			if !strings.Contains(w.Header().Get("Location"), "oidcError=") {
				t.Fatal("plugin disabled during exchange still issued a session")
			}
			var identities int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM oidc_identities").Scan(&identities); err != nil || identities != 1 {
				t.Fatal("disable removed existing identity", err)
			}
		})
	}
}
