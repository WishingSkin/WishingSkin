package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"wishingskin/internal/store"
)

func (a *Server) auditChange(ctx context.Context, field string, before, after any) {
	if fmt.Sprint(before) != fmt.Sprint(after) {
		store.CaptureAdminChange(ctx, field, auditDisplay(before), auditDisplay(after))
	}
}

func auditDisplay(value any) string {
	if value == nil {
		return ""
	}
	text := fmt.Sprint(value)
	if strings.HasPrefix(text, "data:") {
		return "已设置图片"
	}
	if len([]rune(text)) > 500 {
		return string([]rune(text)[:500]) + "…"
	}
	return text
}

// Only the listed settings contain public content or non-secret policy values.
// Encrypted integrations and unknown plugin values are never decoded for audit.
func (a *Server) captureSettingChange(ctx context.Context, tx *sql.Tx, key, next string) error {
	if !store.AdminChangesEnabled(ctx) {
		return nil
	}
	var previous string
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT value FROM settings WHERE setting_key=?"), key).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previous == next {
		return nil
	}
	switch key {
	case "homepage_blocks", "homepage_background", "site_appearance", "content_policy", "backup_schedule", "cdn_config", "registration_email_domains", "registration_blocked_domains", "manual_email_verification", "random_user_ids", "official_invitation":
		var before, after any
		_ = json.Unmarshal([]byte(previous), &before)
		if err := json.Unmarshal([]byte(next), &after); err != nil {
			return err
		}
		a.auditJSON(ctx, key, before, after)
	case "login_image":
		store.CaptureAdminChange(ctx, key, imagePresence(previous), imagePresence(next))
	default:
		store.CaptureAdminChange(ctx, key, "", "配置已更新")
	}
	return nil
}

func imagePresence(value string) string {
	if value == "" || value == `""` || value == "null" {
		return "未设置"
	}
	return "已设置图片"
}

func (a *Server) auditJSON(ctx context.Context, field string, before, after any) {
	oldMap, oldObject := before.(map[string]any)
	newMap, newObject := after.(map[string]any)
	if oldObject || newObject {
		keys := map[string]bool{}
		for key := range oldMap {
			keys[key] = true
		}
		for key := range newMap {
			keys[key] = true
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			a.auditJSON(ctx, field+"."+key, oldMap[key], newMap[key])
		}
		return
	}
	oldList, oldArray := before.([]any)
	newList, newArray := after.([]any)
	if oldArray || newArray {
		for i := 0; i < max(len(oldList), len(newList)); i++ {
			var left, right any
			if i < len(oldList) {
				left = oldList[i]
			}
			if i < len(newList) {
				right = newList[i]
			}
			a.auditJSON(ctx, fmt.Sprintf("%s[%d]", field, i+1), left, right)
		}
		return
	}
	a.auditChange(ctx, field, before, after)
}
