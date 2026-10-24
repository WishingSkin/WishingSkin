package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestResourceAdministrationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			first := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "ManagedOne"}))
			second := webCharacter(t, f.call(2, "POST", "/api/characters", map[string]string{"name": "ManagedTwo"}))
			skin := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"isPublic": "true", "model": "slim"}))
			cape := uploadedRecord(t, f.upload(1, testPNG(t, 64, 32), map[string]string{"kind": "cape", "isPublic": "true"}))
			for kind, record := range map[string]string{"skin": skin.ID, "cape": cape.ID} {
				webStatus(t, f.call(1, "PUT", fmt.Sprintf("/api/characters/%d/textures/%s", first.ID, kind), map[string]string{"recordId": record}), 204)
			}
			webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+skin.ID, nil), 204)
			for _, path := range []string{"/api/admin/characters", "/api/admin/wardrobe"} {
				webStatus(t, f.call(0, "GET", path, nil), 401)
				webStatus(t, f.call(1, "GET", path, nil), 403)
				webStatus(t, f.call(3, "GET", path+"?pageSize=61", nil), 400)
				webStatus(t, f.call(3, "GET", path+"?q="+strings.Repeat("a", 256), nil), 400)
			}
			var list struct {
				Items []managedResource
				Total int
			}
			w := f.call(3, "GET", "/api/admin/characters?pageSize=1", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Total != 2 || len(list.Items) != 1 {
				t.Fatal("character pagination", err)
			}
			w = f.call(3, "GET", "/api/admin/characters?q="+first.UUID, nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Total != 1 || list.Items[0].UUID != first.UUID || list.Items[0].OwnerID != "1" {
				t.Fatal("uuid search", err)
			}
			path := fmt.Sprintf("/api/admin/characters/%d", first.ID)
			webStatus(t, f.call(1, "PATCH", path, map[string]string{"name": "NotAllowed"}), 403)
			webStatus(t, f.call(3, "PATCH", path, map[string]string{"name": "bad name"}), 400)
			webStatus(t, f.call(3, "PATCH", path, map[string]string{"name": second.Name}), 409)
			webStatus(t, f.call(3, "PATCH", path, map[string]bool{}), 400)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", path, map[string]any{"name": "AuditRollback", "clearSkin": true}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			var name, hash, uuid, model string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT name,skin_hash,uuid,model FROM characters WHERE id=?"), first.ID).Scan(&name, &hash, &uuid, &model); err != nil || name != first.Name || hash != skin.FileHash || uuid != first.UUID || model != "slim" {
				t.Fatal("character audit rollback", err)
			}
			webStatus(t, f.call(3, "PATCH", path, map[string]any{"name": "ManagedChanged", "clearSkin": true, "clearCape": true}), 204)
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT name,skin_hash,uuid,model FROM characters WHERE id=?"), first.ID).Scan(&name, &hash, &uuid, &model); err != nil || name != "ManagedChanged" || hash != "" || uuid != first.UUID || model != "default" {
				t.Fatal("character update identity", err)
			}
			webStatus(t, f.call(3, "PATCH", path, map[string]any{"name": "ManagedChanged", "clearSkin": true, "clearCape": true}), 204)
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE target_type='character'").Scan(&count); err != nil || count != 3 {
				t.Fatal("character audit/no-op", count, err)
			}
			// Record changes never rename the shared material or another person's collection.
			wardrobePath := "/api/admin/wardrobe/2/" + skin.ID
			webStatus(t, f.call(1, "DELETE", wardrobePath, nil), 403)
			webStatus(t, f.call(3, "PATCH", wardrobePath, map[string]string{"name": "   "}), 400)
			webStatus(t, f.call(3, "PATCH", wardrobePath, map[string]string{"name": "Admin alias 100%_"}), 204)
			w = f.call(3, "GET", "/api/admin/wardrobe?q=%25_", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Total != 1 || list.Items[0].OwnerID != "2" || list.Items[0].RecordID != skin.ID {
				t.Fatal("wardrobe literal search", err)
			}
			// Removing a retained unlisted collection revokes its grant, not existing character appearance.
			webStatus(t, f.call(2, "PUT", fmt.Sprintf("/api/characters/%d/textures/skin", second.ID), map[string]string{"recordId": skin.ID}), 204)
			webStatus(t, f.call(1, "PATCH", "/api/textures/"+skin.ID, map[string]any{"name": "Original material", "description": "", "isPublic": false}), 204)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "DELETE", wardrobePath, nil), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM wardrobe WHERE user_id=2").Scan(&count); err != nil || count != 1 {
				t.Fatal("wardrobe audit rollback", err)
			}
			webStatus(t, f.call(3, "DELETE", wardrobePath, nil), 204)
			webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+skin.ID, nil), 404)
			var remainingHash string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT skin_hash FROM characters WHERE id=?"), second.ID).Scan(&remainingHash); err != nil || remainingHash != skin.FileHash {
				t.Fatal("removed collection changed appearance", err)
			}
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE action IN ('wardrobe.rename','wardrobe.remove')").Scan(&count); err != nil || count != 2 {
				t.Fatal("wardrobe audit", err)
			}
		})
	}
}
