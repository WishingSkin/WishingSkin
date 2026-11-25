package bsmigration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/WishingSkin/WishingSkin/internal/store"
)

// Each case creates and drops only its own random database/schema, so package
// tests can run alongside store/httpapi tests against the same local server.
func isolatedStore(t *testing.T, dialect, dsn string) *store.Store {
	t.Helper()
	ctx := context.Background()
	base, err := store.Open(ctx, dialect, dsn)
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	name := "ws_bs_" + hex.EncodeToString(random)
	if dialect == "mysql" {
		if _, err := base.DB.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
			base.Close()
			t.Fatal("isolated fixture database:", err)
		}
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.DBName = name
		s, err := store.Open(ctx, dialect, cfg.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			s.Close()
			if _, err := base.DB.ExecContext(context.Background(), "DROP DATABASE `"+name+"`"); err != nil {
				t.Error(err)
			}
			base.Close()
		})
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if _, err := base.DB.ExecContext(ctx, "CREATE SCHEMA "+name); err != nil {
		base.Close()
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["search_path"] = name
	s := &store.Store{DB: stdlib.OpenDB(*cfg), Dialect: dialect}
	t.Cleanup(func() {
		s.Close()
		if _, err := base.DB.ExecContext(context.Background(), "DROP SCHEMA "+name+" CASCADE"); err != nil {
			t.Error(err)
		}
		base.Close()
	})
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

type resultRows struct {
	Email, EmailKey, Password, UUID, Name, Skin, Model, Hash, Kind, TextureModel, Key, Status string
	Score                                                                                     int64
	Permission                                                                                int
	Banned, Public                                                                            bool
}

func readResult(t *testing.T, s *store.Store) resultRows {
	t.Helper()
	var r resultRows
	if err := s.DB.QueryRow("SELECT email,email_key,password_hash,score,permission,banned FROM users WHERE id=7").Scan(&r.Email, &r.EmailKey, &r.Password, &r.Score, &r.Permission, &r.Banned); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT uuid,name,skin_hash,model FROM characters WHERE id=19").Scan(&r.UUID, &r.Name, &r.Skin, &r.Model); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT hash,kind,model,is_public FROM textures").Scan(&r.Hash, &r.Kind, &r.TextureModel, &r.Public); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='ygg_private_key'").Scan(&r.Key); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT status FROM migration_state WHERE id=1").Scan(&r.Status); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"web_sessions", "game_tokens"} {
		var count int
		if err := s.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("sessions not reset: %s %d %v", table, count, err)
		}
	}
	return r
}

