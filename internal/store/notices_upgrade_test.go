package store

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNoticeHistoryUpgradeBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real notice upgrade NOT RUN: DSN unset")
			}
			s := isolatedTestStore(t, dialect, dsn)
			ctx := context.Background()
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{
				"DROP TABLE site_notices",
				"DELETE FROM schema_migrations WHERE version=11",
				"UPDATE settings SET value='closed' WHERE setting_key='registration_mode'",
				"INSERT INTO settings(setting_key,value) VALUES('site_notice','existing notice')",
			} {
				if _, err := s.DB.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := s.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			for key, want := range map[string]string{"site_notice": "existing notice", "registration_mode": "closed"} {
				var got string
				if err := s.DB.QueryRowContext(ctx, s.Bind("SELECT value FROM settings WHERE setting_key=?"), key).Scan(&got); err != nil || got != want {
					t.Fatalf("%s=%s: %v", key, got, err)
				}
			}
			var count int
			if err := s.DB.QueryRow("SELECT COUNT(*) FROM site_notices").Scan(&count); err != nil || count != 0 {
				t.Fatal("notice migration failed", count, err)
			}
		})
	}
}
