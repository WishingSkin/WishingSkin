package backup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"wishingskin/internal/bsmigration"
	"wishingskin/internal/store"
	"wishingskin/internal/testutil"
)

var fixtureKey = sync.OnceValues(func() (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), nil
})

func seed(t *testing.T, dialect string) (*store.Store, string) {
	t.Helper()
	db := testutil.Database(t, dialect)
	snapshot, err := bsmigration.Load("../../testdata/bs/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.SignaturePrivateKey, err = fixtureKey()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "source")
	if _, err := bsmigration.Import(context.Background(), db, snapshot, "../../testdata/bs", dir); err != nil {
		t.Fatal(err)
	}
	// Populate every backed-up table and every excluded credential type.
	var record string
	if err := db.DB.QueryRow("SELECT record_id FROM texture_records LIMIT 1").Scan(&record); err != nil {
		t.Fatal(err)
	}
	var uid int64
	var uuid string
	if err := db.DB.QueryRow("SELECT user_id,uuid FROM characters LIMIT 1").Scan(&uid, &uuid); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.DB.Exec(db.Bind(query), args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE users SET score=?,permission=? WHERE id=?", int64(9007199254740993), 1, uid)
	exec("UPDATE textures SET deleted=?", true)
	exec("UPDATE texture_records SET owner_id=?,description=?,is_public=?", uid, "原样保留描述 🧥", false)
	exec("INSERT INTO wardrobe(user_id,record_id,display_name,collected_at) VALUES(?,?,?,?)", uid, record, "旧收藏", 0)
	exec("INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(?,?,?)", uid, false, false)
	exec("INSERT INTO user_profiles(user_id,avatar_mode,character_uuid,avatar_image) VALUES(?,?,?,?)", uid, "character", uuid, "")
	exec("INSERT INTO user_totp(user_id,secret_encrypted,enabled,created_at,last_used_step) VALUES(?,?,TRUE,?,?)", uid, "synthetic-encrypted-totp", 1700000000, 42)
	exec("INSERT INTO user_totp_recovery(user_id,code_hash) VALUES(?,?)", uid, strings.Repeat("7", 64))
	exec("INSERT INTO plugins(plugin_id,version,enabled,settings_json) VALUES('community.fixture','1.0.0',TRUE,'synthetic-encrypted-plugin-settings')")
	exec("INSERT INTO settings(setting_key,value) VALUES(?,?)", "service_config", "synthetic-opaque-encrypted-setting")
	exec("INSERT INTO settings(setting_key,value) VALUES(?,?)", "oidc_config", "synthetic-opaque-oidc-setting")
	exec("INSERT INTO oidc_identities(identity_key,issuer_key,user_id,issuer,subject,created_at) VALUES(?,?,?,?,?,?)", strings.Repeat("e", 64), strings.Repeat("f", 64), uid, "https://issuer.example.test", "synthetic-subject", 1700000000)
	exec("INSERT INTO oidc_flows(flow_hash,value,expires_at) VALUES(?,?,?)", strings.Repeat("d", 64), "synthetic-encrypted-flow", 2000000000)
	exec("INSERT INTO oauth_clients(client_id,name,redirect_uris,secret_hash,is_public,enabled) VALUES(?,'Fixture','[\"https://app.example/cb\"]',?,FALSE,TRUE)", strings.Repeat("a", 32), strings.Repeat("b", 64))
	exec("INSERT INTO oauth_codes(code_hash,client_id,user_id,redirect_uri,scope,challenge,credential_hash,expires_at) VALUES(?,?,?,'https://app.example/cb','profile',?,'fixture',2000000000)", strings.Repeat("a", 64), strings.Repeat("a", 32), uid, strings.Repeat("a", 43))
	exec("INSERT INTO oauth_tokens(token_hash,client_id,user_id,scope,credential_hash,expires_at) VALUES(?,?,?,'profile','fixture',2000000000)", strings.Repeat("b", 64), strings.Repeat("a", 32), uid)
	exec("INSERT INTO external_identities(identity_key,connector_id,subject,username,user_id,created_at) VALUES(?,'fixture','stable','Fixture',?,1700000000)", strings.Repeat("c", 64), uid)
	exec("INSERT INTO texture_metadata(record_id,category,tags,review_state,review_reason) VALUES(?,'Fixture','[\"fixture\"]','pending','Fixture reason')", record)
	exec("INSERT INTO settings(setting_key,value) VALUES('random_user_ids','false'),('next_user_id','42')")
	exec("INSERT INTO settings(setting_key,value) VALUES(?,?)", "site_name", "Synthetic backup site")
	exec("INSERT INTO settings(setting_key,value) VALUES(?,?)", "mail_template", `{"brandName":"Backup mail brand","accentColor":"#8844cc","verify":{"body":"Custom verification copy"}}`)
	exec("INSERT INTO site_notices(notice_id,content,format,created_at) VALUES(?,?,?,?)", "synthetic-notice", "**Backup notice**", "markdown", 1760000000000)
	exec("UPDATE settings SET value=? WHERE setting_key='character_limit'", "3")
	exec("INSERT INTO admin_events(event_id,actor_id,action,target_type,target_id,created_at) VALUES(?,?,?,?,?,?)", strings.Repeat("a", 32), uid, "fixture", "user", "1", 1700000000)
	exec("INSERT INTO admin_event_changes(event_id,details_json) VALUES(?,?)", strings.Repeat("a", 32), "[{\"field\":\"name\",\"before\":\"Old\",\"after\":\"New\"}]")
	exec("INSERT INTO reports(report_id,reporter_id,record_id,texture_name,reason,status,resolution,reply,reviewer_id,created_at,resolved_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", strings.Repeat("b", 32), uid, record, "Backup fixture", "Fixture reason", "pending", "", "", nil, 1700000000, 0)
	exec("INSERT INTO point_entries(entry_id,user_id,kind,reference_id,delta,balance_after,created_at) VALUES(?,?,?,?,?,?,?)", strings.Repeat("c", 32), uid, "checkin", "2026-10-23", 10, int64(9007199254740993), 1700000000)
	exec("INSERT INTO legacy_reports(id,tid,uploader,reporter,reason,status,report_at) VALUES(9007199254740993,99999,77777,88888,?,0,NULL),(2,0,-1,0,'zero date',2,'0000-00-00 00:00:00')", "History <script>literal</script>\n"+strings.Repeat("旧", 24000))
	exec("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", strings.Repeat("1", 64), uid, 2000000000)
	exec("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?,?,?,?,?)", strings.Repeat("2", 64), "old-client", uid, uuid, 2000000000)
	exec("INSERT INTO ygg_join_sessions(token_hash,server_id,profile_uuid,client_ip,expires_at) VALUES(?,?,?,?,?)", strings.Repeat("2", 64), "old-server", uuid, "127.0.0.1", 2000000000)
	exec("INSERT INTO account_tokens(user_id,purpose,token_hash,expires_at) VALUES(?,?,?,?)", uid, "verify", strings.Repeat("3", 64), 2000000000)
	exec("INSERT INTO invitation_codes(code_hash,expires_at,remaining_uses) VALUES(?,?,?)", strings.Repeat("4", 64), 2000000000, 5)
	if err := os.WriteFile(filepath.Join(dir, "orphan-upload"), []byte("not referenced"), 0600); err != nil {
		t.Fatal(err)
	}
	return db, dir
}

func exportFixture(t *testing.T, dialect string) (*store.Store, string, string, Report) {
	t.Helper()
	db, source := seed(t, dialect)
	path := filepath.Join(t.TempDir(), "backup")
	r, err := Export(context.Background(), db, source, path)
	if err != nil {
		t.Fatal(err)
	}
	return db, source, path, r
}

func loadManifest(t *testing.T, dir string) Manifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func saveManifest(t *testing.T, dir string, m Manifest) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTripBothDatabases(t *testing.T) {
	for _, from := range []string{"mysql", "postgres"} {
		t.Run(from, func(t *testing.T) {
			db, _, path, r := exportFixture(t, from)
			original := loadManifest(t, path)
			if r.Files != 1 || r.CredentialsRevoked || r.Rows["point_entries"] != 1 || r.Rows["user_profiles"] != 1 || r.Rows["legacy_reports"] != 2 || r.Rows["site_notices"] != 1 || len(original.Tables) != len(tables) {
				t.Fatal("incomplete backup report", r)
			}
			for _, to := range []string{"mysql", "postgres"} {
				t.Run(to, func(t *testing.T) {
					target := testutil.Database(t, to)
					dir := filepath.Join(t.TempDir(), "restored")
					restored, err := Restore(context.Background(), target, path, dir)
					if err != nil {
						t.Fatal(err)
					}
					if !restored.CredentialsRevoked || !reflect.DeepEqual(restored.Rows, r.Rows) {
						t.Fatal(restored)
					}
					for _, name := range credentialTables {
						assertCount(t, target, name, 0)
					}
					for _, name := range credentialTables {
						assertCount(t, db, name, 1)
					}
					if _, err := os.Stat(filepath.Join(dir, "orphan-upload")); !os.IsNotExist(err) {
						t.Fatal("unreferenced file copied", err)
					}
					second := filepath.Join(t.TempDir(), "reexport")
					if _, err := Export(context.Background(), target, dir, second); err != nil {
						t.Fatal(err)
					}
					actual := loadManifest(t, second)
					if !reflect.DeepEqual(original.Tables, actual.Tables) || !reflect.DeepEqual(original.Files, actual.Files) {
						t.Fatal("restored business rows or texture bytes differ", original.Tables, actual.Tables)
					}
					// Includes the unchanged signing PEM, password hash/salt, UUID,
					// >2^53 score, disabled file, partial state and NULL reviewer.
					if _, err := Restore(context.Background(), target, path, filepath.Join(t.TempDir(), "again")); err == nil {
						t.Fatal("restored database accepted a second restore")
					}
					for _, marker := range []string{".restore-complete.json", original.Files[0].Hash} {
						if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func assertCount(t *testing.T, db *store.Store, name string, want int) {
	t.Helper()
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&count); err != nil || count != want {
		t.Fatalf("%s count=%d want=%d err=%v", name, count, want, err)
	}
}

func cloneBackup(t *testing.T, source string) string {
	t.Helper()
	destination := t.TempDir()
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			return os.Mkdir(filepath.Join(destination, rel), 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destination, rel), b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func TestInvalidBackupsDoNotWrite(t *testing.T) {
	_, _, original, _ := exportFixture(t, "postgres")
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			target := testutil.Database(t, dialect)
			for _, kind := range []string{"table", "texture", "missing", "schema", "old-schema", "format", "path", "count", "table-name", "pending"} {
				t.Run(kind, func(t *testing.T) {
					path := cloneBackup(t, original)
					m := loadManifest(t, path)
					switch kind {
					case "table":
						appendFile(t, filepath.Join(path, "users.jsonl"), "\n")
					case "texture":
						appendFile(t, filepath.Join(path, "textures", m.Files[0].Hash), "corrupt")
					case "missing":
						if err := os.Remove(filepath.Join(path, "manifest.json")); err != nil {
							t.Fatal(err)
						}
					case "schema":
						m.SchemaVersion++
					case "old-schema":
						m.SchemaVersion--
					case "format":
						m.FormatVersion++
					case "path":
						m.Files[0].Hash = "../../outside"
					case "count":
						m.Tables[0].Rows++
					case "table-name":
						m.Tables[0].Name = "users; DELETE FROM users"
					case "pending":
						replaceTable(t, path, &m, "migration_state", func(row []*string) { *row[2] = "pending" })
					}
					if kind != "missing" {
						saveManifest(t, path, m)
					}
					dir := filepath.Join(t.TempDir(), "target")
					if _, err := Restore(context.Background(), target, path, dir); err == nil {
						t.Fatal("invalid backup accepted")
					}
					if _, err := os.Stat(dir); !os.IsNotExist(err) {
						t.Fatal("preflight wrote target files", err)
					}
					assertCount(t, target, "users", 0)
					// Fresh schema keeps character limit, registration mode, points and homepage defaults.
					assertCount(t, target, "settings", 4)
				})
			}
		})
	}
}

func TestLargeHistoricalReportBackup(t *testing.T) {
	db, source := seed(t, "postgres")
	reason := strings.Repeat("x", 17<<20)
	if _, err := db.DB.Exec(db.Bind("UPDATE legacy_reports SET reason=? WHERE id=9007199254740993"), reason); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backup")
	if _, err := Export(context.Background(), db, source, path); err != nil {
		t.Fatal("long historical reason backup failed", err)
	}
	target := testutil.Database(t, "mysql")
	if _, err := Restore(context.Background(), target, path, filepath.Join(t.TempDir(), "restored")); err != nil {
		t.Fatal("long historical reason restore failed", err)
	}
	var got string
	if err := target.DB.QueryRow("SELECT reason FROM legacy_reports WHERE id=9007199254740993").Scan(&got); err != nil || got != reason {
		t.Fatal("long history text changed", err)
	}
}

func replaceTable(t *testing.T, path string, m *Manifest, name string, change func([]*string)) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(path, name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var row []*string
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatal(err)
	}
	change(row)
	b, err = json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(filepath.Join(path, name+".jsonl"), b, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	for i := range m.Tables {
		if m.Tables[i].Name == name {
			m.Tables[i].SHA256 = hex.EncodeToString(sum[:])
		}
	}
}

func appendFile(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreFailureRollsBackBothDatabases(t *testing.T) {
	_, _, path, _ := exportFixture(t, "postgres")
	m := loadManifest(t, path)
	replaceTable(t, path, &m, "characters", func(row []*string) { *row[1] = "987654321" })
	saveManifest(t, path, m)
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			dir := filepath.Join(t.TempDir(), "target")
			_, err := Restore(context.Background(), db, path, dir)
			if err == nil || !strings.Contains(err.Error(), "restore table characters failed") {
				t.Fatal("expected FK failure", err)
			}
			for _, name := range []string{"users", "textures", "texture_records", "characters"} {
				assertCount(t, db, name, 0)
			}
			assertCount(t, db, "settings", 4)
			if _, err := os.Stat(filepath.Join(dir, ".restore-incomplete.json")); err != nil {
				t.Fatal("failure lost inspection marker", err)
			}
			if _, err := Restore(context.Background(), db, path, dir); err == nil {
				t.Fatal("failed directory reused")
			}
		})
	}
}

func TestTargetProtectionBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db, source, path, _ := exportFixture(t, dialect)
			if _, err := Export(context.Background(), db, source, path); err == nil {
				t.Fatal("existing backup overwritten")
			}
			if _, err := Export(context.Background(), db, source, filepath.Join(source, "nested")); err == nil {
				t.Fatal("nested backup accepted")
			}
			if _, err := Restore(context.Background(), db, path, filepath.Join(t.TempDir(), "target")); err == nil {
				t.Fatal("nonempty database overwritten")
			}
			fresh := testutil.Database(t, dialect)
			existing := t.TempDir()
			if _, err := Restore(context.Background(), fresh, path, existing); err == nil {
				t.Fatal("existing texture directory reused")
			}
			if _, err := fresh.DB.Exec("UPDATE settings SET value='invitation' WHERE setting_key='registration_mode'"); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "new")
			if _, err := Restore(context.Background(), fresh, path, dir); err == nil {
				t.Fatal("custom target settings overwritten")
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("nonempty target wrote files")
			}
			if _, err := db.DB.Exec("UPDATE migration_state SET status='pending'"); err != nil {
				t.Fatal(err)
			}
			pending := filepath.Join(t.TempDir(), "pending")
			if _, err := Export(context.Background(), db, source, pending); err == nil {
				t.Fatal("pending migration backed up")
			}
			if _, err := os.Stat(pending); !os.IsNotExist(err) {
				t.Fatal("pending migration created output")
			}
		})
	}
}

func TestSensitiveReportAndRowValidation(t *testing.T) {
	r := makeReport(Manifest{SchemaVersion: 8, Tables: []TableInfo{{Name: "users", Rows: 1, SHA256: "secret"}}}, true)
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "password") {
		t.Fatal("sensitive report")
	}
	for _, row := range [][]*string{nil, {nil, nil}, {str("no integer"), str("true"), nil}, {str("1"), str("1"), nil}, {nil, str("true"), nil}} {
		if _, err := rowArgs(table{name: "test", types: "ibI"}, row); err == nil {
			t.Fatal("invalid row accepted")
		}
	}
}

func str(s string) *string { return &s }