func installFailure(t *testing.T, s *store.Store) func() {
	t.Helper()
	if s.Dialect == "mysql" {
		if _, err := s.DB.Exec("CREATE TRIGGER ws_bs_fail BEFORE INSERT ON characters FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='local fixture injected failure'"); err != nil {
			t.Fatal(err)
		}
		return func() {
			if _, err := s.DB.Exec("DROP TRIGGER ws_bs_fail"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.DB.Exec("CREATE FUNCTION ws_bs_fail_fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'local fixture injected failure'; END $$"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("CREATE TRIGGER ws_bs_fail BEFORE INSERT ON characters FOR EACH ROW EXECUTE FUNCTION ws_bs_fail_fn()"); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := s.DB.Exec("DROP TRIGGER ws_bs_fail ON characters"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec("DROP FUNCTION ws_bs_fail_fn()"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIntegrationImportBothDatabases(t *testing.T) {
	var previous *resultRows
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("set WS_TEST_" + strings.ToUpper(dialect) + "_DSN for isolated local integration")
			}
			snapshot, source := fixture(t)
			var ordinary resultRows
			for _, recover := range []bool{false, true} {
				t.Run(fmt.Sprintf("recover_%t", recover), func(t *testing.T) {
					s := isolatedStore(t, dialect, dsn)
					target := filepath.Join(t.TempDir(), "textures")
					if recover {
						var homepageBefore string
						if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='homepage_blocks'").Scan(&homepageBefore); err != nil {
							t.Fatal(err)
						}
						undo := installFailure(t, s)
						if failed, err := Import(context.Background(), s, snapshot, source, target); err == nil || failed.Imported {
							t.Fatal("injected failure was ignored")
						}
						for _, table := range []string{"users", "characters", "textures", "texture_records", "settings", "account_states", "account_tokens", "invitation_codes", "admin_events", "reports", "point_entries"} {
							var n int
							query := "SELECT COUNT(*) FROM " + table
							if table == "settings" {
								query += ` WHERE NOT ((setting_key='character_limit' AND value='1') OR (setting_key='registration_mode' AND value='closed') OR (setting_key='points_rules' AND value='{"enabled":false,"dailyReward":0,"uploadCost":0,"characterCost":0}') OR setting_key='homepage_blocks')`
							}
							if err := s.DB.QueryRow(query).Scan(&n); err != nil || n != 0 {
								t.Fatalf("rollback %s: %d %v", table, n, err)
							}
						}
						var homepageAfter string
						if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='homepage_blocks'").Scan(&homepageAfter); err != nil || homepageAfter != homepageBefore {
							t.Fatal("rollback changed default homepage", err)
						}
						var state string
						if err := s.DB.QueryRow("SELECT status FROM migration_state WHERE id=1").Scan(&state); err != nil || state != "pending" {
							t.Fatalf("missing durable checkpoint: %s %v", state, err)
						}
						if _, err := os.Stat(filepath.Join(target, snapshot.Textures[0].Hash)); err != nil {
							t.Fatal("checkpoint file not preserved:", err)
						}
						undo()
					}
					r, err := Import(context.Background(), s, snapshot, source, target)
					if err != nil {
						t.Fatal(err)
					}
					if !r.Imported || r.Status != "partial" || r.AlreadyImported {
						t.Fatalf("unexpected import report: %+v", r)
					}
					got := readResult(t, s)
					if got.EmailKey != "fixture@example.invalid" || got.UUID != snapshot.Characters[0].UUID || got.Password != snapshot.Users[0].PasswordHash || got.Skin != snapshot.Textures[0].Hash || got.Hash != got.Skin || got.Model != "slim" || got.Key != snapshot.SignaturePrivateKey || got.Public || got.Score != 42 || got.Status != "partial" {
						t.Fatalf("migrated rows differ (signing key redacted): email=%s UUID=%s model=%s score=%d", got.EmailKey, got.UUID, got.Model, got.Score)
					}
					if recover {
						if !reflect.DeepEqual(got, ordinary) {
							t.Fatal("recovered import differs from ordinary import")
						}
					} else {
						ordinary = got
					}
					if err := verifyCopies(target, &fileJournal{Files: map[string]string{snapshot.Textures[0].Hash: snapshot.Textures[0].Hash}}); err != nil {
						t.Fatal(err)
					}
					again, err := Import(context.Background(), s, snapshot, source, target)
					if err != nil || !again.AlreadyImported {
						t.Fatalf("same-task repeat: %+v %v", again, err)
					}
					if after := readResult(t, s); !reflect.DeepEqual(got, after) {
						t.Fatal("repeat import changed target")
					}
					changed := *snapshot
					changed.Source.Instance = "another-local-task"
					if _, err := Import(context.Background(), s, &changed, source, target); err == nil {
						t.Fatal("different task accepted nonempty target")
					}
				})
			}
			// The fixture key is dynamically generated once for the package, so both
			// dialects must preserve identical bytes and produce identical rows.
			if previous != nil && !reflect.DeepEqual(*previous, ordinary) {
				t.Fatal("MySQL and PostgreSQL migration outcomes differ")
			}
			previous = &ordinary
		})
	}
}

func TestIntegrationRepeatedHashPreservesRecords(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("test DSN unset")
			}
			s := isolatedStore(t, dialect, dsn)
			snapshot, source := fixture(t)
			copy := snapshot.Textures[0]
			copy.ID++
			copy.Name = "Independent shared image"
			copy.Type = "steve"
			copy.Public = true
			zeroDate := "0000-00-00 00:00:00"
			copy.UploadedAt = &zeroDate
			snapshot.Textures = append(snapshot.Textures, copy)
			r, err := Import(context.Background(), s, snapshot, source, filepath.Join(t.TempDir(), "textures"))
			if err != nil || !r.Imported || r.Files != 1 || r.Textures != 2 {
				t.Fatal("shared file import", r, err)
			}
			for _, query := range []struct {
				sql  string
				want int
			}{
				{"SELECT COUNT(*) FROM textures", 1},
				{"SELECT COUNT(*) FROM texture_records", 2},
				{"SELECT COUNT(*) FROM texture_records WHERE created_at=0", 2},
				{"SELECT COUNT(*) FROM texture_records WHERE name='Independent shared image' AND model='default' AND is_public=TRUE AND owner_id IS NULL", 1},
			} {
				var count int
				if err := s.DB.QueryRow(query.sql).Scan(&count); err != nil || count != query.want {
					t.Fatal(query.sql, count, err)
				}
			}
		})
	}
}

