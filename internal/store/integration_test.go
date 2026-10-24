package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// These opt-in tests only create and remove their own randomly named namespace.
// The supplied account needs CREATE DATABASE (MySQL) or CREATE SCHEMA (Postgres).
func TestDatabaseIntegration(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			envName := "WS_TEST_" + strings.ToUpper(dialect) + "_DSN"
			dsn := os.Getenv(envName)
			if dsn == "" {
				t.Skip(envName + " is unset; real database integration was not run")
			}
			s := isolatedTestStore(t, dialect, dsn)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			t.Run("interrupted_initialization_and_concurrent_migration", func(t *testing.T) {
				source, err := migrationFiles.ReadFile("migrations/001_initial." + dialect + ".sql")
				if err != nil {
					t.Fatal(err)
				}
				// Simulate process exit after the first committed DDL, before version recording.
				if _, err := s.DB.ExecContext(ctx, strings.SplitN(string(source), ";", 2)[0]); err != nil {
					t.Fatal(err)
				}
				results := make(chan error, 4)
				start := make(chan struct{})
				for range 4 {
					go func() {
						<-start
						results <- s.Migrate(ctx)
					}()
				}
				close(start)
				for range 4 {
					if err := <-results; err != nil {
						t.Error(err)
					}
				}
				if t.Failed() {
					t.FailNow()
				}
				if err := s.Migrate(ctx); err != nil {
					t.Fatal("second migration:", err)
				}
				var count int
				if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count); err != nil || count != 1 {
					t.Fatalf("migration version count = %d, error = %v", count, err)
				}
			})
			if t.Failed() {
				t.FailNow()
			}

			t.Run("concurrent_email_unique", func(t *testing.T) {
				assertOneUniqueWinner(t, dialect, func(index int) error {
					return insertTestUser(ctx, s, int64(index+1), "same@example.test")
				})
			})
			t.Run("concurrent_name_unique", func(t *testing.T) {
				if err := insertTestUser(ctx, s, 100, "owner@example.test"); err != nil {
					t.Fatal(err)
				}
				assertOneUniqueWinner(t, dialect, func(index int) error {
					_, err := s.DB.ExecContext(ctx, s.Bind("INSERT INTO characters (id, user_id, uuid, name, name_key) VALUES (?, ?, ?, ?, ?)"), index+1, 100, fmt.Sprintf("%032d", index+1), "Player", "player")
					return err
				})
			})
			t.Run("transaction_rollback", func(t *testing.T) {
				tx, err := s.DB.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err := tx.ExecContext(ctx, s.Bind("INSERT INTO settings (setting_key, value) VALUES (?, ?)"), "rollback", "value"); err != nil {
					t.Fatal(err)
				}
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := s.DB.QueryRowContext(ctx, s.Bind("SELECT COUNT(*) FROM settings WHERE setting_key = ?"), "rollback").Scan(&count); err != nil || count != 0 {
					t.Fatalf("rolled back row count = %d, error = %v", count, err)
				}
			})
			t.Run("binary_no_pad_comparison", func(t *testing.T) {
				for index, key := range []string{"Case", "case", "case "} {
					_, err := s.DB.ExecContext(ctx, s.Bind("INSERT INTO settings (setting_key, value) VALUES (?, ?)"), key, fmt.Sprint(index))
					if err != nil {
						t.Fatalf("distinct key %q rejected: %v", key, err)
					}
				}
			})
		})
	}
}

func insertTestUser(ctx context.Context, s *Store, id int64, emailKey string) error {
	_, err := s.DB.ExecContext(ctx, s.Bind("INSERT INTO users (id, email, email_key, nickname, password_hash, password_method, password_salt) VALUES (?, ?, ?, ?, ?, ?, ?)"), id, emailKey, emailKey, "Test", "hash", "test", "")
	return err
}

func assertOneUniqueWinner(t *testing.T, dialect string, insert func(int) error) {
	t.Helper()
	results := make(chan error, 2)
	start := make(chan struct{})
	for index := range 2 {
		go func() {
			<-start
			results <- insert(index)
		}()
	}
	close(start)
	winners := 0
	for range 2 {
		err := <-results
		if err == nil {
			winners++
			continue
		}
		var mysqlErr *mysql.MySQLError
		var postgresErr *pgconn.PgError
		if dialect == "mysql" && errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			continue
		}
		if dialect == "postgres" && errors.As(err, &postgresErr) && postgresErr.Code == "23505" {
			continue
		}
		t.Errorf("insert failed for a reason other than unique constraint: %v", err)
	}
	if winners != 1 {
		t.Errorf("successful concurrent inserts = %d, want exactly 1", winners)
	}
}

func isolatedTestStore(t *testing.T, dialect, dsn string) *Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := Open(ctx, dialect, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "ws_store_test_" + hex.EncodeToString(suffix[:])
	var isolatedDSN, cleanupSQL string
	if dialect == "mysql" {
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.DB.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
			t.Fatal("create isolated test database:", err)
		}
		cfg.DBName = name
		isolatedDSN = cfg.FormatDSN()
		cleanupSQL = "DROP DATABASE `" + name + "`"
	} else {
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.DB.ExecContext(ctx, `CREATE SCHEMA "`+name+`"`); err != nil {
			t.Fatal("create isolated test schema:", err)
		}
		cfg.RuntimeParams["search_path"] = name
		isolatedDSN = stdlib.RegisterConnConfig(cfg)
		t.Cleanup(func() { stdlib.UnregisterConnConfig(isolatedDSN) })
		cleanupSQL = `DROP SCHEMA "` + name + `" CASCADE`
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := admin.DB.ExecContext(cleanupCtx, cleanupSQL); err != nil {
			t.Errorf("remove test-created namespace %s: %v", name, err)
		}
	})
	s, err := Open(ctx, dialect, isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
