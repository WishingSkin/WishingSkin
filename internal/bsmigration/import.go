package bsmigration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/WishingSkin/WishingSkin/internal/securebox"
	"github.com/WishingSkin/WishingSkin/internal/store"
)

var importMutex sync.Mutex

type fileJournal struct {
	Fingerprint string            `json:"fingerprint"`
	Files       map[string]string `json:"files"`
	Sizes       map[string]int64  `json:"-"`
}

// Import keeps business inserts in one transaction and records a pending task
// before copying files. A matching journal permits recovery of completed copies.
// It must run with application writes stopped.
func Import(ctx context.Context, db *store.Store, snapshot *Snapshot, sourceDir, targetDir string) (Report, error) {
	return ImportWithSettingsKey(ctx, db, snapshot, sourceDir, targetDir, nil)
}

// ImportWithSettingsKey imports a snapshot that may contain recoverable
// invitation values. A settings key is mandatory whenever those values are
// present; Import remains as a compatibility wrapper for snapshots without
// invitation records.
func ImportWithSettingsKey(ctx context.Context, db *store.Store, snapshot *Snapshot, sourceDir, targetDir string, settingsKey []byte) (Report, error) {
	r, err := Preflight(snapshot, sourceDir)
	if err != nil {
		return r, err
	}
	if len(snapshot.InvitationCodes) > 0 && len(settingsKey) != 32 {
		return r, fmt.Errorf("settings key is required to import invitation codes")
	}
	importMutex.Lock()
	defer importMutex.Unlock()
	if db == nil || db.DB == nil {
		return r, fmt.Errorf("target database is required")
	}
	if err := independentDirectory(sourceDir, targetDir); err != nil {
		return r, err
	}
	if err := claimTask(ctx, db, r.Fingerprint); err != nil {
		return r, err
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	var existing, status string
	if err := tx.QueryRowContext(ctx, "SELECT source_fingerprint,status FROM migration_state WHERE id=1 FOR UPDATE").Scan(&existing, &status); err != nil {
		return r, err
	}
	if existing != r.Fingerprint {
		return r, fmt.Errorf("target belongs to another migration task")
	}
	if status != "pending" && status != "partial" {
		return r, fmt.Errorf("target migration state is not resumable")
	}
	journal, err := prepareDirectory(snapshot, sourceDir, targetDir, r.Fingerprint)
	if err != nil {
		return r, err
	}
	if status == "partial" {
		if err := verifyCopies(targetDir, journal); err != nil {
			return r, err
		}
		r.Imported = true
		r.AlreadyImported = true
		return r, nil
	}
	if err := emptyBusiness(ctx, tx); err != nil {
		return r, err
	}
	if err := copyFiles(ctx, snapshot, sourceDir, targetDir, journal); err != nil {
		return r, err
	}
	for _, u := range snapshot.Users {
		_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt,score,permission,banned) VALUES(?,?,?,?,?,?,?,?,?,?)"), u.ID, u.Email, emailKey(u.Email), u.Nickname, u.PasswordHash, snapshot.Source.PasswordMethod, snapshot.Source.PasswordSalt, u.Score, u.Permission, u.Permission == -1)
		if err != nil {
			return r, fmt.Errorf("insert migrated user: %w", err)
		}
		if u.Verified != nil && *u.Verified {
			_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(?,?,?)"), u.ID, true, false)
			if err != nil {
				return r, fmt.Errorf("insert migrated email verification: %w", err)
			}
		}
	}
	textures := map[int64]Texture{}
	files := map[string]bool{}
	for _, t := range snapshot.Textures {
		textures[t.ID] = t
		kind, model := textureKind(t)
		if !files[t.Hash] {
			_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO textures(hash,name,kind,model,is_public,deleted) VALUES(?,?,?,?,?,?)"), t.Hash, t.Name, kind, model, t.Public, false)
			if err != nil {
				return r, fmt.Errorf("insert migrated texture file: %w", err)
			}
			if _, err := tx.ExecContext(ctx, db.Bind("INSERT INTO texture_file_integrity(file_hash,byte_size,sha256) VALUES(?,?,?)"), t.Hash, journal.Sizes[t.Hash], journal.Files[t.Hash]); err != nil {
				return r, fmt.Errorf("insert migrated texture integrity: %w", err)
			}
			files[t.Hash] = true
		}
		var owner any
		if t.Uploader != nil && *t.Uploader > 0 {
			owner = *t.Uploader
		}
		_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO texture_records(record_id,file_hash,owner_id,name,description,kind,model,is_public,deleted) VALUES(LOWER(MD5(?)),?,?,?,?,?,?,?,?)"), fmt.Sprintf("bs-texture-record:%d", t.ID), t.Hash, owner, t.Name, t.Description, kind, model, t.Public, false)
		if err != nil {
			return r, fmt.Errorf("insert migrated texture record: %w", err)
		}
	}
	for _, c := range snapshot.Wardrobe {
		name := textures[c.TextureID].Name
		if c.Name != nil {
			name = *c.Name
		}
		// BS user_closet has no timestamp; zero explicitly denotes unknown time.
		_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO wardrobe(user_id,record_id,display_name,collected_at) VALUES(?,LOWER(MD5(?)),?,?)"), c.UserID, fmt.Sprintf("bs-texture-record:%d", c.TextureID), name, int64(0))
		if err != nil {
			return r, fmt.Errorf("insert migrated collection: %w", err)
		}
	}
	for _, c := range snapshot.Characters {
		skin, cape, model := "", "", "default"
		if c.SkinID != 0 {
			t := textures[c.SkinID]
			skin = t.Hash
			_, model = textureKind(t)
		}
		if c.CapeID != 0 {
			cape = textures[c.CapeID].Hash
		}
		_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO characters(id,user_id,uuid,name,name_key,skin_hash,cape_hash,model) VALUES(?,?,?,?,?,?,?,?)"), c.ID, c.UserID, normalizedUUID(c.UUID), c.Name, strings.ToLower(c.Name), skin, cape, model)
		if err != nil {
			return r, fmt.Errorf("insert migrated character: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO settings(setting_key,value) VALUES(?,?)"), "ygg_private_key", snapshot.SignaturePrivateKey)
	if err != nil {
		return r, fmt.Errorf("insert signing key: %w", err)
	}
	if len(snapshot.Branding) != 0 {
		for _, field := range []struct{ source, target string }{{"site_name", "site_name"}, {"site_description", "site_tagline"}, {"announcement", "site_notice"}} {
			if _, err := tx.ExecContext(ctx, db.Bind("INSERT INTO settings(setting_key,value) VALUES(?,?)"), field.target, snapshot.Branding[field.source]); err != nil {
				return r, fmt.Errorf("insert migrated branding")
			}
		}
	}
	committedReport := r
	if snapshot.LegacyReports != nil {
		for _, item := range snapshot.LegacyReports.Items {
			if _, err := tx.ExecContext(ctx, db.Bind("INSERT INTO legacy_reports(id,tid,uploader,reporter,reason,status,report_at) VALUES(?,?,?,?,?,?,?)"), item.ID, item.Texture, item.Uploader, item.Reporter, item.Reason, item.Status, item.ReportAt); err != nil {
				return r, fmt.Errorf("insert migrated report history")
			}
		}
	}
	for _, item := range snapshot.InvitationCodes {
		sealed, sealErr := securebox.Seal(settingsKey, item.Code, "wishingskin:invitation_code:v1")
		if sealErr != nil {
			return r, fmt.Errorf("encrypt migrated invitation %d: %w", item.ID, sealErr)
		}
		generated, _ := parseSourceTime(item.GeneratedAt, false)
		usedAt := any(nil)
		if item.UsedAt != nil {
			used, parseErr := parseSourceTime(*item.UsedAt, true)
			if parseErr != nil {
				return r, fmt.Errorf("parse migrated invitation %d used_at: %w", item.ID, parseErr)
			}
			usedAt = used
		}
		remaining := 1
		if item.UsedBy != 0 || item.UsedAt != nil {
			remaining = 0
		}
		_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO invitation_codes(code_hash,expires_at,remaining_uses,encrypted_code,source_id,generated_at,used_at,source_user_id) VALUES(?,?,?, ?,?,?,?,?)"), item.CodeHash, int64(0), remaining, sealed, item.ID, generated, usedAt, nullableSourceUser(item.UsedBy))
		if err != nil {
			return r, fmt.Errorf("insert migrated invitation %d: %w", item.ID, err)
		}
	}
	committedReport.Imported = true
	reportJSON, _ := json.Marshal(committedReport)
	_, err = tx.ExecContext(ctx, db.Bind("UPDATE migration_state SET status=?,report=? WHERE id=1"), "partial", string(reportJSON))
	if err != nil {
		return r, err
	}
	if err := tx.Commit(); err != nil {
		return r, fmt.Errorf("commit import (retry same snapshot to inspect durable state): %w", err)
	}
	r.Imported = true
	return r, nil
}

func nullableSourceUser(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func textureKind(t Texture) (string, string) {
	if t.Type == "cape" {
		return "cape", "default"
	}
	if t.Type == "alex" {
		return "skin", "slim"
	}
	return "skin", "default"
}

func claimTask(ctx context.Context, s *store.Store, fp string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var mode string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode); err != nil {
		return err
	}
	var old string
	err = tx.QueryRowContext(ctx, "SELECT source_fingerprint FROM migration_state WHERE id=1 FOR UPDATE").Scan(&old)
	if err == nil {
		if old != fp {
			return fmt.Errorf("target belongs to another migration task")
		}
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migration_state").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("target has an unrelated migration state")
	}
	if err := emptyBusiness(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET value='closed' WHERE setting_key='registration_mode'"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, s.Bind("INSERT INTO migration_state(id,source_fingerprint,status,report) VALUES(1,?,?,?)"), fp, "pending", "{}")
	if err != nil {
		return fmt.Errorf("claim empty target migration task: %w", err)
	}
	return tx.Commit()
}

func emptyBusiness(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"users", "characters", "textures", "texture_file_integrity", "texture_records", "wardrobe", "settings", "web_sessions", "game_tokens", "ygg_join_sessions", "account_states", "user_profiles", "account_tokens", "invitation_codes", "admin_events", "reports", "point_entries", "legacy_reports"} {
		var count int64
		query := "SELECT COUNT(*) FROM " + table
		if table == "settings" {
			// A fresh schema includes these built-in defaults, not imported business data.
			query += ` WHERE NOT ((setting_key='character_limit' AND value='1') OR (setting_key='registration_mode' AND value IN ('closed','open')) OR (setting_key='points_rules' AND value='{"enabled":false,"dailyReward":0,"uploadCost":0,"characterCost":0}') OR (setting_key='homepage_blocks' AND value='[{"id":"intro","kind":"intro","enabled":true,"title":"Minecraft 外观工作台","body":"管理角色、皮肤和披风，在公开库中发现新的外观，并通过 Yggdrasil 接入游戏。","buttonLabel":"登录工作台","buttonHref":"/login"},{"id":"features","kind":"features","enabled":true,"title":"从角色到游戏","features":[{"title":"角色与外观","body":"用模型预览管理标准和纤细模型，皮肤与披风分别绑定。","href":"/login"},{"title":"公开库","body":"浏览公开材质并收藏到自己的衣柜。","href":"/library"},{"title":"游戏接入","body":"使用本站提供的 Yggdrasil 或兼容地址加载外观。","href":"/login"}]},{"id":"notice","kind":"notice","enabled":true,"title":"最新公告"}]'))`
		}
		if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("target business table %s is not empty", table)
		}
	}
	return nil
}

