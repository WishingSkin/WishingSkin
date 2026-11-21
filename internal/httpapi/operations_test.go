package httpapi

import (
	"bytes"
	"context"
	"crypto/sha1" // Content identifier in a legacy fixture.
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"github.com/WishingSkin/WishingSkin/internal/backup"
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

func TestBackupRejectsDamagedSourceBeforePruningBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), nil))
			destination := t.TempDir()
			body := map[string]any{"backup": backupSchedule{Directory: destination, Time: "03:00", Timezone: "UTC", Keep: 1}, "cdn": cdnSettings{MaxAge: 60}}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", body), 204)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/backup", map[string]any{}), 202)
			good := operationResult(t, f)
			if good.State != "completed" {
				t.Fatalf("healthy backup failed: %+v", good)
			}
			path := filepath.Join(f.app.cfg.TextureDir, record.FileHash)
			if err := os.WriteFile(path, testPNG(t, 128, 64), 0600); err != nil {
				t.Fatal(err)
			}
			webStatus(t, f.call(3, "POST", "/api/admin/operations/backup", map[string]any{}), 202)
			failed := operationResult(t, f)
			if failed.State != "failed" {
				t.Fatalf("damaged source backup was accepted: %+v", failed)
			}
			if _, err := os.Stat(filepath.Join(failed.Destination, "manifest.json")); !os.IsNotExist(err) {
				t.Fatalf("damaged source was published: %v", err)
			}
			if _, err := backup.Verify(context.Background(), good.Destination); err != nil {
				t.Fatalf("previous healthy backup was lost: %v", err)
			}
		})
	}
}

func TestBackupRetainsUnverifiedLegacySourcesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			ctx := context.Background()
			f.exec("INSERT INTO migration_state(id,source_fingerprint,status,report) VALUES(?,?,?,?)", 1, strings.Repeat("1", 64), "partial", `{"unsupported":[{"scope":"texture_hash_filter"}]}`)
			data := testPNG(t, 64, 64)
			sum := sha1.Sum(data)
			known := hex.EncodeToString(sum[:])
			unverified := strings.Repeat("2", 64)
			addLegacy := func(hash string) {
				t.Helper()
				if _, err := saveTexture(f.app.cfg.TextureDir, hash, data); err != nil {
					t.Fatal(err)
				}
				f.exec("INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,'legacy','skin','default',FALSE,FALSE)", hash)
			}
			addLegacy(known)
			body := map[string]any{"backup": backupSchedule{Directory: t.TempDir(), Time: "03:00", Timezone: "UTC", Keep: 1}, "cdn": cdnSettings{MaxAge: 60}}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", body), 204)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/backup", map[string]any{}), 202)
			first := operationResult(t, f)
			if first.State != "completed" || first.UnverifiedFiles != 0 {
				t.Fatalf("verifiable legacy SHA-1 was not recognized: %+v", first)
			}
			addLegacy(unverified)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/backup", map[string]any{}), 202)
			second := operationResult(t, f)
			if second.State != "completed_with_errors" || second.UnverifiedFiles != 1 || second.Completed != 1 {
				t.Fatalf("source trust distinction missing: %+v", second)
			}
			var result struct {
				Valid           bool `json:"valid"`
				UnverifiedFiles int  `json:"unverifiedFiles"`
			}
			path := "/api/admin/backups/" + filepath.Base(second.Destination)
			response := f.call(3, "POST", path+"/verify", nil)
			webStatus(t, response, 200)
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result.Valid || result.UnverifiedFiles != 1 {
				t.Fatalf("source verification status not reported: %+v, %v", result, err)
			}
			webStatus(t, f.call(3, "POST", path+"/restore-plan", nil), 200)
			if _, err := backup.Verify(ctx, first.Destination); err != nil {
				t.Fatalf("unverified backup displaced a healthy copy: %v", err)
			}
			f.exec("DELETE FROM textures WHERE hash=?", unverified)
			webStatus(t, f.call(3, "POST", "/api/admin/operations/backup", map[string]any{}), 202)
			third := operationResult(t, f)
			if third.State != "completed" {
				t.Fatalf("new verified backup failed: %+v", third)
			}
			if _, err := os.Stat(first.Destination); !os.IsNotExist(err) {
				t.Fatalf("retention did not prune the old healthy copy: %v", err)
			}
			if _, err := backup.Verify(ctx, second.Destination); err != nil {
				t.Fatalf("retention removed the unverified copy: %v", err)
			}
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
			if _, err := saveTexture(f.app.cfg.TextureDir, orphan, []byte("orphan")); err != nil {
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
			if _, err := saveTexture(f.app.cfg.TextureDir, orphan, []byte("orphan")); err != nil {
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
			body := map[string]any{"backup": backupSchedule{Directory: destination, Time: "03:00", Timezone: "UTC"}, "cdn": cdnSettings{MaxAge: 60}}
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
			// Manual backups do not consume the daily scheduled slot.
			now := time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC)
			f.app.runScheduledBackup(context.Background(), now)
			if after := operationResult(t, f); after.ID != job.ID {
				t.Fatal("disabled schedule ran")
			}
			body["backup"] = backupSchedule{Enabled: true, Directory: destination, Time: "03:00", Timezone: "UTC"}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", body), 204)
			f.app.runScheduledBackup(context.Background(), now.Add(-time.Minute))
			if after := operationResult(t, f); after.ID != job.ID {
				t.Fatal("schedule ran before fixed time")
			}
			f.app.runScheduledBackup(context.Background(), now)
			after := operationResult(t, f)
			if after.ID == job.ID || after.State != "completed" {
				t.Fatal("due backup did not complete after manual backup")
			}
			f.app.runScheduledBackup(context.Background(), now.Add(time.Minute))
			if duplicate := operationResult(t, f); duplicate.ID != after.ID {
				t.Fatal("scheduled backup duplicated within day")
			}
			f.app.runScheduledBackup(context.Background(), now.AddDate(0, 0, 1))
			if next := operationResult(t, f); next.ID == after.ID || next.State != "completed" {
				t.Fatal("next daily backup did not complete")
			}
			stale := operationStatus{ID: "interrupted", Kind: "sync", State: "running"}
			if err := f.app.persistOperation(context.Background(), stale); err != nil {
				t.Fatal(err)
			}
			f.app.recoverInterruptedOperation(context.Background())
			if recovered := operationResult(t, f); recovered.State != "failed" || recovered.FinishedAt == 0 {
				t.Fatal("interrupted job remains running")
			}
			body["backup"] = backupSchedule{Enabled: true, Directory: f.app.cfg.TextureDir, Time: "03:00", Timezone: "UTC"}
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", body), 400)
		})
	}
}

