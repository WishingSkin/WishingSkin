package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"fmt"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const schemaVersion = 25

// CurrentSchemaVersion identifies the schema understood by offline backups.
const CurrentSchemaVersion = schemaVersion

var schemaMigrations = []string{"001_initial", "002_ygg_sessions", "003_texture_records", "004_wardrobe", "005_account_access", "006_admin_events", "007_reports", "008_points", "009_legacy_reports", "010_user_profiles", "011_site_notices", "012_plugins", "013_invitation_export", "014_homepage_blocks", "015_oidc", "016_totp", "017_account_integrations", "018_content_review", "019_admin_event_changes", "020_extensions", "021_oauth_extensions", "022_account_verification", "023_theme_assets", "024_storage_integrity", "025_texture_likes"}

// Migrate serializes initialization across processes using a session lock.
// Each DDL statement is independently retryable; MySQL DDL is not transactional.
func (s *Store) Migrate(ctx context.Context) (err error) {
	if s.Dialect != "mysql" && s.Dialect != "postgres" {
		return fmt.Errorf("unsupported database dialect %q", s.Dialect)
	}
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()
	if err := s.lockMigration(ctx, conn); err != nil {
		return err
	}
	defer func() {
		// Cancellation of the caller must not leave a lock in the connection pool.
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if unlockErr := s.unlockMigration(unlockCtx, conn); unlockErr != nil {
			// A failed unlock cannot be returned to the pool with a session lock.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if err == nil {
				err = unlockErr
			}
		}
	}()

	if _, err := conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER NOT NULL PRIMARY KEY)"); err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}
	var version int
	if err := conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, schemaVersion)
	}
	for next := version + 1; next <= schemaVersion; next++ {
		source, err := migrationFiles.ReadFile("migrations/" + schemaMigrations[next-1] + "." + s.Dialect + ".sql")
		if err != nil {
			return fmt.Errorf("read schema migration %d: %w", next, err)
		}
		for _, statement := range strings.Split(string(source), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if next == 13 {
				exists, err := s.invitationExportDDLExists(ctx, conn, statement)
				if err != nil {
					return fmt.Errorf("inspect schema migration 13: %w", err)
				}
				if exists {
					continue
				}
			}
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply schema migration %d: %w", next, err)
			}
		}
		if next == 23 {
			if err := s.migrateThemeAssetSettings(ctx, conn); err != nil {
				return fmt.Errorf("split theme asset settings: %w", err)
			}
		}
		if _, err := conn.ExecContext(ctx, s.Bind("INSERT INTO schema_migrations (version) VALUES (?)"), next); err != nil {
			return fmt.Errorf("record schema migration %d: %w", next, err)
		}
	}
	return nil
}

// MySQL lacks ADD COLUMN IF NOT EXISTS. Inspect each schema-13 DDL under the
// migration lock so an exit between statements can resume without losing data.
func (s *Store) invitationExportDDLExists(ctx context.Context, conn *sql.Conn, statement string) (bool, error) {
	var query, name string
	if strings.HasPrefix(statement, "ALTER TABLE invitation_codes ADD COLUMN ") {
		name = strings.Fields(statement)[5]
		query = "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='invitation_codes' AND column_name=?"
		if s.Dialect == "mysql" {
			query = "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='invitation_codes' AND column_name=?"
		}
	} else if strings.HasPrefix(statement, "CREATE UNIQUE INDEX invitation_codes_source_id ") {
		name = "invitation_codes_source_id"
		query = "SELECT COUNT(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='invitation_codes' AND indexname=?"
		if s.Dialect == "mysql" {
			query = "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='invitation_codes' AND index_name=?"
		}
	} else {
		return false, fmt.Errorf("unexpected invitation export DDL")
	}
	var count int
	err := conn.QueryRowContext(ctx, s.Bind(query), name).Scan(&count)
	return count != 0, err
}

func (s *Store) lockMigration(ctx context.Context, conn *sql.Conn) error {
	if s.Dialect == "mysql" {
		var acquired sql.NullInt64
		// GET_LOCK names are server-wide and limited to 64 characters.
		if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT('wishingskin:', LEFT(SHA2(DATABASE(), 256), 40)), 30)").Scan(&acquired); err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
		if !acquired.Valid || acquired.Int64 != 1 {
			return fmt.Errorf("acquire migration lock: timed out or no database selected")
		}
		return nil
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(6289653488640181)"); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	return nil
}

func (s *Store) unlockMigration(ctx context.Context, conn *sql.Conn) error {
	query := "SELECT pg_advisory_unlock(6289653488640181)"
	if s.Dialect == "mysql" {
		query = "SELECT RELEASE_LOCK(CONCAT('wishingskin:', LEFT(SHA2(DATABASE(), 256), 40)))"
	}
	if _, err := conn.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("release migration lock: %w", err)
	}
	return nil
}
