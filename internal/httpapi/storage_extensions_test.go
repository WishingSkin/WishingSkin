package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"wishingskin/internal/backup"
	blob "wishingskin/pkg/storage"
)

type inventoryOperationStore struct {
	operationStore
	modified map[string]time.Time
}

func (s *inventoryOperationStore) List(_ context.Context, cursor string, limit int) (blob.ObjectPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{}
	for key := range s.files {
		if key > cursor {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	page := blob.ObjectPage{Objects: []blob.ObjectInfo{}}
	for _, key := range keys {
		if len(page.Objects) == limit {
			page.Next = page.Objects[len(page.Objects)-1].Key
			break
		}
		page.Objects = append(page.Objects, blob.ObjectInfo{Key: key, Bytes: int64(len(s.files[key])), ModifiedAt: s.modified[key]})
	}
	return page, nil
}
func (s *inventoryOperationStore) Stat(_ context.Context, key string) (blob.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[key]
	if !ok {
		return blob.ObjectInfo{}, os.ErrNotExist
	}
	return blob.ObjectInfo{Key: key, Bytes: int64(len(data)), ModifiedAt: s.modified[key]}, nil
}
func TestRemoteInventoryAndBackupRetentionBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), nil))
			config := map[string]string{"endpoint": "https://s3.example.test", "bucket": "test-bucket", "accessKey": "synthetic", "secretKey": "synthetic"}
			webStatus(t, f.call(3, "PATCH", "/api/admin/object-storage", config), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.object-storage", map[string]bool{"enabled": true}), 200)
			_, sealed, err := f.app.objectStorageSettings(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			old, newer, added, orphan := record.FileHash, strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)
			past := time.Now().Add(-2 * time.Hour)
			remote := &inventoryOperationStore{operationStore: operationStore{files: map[string][]byte{old: {1}, newer: {2}, added: {3}, orphan: {4}}}, modified: map[string]time.Time{old: past, newer: time.Now(), added: past, orphan: past}}
			f.app.remoteStorage = remote
			f.app.storageConfigHash = tokenHash(sealed)
			webStatus(t, f.call(1, "GET", "/api/admin/storage/remote-orphans", nil), 403)
			response := f.call(3, "GET", "/api/admin/storage/remote-orphans", nil)
			webStatus(t, response, 200)
			var preview struct {
				Files    []orphanFile `json:"files"`
				Revision string       `json:"revision"`
			}
			if err = json.Unmarshal(response.Body.Bytes(), &preview); err != nil || len(preview.Files) != 2 || len(preview.Revision) != 64 {
				t.Fatal("remote inventory", err, response.Body.String())
			}
			// References created after preview must be protected.
			f.exec("INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,'new','skin','default',FALSE,FALSE)", added)
			body := map[string]any{"hashes": []string{old, newer, added, orphan}, "revision": preview.Revision, "confirmed": false}
			webStatus(t, f.call(3, "POST", "/api/admin/operations/remote-gc", body), 400)
			body["confirmed"] = true
			webStatus(t, f.call(3, "POST", "/api/admin/operations/remote-gc", body), 202)
			job := operationResult(t, f)
			if job.Completed != 1 || job.Failed != 3 {
				t.Fatalf("remote collection %+v", job)
			}
			if len(remote.files) != 3 {
				t.Fatal("referenced or new files removed")
			}
			config["bucket"] = "another-bucket"
			webStatus(t, f.call(3, "PATCH", "/api/admin/object-storage", config), 200)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/remote-gc", body), 202)
			if job = operationResult(t, f); job.State != "failed" || job.Completed != 0 {
				t.Fatal("stale preview accepted", job)
			}
			// Return to local backup data, with all referenced texture files present.
			f.exec("DELETE FROM textures WHERE hash=?", added)
			directory := t.TempDir()
			first := "wishingskin-backup-20250101T000000Z-aaaaaaaa"
			if _, err = backup.Export(context.Background(), f.db, f.app.cfg.TextureDir, filepath.Join(directory, first)); err != nil {
				t.Fatal(err)
			}
			unknown := filepath.Join(directory, "operator-copy")
			if err = os.Mkdir(unknown, 0700); err != nil {
				t.Fatal(err)
			}
			incomplete := filepath.Join(directory, "wishingskin-backup-20240101T000000Z-bbbbbbbb")
			if err = os.Mkdir(incomplete, 0700); err != nil {
				t.Fatal(err)
			}
			corrupt := filepath.Join(directory, "wishingskin-backup-20230101T000000Z-cccccccc")
			if err = os.Mkdir(corrupt, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(corrupt, "manifest.json"), []byte(`{"schemaVersion":21}`), 0600); err != nil {
				t.Fatal(err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", map[string]any{"backup": backupSchedule{Directory: directory, Time: "03:00", Timezone: "UTC", Keep: 1}, "cdn": cdnSettings{MaxAge: 60}}), 204)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/backup", map[string]any{}), 202)
			job = operationResult(t, f)
			if job.State != "completed" {
				t.Fatal("retention backup", job)
			}
			if _, err = os.Stat(filepath.Join(directory, first)); !os.IsNotExist(err) {
				t.Fatal("expired complete backup retained", err)
			}
			for _, name := range []string{unknown, incomplete, corrupt, job.Destination} {
				if _, err = os.Stat(name); err != nil {
					t.Fatal("backup retention removed unrelated or incomplete directory", err)
				}
			}
			webStatus(t, f.call(1, "GET", "/api/admin/backups", nil), 403)
			response = f.call(3, "GET", "/api/admin/backups", nil)
			webStatus(t, response, 200)
			var inventory struct {
				Backups []backupEntry `json:"backups"`
			}
			json.Unmarshal(response.Body.Bytes(), &inventory)
			if len(inventory.Backups) != 3 {
				t.Fatal("backup inventory", response.Body.String())
			}
			id := filepath.Base(job.Destination)
			webStatus(t, f.call(3, "POST", "/api/admin/backups/"+id+"/verify", nil), 200)
			webStatus(t, f.call(3, "POST", "/api/admin/backups/"+id+"/restore-plan", nil), 200)
			webStatus(t, f.call(3, "POST", "/api/admin/backups/"+filepath.Base(corrupt)+"/verify", nil), 409)
			webStatus(t, f.call(3, "POST", "/api/admin/backups/operator-copy/verify", nil), 404)
		})
	}
}
