package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestWardrobeVisibilityBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			public := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Public", "isPublic": "true"}))
			private := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Private"}))
			path := "/api/wardrobe/" + public.ID
			webStatus(t, f.call(0, "PUT", path, nil), 401)
			webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+private.ID, nil), 404)
			webStatus(t, f.call(1, "PUT", "/api/wardrobe/"+private.ID, nil), 204)
			var wg sync.WaitGroup
			responses := make(chan *httptest.ResponseRecorder, 8)
			for range 8 {
				wg.Add(1)
				go func() { defer wg.Done(); responses <- f.call(2, "PUT", path, nil) }()
			}
			wg.Wait()
			close(responses)
			for w := range responses {
				webStatus(t, w, 204)
			}
			var count int
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT COUNT(*) FROM wardrobe WHERE record_id=?"), public.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("duplicate collection count", count, err)
			}
			webStatus(t, f.call(2, "PATCH", path, map[string]string{"name": "My alias"}), 204)
			var authorName string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT name FROM texture_records WHERE record_id=?"), public.ID).Scan(&authorName); err != nil || authorName != "Public" {
				t.Fatal("alias changed author metadata", authorName, err)
			}
			webStatus(t, f.call(2, "PUT", path, nil), 204)
			webStatus(t, f.call(3, "PATCH", path, map[string]string{"name": "Stolen"}), 404)
			webStatus(t, f.call(2, "PATCH", path, map[string]string{"name": " "}), 400)
			webStatus(t, f.call(1, "PATCH", "/api/textures/"+public.ID, map[string]any{"name": "Now unlisted", "description": "Retained", "isPublic": false}), 204)
			webStatus(t, f.call(0, "GET", "/api/library/"+public.ID, nil), 404)
			webStatus(t, f.call(2, "GET", "/api/library/"+public.ID, nil), 404)
			webStatus(t, f.call(3, "PUT", path, nil), 404)
			webStatus(t, f.call(2, "PUT", path, nil), 204)
			w := f.call(2, "GET", "/api/wardrobe", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "My alias") || !strings.Contains(w.Body.String(), "Retained") || strings.Contains(w.Body.String(), private.ID) {
				t.Fatal("retained wardrobe data or account isolation", w.Body)
			}
			c := webCharacter(t, f.call(2, "POST", "/api/characters", map[string]string{"name": "Collector"}))
			binding := fmt.Sprintf("/api/characters/%d/textures/skin", c.ID)
			webStatus(t, f.call(2, "PUT", binding, map[string]string{"recordId": public.ID}), 204)
			webStatus(t, f.call(2, "DELETE", path, nil), 204)
			webStatus(t, f.call(2, "DELETE", path, nil), 204)
			webStatus(t, f.call(2, "PUT", path, nil), 404)
			webStatus(t, f.call(2, "PUT", binding, map[string]string{"recordId": public.ID}), 404)
			webStatus(t, f.call(0, "GET", "/textures/"+public.FileHash, nil), 200)
			f.exec("UPDATE texture_records SET is_public=? WHERE record_id=?", true, public.ID)
			webStatus(t, f.call(2, "PUT", path, nil), 204)
			webStatus(t, f.call(1, "DELETE", "/api/textures/"+public.ID, nil), 204)
			w = f.call(2, "GET", "/api/wardrobe", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), public.ID) {
				t.Fatal("deleted record remained visible", w.Body)
			}
			webStatus(t, f.call(2, "PUT", binding, map[string]string{"recordId": public.ID}), 404)
			webStatus(t, f.call(2, "DELETE", path, nil), 204)
		})
	}
}

func TestPublicLibraryBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			a := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Alpha%_", "isPublic": "true"}))
			b := uploadedRecord(t, f.upload(2, testPNG(t, 64, 32), map[string]string{"name": "Beta", "kind": "cape", "isPublic": "true"}))
			private := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Private"}))
			webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+a.ID, nil), 204)
			for _, tc := range []struct {
				query, first string
				total        int
			}{
				{"", a.ID, 2}, {"?kind=cape", b.ID, 1}, {"?owner=1", a.ID, 1}, {"?q=%25_", a.ID, 1},
				{"?sort=collections&pageSize=1", a.ID, 2}, {"?sort=name-desc&pageSize=1", b.ID, 2}, {"?page=2&pageSize=1", b.ID, 2},
			} {
				w := f.call(0, "GET", "/api/library"+tc.query, nil)
				webStatus(t, w, 200)
				var result struct {
					Textures []struct {
						ID string `json:"id"`
					} `json:"textures"`
					Total int `json:"total"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Total != tc.total || len(result.Textures) == 0 || result.Textures[0].ID != tc.first {
					t.Fatal(tc.query, w.Body, err)
				}
				if strings.Contains(w.Body.String(), private.ID) || strings.Contains(w.Body.String(), "@example.test") {
					t.Fatal("private metadata leaked", w.Body)
				}
			}
			webStatus(t, f.call(0, "GET", "/api/library/"+a.ID, nil), 200)
			var detail libraryRecord
			if err := json.Unmarshal(f.call(0, "GET", "/api/library/"+a.ID, nil).Body.Bytes(), &detail); err != nil || detail.CollectionCount != 1 || detail.OwnerID != "1" {
				t.Fatal("collection count or owner identity", detail, err)
			}
			webStatus(t, f.call(2, "DELETE", "/api/wardrobe/"+a.ID, nil), 204)
			if err := json.Unmarshal(f.call(0, "GET", "/api/library/"+a.ID, nil).Body.Bytes(), &detail); err != nil || detail.CollectionCount != 0 {
				t.Fatal("count did not follow removal", detail, err)
			}
			for _, query := range []string{"?kind=bad", "?sort=DROP", "?owner=-1", "?page=0", "?pageSize=1000", "?page=99999999999999999999999"} {
				webStatus(t, f.call(0, "GET", "/api/library"+query, nil), 400)
			}
			f.exec("UPDATE textures SET deleted=? WHERE hash=?", true, a.FileHash)
			webStatus(t, f.call(0, "GET", "/api/library/"+a.ID, nil), 404)
			webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+a.ID, nil), 404)
		})
	}
}

func TestWardrobeWriteGuardsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			item := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "Public", "isPublic": "true"}))
			path := "/api/wardrobe/" + item.ID
			for _, method := range []string{"PUT", "PATCH", "DELETE"} {
				for _, guard := range []string{"csrf", "origin", "fetch-site"} {
					r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(`{"name":"alias"}`))
					token := fmt.Sprintf("%064d", 2)
					r.AddCookie(&http.Cookie{Name: "ws_session", Value: token})
					r.Header.Set("Origin", "http://example.test")
					r.Header.Set("X-CSRF-Token", csrf(token))
					switch guard {
					case "csrf":
						r.Header.Del("X-CSRF-Token")
					case "origin":
						r.Header.Set("Origin", "https://other.test")
					case "fetch-site":
						r.Header.Set("Sec-Fetch-Site", "cross-site")
					}
					w := httptest.NewRecorder()
					f.app.ServeHTTP(w, r)
					webStatus(t, w, 403)
				}
			}
			webStatus(t, f.call(2, "GET", "/api/wardrobe", nil), 200)
			webStatus(t, f.call(2, "PUT", path, nil), 204)
			webStatus(t, f.call(2, "PATCH", path, map[string]string{"name": strings.Repeat("😀", 255)}), 204)
			webStatus(t, f.call(2, "PATCH", path, map[string]string{"name": strings.Repeat("😀", 256)}), 400)
			f.exec("UPDATE users SET banned=? WHERE id=?", true, 2)
			webStatus(t, f.call(2, "GET", "/api/wardrobe", nil), 401)
			webStatus(t, f.call(2, "PUT", path, nil), 401)
		})
	}
}
