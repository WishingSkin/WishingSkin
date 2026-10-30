package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"
)

const (
	oidcConfirmLinkPurpose  = "confirm_link"
	oidcConfirmLoginPurpose = "confirm_login"
)

func (a *Server) beginOIDCConfirmation(w http.ResponseWriter, r *http.Request, flow oidcFlow, purpose string) {
	if purpose == oidcConfirmLinkPurpose {
		u, session, err := a.current(r)
		if err != nil || u.ID != flow.UserID || tokenHash(session) != flow.SessionHash {
			a.oidcFailure(w, r, "session")
			return
		}
		local, err := a.oidcUser(r.Context(), u.ID)
		if err != nil || local.credentialHash != flow.CredentialHash {
			a.oidcFailure(w, r, "session")
			return
		}
		var linked int
		err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM oidc_identities WHERE identity_key=?"), oidcIdentityKey(flow.Issuer, flow.Subject)).Scan(&linked)
		if err != nil {
			a.oidcFailure(w, r, "failed")
			return
		}
		if linked != 0 {
			a.oidcFailure(w, r, "linked")
			return
		}
	}
	flow.Purpose = purpose
	flow.BrowserHash = ""
	flow.Nonce = ""
	flow.Verifier = ""
	token, err := secret()
	if err == nil {
		err = a.saveOIDCFlow(r.Context(), token, flow)
	}
	if err != nil {
		a.oidcFailure(w, r, "failed")
		return
	}
	a.oidcCookie(w, "ws_oidc_confirm", token, 600)
	if purpose == oidcConfirmLinkPurpose {
		http.Redirect(w, r, a.cfg.PublicURL+"/user/account/profile?oidcConfirm=1", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, a.cfg.PublicURL+"/login?oidcConfirm=1", http.StatusSeeOther)
}

func (a *Server) readOIDCConfirmationFlow(ctx context.Context, reader settingReader, token string, lock bool) (oidcFlow, error) {
	flow, err := a.readOIDCFlow(ctx, reader, token, oidcConfirmLinkPurpose, lock)
	if err == nil {
		return flow, nil
	}
	return a.readOIDCFlow(ctx, reader, token, oidcConfirmLoginPurpose, lock)
}

func (a *Server) oidcConfirmationState(ctx context.Context, r *http.Request, flow oidcFlow) (OIDCSettings, User, error) {
	cfg, err := a.oidcSettingsFor(ctx, flow.ProviderID)
	if err != nil || !cfg.Enabled || oidcFingerprint(cfg) != flow.ConfigHash {
		return OIDCSettings{}, User{}, errCredentials
	}
	enabled, err := a.storedPluginEnabled(ctx, "wishingskin.oidc")
	if err != nil {
		return OIDCSettings{}, User{}, err
	}
	if !enabled {
		return OIDCSettings{}, User{}, errCredentials
	}
	if flow.Purpose == oidcConfirmLinkPurpose {
		_, session, err := a.current(r)
		if err != nil || tokenHash(session) != flow.SessionHash {
			return OIDCSettings{}, User{}, errCredentials
		}
	} else if _, _, err := a.current(r); err == nil {
		return OIDCSettings{}, User{}, errCredentials
	}
	u, err := a.oidcUser(ctx, flow.UserID)
	if err != nil || flow.Purpose == oidcConfirmLinkPurpose && u.credentialHash != flow.CredentialHash {
		return OIDCSettings{}, User{}, errCredentials
	}
	var linked int
	if flow.Purpose == oidcConfirmLinkPurpose {
		err = a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM oidc_identities WHERE identity_key=?"), oidcIdentityKey(flow.Issuer, flow.Subject)).Scan(&linked)
		if err == nil && linked != 0 {
			return OIDCSettings{}, User{}, errCredentials
		}
	} else {
		err = a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM oidc_identities WHERE identity_key=? AND user_id=?"), oidcIdentityKey(flow.Issuer, flow.Subject), u.ID).Scan(&linked)
		if err == nil && linked != 1 {
			return OIDCSettings{}, User{}, errCredentials
		}
	}
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return OIDCSettings{}, User{}, err
		}
		return OIDCSettings{}, User{}, errCredentials
	}
	return cfg, u, nil
}

func (a *Server) oidcConfirmationFlow(r *http.Request) (string, oidcFlow, error) {
	cookie, err := r.Cookie("ws_oidc_confirm")
	if err != nil {
		return "", oidcFlow{}, errCredentials
	}
	flow, err := a.readOIDCConfirmationFlow(r.Context(), a.s.DB, cookie.Value, false)
	return cookie.Value, flow, err
}

func (a *Server) oidcConfirmationInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_, flow, err := a.oidcConfirmationFlow(r)
	if err != nil {
		failure(w, 401, "OIDC 确认已失效，请重新开始")
		return
	}
	cfg, u, err := a.oidcConfirmationState(r.Context(), r, flow)
	if err != nil {
		status := 401
		if !errors.Is(err, errCredentials) && !errors.Is(err, sql.ErrNoRows) {
			status = 503
		}
		failure(w, status, "OIDC 确认已失效，请重新开始")
		return
	}
	purpose := "login"
	if flow.Purpose == oidcConfirmLinkPurpose {
		purpose = "link"
	}
	respond(w, 200, map[string]any{
		"purpose":       purpose,
		"provider":      cfg.Name,
		"accountName":   u.Nickname,
		"accountEmail":  u.Email,
		"name":          flow.Name,
		"username":      flow.Username,
		"email":         flow.Email,
		"emailVerified": flow.EmailVerified,
	})
}

