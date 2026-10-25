package bsmigration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func legacyReportsFixture(t *testing.T, change func(map[string]any)) (*Snapshot, string) {
	t.Helper()
	s, dir := relationshipFixture(t)
	raw, _ := json.Marshal(s)
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["legacy_reports"] = map[string]any{"items": []any{
		map[string]any{"id": int64(9007199254740993), "tid": int64(99999), "uploader": int64(77777), "reporter": int64(88888), "reason": "Historical <script>plain text</script>\n" + strings.Repeat("旧", 24000), "status": 0, "report_at": "0000-00-00 00:00:00"},
		map[string]any{"id": int64(2), "tid": int64(0), "uploader": int64(-1), "reporter": int64(0), "reason": "", "status": 1, "report_at": nil},
		map[string]any{"id": int64(3), "tid": int64(99999), "uploader": int64(77777), "reporter": int64(88888), "reason": "Rejected reason", "status": 2, "report_at": "2020-01-02 03:04:05"},
	}}
	if change != nil {
		change(document)
	}
	raw, _ = json.Marshal(document)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal("source report history rejected", err)
	}
	return s, dir
}

func TestLegacyReportsSnapshotValidation(t *testing.T) {
	s, dir := legacyReportsFixture(t, nil)
	r, err := Preflight(s, dir)
	if err != nil {
		t.Fatal(r.Blockers, err)
	}
	raw, _ := json.Marshal(r)
	var report struct {
		LegacyReports struct {
			Policy, Source                     string
			Total, Pending, Resolved, Rejected int
		} `json:"legacy_reports"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.LegacyReports.Policy != "read_only_history" || report.LegacyReports.Source != "included" || report.LegacyReports.Total != 3 || report.LegacyReports.Pending != 1 || report.LegacyReports.Resolved != 1 || report.LegacyReports.Rejected != 1 {
		t.Fatal("missing history reconciliation", string(raw))
	}
	if strings.Contains(string(raw), "Historical") || strings.Contains(string(raw), "Rejected reason") || strings.Contains(string(raw), "77777") {
		t.Fatal("history report leaked source content")
	}
	for name, change := range map[string]func(map[string]any){
		"v1": func(d map[string]any) { d["format_version"] = 1 },
		"duplicate": func(d map[string]any) {
			v := d["legacy_reports"].(map[string]any)["items"].([]any)
			v[1].(map[string]any)["id"] = v[0].(map[string]any)["id"]
		},
		"invalid_id": func(d map[string]any) {
			d["legacy_reports"].(map[string]any)["items"].([]any)[0].(map[string]any)["id"] = 0
		},
		"unknown_status": func(d map[string]any) {
			d["legacy_reports"].(map[string]any)["items"].([]any)[0].(map[string]any)["status"] = 3
		},
		"nul": func(d map[string]any) {
			d["legacy_reports"].(map[string]any)["items"].([]any)[0].(map[string]any)["reason"] = "\x00"
		},
		"date": func(d map[string]any) {
			d["legacy_reports"].(map[string]any)["items"].([]any)[0].(map[string]any)["report_at"] = "2020-01-02T03:04:05Z"
		},
		"missing_items": func(d map[string]any) { d["legacy_reports"] = map[string]any{} },
	} {
		t.Run(name, func(t *testing.T) {
			invalid, dir := legacyReportsFixture(t, change)
			if r, err := Preflight(invalid, dir); err == nil || r.Status != "blocked" {
				t.Fatal("invalid history accepted")
			}
		})
	}
	legacy, dir := relationshipFixture(t)
	r, err = Preflight(legacy, dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(r)
	if !strings.Contains(string(raw), `"source":"unavailable_in_snapshot"`) {
		t.Fatal("missing history treated as zero")
	}
}

func TestLegacyReportsImportBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("test DSN unset")
			}
			db := isolatedStore(t, dialect, dsn)
			s, source := legacyReportsFixture(t, nil)
			s.Branding = map[string]string{"site_name": "Rollback source", "site_description": "Historical import", "announcement": "Preserved text"}
			target := filepath.Join(t.TempDir(), "textures")
			var homepageBefore string
			if err := db.DB.QueryRow("SELECT value FROM settings WHERE setting_key='homepage_blocks'").Scan(&homepageBefore); err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB.Exec("ALTER TABLE legacy_reports RENAME COLUMN reason TO unavailable_reason"); err != nil {
				t.Fatal(err)
			}
			if r, err := Import(context.Background(), db, s, source, target); err == nil || r.Imported {
				t.Fatal("history failure accepted")
			}
			for _, table := range []string{"users", "characters", "textures", "texture_records", "wardrobe", "legacy_reports", "settings WHERE setting_key NOT IN ('character_limit','registration_mode','points_rules','homepage_blocks')"} {
				var count int
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatal("historical import failed to roll back", table, count, err)
				}
			}
			var homepageAfter string
			if err := db.DB.QueryRow("SELECT value FROM settings WHERE setting_key='homepage_blocks'").Scan(&homepageAfter); err != nil || homepageAfter != homepageBefore {
				t.Fatal("rollback changed default homepage", err)
			}
			if _, err := db.DB.Exec("ALTER TABLE legacy_reports RENAME COLUMN unavailable_reason TO reason"); err != nil {
				t.Fatal(err)
			}
			r, err := Import(context.Background(), db, s, source, target)
			if err != nil || !r.Imported {
				t.Fatal("history import failed", r.Blockers, err)
			}
			read := func() []string {
				t.Helper()
				rows, err := db.DB.Query("SELECT id,tid,uploader,reporter,reason,status,report_at FROM legacy_reports ORDER BY id")
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				var values []string
				for rows.Next() {
					var id, tid, uploader, reporter int64
					var reason string
					var status int
					var at *string
					if err := rows.Scan(&id, &tid, &uploader, &reporter, &reason, &status, &at); err != nil {
						t.Fatal(err)
					}
					v, _ := json.Marshal([]any{id, tid, uploader, reporter, reason, status, at})
					values = append(values, string(v))
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				return values
			}
			before := read()
			if len(before) != 3 || !strings.Contains(before[2], "9007199254740993") || !strings.Contains(before[2], strings.Repeat("旧", 24000)) || !strings.HasSuffix(before[0], ",null]") {
				t.Fatal("history bytes/IDs/time changed")
			}
			if r, err := Import(context.Background(), db, s, source, target); err != nil || !r.AlreadyImported || !reflect.DeepEqual(before, read()) {
				t.Fatal("repeat changed history", err)
			}
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM reports").Scan(&count); err != nil || count != 0 {
				t.Fatal("history entered new queue", err)
			}
		})
	}
}
