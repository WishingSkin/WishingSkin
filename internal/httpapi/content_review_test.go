package httpapi

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestContentReviewBoundariesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			w := f.call(0, "GET", "/api/content-policy", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"reviewUploads":false`) {
				t.Fatal("review enabled by default")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/content-policy", contentPolicy{ReviewUploads: true, Categories: []string{"风景"}}), 200)
			image := testPNG(t, 64, 64)
			record := uploadedRecord(t, f.upload(1, image, map[string]string{"name": "Review", "isPublic": "true", "category": "风景", "tags": "sky,风景"}))
			if record.ReviewState != "pending" {
				t.Fatal("review bypassed")
			}
			character := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "ReviewPlayer"}))
			binding := fmt.Sprintf("/api/characters/%d/textures/skin", character.ID)
			for _, path := range []string{"/textures/" + record.FileHash, "/csl/textures/" + record.FileHash, "/api/library/" + record.ID} {
				webStatus(t, f.call(0, "GET", path, nil), 404)
			}
			webStatus(t, f.call(1, "PUT", binding, map[string]string{"recordId": record.ID}), 404)
			webStatus(t, f.call(1, "PUT", "/api/wardrobe/"+record.ID, nil), 404)
			webStatus(t, f.call(1, "GET", "/api/admin/review/"+record.ID+"/image", nil), 403)
			webStatus(t, f.call(3, "GET", "/api/admin/review/"+record.ID+"/image", nil), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/review/"+record.ID, map[string]string{"state": "approved"}), 204)
			webStatus(t, f.call(0, "GET", "/textures/"+record.FileHash, nil), 200)
			webStatus(t, f.call(1, "PUT", binding, map[string]string{"recordId": record.ID}), 204)
			fallback := f.call(0, "GET", "/api/avatars/1.png", nil)
			webStatus(t, fallback, 200)
			webStatus(t, f.call(1, "PATCH", "/api/account/profile", map[string]string{"nickname": "Owner", "avatarMode": "character", "characterUUID": character.UUID}), 200)
			avatar := f.call(0, "GET", "/api/avatars/1.png", nil)
			webStatus(t, avatar, 200)
			if bytes.Equal(avatar.Body.Bytes(), fallback.Body.Bytes()) {
				t.Fatal("approved skin missing from avatar")
			}
			legacy := f.call(0, "GET", "/csl/ReviewPlayer.json", nil)
			webStatus(t, legacy, 200)
			if !strings.Contains(legacy.Body.String(), record.FileHash) {
				t.Fatal("approved skin missing from legacy profile")
			}
			w = f.call(0, "GET", "/api/library?category=%E9%A3%8E%E6%99%AF&tag=sky", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), record.ID) {
				t.Fatal("category/tag missing")
			}
			w = f.call(0, "GET", "/api/library?tag=sk", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), record.ID) {
				t.Fatal("partial tag matched")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/review/"+record.ID, map[string]string{"state": "rejected", "reason": "test"}), 204)
			webStatus(t, f.call(0, "GET", "/textures/"+record.FileHash, nil), 404)
			webStatus(t, f.call(0, "GET", "/skin/ReviewPlayer.png", nil), 404)
			legacy = f.call(0, "GET", "/csl/ReviewPlayer.json", nil)
			webStatus(t, legacy, 200)
			if strings.Contains(legacy.Body.String(), record.FileHash) {
				t.Fatal("rejected skin appears in legacy profile")
			}
			avatar = f.call(0, "GET", "/api/avatars/1.png", nil)
			webStatus(t, avatar, 200)
			if !bytes.Equal(avatar.Body.Bytes(), fallback.Body.Bytes()) {
				t.Fatal("rejected skin appears in avatar")
			}
			w = f.call(1, "GET", "/api/characters", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), record.FileHash) {
				t.Fatal("rejected texture in character")
			}
			webStatus(t, f.call(1, "PATCH", "/api/textures/"+record.ID, map[string]any{"name": "Resubmit", "description": "changed", "isPublic": true, "tags": []string{"new"}}), 204)
			w = f.call(1, "GET", "/api/textures", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"reviewState":"pending"`) {
				t.Fatal("edit skipped re-review")
			}
			webStatus(t, f.upload(1, image, map[string]string{"name": "Invalid", "tags": "bad!"}), 400)
		})
	}
}