func (a *Server) confirmOIDC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.publicWrite(w, r) {
		return
	}
	if !a.loginLimit.allow(r) {
		failure(w, 429, "登录尝试过于频繁，请稍后重试")
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Action != "confirm" && body.Action != "cancel" {
		failure(w, 400, "请求内容无效")
		return
	}
	token, flow, err := a.oidcConfirmationFlow(r)
	if err != nil {
		failure(w, 401, "OIDC 确认已失效，请重新开始")
		return
	}
	if body.Action == "cancel" {
		if _, err := a.s.DB.ExecContext(r.Context(), a.s.Bind("DELETE FROM oidc_flows WHERE flow_hash=?"), tokenHash(token)); err != nil {
			failure(w, 503, "暂时无法取消 OIDC 确认")
			return
		}
		a.oidcCookie(w, "ws_oidc_confirm", "", -1)
		next := "/login"
		if flow.Purpose == oidcConfirmLinkPurpose {
			next = "/user/account/profile"
		}
		respond(w, 200, map[string]string{"next": next})
		return
	}
	_, u, err := a.oidcConfirmationState(r.Context(), r, flow)
	if err != nil {
		status := 401
		if !errors.Is(err, errCredentials) && !errors.Is(err, sql.ErrNoRows) {
			status = 503
		}
		failure(w, status, "OIDC 确认已失效，请重新开始")
		return
	}
	if flow.Purpose == oidcConfirmLinkPurpose {
		_, session, err := a.current(r)
		if err != nil {
			failure(w, 401, "OIDC 确认已失效，请重新开始")
			return
		}
		if !a.validWriteRequest(w, r, session) {
			return
		}
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法完成 OIDC 确认")
		return
	}
	defer tx.Rollback()
	err = a.lockOIDCConfiguration(r.Context(), tx, flow.ConfigHash)
	if err == nil {
		err = a.lockAuthenticatedUser(r.Context(), tx, u)
	}
	if err == nil {
		flow, err = a.readOIDCConfirmationFlow(r.Context(), tx, token, true)
	}
	identityKey := oidcIdentityKey(flow.Issuer, flow.Subject)
	if err == nil && flow.Purpose == oidcConfirmLinkPurpose {
		var active int
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM web_sessions WHERE token_hash=? AND user_id=? AND expires_at>?"), flow.SessionHash, u.ID, time.Now().Unix()).Scan(&active)
		if err == nil && active != 1 {
			err = errCredentials
		}
	}
	if err == nil && flow.Purpose == oidcConfirmLinkPurpose {
		var linked int
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM oidc_identities WHERE identity_key=?"), identityKey).Scan(&linked)
		if err == nil && linked != 0 {
			err = errCredentials
		}
	}
	if err == nil && flow.Purpose == oidcConfirmLoginPurpose {
		var linked int
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM oidc_identities WHERE identity_key=? AND user_id=?"), identityKey, u.ID).Scan(&linked)
		if err == nil && linked != 1 {
			err = errCredentials
		}
	}
	if err == nil && flow.Purpose == oidcConfirmLinkPurpose {
		err = a.insertOIDCIdentity(r.Context(), tx, u.ID, flow)
		if err == nil {
			err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "oidc.link", "account", "oidc")
		}
	}
	if err == nil && flow.Purpose == oidcConfirmLoginPurpose {
		var enabled bool
		totpErr := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT enabled FROM user_totp WHERE user_id=?"), u.ID).Scan(&enabled)
		if totpErr != nil && !errors.Is(totpErr, sql.ErrNoRows) {
			err = totpErr
		}
		if err == nil && enabled {
			challenge, challengeErr := a.createOIDCTOTP(r.Context(), tx, u, flow)
			if challengeErr == nil {
				_, challengeErr = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oidc_flows WHERE flow_hash=?"), tokenHash(token))
			}
			if challengeErr == nil {
				challengeErr = tx.Commit()
			}
			if challengeErr != nil {
				failure(w, 503, "暂时无法完成 OIDC 确认")
				return
			}
			a.oidcCookie(w, "ws_oidc_confirm", "", -1)
			a.oidcCookie(w, "ws_totp", challenge, 300)
			respond(w, 200, map[string]string{"next": "/login?totp=1"})
			return
		}
	}
	var session string
	if err == nil && flow.Purpose == oidcConfirmLoginPurpose {
		session, err = secret()
		if err == nil {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)"), tokenHash(session), u.ID, time.Now().Add(24*time.Hour).Unix())
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oidc_flows WHERE flow_hash=?"), tokenHash(token))
	}
	if err == nil {
		err = tx.Commit()
	}
	if uniqueViolation(err) {
		failure(w, 409, "此 OIDC 身份或当前账号已绑定，请检查后重试。")
		return
	}
	if err != nil {
		failure(w, 401, "OIDC 确认已失效，请重新开始")
		return
	}
	a.oidcCookie(w, "ws_oidc_confirm", "", -1)
	if flow.Purpose == oidcConfirmLinkPurpose {
		respond(w, 200, map[string]string{"next": "/user/account/profile?oidcLinked=1"})
		return
	}
	a.oidcCookie(w, "ws_session", session, 86400)
	respond(w, 200, map[string]string{"next": flow.Next})
}
