package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wishingskin/internal/credential"
)

// Serialize credential issuance with password changes and compare the exact
// hash that was verified, so an in-flight old login cannot escape revocation.
func (a *Server) lockAuthenticatedUser(ctx context.Context, tx *sql.Tx, u User) error {
	var hash string
	var banned bool
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT password_hash,banned FROM users WHERE id=? FOR UPDATE"), u.ID).Scan(&hash, &banned)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (banned || hash != u.credentialHash) {
		return errCredentials
	}
	if err == nil {
		var blocked int
		err = tx.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM account_states WHERE user_id=? AND verification_required=TRUE AND email_verified=FALSE"), u.ID).Scan(&blocked)
		if err == nil && blocked != 0 {
			return errCredentials
		}
	}
	return err
}

func (a *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	if !a.passwordLimit.allow(r) || !a.passwordLimit.allowKey("account:"+strconv.FormatInt(u.ID, 10)) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "修改密码尝试过于频繁，请稍后重试")
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !credential.ValidNewPassword(body.NewPassword) {
		failure(w, 400, "新密码须为 8–128 个字符，且同时包含英文字母、数字和符号")
		return
	}
	if len(body.CurrentPassword) == 0 || len(body.CurrentPassword) > 1024 {
		failure(w, 400, "当前密码不正确")
		return
	}
	select {
	case a.hashes <- struct{}{}:
		defer func() { <-a.hashes }()
	default:
		failure(w, 429, "密码处理繁忙，请稍后重试")
		return
	}
	var hash, method, salt string
	err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT password_hash,password_method,password_salt FROM users WHERE id=?"), u.ID).Scan(&hash, &method, &salt)
	if err != nil {
		failure(w, 503, "暂时无法修改密码")
		return
	}
	ok, err := credential.Verify(method, hash, salt, body.CurrentPassword)
	if err != nil || !ok {
		failure(w, 400, "当前密码不正确")
		return
	}
	updated, err := credential.Hash(body.NewPassword)
	if err != nil {
		failure(w, 503, "暂时无法修改密码")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法修改密码")
		return
	}
	defer tx.Rollback()
	u.credentialHash = hash
	err = a.lockAuthenticatedUser(r.Context(), tx, u)
	if err == nil {
		var active int
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM web_sessions WHERE token_hash=? AND user_id=? AND expires_at>?"), tokenHash(token), u.ID, time.Now().Unix()).Scan(&active)
		if err == nil && active != 1 {
			err = errCredentials
		}
	}
	if errors.Is(err, errCredentials) {
		failure(w, 401, "账号凭据已变化，请重新登录")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE users SET password_hash=?,password_method='ARGON2ID',password_salt='' WHERE id=?"), updated, u.ID)
	}
	if err == nil {
		err = a.revokeAccountCredentials(r.Context(), tx, u.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法修改密码")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "ws_session", Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}
