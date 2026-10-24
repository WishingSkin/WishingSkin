package bsmigration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func brandingFixture(t *testing.T) (*Snapshot, string) {
	t.Helper()
	s, dir := relationshipFixture(t)
	raw, _ := json.Marshal(s)
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["branding"] = map[string]string{"site_name": "BS <b>Name</b>", "site_description": "Source description", "announcement": "Line one\n<script>plain text</script> {version}"}
	raw, _ = json.Marshal(document)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal("source branding rejected", err)
	}
	return s, dir
}

func TestBrandingSnapshotValidation(t *testing.T) {
	s, dir := brandingFixture(t)
	r, err := Preflight(s, dir)
	if err != nil {
		t.Fatal(r, err)
	}
	if r.Branding != "preserved_as_plain_text" {
		t.Fatal("branding policy missing", r.Branding)
	}
	for _, change := range []func(map[string]string){
		func(v map[string]string) { v["smtp_password"] = "do-not-report-secret" },
		func(v map[string]string) { delete(v, "site_name") },
		func(v map[string]string) { v["site_name"] = " " },
		func(v map[string]string) { v["site_name"] = "two\nlines" },
		func(v map[string]string) { v["site_description"] = strings.Repeat("😀", 201) },
		func(v map[string]string) { v["announcement"] = strings.Repeat("a", 1001) },
		func(v map[string]string) { v["announcement"] = "\x00" },
	} {
		copy := map[string]string{}
		for key, value := range s.Branding {
			copy[key] = value
		}
		change(copy)
		invalid := *s
		invalid.Branding = copy
		report, err := Preflight(&invalid, dir)
		raw, _ := json.Marshal(report)
		if err == nil || strings.Contains(string(raw), "do-not-report-secret") || strings.Contains(string(raw), "smtp_password") {
			t.Fatal("invalid branding accepted or leaked")
		}
	}
}

func TestBrandingImportBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("test DSN unset")
			}
			db := isolatedStore(t, dialect, dsn)
			s, dir := brandingFixture(t)
			target := filepath.Join(t.TempDir(), "textures")
			r, err := Import(context.Background(), db, s, dir, target)
			if err != nil || !r.Imported {
				t.Fatal(r, err)
			}
			for key, want := range map[string]string{"site_name": "BS <b>Name</b>", "site_tagline": "Source description", "site_notice": "Line one\n<script>plain text</script> {version}", "registration_mode": "closed", "points_rules": `{"enabled":false,"dailyReward":0,"uploadCost":0,"characterCost":0}`} {
				var got string
				if err := db.DB.QueryRow(db.Bind("SELECT value FROM settings WHERE setting_key=?"), key).Scan(&got); err != nil || got != want {
					t.Fatal("branding/default setting", key, got, err)
				}
			}
			if r, err := Import(context.Background(), db, s, dir, target); err != nil || !r.AlreadyImported {
				t.Fatal("branding repeat import", r, err)
			}
			raw, _ := json.Marshal(r)
			if strings.Contains(string(raw), "BS <b>Name</b>") || strings.Contains(string(raw), "Source description") {
				t.Fatal("reconciliation report copies public text")
			}
		})
	}
}
