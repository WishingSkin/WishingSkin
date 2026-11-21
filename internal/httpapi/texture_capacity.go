package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"
)

func (a *Server) checkTextureCapacity(w http.ResponseWriter, ctx context.Context, tx *sql.Tx, userID, size int64, existing bool) bool {
	type capacityCheck struct {
		remaining int64
		from      string
		name      string
		code      string
		status    int
		args      []any
	}
	var checks []capacityCheck
	if a.cfg.TextureAccountLimitBytes > 0 {
		checks = append(checks, capacityCheck{a.cfg.TextureAccountLimitBytes - size,
			" FROM texture_records t LEFT JOIN texture_file_integrity i ON i.file_hash=t.file_hash WHERE t.owner_id=? AND t.deleted=FALSE",
			"t.file_hash", "texture_account_capacity", http.StatusConflict, []any{userID}})
	}
	if !existing && a.cfg.TextureSiteLimitBytes > 0 {
		checks = append(checks, capacityCheck{a.cfg.TextureSiteLimitBytes - size,
			" FROM textures t LEFT JOIN texture_file_integrity i ON i.file_hash=t.hash WHERE TRUE",
			"t.hash", "texture_site_capacity", http.StatusInsufficientStorage, nil})
	}
	for _, check := range checks {
		ok, err := a.withinTextureCapacity(ctx, tx, check.remaining, check.from, check.name, check.args...)
		if err != nil {
			failure(w, 503, "暂时无法读取材质文件记录")
			return false
		}
		if !ok {
			respond(w, check.status, map[string]string{"error": "无法保存材质文件", "code": check.code})
			return false
		}
	}
	return true
}

func (a *Server) withinTextureCapacity(ctx context.Context, tx *sql.Tx, remaining int64, from, hashColumn string, args ...any) (bool, error) {
	if remaining < 0 {
		return false, nil
	}
	var used int64
	if err := tx.QueryRowContext(ctx, a.s.Bind("SELECT COALESCE(SUM(i.byte_size),0)"+from), args...).Scan(&used); err != nil {
		return false, err
	}
	remaining -= used
	if remaining < 0 {
		return false, nil
	}
	rows, err := tx.QueryContext(ctx, a.s.Bind("SELECT "+hashColumn+",COUNT(*)"+from+" AND i.file_hash IS NULL GROUP BY "+hashColumn), args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var root *os.Root
	defer func() {
		if root != nil {
			root.Close()
		}
	}()
	for rows.Next() {
		var hash string
		var count int64
		if err := rows.Scan(&hash, &count); err != nil {
			return false, err
		}
		if !validHash.MatchString(strings.ToLower(hash)) {
			return false, errors.New("invalid stored texture hash")
		}
		if root == nil {
			root, err = os.OpenRoot(a.cfg.TextureDir)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
		// An old missing file may still exist in object storage. Reserve one
		// upload allowance when its size cannot be read from the local copy.
		size := int64(maxTextureBytes)
		if root != nil {
			info, err := root.Lstat(hash)
			if err == nil && info.Mode().IsRegular() {
				size = info.Size()
			} else if !errors.Is(err, os.ErrNotExist) {
				return false, errors.New("cannot read stored texture size")
			}
		}
		if size > remaining/count {
			return false, nil
		}
		remaining -= size * count
	}
	err = rows.Err()
	return err == nil, err
}

func (a *Server) recordTextureIntegrity(ctx context.Context, tx *sql.Tx, hash string, size int64) error {
	var storedSize int64
	var digest string
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT byte_size,sha256 FROM texture_file_integrity WHERE file_hash=?"), hash).Scan(&storedSize, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO texture_file_integrity(file_hash,byte_size,sha256) VALUES(?,?,?)"), hash, size, hash)
	} else if err == nil && (storedSize != size || digest != hash) {
		return errors.New("texture conflicts with its recorded digest")
	}
	return err
}
