package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/WishingSkin/WishingSkin/internal/textureimport"
)

const maxConfigurableQuotaBytes int64 = 100 << 40

type storageQuotaSettings = textureimport.QuotaSettings

type storageQuotaView struct {
	AccountLimitBytes int64 `json:"accountLimitBytes"`
	SiteLimitBytes    int64 `json:"siteLimitBytes"`
}

func (a *Server) effectiveTextureLimits(ctx context.Context, reader settingReader) (int64, int64, error) {
	return textureimport.EffectiveLimits(ctx, reader, a.cfg.TextureAccountLimitBytes, a.cfg.TextureSiteLimitBytes)
}

func (a *Server) checkTextureCapacity(w http.ResponseWriter, ctx context.Context, tx *sql.Tx, userID, size int64, existing bool) bool {
	accountLimit, siteLimit, err := a.effectiveTextureLimits(ctx, tx)
	if err != nil {
		failure(w, 503, "暂时无法读取存储配额设置")
		return false
	}
	type capacityCheck struct {
		remaining int64
		from      string
		name      string
		code      string
		status    int
		args      []any
	}
	var checks []capacityCheck
	if accountLimit > 0 {
		checks = append(checks, capacityCheck{accountLimit - size,
			" FROM texture_records t LEFT JOIN texture_file_integrity i ON i.file_hash=t.file_hash WHERE t.owner_id=? AND t.deleted=FALSE",
			"t.file_hash", "texture_account_capacity", http.StatusConflict, []any{userID}})
	}
	if !existing && siteLimit > 0 {
		checks = append(checks, capacityCheck{siteLimit - size,
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

func (a *Server) textureUsage(ctx context.Context, q textureimport.UsageReader, from, hashColumn string, args ...any) (int64, error) {
	return textureimport.Usage(ctx, q, a.s, a.cfg.TextureDir, from, hashColumn, args...)
}

func (a *Server) accountQuota(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	accountUsed, err := a.textureUsage(
		r.Context(),
		a.s.DB,
		" FROM texture_records t LEFT JOIN texture_file_integrity i ON i.file_hash=t.file_hash WHERE t.owner_id=? AND t.deleted=FALSE",
		"t.file_hash",
		u.ID,
	)
	if err != nil {
		failure(w, 503, "暂时无法读取材质存储用量")
		return
	}
	siteUsed, err := a.textureUsage(
		r.Context(),
		a.s.DB,
		" FROM textures t LEFT JOIN texture_file_integrity i ON i.file_hash=t.hash WHERE TRUE",
		"t.hash",
	)
	if err != nil {
		failure(w, 503, "暂时无法读取材质存储用量")
		return
	}
	accountLimit, siteLimit, err := a.effectiveTextureLimits(r.Context(), a.s.DB)
	if err != nil {
		failure(w, 503, "暂时无法读取存储配额设置")
		return
	}
	respond(w, 200, map[string]any{
		"accountUsedBytes":  accountUsed,
		"accountLimitBytes": accountLimit,
		"siteUsedBytes":     siteUsed,
		"siteLimitBytes":    siteLimit,
		"maxFileBytes":      int64(maxTextureBytes),
	})
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
	return textureimport.RecordIntegrity(ctx, tx, a.s, hash, size)
}
