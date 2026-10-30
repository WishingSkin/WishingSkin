package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/skip2/go-qrcode"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"wishingskin/internal/securebox"
)

func (a *Server) totpRequest(w http.ResponseWriter, r *http.Request) (User, string, bool) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return u, "", false
	}
	if !a.validWriteRequest(w, r, token) {
		return u, "", false
	}
	if !a.totpLimit.allow(r) || !a.totpLimit.allowKey("account:"+strconv.FormatInt(u.ID, 10)) {
		w.Header().Set("Retry-After", "300")
		failure(w, 429, "验证尝试过于频繁，请稍后重试")
		return u, "", false
	}
	return u, token, true
}

func (a *Server) totpPassword(w http.ResponseWriter, r *http.Request, u User, password string) (User, bool) {
	if password == "" {
		failure(w, 400, "请输入当前密码")
		return u, false
	}
	verified, err := a.authenticate(r.Context(), u.Email, password)
	if errors.Is(err, errCredentials) {
		failure(w, 400, "当前密码不正确")
		return u, false
	}
	if err != nil {
		a.authError(w, err)
		return u, false
	}
	return verified, true
}

func (a *Server) setupTOTP(w http.ResponseWriter, r *http.Request) {
	u, token, ok := a.totpRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
	}
	if !decode(w, r, &body) {
		return
	}
	u, ok = a.totpPassword(w, r, u, body.CurrentPassword)
	if !ok {
		return
	}
	secret, err := newTOTPSecret()
	if err != nil {
		failure(w, 503, "暂时无法生成双重验证密钥")
		return
	}
	sealed, err := securebox.Seal(a.cfg.SettingsKey, secret, totpPurpose)
	if err != nil {
		failure(w, 503, "站点设置密钥不可用")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法保存双重验证密钥")
		return
	}
	defer tx.Rollback()
	if err = a.lockWebAccount(r.Context(), tx, u.ID, token); err == nil {
		err = a.lockAuthenticatedUser(r.Context(), tx, u)
	}
	if err != nil {
		sessionError(w, err)
		return
	}
	var enabled bool
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT enabled FROM user_totp WHERE user_id=? FOR UPDATE"), u.ID).Scan(&enabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		failure(w, 503, "暂时无法保存双重验证密钥")
		return
	}
	if enabled {
		failure(w, 409, "请先停用双重验证后再更换密钥")
		return
	}
	query := "INSERT INTO user_totp(user_id,secret_encrypted,enabled,created_at,last_used_step) VALUES(?,?,FALSE,?,-1) ON CONFLICT(user_id) DO UPDATE SET secret_encrypted=EXCLUDED.secret_encrypted,created_at=EXCLUDED.created_at,last_used_step=-1"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO user_totp(user_id,secret_encrypted,enabled,created_at,last_used_step) VALUES(?,?,FALSE,?,-1) ON DUPLICATE KEY UPDATE secret_encrypted=VALUES(secret_encrypted),created_at=VALUES(created_at),last_used_step=-1"
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind(query), u.ID, sealed, a.totpNow().Unix())
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存双重验证密钥")
		return
	}
	issuer := "WishingSkin"
	uri := "otpauth://totp/" + url.PathEscape(issuer+":"+u.Email) + "?secret=" + url.QueryEscape(secret) + "&issuer=" + url.QueryEscape(issuer) + "&algorithm=SHA1&digits=6&period=30"
	qr, err := qrcode.Encode(uri, qrcode.Medium, 320)
	if err != nil {
		failure(w, 503, "暂时无法生成二维码")
		return
	}
	respond(w, 200, map[string]string{"secret": secret, "otpauthUrl": uri, "qrCode": "data:image/png;base64," + base64.StdEncoding.EncodeToString(qr)})
}