func independentDirectory(source, target string) error {
	s, err := filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("source directory is unavailable")
	}
	t, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	// Resolve the existing parent as well, so a symlink cannot alias the source.
	parent, err := filepath.EvalSymlinks(filepath.Dir(t))
	if err != nil {
		return fmt.Errorf("target parent directory must exist")
	}
	t = filepath.Join(parent, filepath.Base(t))
	if _, err := os.Lstat(t); err == nil {
		actual, e := filepath.EvalSymlinks(t)
		if e != nil || actual != t {
			return fmt.Errorf("target directory links are not supported")
		}
	}
	s = strings.ToLower(filepath.Clean(s))
	t = strings.ToLower(filepath.Clean(t))
	sep := string(filepath.Separator)
	if s == t || strings.HasPrefix(t, s+sep) || strings.HasPrefix(s, t+sep) {
		return fmt.Errorf("source and target texture directories must be independent")
	}
	return nil
}

func prepareDirectory(s *Snapshot, source, target, fp string) (*fileJournal, error) {
	if err := os.MkdirAll(target, 0700); err != nil {
		return nil, err
	}
	journal := &fileJournal{Fingerprint: fp, Files: map[string]string{}, Sizes: map[string]int64{}}
	for _, t := range s.Textures {
		if _, seen := journal.Files[t.Hash]; seen {
			continue
		}
		b, err := readTexture(source, t.Hash)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		journal.Files[t.Hash] = hex.EncodeToString(sum[:])
		journal.Sizes[t.Hash] = int64(len(b))
	}
	path := filepath.Join(target, ".wishingskin-migration.json")
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	settingsKeyPresent := false
	for _, entry := range entries {
		if entry.Name() != ".settings.key" {
			continue
		}
		info, err := os.Lstat(filepath.Join(target, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || info.Size() != 32 {
			return nil, fmt.Errorf("target settings key is invalid")
		}
		settingsKeyPresent = true
	}
	if len(entries) == 0 || len(entries) == 1 && settingsKeyPresent {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		data, _ := json.Marshal(journal)
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			os.Remove(path)
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("nonempty target has no migration journal")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var previous fileJournal
		if err := json.Unmarshal(data, &previous); err != nil {
			return nil, fmt.Errorf("invalid target migration journal")
		}
		a, _ := json.Marshal(journal)
		b, _ := json.Marshal(previous)
		if string(a) != string(b) {
			return nil, fmt.Errorf("target file journal does not match this source snapshot")
		}
		for _, e := range entries {
			if e.Name() == ".wishingskin-migration.json" || settingsKeyPresent && e.Name() == ".settings.key" {
				continue
			}
			name := strings.TrimPrefix(e.Name(), ".copy-")
			if _, ok := journal.Files[name]; !ok || !e.Type().IsRegular() {
				return nil, fmt.Errorf("target contains an unrelated file")
			}
		}
	}
	return journal, nil
}

func verifyCopies(target string, j *fileJournal) error {
	for hash, want := range j.Files {
		if err := verifyCopy(target, hash, want); err != nil {
			return err
		}
	}
	return nil
}

func verifyCopy(target, hash, want string) error {
	b, err := readTexture(target, hash)
	if err != nil {
		return fmt.Errorf("target texture %s is unavailable", hash)
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("target texture %s differs; refusing overwrite", hash)
	}
	return nil
}

func copyFiles(ctx context.Context, s *Snapshot, source, target string, j *fileJournal) error {
	for _, t := range s.Textures {
		if err := ctx.Err(); err != nil {
			return err
		}
		stage := filepath.Join(target, ".copy-"+t.Hash)
		if info, err := os.Lstat(stage); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("invalid pending copy")
			}
			if err := os.Remove(stage); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		final := filepath.Join(target, t.Hash)
		if _, err := os.Lstat(final); err == nil {
			if err := verifyCopy(target, t.Hash, j.Files[t.Hash]); err != nil {
				return err
			}
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		b, err := readTexture(source, t.Hash)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != j.Files[t.Hash] {
			return fmt.Errorf("source texture changed during import")
		}
		f, err := os.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Link(stage, final)
		}
		os.Remove(stage)
		if err != nil {
			return fmt.Errorf("publish texture without overwrite: %w", err)
		}
	}
	return nil
}
