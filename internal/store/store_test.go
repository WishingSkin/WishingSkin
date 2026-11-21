package store

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestBind(t *testing.T) {
	for _, test := range []struct {
		name, dialect, query, want string
	}{
		{"mysql", "mysql", "SELECT ? + ?", "SELECT ? + ?"},
		{"postgres", "postgres", "SELECT ? + ?", "SELECT $1 + $2"},
		{"no parameters", "postgres", "SELECT 1", "SELECT 1"},
		{"unicode", "postgres", "SELECT 名称 FROM users WHERE id = ?", "SELECT 名称 FROM users WHERE id = $1"},
		{"many parameters", "postgres", "SELECT ?,?,?,?,?,?,?,?,?,?,?,?", "SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Store{Dialect: test.dialect}
			if got := s.Bind(test.query); got != test.want {
				t.Fatalf("Bind() = %q, want %q", got, test.want)
			}
			if got := s.Bind(test.query); got != test.want {
				t.Fatalf("placeholder counter did not reset: %q", got)
			}
		})
	}
}

func TestOpenRejectsUnsupportedDialect(t *testing.T) {
	if _, err := Open(context.Background(), "sqlite", ""); err == nil {
		t.Fatal("expected unsupported dialect error")
	}
}

func TestEmbeddedSchemasAgree(t *testing.T) {
	for _, migration := range schemaMigrations {
		t.Run(migration, func(t *testing.T) { assertEmbeddedSchemasAgree(t, migration) })
	}
}

