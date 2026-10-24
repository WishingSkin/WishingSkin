package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOwnedTextureRecordsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			data := testPNG(t, 64, 64)
			own := uploadedRecord(t, f.upload(1, data, map[string]string{"name": "First"}))
			other := uploadedRecord(t, f.upload(2, data, map[string]string{"name": "Other"}))
			path := "/api/textures/" + own.ID
			body := map[string]any{"name": "Updated", "description": strings.Repeat("😀", 200), "isPublic": true}
			for _, method := range []string{"PATCH", "DELETE"} {
				webStatus(t, f.call(0, method, path, body), 401)
				webStatus(t, f.call(2, method, path, body), 404)
				webStatus(t, f.call(3, method, path, body), 404)
				r := httptest.NewRequest(method, "http://example.test"+path, bytes.NewReader([]byte(`{}`)))
				r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 1)})
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				webStatus(t, w, 403)
			}
			for _, invalid := range []any{
				map[string]any{"name": " ", "description": "", "isPublic": false},
				map[string]any{"name": strings.Repeat("名", 256), "description": "", "isPublic": false},
				map[string]any{"name": "Valid", "description": strings.Repeat("😀", 201), "isPublic": false},
				map[string]any{"name": "Valid", "description": ""},
				map[string]any{"name": "Valid", "description": "", "isPublic": "yes"},
			} {
				webStatus(t, f.call(1, "PATCH", path, invalid), 400)
			}
			for range 2 {
				webStatus(t, f.call(1, "PATCH", path, body), 204)
			}
			var name, description string
			var public bool
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT name,description,is_public FROM texture_records WHERE record_id=?"), own.ID).Scan(&name, &description, &public); err != nil || name != "Updated" || description != strings.Repeat("😀", 200) || !public {
				t.Fatal("metadata update mismatch", err)
			}
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT name,is_public FROM texture_records WHERE record_id=?"), other.ID).Scan(&name, &public); err != nil || name != "Other" || public {
				t.Fatal("shared file changed another record", err)
			}
			f.exec("UPDATE textures SET deleted=? WHERE hash=?", true, own.FileHash)
			webStatus(t, f.call(1, "PATCH", path, body), 404)
			webStatus(t, f.call(0, "GET", "/textures/"+own.FileHash, nil), 404)
			f.exec("UPDATE textures SET deleted=? WHERE hash=?", false, own.FileHash)
			var list struct {
				Textures []struct {
					ID    string `json:"id"`
					Owned bool   `json:"owned"`
				} `json:"textures"`
			}
			w := f.call(1, "GET", "/api/textures", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Textures) != 1 || !list.Textures[0].Owned {
				t.Fatal("owned record not marked", w.Body, err)
			}
			f.exec("UPDATE texture_records SET owner_id=NULL WHERE record_id=?", other.ID)
			webStatus(t, f.call(1, "PATCH", "/api/textures/"+other.ID, body), 404)
			c := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "KeptAppearance"}))
			bindingPath := fmt.Sprintf("/api/characters/%d/textures/skin", c.ID)
			webStatus(t, f.call(1, "PUT", bindingPath, map[string]string{"recordId": own.ID}), 204)
			w = f.call(1, "GET", "/api/textures", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Textures) != 2 {
				t.Fatal("legacy bound record missing", w.Body, err)
			}
			for _, item := range list.Textures {
				if item.ID == other.ID && item.Owned {
					t.Fatal("legacy record ownership inferred from binding")
				}
			}
			for range 2 {
				webStatus(t, f.call(1, "DELETE", path, nil), 204)
			}
			webStatus(t, f.call(1, "PATCH", path, body), 404)
			webStatus(t, f.call(0, "GET", "/textures/"+own.FileHash, nil), 200)
			f.exec("UPDATE texture_records SET deleted=? WHERE record_id=?", true, other.ID)
			webStatus(t, f.call(0, "GET", "/textures/"+own.FileHash, nil), 200)
			webStatus(t, f.call(0, "GET", "/api/yggdrasil/sessionserver/session/minecraft/profile/"+c.UUID, nil), 200)
			var hash, uuid string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT skin_hash,uuid FROM characters WHERE id=?"), c.ID).Scan(&hash, &uuid); err != nil || hash != own.FileHash || uuid != c.UUID {
				t.Fatal("record deletion changed existing appearance", err)
			}
			webStatus(t, f.call(1, "PUT", bindingPath, map[string]string{"recordId": own.ID}), 404)
			webStatus(t, f.call(1, "PUT", bindingPath, map[string]string{"recordId": ""}), 204)
			for _, method := range []string{"GET", "HEAD"} {
				r := httptest.NewRequest(method, "http://example.test/textures/"+own.FileHash, nil)
				r.Header.Set("If-None-Match", `"`+own.FileHash+`"`)
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				webStatus(t, w, 404)
			}
		})
	}
}
