package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"wishingskin/internal/store"
)

// Restore requires application writes stopped and a freshly migrated database.
// Files survive failure for inspection; a retry must use a new empty directory.
func Restore(ctx context.Context, db *store.Store, sourceDir, textureDir string) (Report, error) {
	var report Report
	if err := independent(sourceDir, textureDir); err != nil {
		return report, err
	}
	root, err := os.OpenRoot(sourceDir)
	if err != nil {
		return report, err
	}
	defer root.Close()
	m, err := verify(ctx, root)
	if err != nil {
		return report, err
	}
	tx, err := db.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	if err := checkSchema(ctx, tx); err != nil {
		return report, err
	}
	if err := emptyTarget(ctx, tx); err != nil {
		return report, err
	}
	if err := os.Mkdir(textureDir, 0700); err != nil {
		return report, fmt.Errorf("restore requires a new texture directory: %w", err)
	}
	target, err := os.OpenRoot(textureDir)
	if err != nil {
		return report, err
	}
	defer target.Close()
	if err := writeJSON(target, ".restore-incomplete.json", makeReport(m, true)); err != nil {
		return report, err
	}
	for _, file := range m.Files {
		if _, err := copyTexture(ctx, root, "textures/"+file.Hash, target, file.Hash, &file); err != nil {
			return report, err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM settings"); err != nil {
		return report, err
	}
	for i, t := range tables {
		err := walkTable(ctx, root, t, m.Tables[i], func(row []*string) error {
			args, err := rowArgs(t, row)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, db.Bind(t.insert()), args...); err != nil {
				// Driver errors can contain row data, including hashes and keys.
				return fmt.Errorf("restore table %s failed; transaction rolled back", t.name)
			}
			return nil
		})
		if err != nil {
			return report, err
		}
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("restore commit not confirmed; inspect durable target state before retrying")
	}
	if err := target.Rename(".restore-incomplete.json", ".restore-complete.json"); err != nil {
		return report, fmt.Errorf("database committed but completion marker failed; inspect target before use")
	}
	return makeReport(m, true), nil
}

func verify(ctx context.Context, root *os.Root) (Manifest, error) {
	var m Manifest
	f, err := openRegular(root, "manifest.json")
	if err != nil {
		return m, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > maxManifestBytes {
		return m, fmt.Errorf("backup manifest exceeds supported size")
	}
	decoder := json.NewDecoder(io.LimitReader(f, maxManifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, fmt.Errorf("invalid backup manifest")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return m, fmt.Errorf("trailing backup manifest data")
	}
	if m.FormatVersion != formatVersion || m.SchemaVersion != store.CurrentSchemaVersion || len(m.Tables) != len(tables) {
		return m, fmt.Errorf("unsupported backup format or schema")
	}
	fileHashes := map[string]bool{}
	for i, t := range tables {
		info := m.Tables[i]
		if info.Name != t.name || info.Rows < 0 || !sha256Text.MatchString(info.SHA256) {
			return m, fmt.Errorf("invalid backup table inventory")
		}
		err := walkTable(ctx, root, t, info, func(row []*string) error {
			if t.name == "textures" {
				hash := *row[0]
				if !hashName.MatchString(hash) || fileHashes[strings.ToLower(hash)] {
					return fmt.Errorf("invalid or colliding texture hashes")
				}
				fileHashes[strings.ToLower(hash)] = true
			}
			if t.name == "migration_state" && *row[2] != "partial" {
				return fmt.Errorf("unsupported or pending migration state")
			}
			return nil
		})
		if err != nil {
			return m, err
		}
	}
	if len(fileHashes) != len(m.Files) {
		return m, fmt.Errorf("texture inventory mismatch")
	}
	for _, file := range m.Files {
		key := strings.ToLower(file.Hash)
		if !hashName.MatchString(file.Hash) || !fileHashes[key] || file.Bytes < 0 || !sha256Text.MatchString(file.SHA256) {
			return m, fmt.Errorf("invalid texture inventory")
		}
		delete(fileHashes, key)
		if _, err := copyTexture(ctx, root, "textures/"+file.Hash, nil, "", &file); err != nil {
			return m, err
		}
	}
	return m, nil
}

func emptyTarget(ctx context.Context, tx *sql.Tx) error {
	for _, name := range append(append([]string{}, credentialTables...), tableNames()...) {
		query := "SELECT COUNT(*) FROM " + name
		if name == "settings" {
			query += ` WHERE NOT ((setting_key='character_limit' AND value='1') OR (setting_key='registration_mode' AND value IN ('closed','open')) OR (setting_key='points_rules' AND value='{"enabled":false,"dailyReward":0,"uploadCost":0,"characterCost":0}'))`
		}
		var count int64
		if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fmt.Errorf("inspect restore target table %s", name)
		}
		if count != 0 {
			return fmt.Errorf("restore target table %s is not empty", name)
		}
	}
	return nil
}

func tableNames() []string {
	names := make([]string, len(tables))
	for i, t := range tables {
		names[i] = t.name
	}
	return names
}
