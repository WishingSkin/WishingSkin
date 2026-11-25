package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/textureimport"
)

func TestDynamicUploadLimitsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			const path = "/api/admin/content-policy"
			patch := func(body map[string]any, status int) string {
				t.Helper()
				w := f.call(3, "PATCH", path, body)
				webStatus(t, w, status)
				return w.Body.String()
			}
			patch(map[string]any{"maxSkinWidth": 192}, 400)
			patch(map[string]any{"maxCapeWidth": 2048}, 400)
			patch(map[string]any{"maxFileBytes": (8 << 20) + 1}, 400)
			patch(map[string]any{"maxFileBytes": (1 << 20) - 1}, 400)
			patch(map[string]any{"maxSkinWidth": 64, "maxCapeWidth": 64, "maxFileBytes": 1 << 20}, 200)
			uploadedRecord(t, f.upload(1, testPNG(t, 64, 32), nil))
			webStatus(t, f.upload(1, testPNG(t, 128, 128), nil), 400)
			webStatus(t, f.upload(1, testPNG(t, 128, 64), map[string]string{"kind": "cape"}), 400)
			patch(map[string]any{"maxSkinWidth": 128}, 200)
			uploadedRecord(t, f.upload(1, testPNG(t, 128, 128), nil))
			patch(map[string]any{"maxSkinWidth": 256}, 200)
			uploadedRecord(t, f.upload(1, testPNG(t, 256, 128), nil))
			webStatus(t, f.upload(1, testPNG(t, 512, 256), nil), 400)
			patch(map[string]any{"maxSkinWidth": 512, "maxCapeWidth": 1024}, 200)
			uploadedRecord(t, f.upload(1, testPNG(t, 512, 256), nil))
			uploadedRecord(t, f.upload(2, testPNG(t, 192, 96), map[string]string{"kind": "cape"}))
			uploadedRecord(t, f.upload(2, testPNG(t, 1024, 512), map[string]string{"kind": "cape"}))
			var large bytes.Buffer
			encoder := png.Encoder{CompressionLevel: png.NoCompression}
			if err := encoder.Encode(&large, image.NewNRGBA(image.Rect(0, 0, 512, 512))); err != nil || large.Len() <= 1<<20 {
				t.Fatal("invalid byte-limit fixture", large.Len(), err)
			}
			w := f.upload(2, large.Bytes(), nil)
			webStatus(t, w, 400)
			if !strings.Contains(w.Body.String(), "1 MiB") {
				t.Fatal("size rejection omitted the configured limit", w.Body)
			}
			patch(map[string]any{"maxFileBytes": 2 << 20}, 200)
			state := patch(map[string]any{"reviewUploads": true, "categories": []string{"HD"}}, 200)
			for _, field := range []string{`"maxSkinWidth":512`, `"maxCapeWidth":1024`, `"maxFileBytes":2097152`} {
				if !strings.Contains(state, field) {
					t.Fatal("omitted limit was not preserved", state)
				}
			}
			uploadedRecord(t, f.upload(2, large.Bytes(), nil))
		})
	}
}

