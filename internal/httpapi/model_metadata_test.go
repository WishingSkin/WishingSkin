package httpapi

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestAdminModelMetadataBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			character := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "ModelPreview"}))
			skin := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"model": "slim", "isPublic": "true"}))
			cape := uploadedRecord(t, f.upload(1, testPNG(t, 64, 32), map[string]string{"kind": "cape", "isPublic": "true"}))
			webStatus(t, f.call(1, "PUT", fmt.Sprintf("/api/characters/%d/textures/skin", character.ID), map[string]string{"recordId": skin.ID}), 204)
			for _, record := range []string{skin.ID, cape.ID} {
				webStatus(t, f.call(2, "PUT", "/api/wardrobe/"+record, nil), 204)
			}
			checks := []struct {
				path, key string
				count     int
			}{{"/api/admin/characters?q=ModelPreview", "items", 1}, {"/api/admin/textures", "textures", 2}, {"/api/admin/wardrobe", "items", 2}}
			for _, check := range checks {
				w := f.call(3, "GET", check.path, nil)
				webStatus(t, w, 200)
				var body map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				var items []map[string]any
				if err := json.Unmarshal(body[check.key], &items); err != nil || len(items) != check.count {
					t.Fatalf("%s count %d: %v", check.path, len(items), err)
				}
				for _, item := range items {
					if check.path == "/api/admin/characters?q=ModelPreview" {
						if item["model"] != "slim" {
							t.Fatalf("character model missing: %v", item["model"])
						}
					} else if item["fileHash"] == skin.FileHash {
						if item["kind"] != "skin" || item["model"] != "slim" {
							t.Fatalf("skin metadata missing: %v %v", item["kind"], item["model"])
						}
					} else if item["fileHash"] == cape.FileHash {
						if item["kind"] != "cape" {
							t.Fatalf("cape metadata missing: %v", item["kind"])
						}
					} else {
						t.Fatal("unexpected material")
					}
				}
			}
		})
	}
}
