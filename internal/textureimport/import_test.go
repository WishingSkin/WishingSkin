package textureimport

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/WishingSkin/WishingSkin/internal/testutil"
)

func TestImportUsesLiveContentPolicyBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.DB.Exec(db.Bind(query), args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt,score,permission,banned) VALUES(1,'hd@example.test','hd@example.test','Test','unused','ARGON2ID','',0,0,FALSE)")
			exec("INSERT INTO settings(setting_key,value) VALUES('content_policy',?)", `{"maxSkinWidth":512,"maxCapeWidth":256,"maxFileBytes":1048576}`)
			source := t.TempDir()
			for _, sample := range []struct {
				name          string
				width, height int
				compression   png.CompressionLevel
			}{
				{"a-hd.png", 256, 128, png.DefaultCompression},
				{"b-cape.png", 256, 128, png.DefaultCompression},
				{"c-cape.png", 192, 96, png.DefaultCompression},
				{"d-large.png", 512, 512, png.NoCompression},
			} {
				var data bytes.Buffer
				encoder := png.Encoder{CompressionLevel: sample.compression}
				if err := encoder.Encode(&data, image.NewNRGBA(image.Rect(0, 0, sample.width, sample.height))); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source, sample.name), data.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			options := Options{Dir: source, Owner: "1", TextureDir: filepath.Join(t.TempDir(), "textures")}
			for _, dryRun := range []bool{true, false} {
				options.DryRun = dryRun
				report, err := Run(t.Context(), db, options)
				if err == nil || report.Imported != 3 || report.Failed != 1 || report.Items[0].Kind != "skin" || report.Items[1].Kind != "cape" || report.Items[2].Kind != "cape" || report.Items[3].Status != "failed" {
					t.Fatalf("incorrect custom limits (dry-run=%t): %+v, %v", dryRun, report, err)
				}
			}
			exec("UPDATE settings SET value=? WHERE setting_key='content_policy'", `{"maxSkinWidth":512,"maxCapeWidth":64,"maxFileBytes":2097152}`)
			report, err := Run(t.Context(), db, options)
			if err == nil || report.Imported != 3 || report.Failed != 1 || report.Items[1].Kind != "skin" || report.Items[2].Status != "failed" || report.Items[3].Status != "imported" {
				t.Fatalf("policy change not applied: %+v, %v", report, err)
			}
		})
	}
}

func TestImportBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.DB.Exec(db.Bind(query), args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt,score,permission,banned) VALUES(1,'owner@example.test','owner@example.test','Test','unused','ARGON2ID','',0,0,FALSE)")
			exec("INSERT INTO settings(setting_key,value) VALUES('content_policy',?)", `{"reviewUploads":true,"categories":["测试"]}`)
			var data bytes.Buffer
			if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 64, 32))); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, name := range []string{"a.png", "b.png", "c披风.png"} {
				if err := os.WriteFile(filepath.Join(dir, name), data.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			textures := filepath.Join(t.TempDir(), "textures")
			options := Options{Dir: dir, Owner: "OWNER@example.test", TextureDir: textures, Category: "测试", Tags: []string{" Tag ", "tag"}, Public: true, SkipDuplicates: true, DryRun: true, AccountLimitBytes: int64(data.Len()), SiteLimitBytes: int64(data.Len())}
			result, err := Run(context.Background(), db, options)
			if err == nil || result.Imported != 1 || result.Skipped != 1 || result.Failed != 1 || result.Items[2].Kind != "cape" {
				t.Fatalf("dry-run quota: %+v, %v", result, err)
			}
			if _, err := os.Stat(textures); !os.IsNotExist(err) {
				t.Fatal("dry-run created texture directory")
			}
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM texture_records").Scan(&count); err != nil || count != 0 {
				t.Fatal("dry-run wrote records")
			}
			options.DryRun = false
			options.AccountLimitBytes *= 2
			result, err = Run(context.Background(), db, options)
			if err != nil || result.Imported != 2 || result.Skipped != 1 || result.ReusedFiles != 1 || result.ReviewState != "pending" {
				t.Fatalf("import: %+v, %v", result, err)
			}
			var integrity, records int
			if err := db.DB.QueryRow("SELECT (SELECT COUNT(*) FROM texture_file_integrity),(SELECT COUNT(*) FROM texture_records r JOIN texture_metadata m ON m.record_id=r.record_id WHERE r.created_at>0 AND r.is_public=TRUE AND m.review_state='pending' AND m.category='测试' AND m.tags='[\"tag\"]')").Scan(&integrity, &records); err != nil || integrity != 1 || records != 2 {
				t.Fatalf("integrity=%d records=%d %v", integrity, records, err)
			}
			exec("UPDATE textures SET deleted=TRUE")
			result, err = Run(context.Background(), db, options)
			if err == nil || result.Failed != 3 {
				t.Fatalf("disabled files: %+v, %v", result, err)
			}
		})
	}
}
