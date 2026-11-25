package store

import (
	"os"
	"strings"
	"testing"
)

func TestSchema29ResumesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real database DSN unset")
			}
			db := isolatedTestStore(t, dialect, dsn)
			ctx := t.Context()
			if err := db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.DB.ExecContext(ctx, db.Bind(query), args...); err != nil {
					t.Fatal(err)
				}
			}
			const client = "12345678901234567890123456789012"
			codeHash := strings.Repeat("a", 64)
			exec("INSERT INTO oauth_clients(client_id,name,redirect_uris,secret_hash,is_public,enabled) VALUES(?,'Legacy','[]','',TRUE,TRUE)", client)
			exec("INSERT INTO point_codes(code_hash,encrypted_code,points,max_uses,remaining_uses,created_at) VALUES(?,'sealed',1,5,3,1)", codeHash)
			exec("DROP TABLE point_code_redemptions")
			exec("DELETE FROM schema_migrations WHERE version=29")
			for range 2 {
				if err := db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var scopes string
			var version, remaining int
			if err := db.DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 29 {
				t.Fatalf("schema version = %d: %v", version, err)
			}
			if err := db.DB.QueryRow("SELECT allowed_scopes FROM oauth_clients").Scan(&scopes); err != nil || scopes != "" {
				t.Fatalf("legacy scopes = %q: %v", scopes, err)
			}
			if err := db.DB.QueryRow("SELECT remaining_uses FROM point_codes").Scan(&remaining); err != nil || remaining != 3 {
				t.Fatalf("remaining = %d: %v", remaining, err)
			}
			if err := insertTestUser(ctx, db, 71, "points@example.test"); err != nil {
				t.Fatal(err)
			}
			exec("INSERT INTO point_code_redemptions(code_hash,user_id,redeemed_at) VALUES(?,71,1)", codeHash)
			exec("DELETE FROM point_codes WHERE code_hash=?", codeHash)
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM point_code_redemptions").Scan(&count); err != nil || count != 0 {
				t.Fatalf("code deletion did not cascade: %d %v", count, err)
			}
		})
	}
}
