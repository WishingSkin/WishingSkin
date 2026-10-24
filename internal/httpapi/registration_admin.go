package httpapi

import (
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
	var body struct{ Mode string }
	if !decode(w, r, &body) {
		return
	}
	if body.Mode != "open" && body.Mode != "closed" && body.Mode != "invitation" {
		failure(w, 400, "注册模式无效")
		return
	}
	services, serviceErr := a.services(r.Context())
	if serviceErr != nil {
		failure(w, 503, "暂时无法读取服务设置")
		return
	}
	if body.Mode != "closed" && (services.Mail == nil || services.Challenge == nil || services.SiteKey == "") {
		failure(w, 400, "开放注册前须配置邮件和 Turnstile")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	_, err := tx.ExecContext(r.Context(), a.s.Bind("UPDATE settings SET value=? WHERE setting_key='registration_mode'"), body.Mode)
	if err == nil && body.Mode == "invitation" {
		err = a.putSetting(r.Context(), tx, "official_invitation", invitationOptions{Enabled: true, Required: true})
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