func TestIntegrationRejectsNonemptyBusinessTarget(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("local database DSN is not configured")
			}
			s := isolatedStore(t, dialect, dsn)
			if _, err := s.DB.Exec("INSERT INTO settings(setting_key,value) VALUES('preexisting_fixture','preserve')"); err != nil {
				t.Fatal(err)
			}
			snapshot, source := fixture(t)
			target := filepath.Join(t.TempDir(), "must-not-be-created")
			if r, err := Import(context.Background(), s, snapshot, source, target); err == nil || r.Imported {
				t.Fatal("nonempty business target accepted")
			}
			var value string
			if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='preexisting_fixture'").Scan(&value); err != nil || value != "preserve" {
				t.Fatal("existing target data changed", err)
			}
			var n int
			if err := s.DB.QueryRow("SELECT COUNT(*) FROM migration_state").Scan(&n); err != nil || n != 0 {
				t.Fatal("nonempty target was claimed", err)
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("nonempty business target created texture files")
			}
			if _, err := s.DB.Exec("DELETE FROM settings WHERE setting_key='preexisting_fixture'"); err != nil {
				t.Fatal(err)
			}
			custom := `{"enabled":false,"dailyReward":5,"uploadCost":0,"characterCost":0}`
			if _, err := s.DB.Exec(s.Bind("UPDATE settings SET value=? WHERE setting_key='points_rules'"), custom); err != nil {
				t.Fatal(err)
			}
			if r, err := Import(context.Background(), s, snapshot, source, target); err == nil || r.Imported {
				t.Fatal("custom points rules accepted as empty target")
			}
			if err := s.DB.QueryRow("SELECT value FROM settings WHERE setting_key='points_rules'").Scan(&value); err != nil || value != custom {
				t.Fatal("custom rules changed", err)
			}
		})
	}
}

