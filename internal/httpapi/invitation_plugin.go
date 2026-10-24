package httpapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wishingskin/internal/securebox"
)

type invitationOptions struct {
	Enabled  bool `json:"enabled"`
	Required bool `json:"required"`
}
type invitationMigrationStats struct {
	Total  int64 `json:"total"`
	Unused int64 `json:"unused"`
	Used   int64 `json:"used"`
}
type settingReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (a *Server) invitationOptions(ctx context.Context, reader settingReader, mode string) (invitationOptions, error) {
	result := invitationOptions{Enabled: mode == "invitation", Required: mode == "invitation"}
	var raw string
	err := reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='official_invitation'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(raw), &result)
	if err == nil {
		var enabled bool
		pluginErr := reader.QueryRowContext(ctx, "SELECT enabled FROM plugins WHERE plugin_id='wishingskin.invitations'").Scan(&enabled)
		if errors.Is(pluginErr, sql.ErrNoRows) {
			enabled = true
		} else if pluginErr != nil {
			return result, pluginErr
		}
		if !enabled {
			result = invitationOptions{}
		}
	}
	return result, err
}
func (a *Server) invitationPlugin(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	var mode string
	err := a.s.DB.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode'").Scan(&mode)
	if err != nil {
		failure(w, 503, "暂时无法读取插件设置")
		return
	}
	value, err := a.invitationOptions(r.Context(), a.s.DB, mode)
	if err != nil {
		failure(w, 503, "暂时无法读取插件设置")
		return
	}
	var stats invitationMigrationStats
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*),COALESCE(SUM(CASE WHEN remaining_uses>0 THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN remaining_uses=0 THEN 1 ELSE 0 END),0) FROM invitation_codes WHERE source_id IS NOT NULL")).Scan(&stats.Total, &stats.Unused, &stats.Used); err != nil {
		failure(w, 503, "暂时无法读取邀请码迁移统计")
		return
	}
	respond(w, 200, map[string]any{"enabled": value.Enabled, "required": value.Required, "migration": stats})
}
func (a *Server) updateInvitationPlugin(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body invitationOptions
	if !decode(w, r, &body) {
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	var mode string
	err := tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode)
	if err == nil {
		err = a.putSetting(r.Context(), tx, "official_invitation", body)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE plugins SET enabled=? WHERE plugin_id='wishingskin.invitations'"), body.Enabled)
	}
	if err == nil && mode == "invitation" {
		_, err = tx.ExecContext(r.Context(), "UPDATE settings SET value='open' WHERE setting_key='registration_mode'")
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "plugin.invitations", "site", "invitations")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存插件设置")
		return
	}
	respond(w, 200, body)
}
func (a *Server) createInvitationBatch(w http.ResponseWriter, r *http.Request) {
	a.generateInvitations(w, r, true)
}
func (a *Server) generateInvitations(w http.ResponseWriter, r *http.Request, batch bool) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Count int `json:"count"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !batch {
		body.Count = 1
	}
	if body.Count < 1 || body.Count > 100 {
		failure(w, 400, "批量数量须为 1–100")
		return
	}
	if len(a.cfg.SettingsKey) != 32 {
		failure(w, 503, "本地设置密钥尚不可用，无法安全保存邀请码")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	var mode string
	err := tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode)
	if err != nil {
		failure(w, 503, "暂时无法创建邀请码")
		return
	}
	options, err := a.invitationOptions(r.Context(), tx, mode)
	if err != nil {
		failure(w, 503, "暂时无法读取插件设置")
		return
	}
	if !options.Enabled {
		failure(w, 409, "请先启用官方邀请码插件")
		return
	}
	codes := make([]string, 0, body.Count)
	generatedAt := time.Now().Unix()
	for i := 0; i < body.Count; i++ {
		code, e := secret()
		if e != nil {
			err = e
			break
		}
		sealed, sealErr := securebox.Seal(a.cfg.SettingsKey, code, "wishingskin:invitation_code:v1")
		if sealErr != nil {
			err = sealErr
			break
		}
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO invitation_codes(code_hash,expires_at,remaining_uses,encrypted_code,generated_at) VALUES(?,?,?,?,?)"), tokenHash(code), int64(0), 1, sealed, generatedAt)
		if err != nil {
			break
		}
		codes = append(codes, code)
	}
	if err == nil {
		target := "batch"
		if !batch {
			target = tokenHash(codes[0])
		}
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "invitation.create", "invitation", target)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法创建邀请码")
		return
	}
	if !batch {
		respond(w, 201, map[string]any{"code": codes[0], "expiresAt": int64(0), "remainingUses": 1})
		return
	}
	respond(w, 201, map[string]any{"codes": codes, "expiresAt": int64(0), "remainingUses": 1})
}

type exportInvitation struct {
	Code         string
	Status       string
	GeneratedAt  int64
	UsedAt       sql.NullInt64
	SourceUserID sql.NullInt64
}

func (a *Server) exportInvitationCodes(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("status"))
	if filter == "" {
		filter = "unused"
	}
	if filter != "unused" && filter != "all" {
		failure(w, 400, "邀请码筛选条件无效")
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "txt" && format != "csv" {
		failure(w, 400, "邀请码导出格式无效")
		return
	}
	if len(a.cfg.SettingsKey) != 32 {
		failure(w, 503, "本地设置密钥尚不可用，无法导出邀请码")
		return
	}
	where := " WHERE encrypted_code IS NOT NULL"
	if filter == "unused" {
		where += " AND remaining_uses>0"
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT encrypted_code,remaining_uses,generated_at,used_at,source_user_id FROM invitation_codes"+where+" ORDER BY generated_at,code_hash"))
	if err != nil {
		failure(w, 503, "暂时无法读取邀请码")
		return
	}
	decrypted := make([]exportInvitation, 0)
	for rows.Next() {
		var sealed sql.NullString
		var remaining int
		var item exportInvitation
		if err := rows.Scan(&sealed, &remaining, &item.GeneratedAt, &item.UsedAt, &item.SourceUserID); err != nil {
			rows.Close()
			failure(w, 503, "暂时无法读取邀请码")
			return
		}
		if !sealed.Valid {
			rows.Close()
			failure(w, 503, "存在未加密的邀请码，无法安全导出")
			return
		}
		item.Code, err = securebox.Open(a.cfg.SettingsKey, sealed.String, "wishingskin:invitation_code:v1")
		if err != nil {
			rows.Close()
			failure(w, 503, "邀请码密文无法解密，请检查本地设置密钥")
			return
		}
		if remaining > 0 {
			item.Status = "unused"
		} else {
			item.Status = "used"
		}
		decrypted = append(decrypted, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		failure(w, 503, "暂时无法读取邀请码")
		return
	}
	rows.Close()
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	if err := a.s.RecordAdminEvent(r.Context(), tx, u.ID, "invitation.export", "invitation", fmt.Sprintf("status=%s,count=%d", filter, len(decrypted))); err != nil || tx.Commit() != nil {
		_ = tx.Rollback()
		failure(w, 503, "无法记录邀请码导出操作")
		return
	}
	if format == "txt" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="wishingskin-invitations.txt"`)
		for _, item := range decrypted {
			_, _ = fmt.Fprintln(w, item.Code)
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="wishingskin-invitations.csv"`)
	writer := csv.NewWriter(w)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	_ = writer.Write([]string{"code", "status", "generated_at", "used_at", "source_user_id"})
	for _, item := range decrypted {
		usedAt := ""
		if item.UsedAt.Valid {
			usedAt = strconv.FormatInt(item.UsedAt.Int64, 10)
		}
		sourceUser := ""
		if item.SourceUserID.Valid {
			sourceUser = strconv.FormatInt(item.SourceUserID.Int64, 10)
		}
		_ = writer.Write([]string{item.Code, item.Status, strconv.FormatInt(item.GeneratedAt, 10), usedAt, sourceUser})
	}
	writer.Flush()
}
