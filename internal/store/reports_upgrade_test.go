package store

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSchemaSixReportsUpgradeAndRetry(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real schema 6 to 7 upgrade NOT RUN: DSN unset")
			}
			s := isolatedTestStore(t, dialect, dsn)
			ctx := context.Background()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := s.DB.ExecContext(ctx, s.Bind(query), args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE TABLE schema_migrations(version INTEGER NOT NULL PRIMARY KEY)")
			for version, migration := range schemaMigrations[:6] {
				source, err := migrationFiles.ReadFile("migrations/" + migration + "." + dialect + ".sql")
				if err != nil {
					t.Fatal(err)
				}
				for _, statement := range strings.Split(string(source), ";") {
					if strings.TrimSpace(statement) != "" {
						exec(statement)
					}
				}
				exec("INSERT INTO schema_migrations(version) VALUES(?)", version+1)
			}
			if err := insertTestUser(ctx, s, 73, "retained@example.test"); err != nil {
				t.Fatal(err)
			}
			exec("INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,'retained','skin','default',TRUE,FALSE)", strings.Repeat("a", 64))
			exec("INSERT INTO texture_records(record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted) VALUES(?,?,73,'retained','','skin','default',TRUE,FALSE)", strings.Repeat("b", 32), strings.Repeat("a", 64))
			exec("INSERT INTO admin_events(event_id,actor_id,action,target_type,target_id,created_at) VALUES(?,73,'retained','user','73',1)", strings.Repeat("c", 32))
			exec("UPDATE settings SET value='invitation' WHERE setting_key='registration_mode'")
			// DDL completed without the version marker; existing data must survive a retry.
			source, err := migrationFiles.ReadFile("migrations/007_reports." + dialect + ".sql")
			if err != nil {
				t.Fatal(err)
			}
			exec(string(source))
			exec("INSERT INTO reports(report_id,reporter_id,record_id,texture_name,reason,reply,created_at) VALUES(?,73,?,'snapshot','kept reason','',1)", strings.Repeat("d", 32), strings.Repeat("b", 32))
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("interrupted retry", err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("repeat migration", err)
			}
			for _, query := range []string{"SELECT COUNT(*) FROM users WHERE id=73", "SELECT COUNT(*) FROM texture_records WHERE owner_id=73", "SELECT COUNT(*) FROM reports WHERE reason='kept reason' AND status='pending'", "SELECT COUNT(*) FROM admin_events WHERE action='retained'", "SELECT COUNT(*) FROM settings WHERE setting_key='registration_mode' AND value='invitation'"} {
				var count int
				if err := s.DB.QueryRow(query).Scan(&count); err != nil || count != 1 {
					t.Fatal("upgrade changed data", query, count, err)
				}
			}
			var version int
			if err := s.DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != schemaVersion {
				t.Fatal(version, err)
			}
		})
	}
}
