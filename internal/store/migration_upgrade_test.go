package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSchemaOneUpgradePreservesData(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("WS_TEST_" + strings.ToUpper(dialect) + "_DSN unset: real upgrade test NOT RUN")
			}
			s := isolatedTestStore(t, dialect, dsn)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			initial, err := migrationFiles.ReadFile("migrations/001_initial." + dialect + ".sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range strings.Split(string(initial), ";") {
				if strings.TrimSpace(statement) == "" {
					continue
				}
				if _, err := s.DB.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.DB.ExecContext(ctx, "CREATE TABLE schema_migrations (version INTEGER NOT NULL PRIMARY KEY)"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (1)"); err != nil {
				t.Fatal(err)
			}
			if err := insertTestUser(ctx, s, 73, "retained@example.test"); err != nil {
				t.Fatal(err)
			}
			for _, seed := range []struct {
				query string
				args  []any
			}{
				{"INSERT INTO settings(setting_key,value) VALUES(?,?)", []any{"preserved", "existing value"}},
				{"INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,?,?,?,?,?)", []any{"abcdef0123456789abcdef0123456789", "kept texture", "skin", "default", true, false}},
				{"INSERT INTO characters(id,user_id,uuid,name,name_key) VALUES(?,?,?,?,?)", []any{9, 73, strings.Repeat("a", 32), "KeptPlayer", "keptplayer"}},
				{"INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", []any{strings.Repeat("b", 64), 73, time.Now().Add(time.Hour).Unix()}},
				{"INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?,?,?,?,?)", []any{strings.Repeat("c", 64), "kept-client", 73, strings.Repeat("a", 32), time.Now().Add(time.Hour).Unix()}},
			} {
				if _, err := s.DB.ExecContext(ctx, s.Bind(seed.query), seed.args...); err != nil {
					t.Fatal(err)
				}
			}
			// Simulate completion of 002 DDL followed by exit before recording version 2.
			second, err := migrationFiles.ReadFile("migrations/002_ygg_sessions." + dialect + ".sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.ExecContext(ctx, string(second)); err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("interrupted schema 1 to 2 retry:", err)
			}
			// Simulate completed 003 statements without the version marker, then retry.
			if _, err := s.DB.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version>=3"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.ExecContext(ctx, "UPDATE texture_records SET description='retained metadata'"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.ExecContext(ctx, "UPDATE settings SET value='5' WHERE setting_key='character_limit'"); err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("interrupted schema 2 to 3 retry:", err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("repeat schema 3 migration:", err)
			}
			var maxVersion, versionCount int
			if err := s.DB.QueryRowContext(ctx, "SELECT MAX(version),COUNT(*) FROM schema_migrations").Scan(&maxVersion, &versionCount); err != nil || maxVersion != schemaVersion || versionCount != schemaVersion {
				t.Fatal("upgrade version history", maxVersion, versionCount, err)
			}
			for _, query := range []string{
				"SELECT COUNT(*) FROM users WHERE id=73 AND email='retained@example.test'",
				"SELECT COUNT(*) FROM settings WHERE setting_key='preserved' AND value='existing value'",
				"SELECT COUNT(*) FROM texture_records WHERE file_hash='abcdef0123456789abcdef0123456789' AND name='kept texture' AND is_public=TRUE",
				"SELECT COUNT(*) FROM texture_records WHERE description='retained metadata' AND owner_id IS NULL",
				"SELECT COUNT(*) FROM settings WHERE setting_key='character_limit' AND value='5'",
				"SELECT COUNT(*) FROM characters WHERE id=9 AND name='KeptPlayer'",
				"SELECT COUNT(*) FROM web_sessions WHERE user_id=73",
				"SELECT COUNT(*) FROM game_tokens WHERE user_id=73 AND client_token='kept-client'",
			} {
				var count int
				if err := s.DB.QueryRowContext(ctx, query).Scan(&count); err != nil || count != 1 {
					t.Fatal("existing data changed during upgrade", query, count, err)
				}
			}
			if _, err := s.DB.ExecContext(ctx, s.Bind("INSERT INTO ygg_join_sessions(token_hash,server_id,profile_uuid,client_ip,expires_at) VALUES(?,?,?,?,?)"), strings.Repeat("c", 64), "upgraded", strings.Repeat("a", 32), "127.0.0.1", time.Now().Add(30*time.Second).Unix()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.ExecContext(ctx, s.Bind("DELETE FROM game_tokens WHERE token_hash=?"), strings.Repeat("c", 64)); err != nil {
				t.Fatal(err)
			}
			var pending int
			if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM ygg_join_sessions").Scan(&pending); err != nil || pending != 0 {
				t.Fatal("revocation did not cascade after upgrade", pending, err)
			}
		})
	}
}

