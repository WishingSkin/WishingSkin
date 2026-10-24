package store

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSchemaEightLegacyReportsUpgradeAndRetry(t *testing.T) {
	if CurrentSchemaVersion < 9 {
		t.Fatal("schema 9 report archive is not implemented")
	}
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real schema upgrade NOT RUN: DSN unset")
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
			for version, migration := range schemaMigrations[:8] {
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
			exec("UPDATE settings SET value='invitation' WHERE setting_key='registration_mode'")
			// Simulate DDL completing before its version marker was written.
			source, err := migrationFiles.ReadFile("migrations/009_legacy_reports." + dialect + ".sql")
			if err != nil {
				t.Fatal(err)
			}
			exec(string(source))
			exec("INSERT INTO legacy_reports(id,tid,uploader,reporter,reason,status,report_at) VALUES(9007199254740993,999,888,777,?,0,NULL)", strings.Repeat("旧", 24000))
			for range 2 {
				if err := s.Migrate(ctx); err != nil {
					t.Fatal("migration retry", err)
				}
			}
			for _, query := range []string{"SELECT COUNT(*) FROM users WHERE id=73", "SELECT COUNT(*) FROM legacy_reports WHERE id=9007199254740993 AND tid=999 AND report_at IS NULL", "SELECT COUNT(*) FROM settings WHERE setting_key='registration_mode' AND value='invitation'"} {
				var count int
				if err := s.DB.QueryRow(query).Scan(&count); err != nil || count != 1 {
					t.Fatal("upgrade changed data", count, err)
				}
			}
			var version int
			if err := s.DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != CurrentSchemaVersion {
				t.Fatal(version, err)
			}
		})
	}
}
