// Package store owns the SQL connection pool and versioned database schema.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	DB      *sql.DB
	Dialect string
}

// Open verifies connectivity without changing the database schema.
func Open(ctx context.Context, dialect, dsn string) (*Store, error) {
	dialect = strings.ToLower(strings.TrimSpace(dialect))
	var driver string
	switch dialect {
	case "mysql":
		driver = "mysql"
	case "postgres":
		driver = "pgx"
	default:
		return nil, fmt.Errorf("unsupported database dialect %q", dialect)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", dialect, err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s database: %w", dialect, err)
	}
	return &Store{DB: db, Dialect: dialect}, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

// Bind converts placeholders in internal static queries to the selected dialect.
// It is not a SQL parser: query literals and comments must not contain '?'.
func (s *Store) Bind(query string) string {
	if s.Dialect != "postgres" {
		return query
	}
	var out strings.Builder
	out.Grow(len(query))
	index := 0
	for _, char := range query {
		if char == '?' {
			index++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(index))
		} else {
			out.WriteRune(char)
		}
	}
	return out.String()
}