func TestSchemaThreeUpgradeAndWardrobeRetry(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real schema 3 to 4 upgrade NOT RUN: DSN unset")
			}
			s := isolatedTestStore(t, dialect, dsn)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// Construct the exact pre-wardrobe schema, including its version history.
			if _, err := s.DB.ExecContext(ctx, "CREATE TABLE schema_migrations(version INTEGER NOT NULL PRIMARY KEY)"); err != nil {
				t.Fatal(err)
			}
			for version, migration := range schemaMigrations[:3] {
				source, err := migrationFiles.ReadFile("migrations/" + migration + "." + dialect + ".sql")
				if err != nil {
					t.Fatal(err)
				}
				for _, statement := range strings.Split(string(source), ";") {
					if strings.TrimSpace(statement) == "" {
						continue
					}
					if _, err := s.DB.ExecContext(ctx, statement); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := s.DB.ExecContext(ctx, s.Bind("INSERT INTO schema_migrations(version) VALUES(?)"), version+1); err != nil {
					t.Fatal(err)
				}
			}
			if err := insertTestUser(ctx, s, 73, "retained@example.test"); err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("3 to 4:", err)
			}
			for _, query := range []string{
				"INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES('abcdef0123456789abcdef0123456789','kept','skin','default',TRUE,FALSE)",
				"INSERT INTO texture_records(record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted) VALUES('abcdef0123456789abcdef0123456789','abcdef0123456789abcdef0123456789',73,'kept','','skin','default',TRUE,FALSE)",
				"INSERT INTO wardrobe(user_id,record_id,display_name,collected_at) VALUES(73,'abcdef0123456789abcdef0123456789','kept alias',1)",
				"DELETE FROM schema_migrations WHERE version>=4",
			} {
				if _, err := s.DB.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("004 DDL retry:", err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("004 repeat:", err)
			}
			var count int
			if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM wardrobe WHERE user_id=73 AND display_name='kept alias'").Scan(&count); err != nil || count != 1 {
				t.Fatal("collection changed on retry", count, err)
			}
		})
	}
}

func TestSchemaFourUpgradeAndAccountRetry(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real schema 4 to 5 upgrade NOT RUN: DSN unset")
			}
			s := isolatedTestStore(t, dialect, dsn)
			ctx := context.Background()
			mustExec := func(query string, args ...any) {
				t.Helper()
				if _, err := s.DB.ExecContext(ctx, s.Bind(query), args...); err != nil {
					t.Fatal(err)
				}
			}
			mustExec("CREATE TABLE schema_migrations(version INTEGER NOT NULL PRIMARY KEY)")
			for version, migration := range schemaMigrations[:4] {
				source, err := migrationFiles.ReadFile("migrations/" + migration + "." + dialect + ".sql")
				if err != nil {
					t.Fatal(err)
				}
				for _, statement := range strings.Split(string(source), ";") {
					if strings.TrimSpace(statement) != "" {
						mustExec(statement)
					}
				}
				mustExec("INSERT INTO schema_migrations(version) VALUES(?)", version+1)
			}
			if err := insertTestUser(ctx, s, 73, "retained@example.test"); err != nil {
				t.Fatal(err)
			}
			mustExec("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,73,?)", strings.Repeat("a", 64), time.Now().Add(time.Hour).Unix())
			// Simulate exit after the first table, before completing schema 5.
			source, err := migrationFiles.ReadFile("migrations/005_account_access." + dialect + ".sql")
			if err != nil {
				t.Fatal(err)
			}
			mustExec(strings.SplitN(string(source), ";", 2)[0])
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("partial DDL retry", err)
			}
			var states int
			if err := s.DB.QueryRow("SELECT COUNT(*) FROM account_states").Scan(&states); err != nil || states != 0 {
				t.Fatal("legacy accounts marked verified or pending", err)
			}
			mustExec("INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(73,TRUE,FALSE)")
			mustExec("INSERT INTO account_tokens(user_id,purpose,token_hash,expires_at) VALUES(73,'reset',?,?)", strings.Repeat("b", 64), time.Now().Add(time.Minute).Unix())
			mustExec("INSERT INTO invitation_codes(code_hash,expires_at,remaining_uses) VALUES(?,1,3)", strings.Repeat("c", 64))
			mustExec("UPDATE settings SET value='invitation' WHERE setting_key='registration_mode'")
			mustExec("INSERT INTO admin_events(event_id,actor_id,action,target_type,target_id,created_at) VALUES(?,73,'retained','user','73',1)", strings.Repeat("d", 32))
			mustExec("DELETE FROM schema_migrations WHERE version>=5")
			if err := s.Migrate(ctx); err != nil {
				t.Fatal("completed DDL retry", err)
			}
			for _, query := range []string{
				"SELECT COUNT(*) FROM users WHERE id=73",
				"SELECT COUNT(*) FROM account_states WHERE user_id=73 AND email_verified=TRUE AND verification_required=FALSE",
				"SELECT COUNT(*) FROM account_tokens WHERE user_id=73 AND purpose='reset'",
				"SELECT COUNT(*) FROM web_sessions WHERE user_id=73",
				"SELECT COUNT(*) FROM invitation_codes WHERE remaining_uses=3",
				"SELECT COUNT(*) FROM settings WHERE setting_key='registration_mode' AND value='invitation'",
				"SELECT COUNT(*) FROM admin_events WHERE actor_id=73 AND action='retained'",
			} {
				var count int
				if err := s.DB.QueryRow(query).Scan(&count); err != nil || count != 1 {
					t.Fatal("upgrade lost data", query, err)
				}
			}
		})
	}
}
