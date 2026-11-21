package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	ThemeAssetsSetting        = "theme_assets"
	ThemeTextureSettingPrefix = "theme.texture."
	ThemeFontSetting          = "theme_custom_font"
	ThemeFontDataSetting      = "theme.font.data"
)

type ThemeAssetsMetadata struct {
	Version    string   `json:"version"`
	SourceName string   `json:"sourceName,omitempty"`
	ImportedAt string   `json:"importedAt,omitempty"`
	Count      int      `json:"count"`
	Keys       []string `json:"keys"`
}

type ThemeFontMetadata struct {
	Version    string `json:"version"`
	FileName   string `json:"fileName,omitempty"`
	Size       int    `json:"size,omitempty"`
	UploadedAt string `json:"uploadedAt,omitempty"`
}

type themeBlob struct {
	Version string `json:"version"`
	Data    string `json:"data"`
}

// The asset row carries its version so a read sees bytes and version from the same update.
func (s *Store) PutThemeAsset(ctx context.Context, tx *sql.Tx, key, version, encoded string) error {
	raw, err := json.Marshal(themeBlob{Version: version, Data: encoded})
	if err != nil {
		return err
	}
	query := "INSERT INTO settings(setting_key,value) VALUES(?,?) ON CONFLICT(setting_key) DO UPDATE SET value=EXCLUDED.value"
	if s.Dialect == "mysql" {
		query = "INSERT INTO settings(setting_key,value) VALUES(?,?) ON DUPLICATE KEY UPDATE value=VALUES(value)"
	}
	_, err = tx.ExecContext(ctx, s.Bind(query), key, string(raw))
	return err
}

func (s *Store) ReadThemeAsset(ctx context.Context, key string) (version, encoded string, err error) {
	var raw string
	err = s.DB.QueryRowContext(ctx, s.Bind("SELECT value FROM settings WHERE setting_key=?"), key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	var blob themeBlob
	if err := json.Unmarshal([]byte(raw), &blob); err != nil {
		return "", "", err
	}
	return blob.Version, blob.Data, nil
}

// Keep assets in settings for backup and restore, but split the old inline data
// once during upgrade. A retry after the split sees no inline data to move.
func (s *Store) migrateThemeAssetSettings(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	read := func(key string, target any) error {
		var raw string
		err := tx.QueryRowContext(ctx, s.Bind("SELECT value FROM settings WHERE setting_key=?"), key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), target); err != nil {
			return fmt.Errorf("read legacy setting %s: %w", key, err)
		}
		return nil
	}
	writeMetadata := func(key string, metadata any) error {
		data, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, s.Bind("UPDATE settings SET value=? WHERE setting_key=?"), string(data), key)
		return err
	}
	var pack struct {
		ThemeAssetsMetadata
		Textures map[string]string `json:"textures"`
	}
	if err := read(ThemeAssetsSetting, &pack); err != nil {
		return err
	}
	if pack.Version != "" && len(pack.Textures) > 0 {
		pack.Keys = make([]string, 0, len(pack.Textures))
		for key, data := range pack.Textures {
			if err := s.PutThemeAsset(ctx, tx, ThemeTextureSettingPrefix+key, pack.Version, data); err != nil {
				return err
			}
			pack.Keys = append(pack.Keys, key)
		}
		sort.Strings(pack.Keys)
		pack.Count = len(pack.Keys)
		if err := writeMetadata(ThemeAssetsSetting, pack.ThemeAssetsMetadata); err != nil {
			return err
		}
	}
	var font struct {
		ThemeFontMetadata
		Data string `json:"data"`
	}
	if err := read(ThemeFontSetting, &font); err != nil {
		return err
	}
	if font.Version != "" && font.Data != "" {
		if err := s.PutThemeAsset(ctx, tx, ThemeFontDataSetting, font.Version, font.Data); err != nil {
			return err
		}
		if err := writeMetadata(ThemeFontSetting, font.ThemeFontMetadata); err != nil {
			return err
		}
	}
	return tx.Commit()
}
