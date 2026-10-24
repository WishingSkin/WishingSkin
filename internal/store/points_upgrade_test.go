package store

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSchemaSevenPointsUpgradeAndRetry(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real schema 7 to 8 upgrade NOT RUN: DSN unset")
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
			for version, migration := range schemaMigrations[:7] {
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
			exec("UPDATE users SET score=123 WHERE id=73")
			source, err := migrationFiles.ReadFile("migrations/008_points." + dialect + ".sql")
			if err != nil {
				t.Fatal(err)
			}
			// Simulate interruption between table creation and default settings/version marker.
			exec(strings.SplitN(string(source), ";", 2)[0])
			exec("INSERT INTO point_entries(entry_id,user_id,kind,reference_id,delta,balance_after,created_at) VALUES(?,73,'checkin','retained',0,123,1)", strings.Repeat("a", 32))
			custom := `{"enabled":true,"dailyReward":5,"uploadCost":2,"characterCost":3}`
			exec("INSERT INTO settings(setting_key,value) VALUES('points_rules',?)", custom)
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("interrupted retry", err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("repeat migration", err)
			}
			var balance, count, version int
			if err := s.DB.QueryRow("SELECT score FROM users WHERE id=73").Scan(&balance); err != nil || balance != 123 {
				t.Fatal("retained balance", balance, err)
			}
			if err := s.DB.QueryRow("SELECT COUNT(*) FROM point_entries WHERE balance_after=123").Scan(&count); err != nil || count != 1 {
				t.Fatal("ledger changed", count, err)
			}
			var rules string
			if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='points_rules'").Scan(&rules); err != nil || rules != custom {
				t.Fatal("rules overwritten", rules, err)
			}
			if err := s.DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != schemaVersion {
				t.Fatal(version, err)
			}
		})
	}
}