func TestIntegrationReadOnlySourceExport(t *testing.T) {
	dsn := os.Getenv("WS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set WS_TEST_MYSQL_DSN")
	}
	s := isolatedStore(t, "mysql", dsn)
	fixtureSnapshot, source := fixture(t)
	fixtureSnapshot.FormatVersion = FormatVersion
	verified := true
	fixtureSnapshot.Users[0].Verified = &verified
	fixtureSnapshot.Textures[0].Uploader = &fixtureSnapshot.Users[0].ID
	fixtureSnapshot.Textures[0].Description = "Migrated <b>plain text</b> 😀"
	alias := "收藏别名 <script>plain</script>"
	fixtureSnapshot.Wardrobe = []Collection{{UserID: 7, TextureID: 11, Name: &alias}}
	statements := []string{
		"CREATE TABLE bs_users(uid INT PRIMARY KEY,email VARCHAR(100),nickname VARCHAR(50),password VARCHAR(255),score INT,permission INT,verified TINYINT)",
		"CREATE TABLE bs_players(pid INT PRIMARY KEY,uid INT,name VARCHAR(50),tid_skin INT,tid_cape INT)",
		"CREATE TABLE bs_textures(tid INT PRIMARY KEY,name VARCHAR(50),type VARCHAR(10),hash VARCHAR(64),public TINYINT,uploader INT)",
		"CREATE TABLE bs_user_closet(user_uid INT,texture_tid INT,item_name TEXT)",
		"CREATE TABLE bs_textures_description(id INT PRIMARY KEY,tid INT,description VARCHAR(255))",
		"CREATE TABLE bs_uuid(id INT PRIMARY KEY,name VARCHAR(255),uuid VARCHAR(255))",
		"CREATE TABLE bs_options(id INT PRIMARY KEY,option_name VARCHAR(50),option_value LONGTEXT)",
		"CREATE TABLE bs_extra_plugin(id INT PRIMARY KEY,payload TEXT)",
		"CREATE TABLE bs_reports(id BIGINT PRIMARY KEY,tid INT,uploader INT,reporter INT,reason LONGTEXT,status INT,report_at DATETIME NULL)",
	}
	for _, q := range statements {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	u, c, texture := fixtureSnapshot.Users[0], fixtureSnapshot.Characters[0], fixtureSnapshot.Textures[0]
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO bs_users VALUES(?,?,?,?,?,?,?)", []any{u.ID, u.Email, u.Nickname, u.PasswordHash, u.Score, u.Permission, *u.Verified}},
		{"INSERT INTO bs_players VALUES(?,?,?,?,?)", []any{c.ID, c.UserID, c.Name, c.SkinID, c.CapeID}},
		{"INSERT INTO bs_textures VALUES(?,?,?,?,?,?)", []any{texture.ID, texture.Name, texture.Type, texture.Hash, texture.Public, *texture.Uploader}},
		{"INSERT INTO bs_user_closet VALUES(?,?,?)", []any{u.ID, texture.ID, alias}},
		{"INSERT INTO bs_textures_description VALUES(1,?,?)", []any{texture.ID, texture.Description}},
		{"INSERT INTO bs_uuid VALUES(1,?,?)", []any{c.Name, c.UUID}},
		{"INSERT INTO bs_options VALUES(1,'ygg_private_key',?),(2,'ygg_uuid_algorithm','v4'),(3,'unhandled_plugin_option','keep'),(4,'sign_score','5,50'),(5,'score_per_player','250'),(6,'smtp_password','must-not-export'),(7,'site_name','Source <b>name</b>'),(8,'announcement','Source <script>literal</script>')", []any{fixtureSnapshot.SignaturePrivateKey}},
		{"INSERT INTO bs_extra_plugin VALUES(1,'not migrated')", nil},
		{"INSERT INTO bs_reports VALUES(31,99999,77777,88888,?,0,'2020-01-02 03:04:05'),(32,0,-1,0,'resolved',1,NULL)", []any{"Historical <b>plain</b>\n" + strings.Repeat("旧", 24000)}},
	} {
		if _, err := s.DB.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	exported, err := ExportMySQL(context.Background(), s.DB, ExportOptions{Source: fixtureSnapshot.Source, TablePrefix: "bs_"})
	if err != nil {
		t.Fatal(err)
	}
	if exported.FormatVersion != FormatVersion || !reflect.DeepEqual(exported.Wardrobe, fixtureSnapshot.Wardrobe) || !reflect.DeepEqual(exported.Users, fixtureSnapshot.Users) || !reflect.DeepEqual(exported.Characters, fixtureSnapshot.Characters) || !reflect.DeepEqual(exported.Textures, fixtureSnapshot.Textures) || exported.SignaturePrivateKey != fixtureSnapshot.SignaturePrivateKey {
		t.Fatal("export changed identity, credentials, material or key")
	}
	if _, err := s.DB.Exec("ALTER TABLE bs_textures ADD COLUMN upload_at DATETIME(6) NULL"); err != nil {
		t.Fatal(err)
	}
	withNullDate, err := ExportMySQL(context.Background(), s.DB, ExportOptions{Source: fixtureSnapshot.Source, TablePrefix: "bs_"})
	if err != nil || !reflect.DeepEqual(withNullDate.Textures, fixtureSnapshot.Textures) {
		t.Fatal("NULL upload date changed exported textures", err)
	}
	const sourceUpload = "2024-01-02 03:04:05.123456"
	if _, err := s.DB.Exec("UPDATE bs_textures SET upload_at=?", sourceUpload); err != nil {
		t.Fatal(err)
	}
	exported, err = ExportMySQL(context.Background(), s.DB, ExportOptions{Source: fixtureSnapshot.Source, TablePrefix: "bs_"})
	if err != nil {
		t.Fatal(err)
	}
	if uploadedAt := exported.Textures[0].UploadedAt; uploadedAt == nil || *uploadedAt != sourceUpload {
		t.Fatal("source texture upload date was not exported")
	}
	if len(exported.ScoreRules) != len(scoreDefaults) || exported.ScoreRules["sign_score"] != "5,50" || exported.ScoreRules["score_per_player"] != "250" || exported.ScoreRules["return_score"] != "true" || exported.ScoreRules["smtp_password"] != "" {
		t.Fatal("score inventory override, default or secret boundary")
	}
	if len(exported.Branding) != 3 || exported.Branding["site_name"] != "Source <b>name</b>" || exported.Branding["announcement"] != "Source <script>literal</script>" || exported.Branding["site_description"] != brandingDefaults["site_description"] {
		t.Fatal("source branding/defaults changed")
	}
	exportedJSON, _ := json.Marshal(exported)
	if !strings.Contains(string(exportedJSON), `"legacy_reports":{"items":[{"id":31,`) || !strings.Contains(string(exportedJSON), `"report_at":"2020-01-02 03:04:05"`) || !strings.Contains(string(exportedJSON), `"report_at":null`) || !strings.Contains(string(exportedJSON), strings.Repeat("旧", 24000)) {
		t.Fatal("source report history/time/long reason missing")
	}
	r, err := Preflight(exported, source)
	if err != nil {
		t.Fatalf("exported preflight: %v %+v", err, r.Blockers)
	}
	var found bool
	for _, o := range r.Unsupported {
		if o.Scope == "table:extra_plugin" && o.Count == 1 {
			found = true
		}
		if o.Scope == "column:textures.upload_at" || o.Scope == "texture_upload_dates" {
			t.Fatalf("valid upload date incorrectly reported as omitted: %+v", o)
		}
	}
	if !found {
		t.Fatal("unknown plugin table was not reported")
	}
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM bs_uuid").Scan(&count); err != nil || count != 1 {
		t.Fatal("export altered UUID source", count, err)
	}
	var payload string
	if err := s.DB.QueryRow("SELECT payload FROM bs_extra_plugin WHERE id=1").Scan(&payload); err != nil || payload != "not migrated" {
		t.Fatal("export altered unknown source data", err)
	}
	if _, err := ExportMySQL(context.Background(), s.DB, ExportOptions{Source: fixtureSnapshot.Source, TablePrefix: "bs_; DELETE"}); err == nil {
		t.Fatal("unsafe prefix accepted")
	}
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run("export_to_"+dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("test DSN unset")
			}
			target := isolatedStore(t, dialect, dsn)
			r, err := Import(context.Background(), target, exported, source, filepath.Join(t.TempDir(), "textures"))
			if err != nil || !r.Imported || r.Collections != 1 {
				t.Fatal("v2 export import", r, err)
			}
			var points string
			var historicalReason, historicalAt string
			if err := target.DB.QueryRow("SELECT reason,report_at FROM legacy_reports WHERE id=31").Scan(&historicalReason, &historicalAt); err != nil || historicalReason != "Historical <b>plain</b>\n"+strings.Repeat("旧", 24000) || historicalAt != "2020-01-02 03:04:05" {
				t.Fatal("export/import changed original report text/time", err)
			}
			var balance int64
			if err := target.DB.QueryRow("SELECT value FROM settings WHERE setting_key='points_rules'").Scan(&points); err != nil || points != `{"enabled":false,"dailyReward":0,"uploadCost":0,"characterCost":0}` {
				t.Fatal("source prices enabled target points", err)
			}
			if err := target.DB.QueryRow("SELECT score FROM users").Scan(&balance); err != nil || balance != u.Score || r.Points.OriginalRules["sign_score"] != "5,50" || r.Points.Policy != "preserve_balance_disabled_manual_configuration" {
				t.Fatal("balance or score reconciliation missing", err)
			}
			var owner int64
			var description, displayName string
			var collected, uploaded int64
			if err := target.DB.QueryRow("SELECT r.owner_id,r.description,w.display_name,w.collected_at,r.created_at FROM texture_records r JOIN wardrobe w ON w.record_id=r.record_id").Scan(&owner, &description, &displayName, &collected, &uploaded); err != nil || owner != u.ID || description != texture.Description || displayName != alias || collected != 0 || uploaded != 1704164645 {
				t.Fatal("v2 relationship or timestamp mismatch", owner, description, displayName, collected, uploaded, err)
			}
		})
	}
	if _, err := s.DB.Exec("UPDATE bs_users SET verified=2"); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportMySQL(context.Background(), s.DB, ExportOptions{Source: fixtureSnapshot.Source, TablePrefix: "bs_"}); err == nil || !strings.Contains(err.Error(), "email verification") {
		t.Fatal("invalid verification state accepted", err)
	}
	if _, err := s.DB.Exec("UPDATE bs_users SET verified=0"); err != nil {
		t.Fatal(err)
	}
	unverified, err := ExportMySQL(context.Background(), s.DB, ExportOptions{Source: fixtureSnapshot.Source, TablePrefix: "bs_"})
	if err != nil || unverified.Users[0].Verified == nil || *unverified.Users[0].Verified {
		t.Fatal("explicit unverified state lost", err)
	}
	if _, err := s.DB.Exec("INSERT INTO bs_user_closet VALUES(7,11,NULL)"); err != nil {
		t.Fatal(err)
	}
	duplicate, err := ExportMySQL(context.Background(), s.DB, ExportOptions{Source: fixtureSnapshot.Source, TablePrefix: "bs_"})
	if err != nil {
		t.Fatal(err)
	}
	if report, err := Preflight(duplicate, source); err == nil || report.Status != "blocked" {
		t.Fatal("duplicate source collection merged")
	}
	if _, err := s.DB.Exec("INSERT INTO bs_textures_description VALUES(2,999,'orphan')"); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportMySQL(context.Background(), s.DB, ExportOptions{Source: fixtureSnapshot.Source, TablePrefix: "bs_"}); err == nil {
		t.Fatal("orphan description silently discarded")
	}
}