func totpRecoveryHash(userID int64, code string) string {
	return tokenHash("totp-recovery:" + strconv.FormatInt(userID, 10) + ":" + strings.ToLower(code))
}
func (a *Server) newRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64) ([]string, error) {
	if _, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM user_totp_recovery WHERE user_id=?"), userID); err != nil {
		return nil, err
	}
	codes := make([]string, 0, 10)
	for range 10 {
		var b [10]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		raw := hex.EncodeToString(b[:])
		code := raw[:10] + "-" + raw[10:]
		if _, err := tx.ExecContext(ctx, a.s.Bind("INSERT INTO user_totp_recovery(user_id,code_hash) VALUES(?,?)"), userID, totpRecoveryHash(userID, code)); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func (a *Server) enableTOTP(w http.ResponseWriter, r *http.Request) {
	u, token, ok := a.totpRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法启用双重验证")
		return
	}
	defer tx.Rollback()
	if err = a.lockWebAccount(r.Context(), tx, u.ID, token); err != nil {
		sessionError(w, err)
		return
	}
	var sealed string
	var enabled bool
	var created int64
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT secret_encrypted,enabled,created_at FROM user_totp WHERE user_id=? FOR UPDATE"), u.ID).Scan(&sealed, &enabled, &created)
	if errors.Is(err, sql.ErrNoRows) || err == nil && a.totpNow().Unix()-created > 600 {
		failure(w, 400, "请重新生成双重验证密钥")
		return
	}
	if err != nil {
		failure(w, 503, "暂时无法启用双重验证")
		return
	}
	if enabled {
		failure(w, 409, "双重验证已启用")
		return
	}
	secret, err := securebox.Open(a.cfg.SettingsKey, sealed, totpPurpose)
	if err != nil {
		failure(w, 503, "站点设置密钥不可用")
		return
	}
	step, ok := validTOTP(secret, body.Code, a.totpNow())
	if !ok {
		failure(w, 400, "验证码无效")
		return
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE user_totp SET enabled=TRUE,last_used_step=? WHERE user_id=?"), step, u.ID)
	var codes []string
	if err == nil {
		codes, err = a.newRecoveryCodes(r.Context(), tx, u.ID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM web_sessions WHERE user_id=? AND token_hash<>?"), u.ID, tokenHash(token))
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_codes WHERE user_id=?"), u.ID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE user_id=?"), u.ID)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "account.totp.enable", "user", strconv.FormatInt(u.ID, 10))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法启用双重验证")
		return
	}
	respond(w, 200, totpStatus{Enabled: true, Configured: true, RecoveryCodes: codes})
}

func (a *Server) disableTOTP(w http.ResponseWriter, r *http.Request) { a.changeTOTP(w, r, false) }
func (a *Server) regenerateTOTPRecovery(w http.ResponseWriter, r *http.Request) {
	a.changeTOTP(w, r, true)
}
func (a *Server) changeTOTP(w http.ResponseWriter, r *http.Request, regenerate bool) {
	u, token, ok := a.totpRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Code            string `json:"code"`
		CurrentPassword string `json:"currentPassword"`
	}
	if !decode(w, r, &body) {
		return
	}
	u, ok = a.totpPassword(w, r, u, body.CurrentPassword)
	if !ok {
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法修改双重验证")
		return
	}
	defer tx.Rollback()
	if err = a.lockWebAccount(r.Context(), tx, u.ID, token); err == nil {
		err = a.lockAuthenticatedUser(r.Context(), tx, u)
	}
	if err != nil {
		sessionError(w, err)
		return
	}
	var enabled bool
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT enabled FROM user_totp WHERE user_id=?"), u.ID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !enabled {
		failure(w, 409, "双重验证尚未启用")
		return
	}
	if err != nil {
		failure(w, 503, "暂时无法修改双重验证")
		return
	}
	err = a.consumeTOTP(r.Context(), tx, u.ID, body.Code)
	if errors.Is(err, errTOTP) || errors.Is(err, errTOTPRequired) {
		failure(w, 400, "验证码无效")
		return
	}
	if err != nil {
		failure(w, 503, "暂时无法修改双重验证")
		return
	}
	var codes []string
	action := "account.totp.disable"
	if regenerate {
		action = "account.totp.recovery"
		codes, err = a.newRecoveryCodes(r.Context(), tx, u.ID)
	} else {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM user_totp_recovery WHERE user_id=?"), u.ID)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM user_totp WHERE user_id=?"), u.ID)
		}
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, action, "user", strconv.FormatInt(u.ID, 10))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法修改双重验证")
		return
	}
	respond(w, 200, totpStatus{Enabled: regenerate, Configured: regenerate, RecoveryCodes: codes})
}
