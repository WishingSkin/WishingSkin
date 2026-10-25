package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestInvitationExportUpgradeRetryBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		for _, completed := range []int{1, 5, 6} {
			t.Run(fmt.Sprintf("%s/after_%d_statements", dialect, completed), func(t *testing.T) {
				dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
				if dsn == "" {
					t.Skip("real invitation upgrade NOT RUN: DSN unset")
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
				for version, migration := range schemaMigrations[:12] {
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
				// Reproduce exit after individual DDLs, including before the version marker.
				columns := []string{"encrypted_code TEXT", "source_id BIGINT", "generated_at BIGINT NOT NULL DEFAULT 0", "used_at BIGINT", "source_user_id BIGINT"}
				for i, column := range columns {
					if i < completed {
						exec("ALTER TABLE invitation_codes ADD COLUMN " + column)
					}
				}
				if completed == 6 {
					exec("CREATE UNIQUE INDEX invitation_codes_source_id ON invitation_codes(source_id)")
				}
				exec("INSERT INTO invitation_codes(code_hash,expires_at,remaining_uses,encrypted_code) VALUES(?,0,1,?)", strings.Repeat("a", 64), "retained synthetic ciphertext")
				for range 2 {
					if err := s.Migrate(ctx); err != nil {
						t.Fatal("interrupted invitation migration retry", err)
					}
				}
				var sealed string
				if err := s.DB.QueryRow("SELECT encrypted_code FROM invitation_codes").Scan(&sealed); err != nil || sealed != "retained synthetic ciphertext" {
					t.Fatal("retry changed invitation", err)
				}
				exec("UPDATE invitation_codes SET source_id=73,generated_at=1,used_at=2,source_user_id=4")
				if _, err := s.DB.ExecContext(ctx, s.Bind("INSERT INTO invitation_codes(code_hash,expires_at,remaining_uses,source_id) VALUES(?,0,1,73)"), strings.Repeat("b", 64)); err == nil {
					t.Fatal("source ID unique index missing after retry")
				}
			})
		}
	}
}
