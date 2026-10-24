package bsmigration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Public text defaults from config/options.php at CoreCommit. HTML and template
// placeholders remain literal text; source CSS/JS and credentials stay excluded.
var brandingDefaults = map[string]string{
	"site_name":        "Blessing Skin",
	"site_description": "Open-source PHP Minecraft Skin Hosting Service",
	"announcement":     "Welcome to Blessing Skin {version}!",
}

func readBranding(ctx context.Context, tx *sql.Tx, quotedTable string) (map[string]string, error) {
	values := map[string]string{}
	for key, value := range brandingDefaults {
		values[key] = value
	}
	rows, err := tx.QueryContext(ctx, "SELECT option_name,option_value FROM "+quotedTable+" WHERE option_name IN ('site_name','site_description','announcement')")
	if err != nil {
		return nil, fmt.Errorf("read source branding")
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("read source branding value")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate source branding option")
		}
		seen[key], values[key] = true, value
	}
	return values, rows.Err()
}

func validBranding(values map[string]string) bool {
	if len(values) != len(brandingDefaults) {
		return false
	}
	for key, value := range values {
		if _, known := brandingDefaults[key]; !known || !utf8.ValidString(value) {
			return false
		}
		max := 1000
		if key == "site_name" {
			max = 64
			if strings.TrimSpace(value) == "" {
				return false
			}
		} else if key == "site_description" {
			max = 200
		}
		if utf8.RuneCountInString(value) > max || key != "announcement" && strings.ContainsAny(value, "\r\n") {
			return false
		}
		for _, char := range value {
			if unicode.IsControl(char) && char != '\r' && char != '\n' && char != '\t' {
				return false
			}
		}
	}
	return true
}
