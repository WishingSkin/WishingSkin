package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type registrationRuleChallenge struct{ change func() }

func (c registrationRuleChallenge) Verify(context.Context, string, string, string) error {
	c.change()
	return nil
}

func TestRegistrationDomainExclusionsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, _ := accessApp(t, dialect)
			path := "/api/admin/registration"
			body := map[string]any{"mode": "open", "emailDomains": []string{"allowed.test", "blocked.test"}, "blockedEmailDomains": []string{"@BLOCKED.TEST", "blocked.test"}}
			webStatus(t, f.call(1, "PATCH", path, body), 403)
			webStatus(t, f.call(3, "PATCH", path, body), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/register", registrationBody("denied@blocked.test")), 400)
			allowed, err := f.app.registrationEmailAllowed(context.Background(), f.db.DB, "ok@allowed.test")
			if err != nil || !allowed {
				t.Fatal("allowlist lost valid domain", err)
			}
			allowed, err = f.app.registrationEmailAllowed(context.Background(), f.db.DB, "denied@other.test")
			if err != nil || allowed {
				t.Fatal("allowlist bypassed", err)
			}
			response := f.call(3, "GET", "/api/site", nil)
			webStatus(t, response, 200)
			var site struct {
				Blocked []string `json:"blockedEmailDomains"`
			}
			if json.Unmarshal(response.Body.Bytes(), &site) != nil || len(site.Blocked) != 1 || site.Blocked[0] != "blocked.test" {
				t.Fatal("blocked domains not normalized")
			}
			webStatus(t, f.call(3, "PATCH", path, map[string]any{"mode": "open", "emailDomains": []string{}}), 204)
			allowed, err = f.app.registrationEmailAllowed(context.Background(), f.db.DB, "ok@sub.blocked.test")
			if err != nil || !allowed {
				t.Fatal("blocklist must match full domain", err)
			}
			allowed, err = f.app.registrationEmailAllowed(context.Background(), f.db.DB, "denied@blocked.test")
			if err != nil || allowed {
				t.Fatal("omitted blocklist was cleared", err)
			}
			webStatus(t, f.call(3, "PATCH", path, map[string]any{"mode": "open", "blockedEmailDomains": []string{"*.test"}}), 400)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", path, map[string]any{"mode": "open", "blockedEmailDomains": []string{}}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			allowed, err = f.app.registrationEmailAllowed(context.Background(), f.db.DB, "denied@blocked.test")
			if err != nil || allowed {
				t.Fatal("audit failure changed rules", err)
			}
			webStatus(t, f.call(3, "PATCH", path, map[string]any{"mode": "open", "blockedEmailDomains": []string{}}), 204)
			allowed, err = f.app.registrationEmailAllowed(context.Background(), f.db.DB, "ok@blocked.test")
			if err != nil || !allowed {
				t.Fatal("clearing blocklist failed", err)
			}
			if strings.Contains(f.call(0, "GET", "/api/site", nil).Body.String(), "@BLOCKED") {
				t.Fatal("noncanonical domain exposed")
			}
			// Policy changes after initial input validation must still block insertion.
			f.app.cfg.Challenge = registrationRuleChallenge{change: func() {
				f.exec("UPDATE settings SET value=? WHERE setting_key='registration_blocked_domains'", `["race.test"]`)
			}}
			webStatus(t, f.call(0, "POST", "/api/auth/register", registrationBody("race@race.test")), 400)
			var inserted int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM users WHERE email='race@race.test'").Scan(&inserted); err != nil || inserted != 0 {
				t.Fatal("registration bypassed transaction policy recheck", err)
			}
		})
	}
}
