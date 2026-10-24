package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type invitationOptions struct {
	Enabled  bool `json:"enabled"`
	Required bool `json:"required"`
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
	respond(w, 200, value)
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
		Count     int  `json:"count"`
		Uses      int  `json:"uses"`
		Hours     int  `json:"hours"`
		Permanent bool `json:"permanent"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !batch {
		body.Count = 1
	}
	if body.Count < 1 || body.Count > 100 || body.Uses < 1 || body.Uses > 1000 || !body.Permanent && (body.Hours < 1 || body.Hours > 8760) {
		failure(w, 400, "批量数量须为 1–100，次数为 1–1000，有效期为 1–8760 小时或长期有效")
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
	expires := int64(0)
	if !body.Permanent {
		expires = time.Now().Add(time.Duration(body.Hours) * time.Hour).Unix()
	}
	for i := 0; i < body.Count; i++ {
		code, e := secret()
		if e != nil {
			err = e
			break
		}
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO invitation_codes(code_hash,expires_at,remaining_uses) VALUES(?,?,?)"), tokenHash(code), expires, body.Uses)
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
		respond(w, 201, map[string]any{"code": codes[0], "expiresAt": expires, "remainingUses": body.Uses})
		return
	}
	respond(w, 201, map[string]any{"codes": codes, "expiresAt": expires, "remainingUses": body.Uses})
}
