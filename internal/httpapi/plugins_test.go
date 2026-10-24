package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOfficialPluginsAndPointsDisablePreserveLedgerBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			response := f.call(3, "GET", "/api/admin/plugins", nil)
			webStatus(t, response, 200)
			if !strings.Contains(response.Body.String(), "wishingskin.points") || !strings.Contains(response.Body.String(), "wishingskin.invitations") {
				t.Fatal("builtin plugins missing", response.Body)
			}
			configurePoints(t, f, true, 10, 5, 7)
			f.exec("UPDATE users SET score=5 WHERE id=1")
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.points", map[string]bool{"enabled": false}), 200)
			view := f.call(1, "GET", "/api/points", nil)
			webStatus(t, view, 200)
			var payload struct{ Rules pointRules }
			if err := json.Unmarshal(view.Body.Bytes(), &payload); err != nil || payload.Rules.Enabled {
				t.Fatal("disabled points still active", view.Body.String(), err)
			}
			uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), nil))
			if pointsBalance(t, f, 1) != 5 {
				t.Fatal("disabled points changed balance")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.points", map[string]bool{"enabled": true}), 200)
			view = f.call(1, "GET", "/api/points", nil)
			webStatus(t, view, 200)
			if err := json.Unmarshal(view.Body.Bytes(), &payload); err != nil || !payload.Rules.Enabled {
				t.Fatal("re-enabled points did not restore rules", view.Body.String(), err)
			}
		})
	}
}
