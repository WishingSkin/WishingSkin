package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
)

func (a *Server) adminRequest(w http.ResponseWriter, r *http.Request) (User, bool) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return User{}, false
	}
	if !a.validWriteRequest(w, r, token) {
		return User{}, false
	}
	if !u.IsAdmin {
		failure(w, 403, "需要管理员权限")
		return User{}, false
	}
	return u, true
}

func (a *Server) setRegistration(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		ManualEmailVerification *bool     `json:"manualEmailVerification"`
		RandomUserIDs           *bool     `json:"randomUserIds"`
		Mode                    string    `json:"mode"`
		EmailDomains            *[]string `json:"emailDomains"`
		BlockedEmailDomains     *[]string `json:"blockedEmailDomains"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Mode != "open" && body.Mode != "closed" && body.Mode != "invitation" {
		failure(w, 400, "注册模式无效")
		return
	}
	for _, list := range []*[]string{body.EmailDomains, body.BlockedEmailDomains} {
		if list != nil {
			normalized, err := normalizeEmailDomains(*list)
			if err != nil {
				failure(w, 400, "注册邮箱域名格式无效")
				return
			}
			*list = normalized
		}
	}
	services, serviceErr := a.services(r.Context())
	if serviceErr != nil {
		failure(w, 503, "暂时无法读取服务设置")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	manual, err := manualEmailVerification(r.Context(), tx)
	if err != nil {
		failure(w, 503, "暂时无法读取注册设置")
		return
	}
	if body.ManualEmailVerification != nil {
		manual = *body.ManualEmailVerification
	}
	if body.Mode != "closed" && (services.Challenge == nil || services.SiteKey == "" || !manual && services.Mail == nil) {
		failure(w, 400, "开放注册前须配置验证服务")
		return
	}
	var previous string
	if err = tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode'").Scan(&previous); err != nil {
		failure(w, 503, "暂时无法读取注册设置")
		return
	}
	a.auditChange(r.Context(), "registration_mode", previous, body.Mode)
	_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE settings SET value=? WHERE setting_key='registration_mode'"), body.Mode)
	if err == nil && body.EmailDomains != nil {
		err = a.putSetting(r.Context(), tx, "registration_email_domains", *body.EmailDomains)
	}
	if err == nil && body.BlockedEmailDomains != nil {
		err = a.putSetting(r.Context(), tx, "registration_blocked_domains", *body.BlockedEmailDomains)
	}
	if err == nil && body.ManualEmailVerification != nil {
		err = a.putSetting(r.Context(), tx, "manual_email_verification", manual)
		if err == nil && manual {
			_, err = tx.ExecContext(r.Context(), "DELETE FROM account_tokens WHERE purpose='verify'")
		}
	}
	if err == nil && body.RandomUserIDs != nil {
		err = a.putSetting(r.Context(), tx, "random_user_ids", *body.RandomUserIDs)
	}
	if err == nil && body.Mode == "invitation" {
		err = a.putSetting(r.Context(), tx, "official_invitation", invitationOptions{Enabled: true, Required: true})
		if err == nil {
			_, err = tx.ExecContext(r.Context(), "UPDATE plugins SET enabled=TRUE WHERE plugin_id='wishingskin.invitations'")
		}
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "registration."+body.Mode, "site", "registration")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存站点设置")
		return
	}
	w.WriteHeader(204)
}

func (a *Server) createInvitation(w http.ResponseWriter, r *http.Request) {
	a.generateInvitations(w, r, false)
}

func manualEmailVerification(ctx context.Context, reader settingReader) (bool, error) {
	var raw string
	err := reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='manual_email_verification'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return raw == "true", err
}

func lockRegistrationMode(ctx context.Context, tx *sql.Tx) (string, error) {
	var mode string
	err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode)
	return mode, err
}