func assertEmbeddedSchemasAgree(t *testing.T, migration string) {
	t.Helper()
	mysql, err := migrationFiles.ReadFile("migrations/" + migration + ".mysql.sql")
	if err != nil {
		t.Fatal(err)
	}
	postgres, err := migrationFiles.ReadFile("migrations/" + migration + ".postgres.sql")
	if err != nil {
		t.Fatal(err)
	}
	mysqlOptions := regexp.MustCompile(` ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin`)
	ownerIndex := regexp.MustCompile(`,? KEY texture_records_owner\s*\(owner_id\)|CREATE INDEX IF NOT EXISTS texture_records_owner ON texture_records\(owner_id\);`)
	wardrobeIndex := regexp.MustCompile(`CREATE INDEX IF NOT EXISTS wardrobe_record_id_idx ON wardrobe\(record_id\);`)
	oidcIndex := regexp.MustCompile(`, INDEX oidc_flow_expiry\(expires_at\)|CREATE INDEX IF NOT EXISTS oidc_flow_expiry ON oidc_flows\(expires_at\);`)
	extensionIndex := regexp.MustCompile(`, INDEX (webhook_due|plugin_event_due)\(state,next_at\)|CREATE INDEX IF NOT EXISTS (webhook_due|plugin_event_due) ON (webhook_deliveries|plugin_event_deliveries)\(state,next_at\);`)
	normalize := func(source string) string {
		// Compare DDL here; dialect-specific backfill/upsert behavior is tested against both databases.
		source = strings.SplitN(source, "INSERT INTO", 2)[0]
		source = strings.Join(strings.Fields(source), " ")
		source = strings.ReplaceAll(source, ` COLLATE "C"`, "")
		// PostgreSQL TEXT and MySQL LONGTEXT both preserve long legacy reasons.
		if migration == "009_legacy_reports" {
			source = strings.ReplaceAll(source, "LONGTEXT", "TEXT")
		}
		// PostgreSQL TEXT and MySQL MEDIUMTEXT both support large audit details.
		if migration == "019_admin_event_changes" {
			source = strings.ReplaceAll(source, "MEDIUMTEXT", "TEXT")
		}
		// Both forms widen the refresh credential column while retaining NOT NULL.
		if migration == "021_oauth_extensions" {
			source = strings.ReplaceAll(source, "MODIFY COLUMN credential_hash TEXT NOT NULL", "ALTER COLUMN credential_hash TYPE TEXT")
		}
		if migration == "023_theme_assets" {
			source = strings.ReplaceAll(source, "MODIFY COLUMN value MEDIUMTEXT NOT NULL", "ALTER COLUMN value TYPE TEXT")
		}
		source = extensionIndex.ReplaceAllString(source, "")
		source = mysqlOptions.ReplaceAllString(source, "")
		source = ownerIndex.ReplaceAllString(source, "")
		// InnoDB creates this index for the foreign key automatically.
		source = wardrobeIndex.ReplaceAllString(source, "")
		// The same OIDC expiry index is declared inline on MySQL.
		source = oidcIndex.ReplaceAllString(source, "")
		return strings.Join(strings.Fields(source), " ")
	}
	if normalize(string(mysql)) != normalize(string(postgres)) {
		t.Fatal("MySQL and PostgreSQL schema definitions differ")
	}
	tables := []string{"ygg_join_sessions"}
	if migration == "020_extensions" {
		tables = []string{"plugin_data", "plugin_migrations", "webhook_subscriptions", "webhook_deliveries", "oauth_code_metadata", "oauth_refresh_tokens", "plugin_event_deliveries"}
	}
	if migration == "021_oauth_extensions" {
		tables = []string{"oauth_access_families", "oauth_refresh_metadata", "web_session_auth"}
	}
	if migration == "022_account_verification" {
		tables = nil
	}
	if migration == "023_theme_assets" {
		tables = nil
	}
	if migration == "024_storage_integrity" {
		tables = []string{"texture_file_integrity"}
	}
	if migration == "019_admin_event_changes" {
		tables = []string{"admin_event_changes"}
	}
	if migration == "017_account_integrations" {
		tables = []string{"oauth_clients", "oauth_codes", "oauth_tokens", "external_identities"}
	}
	if migration == "018_content_review" {
		tables = []string{"texture_metadata"}
		if !strings.Contains(string(mysql), "DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin") {
			t.Error("content metadata table must match the referenced record ID collation")
		}
	}
	if migration == "016_totp" {
		tables = []string{"user_totp", "user_totp_recovery"}
	}
	if migration == "015_oidc" {
		tables = []string{"oidc_identities", "oidc_flows"}
		if !strings.Contains(string(mysql), "INDEX oidc_flow_expiry(expires_at)") || !strings.Contains(string(postgres), "INDEX IF NOT EXISTS oidc_flow_expiry ON oidc_flows(expires_at)") {
			t.Error("missing OIDC expiry index")
		}
	}
	if migration == "011_site_notices" {
		tables = []string{"site_notices"}
	}
	if migration == "012_plugins" {
		tables = []string{"plugins"}
	}
	if migration == "013_invitation_export" {
		tables = nil
		if !strings.Contains(string(mysql), "ALTER TABLE invitation_codes") || !strings.Contains(string(postgres), "ALTER TABLE invitation_codes") {
			t.Errorf("missing invitation_codes upgrade")
		}
	}
	if migration == "014_homepage_blocks" {
		tables = nil
		if !strings.Contains(string(mysql), "homepage_blocks") || !strings.Contains(string(postgres), "homepage_blocks") {
			t.Errorf("missing homepage blocks default")
		}
	}
	if migration == "010_user_profiles" {
		tables = []string{"user_profiles"}
	}
	if migration == "009_legacy_reports" {
		tables = []string{"legacy_reports"}
	}
	if migration == "008_points" {
		tables = []string{"point_entries"}
	}
	if migration == "007_reports" {
		tables = []string{"reports"}
	}
	if migration == "006_admin_events" {
		tables = []string{"admin_events"}
	}
	if migration == "005_account_access" {
		tables = []string{"account_states", "account_tokens", "invitation_codes"}
	}
	if migration == "004_wardrobe" {
		tables = []string{"wardrobe"}
	}
	if migration == "003_texture_records" {
		tables = []string{"texture_records"}
	}
	if migration == "001_initial" {
		tables = []string{"users", "characters", "textures", "settings", "web_sessions", "game_tokens", "migration_state"}
	}
	for _, table := range tables {
		if !strings.Contains(string(mysql), "CREATE TABLE IF NOT EXISTS "+table+" (") {
			t.Errorf("missing retryable table creation for %s", table)
		}
	}
	if strings.Contains(string(mysql), "AUTO_INCREMENT") || strings.Contains(string(postgres), "SERIAL") {
		t.Fatal("source IDs must be retained without automatic allocation")
	}
}
