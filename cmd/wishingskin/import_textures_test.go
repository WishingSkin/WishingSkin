package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/WishingSkin/WishingSkin/internal/config"
	"github.com/WishingSkin/WishingSkin/internal/testutil"
	"github.com/WishingSkin/WishingSkin/internal/textureimport"
)

func TestImportTexturesCommandBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			if _, err := db.DB.Exec("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt,score,permission,banned) VALUES(1,'owner@example.test','owner@example.test','Test','unused','ARGON2ID','',0,0,FALSE)"); err != nil {
				t.Fatal(err)
			}
			source := t.TempDir()
			var data bytes.Buffer
			if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 64, 64))); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"a.png", "b.PNG"} {
				if err := os.WriteFile(filepath.Join(source, name), data.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(source, "broken.png"), []byte("not PNG"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{TextureDir: filepath.Join(t.TempDir(), "textures")}
			var output bytes.Buffer
			args := []string{"--dir", source, "--owner", "1", "--dry-run"}
			err := importTextures(context.Background(), db, cfg, args, &output)
			var report textureimport.Report
			if parseErr := json.Unmarshal(output.Bytes(), &report); parseErr != nil || err == nil || !report.DryRun || report.Imported != 1 || report.Skipped != 1 || report.Failed != 1 {
				t.Fatalf("output %q, error %v, parse error %v", output.String(), err, parseErr)
			}
			if _, err := os.Stat(cfg.TextureDir); !os.IsNotExist(err) {
				t.Fatalf("dry-run created texture storage: %v", err)
			}
		})
	}
}
