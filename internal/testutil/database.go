// Package testutil opens a newly created namespace, never the caller's tables.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/WishingSkin/WishingSkin/internal/store"
)

func Database(t *testing.T, dialect string) *store.Store {
	t.Helper()
	env := "WS_TEST_POSTGRES_DSN"
	if dialect == "mysql" {
		env = "WS_TEST_MYSQL_DSN"
	}
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip(env + " not set: real database test NOT RUN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := store.Open(ctx, dialect, dsn)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	name := "ws_http_" + hex.EncodeToString(b)
	if dialect == "mysql" {
		if _, err := admin.DB.ExecContext(ctx, "CREATE DATABASE "+name+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
			admin.Close()
			t.Fatal(err)
		}
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.DBName = name
		dsn = cfg.FormatDSN()
	} else {
		if _, err := admin.DB.ExecContext(ctx, "CREATE SCHEMA "+name); err != nil {
			admin.Close()
			t.Fatal(err)
		}
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", name)
		u.RawQuery = q.Encode()
		dsn = u.String()
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		defer admin.Close()
		query := "DROP SCHEMA " + name + " CASCADE"
		if dialect == "mysql" {
			query = "DROP DATABASE " + name
		}
		if _, err := admin.DB.ExecContext(cleanupCtx, query); err != nil {
			t.Errorf("cleanup isolated namespace: %v", err)
		}
	})
	db, err := store.Open(ctx, dialect, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}