func TestPolicyChangeWhileTextureWriteIsWaitingBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			data := testPNG(t, 256, 256)
			source := t.TempDir()
			if err := os.WriteFile(filepath.Join(source, "hd.png"), data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"HTTP", "CLI"} {
				t.Run(method, func(t *testing.T) {
					webStatus(t, f.call(3, "PATCH", "/api/admin/content-policy", map[string]int{"maxSkinWidth": 256}), 200)
					ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
					defer cancel()
					tx, err := f.db.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback()
					const settingsLock = "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE"
					var mode string
					if err := tx.QueryRowContext(ctx, settingsLock).Scan(&mode); err != nil {
						t.Fatal(err)
					}
					result := make(chan error, 1)
					go func() {
						if method == "HTTP" {
							w := f.upload(1, data, nil)
							if w.Code != 400 || !strings.Contains(w.Body.String(), "128×128") {
								result <- fmt.Errorf("upload accepted stale policy: %d %s", w.Code, w.Body)
								return
							}
						} else {
							report, err := textureimport.Run(ctx, f.db, textureimport.Options{Dir: source, Owner: "1", Kind: "skin", TextureDir: f.app.cfg.TextureDir})
							if err == nil || report.Imported != 0 || report.Failed != 1 || !strings.Contains(report.Items[0].Error, "128×128") {
								result <- fmt.Errorf("import did not reject under the committed policy: %+v, %v", report, err)
								return
							}
						}
						result <- nil
					}()
					waitQuery := "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query=?)"
					if dialect == "mysql" {
						waitQuery = "SELECT EXISTS (SELECT 1 FROM information_schema.processlist WHERE db=DATABASE() AND info=?)"
					}
					ticker := time.NewTicker(20 * time.Millisecond)
					defer ticker.Stop()
					for {
						var waiting bool
						if err := f.db.DB.QueryRowContext(ctx, f.db.Bind(waitQuery), settingsLock).Scan(&waiting); err != nil {
							t.Fatal(err)
						}
						if waiting {
							break
						}
						select {
						case err := <-result:
							t.Fatalf("write finished before the policy transaction: %v", err)
						case <-ctx.Done():
							t.Fatal("write never reached the policy lock")
						case <-ticker.C:
						}
					}
					if _, err := tx.ExecContext(ctx, f.db.Bind("UPDATE settings SET value=? WHERE setting_key='content_policy'"), `{"maxSkinWidth":128}`); err != nil {
						t.Fatal(err)
					}
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
					select {
					case err := <-result:
						if err != nil {
							t.Fatal(err)
						}
					case <-ctx.Done():
						t.Fatal("write stalled after the policy commit")
					}
				})
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM texture_records").Scan(&count); err != nil || count != 0 {
				t.Fatalf("stale policy created %d records: %v", count, err)
			}
		})
	}
}

func TestNewUploadDimensions(t *testing.T) {
	for _, test := range []struct {
		kind          string
		width, height int
		valid         bool
	}{
		{"skin", 64, 64, true}, {"skin", 64, 32, true},
		{"skin", 128, 128, true}, {"skin", 128, 64, true},
		{"skin", 32, 32, false}, {"skin", 63, 64, false},
		{"skin", 96, 96, false}, {"skin", 128, 32, false},
		{"skin", 192, 192, false}, {"skin", 256, 256, false},
		{"skin", 1024, 512, false}, {"skin", 1024, 1024, false},
		{"cape", 64, 32, true}, {"cape", 128, 64, true},
		{"cape", 192, 96, true}, {"cape", 1024, 512, true},
		{"cape", 64, 64, false}, {"cape", 96, 48, false},
		{"cape", 1088, 544, false},
	} {
		t.Run(fmt.Sprintf("%s_%dx%d", test.kind, test.width, test.height), func(t *testing.T) {
			if got := textureimport.ValidDimensions(test.kind, test.width, test.height); got != test.valid {
				t.Fatalf("accepted=%t, want %t", got, test.valid)
			}
		})
	}
}

func TestSkinUploadSizesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			for _, size := range [][2]int{{64, 64}, {64, 32}, {128, 128}, {128, 64}} {
				uploadedRecord(t, f.upload(1, testPNG(t, size[0], size[1]), nil))
			}
			for _, size := range [][2]int{{192, 192}, {256, 256}, {1024, 512}} {
				w := f.upload(1, testPNG(t, size[0], size[1]), nil)
				webStatus(t, w, 400)
				if !strings.Contains(w.Body.String(), "64×64、64×32、128×128 或 128×64") {
					t.Fatal("rejection did not list supported skin sizes")
				}
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM texture_records").Scan(&count); err != nil || count != 4 {
				t.Fatal("rejected skin created a record", count, err)
			}
			uploadedRecord(t, f.upload(1, testPNG(t, 1024, 512), map[string]string{"kind": "cape"}))
		})
	}
}
