package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestImageModerationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			data := testPNG(t, 64, 64)
			first := uploadedRecord(t, f.upload(1, data, map[string]string{"name": "Moderate 100%_", "isPublic": "true"}))
			second := uploadedRecord(t, f.upload(2, data, map[string]string{"name": "Same private file"}))
			other := uploadedRecord(t, f.upload(1, testPNG(t, 64, 32), map[string]string{"name": "Unrelated", "isPublic": "true"}))
			c := webCharacter(t, f.call(2, "POST", "/api/characters", map[string]string{"name": "ModerationTest"}))
			webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+first.ID, nil), 204)
			bindPath := fmt.Sprintf("/api/characters/%d/textures/skin", c.ID)
			webStatus(t, f.call(2, "PUT", bindPath, map[string]string{"recordId": first.ID}), 204)
			path := "/api/admin/textures/" + first.FileHash + "/disable"
			webStatus(t, f.call(0, "GET", "/api/admin/textures", nil), 401)
			webStatus(t, f.call(1, "GET", "/api/admin/textures", nil), 403)
			webStatus(t, f.call(1, "POST", path, nil), 403)
			var listing struct {
				Textures []adminTexture
				Total    int
			}
			w := f.call(3, "GET", "/api/admin/textures?q="+first.FileHash+"&pageSize=1", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil || listing.Total != 2 || len(listing.Textures) != 1 || listing.Textures[0].RecordCount != 2 || listing.Textures[0].CollectionCount != 1 || listing.Textures[0].CharacterCount != 1 {
				t.Fatal("impact listing", listing, err)
			}
			w = f.call(3, "GET", "/api/admin/textures?q=%25_", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil || listing.Total != 1 {
				t.Fatal("literal search", listing, err)
			}
			webStatus(t, f.call(0, "GET", "/textures/"+first.FileHash, nil), 200)
			// Audit failure rolls back the shared file flag and all existing access remains valid.
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "POST", path, nil), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			webStatus(t, f.call(0, "GET", "/textures/"+first.FileHash, nil), 200)
			var wg sync.WaitGroup
			results := make(chan int, 2)
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); results <- f.call(3, "POST", path, nil).Code }()
			}
			wg.Wait()
			close(results)
			for status := range results {
				if status != 204 {
					t.Fatal("concurrent disable", status)
				}
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE action='texture.disable'").Scan(&count); err != nil || count != 1 {
				t.Fatal("duplicate audit", count, err)
			}
			webStatus(t, f.call(0, "GET", "/textures/"+first.FileHash, nil), 404)
			r := httptest.NewRequest("GET", "http://example.test/textures/"+first.FileHash, nil)
			r.Header.Set("If-None-Match", `"`+first.FileHash+`"`)
			cached := httptest.NewRecorder()
			f.app.ServeHTTP(cached, r)
			webStatus(t, cached, 404)
			webStatus(t, f.call(0, "GET", "/textures/"+other.FileHash, nil), 200)
			for _, view := range []struct {
				user int
				path string
			}{{0, "/api/library"}, {1, "/api/textures"}, {2, "/api/textures"}, {2, "/api/wardrobe"}} {
				w := f.call(view.user, "GET", view.path, nil)
				webStatus(t, w, 200)
				if strings.Contains(w.Body.String(), first.ID) || strings.Contains(w.Body.String(), second.ID) {
					t.Fatal("disabled record accessible", view.path, w.Body)
				}
			}
			w = f.call(2, "GET", "/api/characters", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), first.FileHash) {
				t.Fatal("disabled appearance returned", w.Body)
			}
			w = f.call(0, "GET", "/api/yggdrasil/sessionserver/session/minecraft/profile/"+c.UUID, nil)
			webStatus(t, w, 200)
			var profile struct{ Properties []struct{ Value string } }
			if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || len(profile.Properties) != 1 {
				t.Fatal("profile", err)
			}
			payload, err := base64.StdEncoding.DecodeString(profile.Properties[0].Value)
			if err != nil || strings.Contains(string(payload), first.FileHash) {
				t.Fatal("disabled signed texture", err, string(payload))
			}
			webStatus(t, f.call(2, "PUT", bindPath, map[string]string{"recordId": first.ID}), 404)
			webStatus(t, f.call(1, "PUT", "/api/wardrobe/"+second.ID, nil), 404)
			webStatus(t, f.upload(1, data, map[string]string{}), 409)
			if _, err := os.Stat(filepath.Join(f.app.cfg.TextureDir, first.FileHash)); err != nil {
				t.Fatal("original removed", err)
			}
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT COUNT(*) FROM texture_records WHERE file_hash=?"), first.FileHash).Scan(&count); err != nil || count != 2 {
				t.Fatal("records removed", err)
			}
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM wardrobe").Scan(&count); err != nil || count != 1 {
				t.Fatal("collection deleted", err)
			}
			w = f.call(3, "GET", "/api/admin/textures?q="+first.FileHash, nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil || listing.Total != 2 || !listing.Textures[0].Disabled {
				t.Fatal("disabled administration state", err)
			}
		})
	}
}

func TestImageModerationConcurrentWritesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			data := testPNG(t, 64, 64)
			record := uploadedRecord(t, f.upload(1, data, map[string]string{"isPublic": "true"}))
			c := webCharacter(t, f.call(2, "POST", "/api/characters", map[string]string{"name": "ModerationRace"}))
			start := make(chan struct{})
			var wg sync.WaitGroup
			for _, call := range []func() int{
				func() int { return f.upload(2, data, map[string]string{}).Code },
				func() int { return f.call(2, "PUT", "/api/wardrobe/"+record.ID, nil).Code },
				func() int {
					return f.call(2, "PUT", fmt.Sprintf("/api/characters/%d/textures/skin", c.ID), map[string]string{"recordId": record.ID}).Code
				},
				func() int { return f.call(3, "POST", "/api/admin/textures/"+record.FileHash+"/disable", nil).Code },
			} {
				wg.Add(1)
				go func(call func() int) {
					defer wg.Done()
					<-start
					status := call()
					if status != 201 && status != 204 && status != 404 && status != 409 {
						t.Errorf("concurrent status %d", status)
					}
				}(call)
			}
			close(start)
			wg.Wait()
			webStatus(t, f.call(0, "GET", "/textures/"+record.FileHash, nil), 404)
			for _, path := range []string{"/api/characters", "/api/textures", "/api/wardrobe"} {
				w := f.call(2, "GET", path, nil)
				webStatus(t, w, 200)
				if strings.Contains(w.Body.String(), record.FileHash) {
					t.Fatal("concurrent write bypassed moderation", path, w.Body)
				}
			}
			webStatus(t, f.upload(2, data, map[string]string{}), 409)
		})
	}
}