func TestDailyBackupTimezones(t *testing.T) {
	schedule := backupSchedule{Enabled: true, Time: "03:00", Timezone: "Asia/Shanghai"}
	for _, tc := range []struct{ utc, want string }{
		{"2026-10-29T18:59:00Z", ""},
		{"2026-10-29T19:00:00Z", "Asia/Shanghai/2026-10-30"},
		{"2026-10-30T15:59:00Z", "Asia/Shanghai/2026-10-30"},
		{"2026-10-30T16:00:00Z", ""},
	} {
		now, _ := time.Parse(time.RFC3339, tc.utc)
		if got := backupSlot(schedule, now); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.utc, got, tc.want)
		}
	}
	schedule.Timezone, schedule.Time = "America/Los_Angeles", "01:30"
	first, _ := time.Parse(time.RFC3339, "2026-11-01T08:30:00Z")
	repeated := first.Add(time.Hour)
	if backupSlot(schedule, first) == "" || backupSlot(schedule, first) != backupSlot(schedule, repeated) {
		t.Fatal("repeated DST hour must use the same daily slot")
	}
	schedule.Time = "02:30"
	skipped, _ := time.Parse(time.RFC3339, "2026-03-08T10:00:00Z")
	if backupSlot(schedule, skipped) == "" {
		t.Fatal("skipped DST time must catch up after the clock advances")
	}
	for _, invalid := range []backupSchedule{
		{Time: "3:00", Timezone: "UTC"},
		{Time: "24:00", Timezone: "UTC"},
		{Time: "03:00", Timezone: "Unknown/Zone"},
		{Time: "03:00", Timezone: "Local"},
		{},
	} {
		if validBackupTime(invalid) {
			t.Fatal("invalid or legacy schedule accepted")
		}
	}
}

func TestCDNBoundariesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"isPublic": "true"}))
			body := map[string]any{"backup": backupSchedule{Time: "03:00", Timezone: "UTC"}, "cdn": cdnSettings{Enabled: true, BaseURL: "https://cdn.example.test", MaxAge: 60}}
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
