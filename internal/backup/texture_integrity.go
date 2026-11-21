package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

type textureSource struct {
	bytes  int64
	digest string
}

func textureSourceIntegrity(ctx context.Context, root *os.Root, manifest Manifest) (map[string]textureSource, bool, error) {
	sources := map[string]textureSource{}
	legacyCustom := false
	for index, table := range tables {
		switch table.name {
		case "texture_file_integrity":
			err := walkTable(ctx, root, table, manifest.Tables[index], func(row []*string) error {
				hash, digest := *row[0], *row[2]
				count, err := strconv.ParseInt(*row[1], 10, 64)
				_, duplicate := sources[hash]
				if err != nil || count < 0 || !hashName.MatchString(hash) || digest != "" && !sha256Text.MatchString(digest) || duplicate {
					return fmt.Errorf("invalid texture source inventory")
				}
				sources[hash] = textureSource{bytes: count, digest: digest}
				return nil
			})
			if err != nil {
				return nil, false, err
			}
		case "migration_state":
			err := walkTable(ctx, root, table, manifest.Tables[index], func(row []*string) error {
				var report struct {
					Unsupported []struct{ Scope string } `json:"unsupported"`
				}
				if err := json.Unmarshal([]byte(*row[3]), &report); err != nil {
					return fmt.Errorf("invalid BS migration report")
				}
				for _, item := range report.Unsupported {
					legacyCustom = legacyCustom || item.Scope == "texture_hash_filter"
				}
				return nil
			})
			if err != nil {
				return nil, false, err
			}
		}
	}
	return sources, legacyCustom, nil
}

func verifyTextureSource(file FileInfo, source textureSource, legacyCustom bool) (bool, error) {
	if source.digest != "" {
		if file.SHA256 != source.digest || file.Bytes != source.bytes {
			return false, fmt.Errorf("texture %s differs from its source digest", file.Hash)
		}
		return false, nil
	}
	if file.contentMatch {
		return false, nil
	}
	if len(file.Hash) == 64 && !legacyCustom {
		return false, fmt.Errorf("texture %s differs from its content hash", file.Hash)
	}
	return true, nil
}
