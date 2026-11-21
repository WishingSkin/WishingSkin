package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"github.com/WishingSkin/WishingSkin/internal/backup"
	rp "github.com/WishingSkin/WishingSkin/internal/runtimeplugin"
	"github.com/WishingSkin/WishingSkin/internal/testutil"
)

// Restore executable packages and their SQL/KV data into an independent empty
// database. Pending upgrade migrations must never be applied by restoration.
func verifyDynamicBackup(t *testing.T, f *managementApp, pkg rp.Package, archive []byte, active bool) {
	t.Helper()
	ctx := context.Background()
	h := sha256.Sum256([]byte(pkg.Manifest.ID))
	table := "plugin_" + hex.EncodeToString(h[:8]) + "_notes"
	if active {
		insert := "INSERT INTO " + table + "(id,content,label) VALUES(37,?,NULL)"
		if f.db.Dialect == "postgres" {
			insert = "INSERT INTO " + table + "(id,content,label) OVERRIDING SYSTEM VALUE VALUES(37,?,NULL)"
		}
		f.exec(insert, []byte{0, 255, 10, 13, 42})
	}
	// Retain a staged, never activated upgrade next to the installed archive.
	newer := pkg
	newer.Manifest.Version = "2.0.0"
	newer.Manifest.MigrationVersion++
	newer.Migrations = append(append([]rp.Migration{}, pkg.Migrations...), rp.Migration{Version: newer.Manifest.MigrationVersion, MySQL: []string{"CREATE TABLE {{prefix}}future (id INTEGER)"}, Postgres: []string{"CREATE TABLE {{prefix}}future (id INTEGER)"}})
	manifest, _ := json.Marshal(newer)
	input, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	output := zip.NewWriter(&buffer)
	for _, entry := range input.File {
		source, e := entry.Open()
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(source)
		source.Close()
		if e != nil {
			t.Fatal(e)
		}
		if entry.Name == "plugin.json" {
			data = manifest
		}
		target, e := output.Create(entry.Name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = target.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if err = output.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.app.dynamic.Stage(buffer.Bytes(), pkg.Manifest.Permissions); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "backup")
	if _, err = backup.Export(ctx, f.db, f.app.cfg.TextureDir, destination, f.app.cfg.PluginDir); err != nil {
		t.Fatal(err)
	}
	m, err := backup.Verify(ctx, destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.PluginBundles) != 1 || len(m.PluginTables) != 1 || m.PluginBundles[0].Active != active || m.PluginBundles[0].Digest != rp.Digest(archive) {
		t.Fatalf("unexpected package backup: %+v", m.PluginBundles)
	}
	target := testutil.Database(t, f.db.Dialect)
	texture := filepath.Join(t.TempDir(), "textures")
	plugins := filepath.Join(texture, ".plugins")
	if active {
		plugins = filepath.Join(t.TempDir(), "plugins")
	}
	if _, err = backup.Restore(ctx, target, destination, texture, plugins); err != nil {
		t.Fatal(err)
	}
	var content []byte
	var label *string
	if err = target.DB.QueryRow("SELECT content,label FROM "+table+" WHERE id=37").Scan(&content, &label); err != nil || !bytes.Equal(content, []byte{0, 255, 10, 13, 42}) || label != nil {
		t.Fatalf("SQL restore: %v", err)
	}
	if _, err = target.DB.Exec("INSERT INTO " + table + "(label) VALUES('next')"); err != nil {
		t.Fatal("identity sequence", err)
	}
	var count int
	if err = target.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 2 {
		t.Fatal("restored row count", count, err)
	}
	var value string
	if err = target.DB.QueryRow(target.Bind("SELECT value_json FROM plugin_data WHERE plugin_id=? AND data_key='last'"), pkg.Manifest.ID).Scan(&value); err != nil || value != `"saved"` {
		t.Fatal("KV restore", err)
	}
	manager, err := rp.Open(plugins)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if (len(manager.Items()) == 1) != active {
		t.Fatal("installed package state lost")
	}
	if _, err = os.Stat(filepath.Join(texture, ".restore-complete.json")); err != nil {
		t.Fatal(err)
	}
	// Missing packages must fail export, rather than silently omitting SQL data.
	if _, err = backup.Export(ctx, f.db, f.app.cfg.TextureDir, filepath.Join(t.TempDir(), "incomplete"), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing plugin package accepted")
	}
}
