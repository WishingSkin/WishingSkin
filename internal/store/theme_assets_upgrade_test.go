package store

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSchema21ThemeAssetUpgrade(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("WS_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("test database not configured")
			}
			s := isolatedTestStore(t, dialect, dsn)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := s.DB.ExecContext(ctx, s.Bind(query), args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("DELETE FROM schema_migrations WHERE version>21")
			if dialect == "mysql" {
				exec("ALTER TABLE settings MODIFY COLUMN value TEXT NOT NULL")
			}
			exec("INSERT INTO settings(setting_key,value) VALUES(?,?),(?,?)", ThemeAssetsSetting,
				`{"version":"old-pack","sourceName":"legacy.zip","count":1,"textures":{"stone":"YmxvY2s="}}`, ThemeFontSetting,
				`{"version":"old-font","fileName":"legacy.woff2","size":4,"data":"d09GMg=="}`)
			for range 2 {
				if err := s.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				for _, want := range [][3]string{{ThemeTextureSettingPrefix + "stone", "old-pack", "YmxvY2s="}, {ThemeFontDataSetting, "old-font", "d09GMg=="}} {
					version, data, err := s.ReadThemeAsset(ctx, want[0])
					if err != nil || version != want[1] || data != want[2] {
						t.Fatalf("legacy asset %q did not survive upgrade: version %q, error %v", want[0], version, err)
					}
				}
				for key, oldField := range map[string]string{ThemeAssetsSetting: "textures", ThemeFontSetting: "data"} {
					var raw string
					if err := s.DB.QueryRowContext(ctx, s.Bind("SELECT value FROM settings WHERE setting_key=?"), key).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					var value map[string]json.RawMessage
					if err := json.Unmarshal([]byte(raw), &value); err != nil || value[oldField] != nil || value["version"] == nil {
						t.Fatalf("setting %q did not separate metadata from data", key)
					}
				}
				// A restart can happen after the data transaction but before the version marker.
				exec("DELETE FROM schema_migrations WHERE version=23")
			}
		})
	}
}
