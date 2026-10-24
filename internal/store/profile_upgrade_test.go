package store

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestFreshRegistrationAndProfileUpgrade(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real schema upgrade NOT RUN: DSN unset")
			}
			s := isolatedTestStore(t, dialect, dsn)
			ctx := context.Background()
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			var mode string
			if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='registration_mode'").Scan(&mode); err != nil || mode != "open" {
				t.Fatal("fresh install must default to email registration", mode, err)
			}
			if err := insertTestUser(ctx, s, 73, "retained@example.test"); err != nil {
				t.Fatal(err)
			}
			// Simulate schema 9 and DDL completed before the version 10 marker.
			for _, q := range []string{"UPDATE settings SET value='closed' WHERE setting_key='registration_mode'", "DELETE FROM schema_migrations WHERE version=10", "INSERT INTO user_profiles(user_id,avatar_mode,character_uuid,avatar_image) VALUES(73,'default','','')"} {
				if _, err := s.DB.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := s.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			if err := s.DB.QueryRow("SELECT COUNT(*) FROM user_profiles WHERE user_id=73").Scan(&count); err != nil || count != 1 {
				t.Fatal("profile lost on retry", err)
			}
			if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='registration_mode'").Scan(&mode); err != nil || mode != "closed" {
				t.Fatal("upgrade reopened closed registration", mode, err)
			}
		})
	}
}
