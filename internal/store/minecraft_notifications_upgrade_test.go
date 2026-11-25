package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSchema28UpgradeBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("real database DSN unset")
			}
			db := isolatedTestStore(t, dialect, dsn)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.DB.ExecContext(ctx, db.Bind(query), args...); err != nil {
					t.Fatal(err)
				}
			}
			for _, table := range []string{"minecraft_flows", "minecraft_verifications", "user_notifications"} {
				exec("DROP TABLE " + table)
			}
			exec("DELETE FROM schema_migrations WHERE version>=28")
			if err := insertTestUser(ctx, db, 71, "legacy@example.test"); err != nil {
				t.Fatal(err)
			}
			const uuid = "0123456789abcdef0123456789abcdef"
			exec("INSERT INTO characters(id,user_id,uuid,name,name_key) VALUES(71,71,?,'OldPlayer','oldplayer')", uuid)
			if err := db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			var stored string
			if err := db.DB.QueryRowContext(ctx, "SELECT uuid FROM characters WHERE id=71").Scan(&stored); err != nil || stored != uuid {
				t.Fatalf("existing character changed: %v", err)
			}
			exec("INSERT INTO user_notifications(notification_id,user_id,kind,title,body,created_at) VALUES(?,71,'test','title','body',42)", uuid)
			exec("INSERT INTO minecraft_verifications(user_id,mc_uuid,mc_name,ms_subject,verified_at) VALUES(71,?,'Player',?,42)", uuid, strings.Repeat("b", 64))
			exec("DELETE FROM characters WHERE id=71")
			exec("DELETE FROM users WHERE id=71")
			var count int
			if err := db.DB.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM user_notifications)+(SELECT COUNT(*) FROM minecraft_verifications)").Scan(&count); err != nil || count != 0 {
				t.Fatalf("notifications and bindings did not cascade: %d %v", count, err)
			}
		})
	}
}
