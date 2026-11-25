package textureimport

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/securebox"
	"github.com/WishingSkin/WishingSkin/internal/storage"
	"github.com/WishingSkin/WishingSkin/internal/store"
	blob "github.com/WishingSkin/WishingSkin/pkg/storage"
)

const (
	AccountUsageSQL = " FROM texture_records t LEFT JOIN texture_file_integrity i ON i.file_hash=t.file_hash WHERE t.owner_id=? AND t.deleted=FALSE"
	SiteUsageSQL    = " FROM textures t LEFT JOIN texture_file_integrity i ON i.file_hash=t.hash WHERE TRUE"
)

type QuotaSettings struct {
	AccountLimitBytes *int64 `json:"accountLimitBytes,omitempty"`
	SiteLimitBytes    *int64 `json:"siteLimitBytes,omitempty"`
}

func EffectiveLimits(ctx context.Context, reader RowReader, account, site int64) (int64, int64, error) {
	var saved QuotaSettings
	if err := readSetting(ctx, reader, "SELECT value FROM settings WHERE setting_key='storage_quota'", &saved); err != nil {
		return 0, 0, err
	}
	if saved.AccountLimitBytes != nil && *saved.AccountLimitBytes >= 0 {
		account = *saved.AccountLimitBytes
	}
	if saved.SiteLimitBytes != nil && *saved.SiteLimitBytes >= 0 {
		site = *saved.SiteLimitBytes
	}
	return account, site, nil
}

type UsageReader interface {
	RowReader
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

var textureHashPattern = regexp.MustCompile(`^[a-f0-9]{32,128}$`)

func Usage(ctx context.Context, reader UsageReader, db *store.Store, directory, from, hashColumn string, args ...any) (int64, error) {
	var used int64
	if err := reader.QueryRowContext(ctx, db.Bind("SELECT COALESCE(SUM(i.byte_size),0)"+from), args...).Scan(&used); err != nil {
		return 0, err
	}
	rows, err := reader.QueryContext(ctx, db.Bind("SELECT "+hashColumn+",COUNT(*)"+from+" AND i.file_hash IS NULL GROUP BY "+hashColumn), args...)
	if err != nil {
		return 0, err
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
			return 0, err
		}
		if !textureHashPattern.MatchString(strings.ToLower(hash)) {
			return 0, errors.New("invalid stored texture hash")
		}
		if root == nil {
			root, err = os.OpenRoot(directory)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return 0, err
			}
		}
		size := int64(MaxBytes)
		if root != nil {
			info, err := root.Lstat(hash)
			if err == nil && info.Mode().IsRegular() {
				size = info.Size()
			} else if !errors.Is(err, os.ErrNotExist) {
				return 0, errors.New("cannot read stored texture size")
			}
		}
		used += size * count
	}
	return used, rows.Err()
}

func RecordIntegrity(ctx context.Context, tx *sql.Tx, db *store.Store, hash string, size int64) error {
	var storedSize int64
	var digest string
	err := tx.QueryRowContext(ctx, db.Bind("SELECT byte_size,sha256 FROM texture_file_integrity WHERE file_hash=?"), hash).Scan(&storedSize, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, db.Bind("INSERT INTO texture_file_integrity(file_hash,byte_size,sha256) VALUES(?,?,?)"), hash, size, hash)
	} else if err == nil && (storedSize != size || digest != hash) {
		return errors.New("texture conflicts with its recorded digest")
	}
	return err
}

func ReadObjectStorage(ctx context.Context, reader RowReader, key []byte) (storage.S3Options, string, error) {
	var options storage.S3Options
	var sealed string
	err := readSetting(ctx, reader, "SELECT value FROM settings WHERE setting_key='object_storage_config'", &sealed)
	if err != nil || sealed == "" {
		return options, sealed, err
	}
	plain, err := securebox.Open(key, sealed, "wishingskin:object-storage:v1")
	if err == nil {
		err = json.Unmarshal([]byte(plain), &options)
	}
	return options, sealed, err
}

func SaveFile(ctx context.Context, provider blob.BlobStore, directory, hash string, data []byte) (bool, error) {
	created, err := SaveLocal(directory, hash, data)
	if err != nil || provider == nil {
		return created, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return created, provider.Put(ctx, hash, "image/png", bytes.NewReader(data))
}

func SaveLocal(directory, hash string, data []byte) (bool, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return false, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return false, err
	}
	defer root.Close()
	id, err := randomID()
	if err != nil {
		return false, err
	}
	stage := ".upload-" + id
	file, err := root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	defer root.Remove(stage)
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return false, writeErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if err = root.Link(stage, hash); err == nil {
		return true, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return false, err
	}
	existing, err := root.Open(hash)
	if err != nil {
		return false, err
	}
	defer existing.Close()
	previous, err := io.ReadAll(io.LimitReader(existing, MaxBytes+1))
	if err != nil || !bytes.Equal(previous, data) {
		return false, errors.New("existing texture file does not match its content hash")
	}
	return false, nil
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return hex.EncodeToString(raw[:]), nil
}
