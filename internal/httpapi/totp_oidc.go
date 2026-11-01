package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"
)

func (a *Server) createOIDCTOTP(ctx context.Context, tx *sql.Tx, u User, flow oidcFlow) (string, error) {
	token, err := secret()
	if err != nil {
		return "", err
	}
	flow.Purpose = "totp"
	flow.UserID = u.ID
	flow.CredentialHash = u.credentialHash
	sealed, err := a.sealOIDC(flow, "flow")
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO oidc_flows(flow_hash,value,expires_at) VALUES(?,?,?)"), tokenHash(token), sealed, time.Now().Add(5*time.Minute).Unix())
	return token, err
}

func (a *Server) completeOIDCTOTP(w http.ResponseWriter, r *http.Request) {
	if !a.publicWrite(w, r) {
		return
	}
	if !a.totpLimit.allow(r) {
		failure(w, 429, "验证尝试过于频繁，请稍后重试")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	cookie, err := r.Cookie("ws_totp")
	if err != nil {
		failure(w, 401, "登录验证已失效，请重新登录")
		return
	}
	flow, err := a.readOIDCFlow(r.Context(), a.s.DB, cookie.Value, "totp", false)
	if err != nil {
		failure(w, 401, "登录验证已失效，请重新登录")
		return
	}
	u, err := a.oidcUser(r.Context(), flow.UserID)
	if err != nil || u.credentialHash != flow.CredentialHash {
		failure(w, 401, "登录验证已失效，请重新登录")
		return
	}
	if !a.totpLimit.allowKey("account:" + strconv.FormatInt(u.ID, 10)) {
		w.Header().Set("Retry-After", "300")
		failure(w, 429, "验证尝试过于频繁，请稍后重试")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法登录")
		return
	}
	defer tx.Rollback()
	err = a.lockOIDCConfiguration(r.Context(), tx, flow.ConfigHash)
	if err == nil {
		err = a.lockAuthenticatedUser(r.Context(), tx, u)
	}
	if err == nil {
		_, err = a.readOIDCFlow(r.Context(), tx, cookie.Value, "totp", true)
	}
	if err == nil {
		var linked int
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM oidc_identities WHERE identity_key=? AND user_id=?"), oidcIdentityKey(flow.Issuer, flow.Subject), u.ID).Scan(&linked)
		if err == nil && linked != 1 {
			err = errCredentials
		}
	}
	if err == nil {
		err = a.consumeTOTP(r.Context(), tx, u.ID, body.Code)
	}
	if errors.Is(err, errTOTP) || errors.Is(err, errTOTPRequired) {
		respond(w, 401, map[string]any{"error": "双重验证码无效或尚未填写", "totpRequired": true})
		return
	}
	if err != nil {
		if errors.Is(err, errCredentials) || errors.Is(err, sql.ErrNoRows) {
			failure(w, 401, "登录验证已失效，请重新登录")
		} else {
			failure(w, 503, "暂时无法登录")
		}
		return
	}
	session, err := secret()
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)"), tokenHash(session), u.ID, time.Now().Add(24*time.Hour).Unix())
		if err == nil {
			err = a.recordWebAuthTime(r.Context(), tx, session)
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oidc_flows WHERE flow_hash=?"), tokenHash(cookie.Value))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法登录")
		return
	}
	a.oidcCookie(w, "ws_totp", "", -1)
	a.oidcCookie(w, "ws_session", session, 86400)
	respond(w, 200, map[string]any{"user": u, "csrfToken": csrf(session), "next": flow.Next})
}
