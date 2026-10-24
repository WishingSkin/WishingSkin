package bootstrap_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"wishingskin/internal/bootstrap"
	"wishingskin/internal/credential"
	"wishingskin/internal/testutil"
)

func TestInitialAdministratorBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			s := testutil.Database(t, dialect)
			ctx := context.Background()
			for _, test := range []struct{ email, password string }{{"bad", "Initial123!"}, {"admin@example.test", "weak"}} {
				if _, err := bootstrap.CreateAdmin(ctx, s, test.email, "first", test.password); err == nil {
					t.Fatal("invalid input accepted")
				}
			}
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for i := range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := bootstrap.CreateAdmin(ctx, s, fmt.Sprintf("Admin%d@Example.test", i), "first", "Initial123!")
					results <- err
				}()
			}
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				} else if !errors.Is(err, bootstrap.ErrNotEmpty) {
					t.Fatal(err)
				}
			}
			if success != 1 {
				t.Fatal("concurrent initialization", success)
			}
			var id int64
			var permission int
			var email, key, hash, method string
			var verified, required bool
			if err := s.DB.QueryRow("SELECT u.id,u.email,u.email_key,u.password_hash,u.password_method,u.permission,a.email_verified,a.verification_required FROM users u JOIN account_states a ON a.user_id=u.id").Scan(&id, &email, &key, &hash, &method, &permission, &verified, &required); err != nil {
				t.Fatal(err)
			}
			valid, err := credential.Verify(method, hash, "", "Initial123!")
			if err != nil || !valid || hash == "Initial123!" || permission != 2 || verified || required || key != strings.ToLower(email) {
				t.Fatal("unsafe initial account", err)
			}
			var actor int64
			var action, target, mode string
			if err := s.DB.QueryRow("SELECT actor_id,action,target_id FROM admin_events").Scan(&actor, &action, &target); err != nil || actor != 0 || action != "administrator.initialize" || target != fmt.Sprint(id) {
				t.Fatal("initialization audit", err)
			}
			if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='registration_mode'").Scan(&mode); err != nil || mode != "open" {
				t.Fatal("initialization changed registration", err)
			}
			if _, err := bootstrap.CreateAdmin(ctx, s, email, "replace", "Changed123!"); !errors.Is(err, bootstrap.ErrNotEmpty) {
				t.Fatal("existing account overwritten", err)
			}
		})
	}
}

func TestInitialAdministratorRejectsMigrationAndAuditFailure(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			s := testutil.Database(t, dialect)
			ctx := context.Background()
			mustExec := func(query string) {
				t.Helper()
				if _, err := s.DB.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			mustExec("INSERT INTO migration_state(id,source_fingerprint,status,report) VALUES(1,'synthetic','pending','{}')")
			if _, err := bootstrap.CreateAdmin(ctx, s, "admin@example.test", "first", "Initial123!"); !errors.Is(err, bootstrap.ErrNotEmpty) {
				t.Fatal("migration gate", err)
			}
			mustExec("DELETE FROM migration_state WHERE id=1")
			mustExec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			if _, err := bootstrap.CreateAdmin(ctx, s, "admin@example.test", "first", "Initial123!"); err == nil {
				t.Fatal("audit failure ignored")
			}
			var count int
			if err := s.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil || count != 0 {
				t.Fatal("partial administrator survived rollback", err)
			}
			mustExec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			if _, err := bootstrap.CreateAdmin(ctx, s, "admin@example.test", "first", "Initial123!"); err != nil {
				t.Fatal("retry after rollback", err)
			}
		})
	}
}
