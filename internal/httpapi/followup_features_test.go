package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistrationDomainAndPermanentInvitationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "open", "emailDomains": []string{"allowed.example.test"}}), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/register", registrationBody("blocked@other.test")), 400)
			body := registrationBody("first@allowed.example.test")
			webStatus(t, f.call(0, "POST", "/api/auth/register", body), 201)
			if mailbox.token() == "" {
				t.Fatal("verification mail missing")
			}
			clearMailLimits(f)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/invitations", map[string]bool{"enabled": true, "required": true}), 200)
			response := f.call(3, "POST", "/api/admin/plugins/invitations/codes", map[string]any{"count": 1, "uses": 1, "hours": 0, "permanent": true})
			webStatus(t, response, 201)
			var generated struct {
				Codes     []string `json:"codes"`
				ExpiresAt int64    `json:"expiresAt"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &generated); err != nil || len(generated.Codes) != 1 || generated.ExpiresAt != 0 {
				t.Fatalf("permanent invitation response: %s", response.Body)
			}
			clearMailLimits(f)
			second := registrationBody("second@allowed.example.test")
			second["invitation"] = generated.Codes[0]
			webStatus(t, f.call(0, "POST", "/api/auth/register", second), 201)
			clearMailLimits(f)
			third := registrationBody("third@allowed.example.test")
			third["invitation"] = generated.Codes[0]
			webStatus(t, f.call(0, "POST", "/api/auth/register", third), 400)
		})
	}
}

func TestInvitationRepeatableExportsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/invitations", map[string]bool{"enabled": true, "required": true}), 200)
			response := f.call(3, "POST", "/api/admin/plugins/invitations/codes", map[string]int{"count": 2})
			webStatus(t, response, 201)
			var generated struct {
				Codes []string `json:"codes"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &generated); err != nil || len(generated.Codes) != 2 {
				t.Fatalf("generated invitations: %s", response.Body)
			}
			for _, format := range []string{"txt", "csv"} {
				for _, status := range []string{"unused", "all"} {
					response = f.call(3, "GET", "/api/admin/plugins/invitations/export?status="+status+"&format="+format, nil)
					webStatus(t, response, 200)
					for _, code := range generated.Codes {
						if !strings.Contains(response.Body.String(), code) {
							t.Fatalf("%s %s export omitted code", status, format)
						}
					}
				}
			}
			var target string
			if err := f.db.DB.QueryRow("SELECT target_id FROM admin_events WHERE action='invitation.export' ORDER BY event_id DESC LIMIT 1").Scan(&target); err != nil || strings.Contains(target, generated.Codes[0]) {
				t.Fatalf("export audit leaked code: %q %v", target, err)
			}
			f.app.cfg.SettingsKey = nil
			webStatus(t, f.call(3, "GET", "/api/admin/plugins/invitations/export?status=all&format=txt", nil), 503)
		})
	}
}

func TestSearchAndAuditExportBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Searchable skin", "description": "needle description", "isPublic": "true"}))
			webStatus(t, f.call(1, "PUT", "/api/wardrobe/"+record.ID, nil), 204)
			webStatus(t, f.call(1, "PATCH", "/api/wardrobe/"+record.ID, map[string]string{"name": "Wardrobe needle"}), 204)
			var textures struct {
				Textures []textureRecord `json:"textures"`
			}
			if err := json.Unmarshal(f.call(1, "GET", "/api/textures?q=needle", nil).Body.Bytes(), &textures); err != nil || len(textures.Textures) != 1 || textures.Textures[0].ID != record.ID {
				t.Fatal("own texture search", err)
			}
			var wardrobe struct {
				Textures []libraryRecord `json:"textures"`
			}
			if err := json.Unmarshal(f.call(1, "GET", "/api/wardrobe?q=needle", nil).Body.Bytes(), &wardrobe); err != nil || len(wardrobe.Textures) != 1 || wardrobe.Textures[0].DisplayName != "Wardrobe needle" {
				t.Fatal("wardrobe search", err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "closed"}), 204)
			if response := f.call(3, "GET", "/api/admin/events?q=registration", nil); response.Code != 200 || !strings.Contains(response.Body.String(), "registration.closed") {
				t.Fatalf("event search: %d %s", response.Code, response.Body)
			}
			response := f.call(3, "GET", "/api/admin/events/export?q=registration", nil)
			if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Type"), "text/csv") {
				t.Fatalf("event export: %d %s", response.Code, response.Body)
			}
			rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(response.Body.String(), "\ufeff"))).ReadAll()
			if err != nil || len(rows) < 2 || rows[0][2] != "action" || rows[1][2] != "registration.closed" {
				t.Fatalf("event csv: %v %s", err, response.Body)
			}
		})
	}
}
