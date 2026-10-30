package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"wishingskin/internal/backup"
)

type operationStore struct {
	mu    sync.Mutex
	files map[string][]byte
	puts  int
}

func (s *operationStore) Put(_ context.Context, key, _ string, r io.Reader) error {
	data, err := io.ReadAll(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[key] = data
	s.puts++
	return err
}
func (s *operationStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.files[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(v)), nil
}
func (s *operationStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, key)
	return nil
}
func operationResult(t *testing.T, f *managementApp) operationStatus {
	t.Helper()
	f.app.WaitBackground()
	var job operationStatus
	_, err := f.app.readSetting(context.Background(), "storage_operation", &job)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestStorageLeaseExcludesSeparateConnectionsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			release, err := f.db.LeaseStorage(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			second, err := f.db.LeaseStorage(ctx)
			cancel()
			if err == nil {
				second()
				release()
				t.Fatal("overlapping storage leases")
			}
			release()
			third, err := f.db.LeaseStorage(context.Background())
			if err != nil {
				t.Fatal("lease did not release")
			}
			third()
		})
	}
}

func TestStorageOperationsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), nil))
			old := time.Now().Add(-2 * time.Hour)
			orphan := strings.Repeat("a", 64)
			if err := saveTexture(f.app.cfg.TextureDir, orphan, []byte("orphan")); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(f.app.cfg.TextureDir, orphan)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			webStatus(t, f.call(1, "GET", "/api/admin/storage/orphans", nil), 403)
			w := f.call(3, "GET", "/api/admin/storage/orphans", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), orphan) || strings.Contains(w.Body.String(), record.FileHash) {
				t.Fatal("invalid orphan inventory")
			}
			webStatus(t, f.call(3, "POST", "/api/admin/operations/gc", map[string]any{"hashes": []string{orphan, record.FileHash}}), 202)
			job := operationResult(t, f)
			if job.Completed != 1 || job.Failed != 1 {
				t.Fatalf("unsafe collection: %+v", job)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("orphan retained")
			}
			if _, err := os.Stat(filepath.Join(f.app.cfg.TextureDir, record.FileHash)); err != nil {
				t.Fatal("referenced file removed")
			}
			// A record added after preview protects the file at execution time.
			if err := saveTexture(f.app.cfg.TextureDir, orphan, []byte("orphan")); err != nil {
				t.Fatal(err)
			}
			os.Chtimes(path, old, old)
			f.exec("INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,'new-ref','skin','default',FALSE,FALSE)", orphan)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/gc", map[string]any{"hashes": []string{orphan}}), 202)
			job = operationResult(t, f)
			if job.Completed != 0 || job.Failed != 1 {
				t.Fatal("new reference ignored")
			}
			f.exec("DELETE FROM textures WHERE hash=?", orphan)
			webStatus(t, f.call(3, "PATCH", "/api/admin/object-storage", map[string]string{"endpoint": "https://s3.example.test", "bucket": "test-bucket", "accessKey": "synthetic", "secretKey": "synthetic"}), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.object-storage", map[string]bool{"enabled": true}), 200)
			_, sealed, err := f.app.objectStorageSettings(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			remote := &operationStore{files: map[string][]byte{}}
			f.app.remoteStorage = remote
			f.app.storageConfigHash = tokenHash(sealed)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/sync", map[string]any{}), 202)
			job = operationResult(t, f)
			if job.Completed != 1 || job.Failed != 0 {
				t.Fatalf("sync failed: %+v", job)
			}
			webStatus(t, f.call(3, "POST", "/api/admin/operations/sync", map[string]any{}), 202)
			job = operationResult(t, f)
			if job.Completed != 1 || remote.puts != 1 {
				t.Fatal("sync was not idempotent")
			}
			destination := t.TempDir()
			body := map[string]any{"backup": backupSchedule{Directory: destination, IntervalHours: 24}, "cdn": cdnSettings{MaxAge: 60}}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", body), 204)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/backup", map[string]any{}), 202)
			job = operationResult(t, f)
			if job.State != "completed" || job.Completed != 1 {
				t.Fatalf("backup failed: %+v", job)
			}
			data, err := os.ReadFile(filepath.Join(job.Destination, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var m backup.Manifest
			if json.Unmarshal(data, &m) != nil || len(m.Files) != 1 {
				t.Fatal("incomplete backup")
			}
			// Disabled schedule does not launch a backup; enabled but not due does not duplicate it.
			f.app.runScheduledBackup(context.Background(), time.Now())
			if after := operationResult(t, f); after.ID != job.ID {
				t.Fatal("disabled schedule ran")
			}
			body["backup"] = backupSchedule{Enabled: true, Directory: destination, IntervalHours: 24}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", body), 204)
			f.app.runScheduledBackup(context.Background(), time.Now())
			if after := operationResult(t, f); after.ID != job.ID {
				t.Fatal("schedule ignored interval")
			}
			f.exec("UPDATE settings SET value='0' WHERE setting_key='backup_last_attempt'")
			f.app.runScheduledBackup(context.Background(), time.Now())
			after := operationResult(t, f)
			if after.ID == job.ID || after.State != "completed" {
				t.Fatal("due backup did not complete")
			}
			stale := operationStatus{ID: "interrupted", Kind: "sync", State: "running"}
			if err := f.app.persistOperation(context.Background(), stale); err != nil {
				t.Fatal(err)
			}
			f.app.recoverInterruptedOperation(context.Background())
			if recovered := operationResult(t, f); recovered.State != "failed" || recovered.FinishedAt == 0 {
				t.Fatal("interrupted job remains running")
			}
			body["backup"] = backupSchedule{Enabled: true, Directory: f.app.cfg.TextureDir, IntervalHours: 24}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", body), 400)
		})
	}
}

func TestCDNBoundariesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"isPublic": "true"}))
			body := map[string]any{"backup": backupSchedule{IntervalHours: 24}, "cdn": cdnSettings{Enabled: true, BaseURL: "https://cdn.example.test", MaxAge: 60}}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", body), 204)
			w := f.call(0, "GET", "/textures/"+record.FileHash, nil)
			webStatus(t, w, 307)
			location, _ := url.Parse(w.Header().Get("Location"))
			if location.Host != "cdn.example.test" || location.Query().Get("sig") == "" {
				t.Fatal("unsigned CDN redirect")
			}
			path := location.RequestURI()
			w = f.call(0, "GET", path, nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Header().Get("Cache-Control"), "must-revalidate") {
				t.Fatal("unbounded cache")
			}
			q := location.Query()
			q.Set("sig", "bad")
			webStatus(t, f.call(0, "GET", location.Path+"?"+q.Encode(), nil), 404)
			webStatus(t, f.call(3, "PATCH", "/api/admin/review/"+record.ID, map[string]string{"state": "rejected"}), 204)
			webStatus(t, f.call(0, "GET", path, nil), 404)
			webStatus(t, f.call(0, "GET", "/textures/"+record.FileHash, nil), 404)
		})
	}
}
