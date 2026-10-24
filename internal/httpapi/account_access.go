package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"wishingskin/internal/credential"
	"wishingskin/internal/turnstile"
)

type MailSender interface {
	Send(context.Context, string, string, string) error
}
type ChallengeVerifier interface {
	Verify(context.Context, string, string, string) error
}

var errUnverified = errors.New("email verification required")

func (a *Server) registerAccountAccess() {
	a.mux.HandleFunc("POST /api/auth/register", a.register)
	a.mux.HandleFunc("POST /api/auth/email", a.requestAccountMail)
	a.mux.HandleFunc("POST /api/auth/verify", a.verifyEmail)
	a.mux.HandleFunc("POST /api/auth/reset", a.resetPassword)
	a.mux.HandleFunc("PATCH /api/admin/registration", a.setRegistration)
	a.mux.HandleFunc("POST /api/admin/invitations", a.createInvitation)
}

func (a *Server) registrationMode(ctx context.Context) (string, error) {
	var mode string
	err := a.s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='registration_mode'").Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "closed", nil
	}
	if mode != "open" && mode != "invitation" {
		mode = "closed"
	}
	if err != nil {
		return mode, err
	}
	options, err := a.invitationOptions(ctx, a.s.DB, mode)
	if mode != "closed" {
		mode = "open"
		if options.Enabled && options.Required {
			mode = "invitation"
		}
	}
	return mode, err
}

func (a *Server) site(w http.ResponseWriter, r *http.Request) {
	mode, err := a.registrationMode(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取站点设置")
		return
	}
	brand, err := a.siteBranding(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取站点设置")
		return
	}
	points, err := readPointRules(r.Context(), a.s.DB, false)
	if err != nil {
		failure(w, 503, "暂时无法读取站点设置")
		return
	}
	custom, err := a.appearance(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取站点设置")
		return
	}
	homepage, err := a.homepage(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取首页设置")
		return
	}
	services, err := a.services(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取服务设置")
		return
	}
	domains, err := a.registrationEmailDomains(r.Context(), a.s.DB)
	if err != nil {
		failure(w, 503, "暂时无法读取注册设置")
		return
	}
	respond(w, 200, map[string]any{"appearance": custom, "homepage": homepage, "noticeFormat": brand.NoticeFormat, "noticeHtml": renderNotice(brand.Notice, brand.NoticeFormat), "name": brand.Name, "tagline": brand.Tagline, "footer": brand.Footer, "notice": brand.Notice, "registration": mode, "emailDomains": domains, "mailEnabled": services.Mail != nil, "turnstileSiteKey": services.SiteKey, "points": points})
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && len(value) <= 254 && !strings.ContainsAny(value, "\r\n")
}

var emailDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func normalizeEmailDomains(domains []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(domains))
	for _, raw := range domains {
		domain := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "@")))
		if domain == "" {
			continue
		}
		if len(domain) > 253 || !emailDomainPattern.MatchString(domain) {
			return nil, errors.New("invalid email domain")
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		result = append(result, domain)
	}
	if len(result) > 100 {
		return nil, errors.New("too many email domains")
	}
	return result, nil
}

func (a *Server) registrationEmailDomains(ctx context.Context, reader settingReader) ([]string, error) {
	var raw string
	err := reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='registration_email_domains'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var domains []string
	if err := json.Unmarshal([]byte(raw), &domains); err != nil {
		return nil, err
	}
	return normalizeEmailDomains(domains)
}

func (a *Server) registrationEmailAllowed(ctx context.Context, reader settingReader, email string) (bool, error) {
	domains, err := a.registrationEmailDomains(ctx, reader)
	if err != nil {
		return false, err
	}
	if len(domains) == 0 {
		return true, nil
	}
	parts := strings.Split(strings.ToLower(email), "@")
	if len(parts) != 2 {
		return false, nil
	}
	for _, domain := range domains {
		if parts[1] == domain {
			return true, nil
		}
	}
	return false, nil
}

func (a *Server) publicWrite(w http.ResponseWriter, r *http.Request) bool {
	if !a.sameOrigin(r) {
		failure(w, 403, "请求来源无效")
		return false
	}
	return true
}

func (a *Server) challenge(w http.ResponseWriter, r *http.Request, token, action string) bool {
	services, serviceErr := a.services(r.Context())
	if serviceErr != nil || services.Challenge == nil || services.SiteKey == "" {
		failure(w, 503, "人机验证尚未配置，请联系管理员")
		return false
	}
	err := services.Challenge.Verify(r.Context(), token, connectionIP(r), action)
	if errors.Is(err, turnstile.ErrInvalid) {
		respond(w, 403, map[string]any{"error": "请重新完成人机验证", "captchaRequired": true})
		return false
	}
	if err != nil {
		failure(w, 503, "人机验证暂不可用，请重试")
		return false
	}
	return true
}

func (a *Server) mailAllowance(w http.ResponseWriter, r *http.Request, email string) bool {
	key := "email:" + tokenHash(strings.ToLower(email))
	if !a.mailLimit.allow(r) || !a.mailLimit.allowKey(key) || !a.mailCooldown.allowKey(key) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "邮件请求过于频繁，请稍后重试")
		return false
	}
	return true
}

func (a *Server) acquireMail(w http.ResponseWriter, r *http.Request) bool {
	services, err := a.services(r.Context())
	if err != nil || services.Mail == nil {
		failure(w, 503, "邮件服务尚未配置，请联系管理员")
		return false
	}
	select {
	case a.mailSlots <- struct{}{}:
		return true
	default:
		failure(w, 429, "邮件处理繁忙，请稍后重试")
		return false
	}
}

// The caller holds the user lock. Only an accepted mail and a committed
// transaction replace the previous link, so a failed resend keeps it usable.
func (a *Server) issueAccountMail(ctx context.Context, tx *sql.Tx, id int64, email, purpose string) error {
	token, err := secret()
	if err != nil {
		return err
	}
	lifetime := 24 * time.Hour
	if purpose == "reset" {
		lifetime = 30 * time.Minute
	}
	if _, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM account_tokens WHERE user_id=? AND purpose=?"), id, purpose); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, a.s.Bind("INSERT INTO account_tokens(user_id,purpose,token_hash,expires_at) VALUES(?,?,?,?)"), id, purpose, tokenHash(token), time.Now().Add(lifetime).Unix()); err != nil {
		return err
	}
	services, err := a.services(ctx)
	if err != nil {
		return err
	}
	if services.Mail == nil {
		return errors.New("mail unavailable")
	}
	return services.Mail.Send(ctx, email, purpose, a.cfg.PublicURL+"/#"+purpose+"="+token)
}

func (a *Server) register(w http.ResponseWriter, r *http.Request) {
	if !a.publicWrite(w, r) {
		return
	}
	mode, err := a.registrationMode(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取站点设置")
		return
	}
	if mode == "closed" {
		failure(w, 403, "注册当前已关闭")
		return
	}
	if !a.registrationLimit.allow(r) {
		w.Header().Set("Retry-After", "3600")
		failure(w, 429, "注册尝试过于频繁，请稍后重试")
		return
	}
	var body struct{ Email, Nickname, Password, Invitation, TurnstileToken string }
	if !decode(w, r, &body) {
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	body.Nickname = strings.TrimSpace(body.Nickname)
	if !validEmail(body.Email) || !utf8.ValidString(body.Nickname) || utf8.RuneCountInString(body.Nickname) > 255 {
		failure(w, 400, "邮箱或昵称格式无效")
		return
	}
	allowed, err := a.registrationEmailAllowed(r.Context(), a.s.DB, body.Email)
	if err != nil {
		failure(w, 503, "暂时无法读取注册设置")
		return
	}
	if !allowed {
		failure(w, 400, "该邮箱域名不在注册允许范围内")
		return
	}
	if !credential.ValidNewPassword(body.Password) {
		failure(w, 400, "新密码须为 8–128 个字符，且同时包含英文字母、数字和符号")
		return
	}
	if !a.challenge(w, r, body.TurnstileToken, "register") || !a.mailAllowance(w, r, body.Email) || !a.acquireMail(w, r) {
		return
	}
	defer func() { <-a.mailSlots }()
	select {
	case a.hashes <- struct{}{}:
	default:
		failure(w, 429, "密码处理繁忙，请稍后重试")
		return
	}
	hash, err := credential.Hash(body.Password)
	<-a.hashes
	if err != nil {
		failure(w, 503, "暂时无法注册")
		return
	}
	id, err := randomID()
	if err != nil {
		failure(w, 503, "暂时无法注册")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法注册")
		return
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode); err != nil {
		failure(w, 503, "暂时无法注册")
		return
	}
	if mode != "open" && mode != "invitation" {
		failure(w, 403, "注册当前已关闭")
		return
	}
	options, err := a.invitationOptions(r.Context(), tx, mode)
	if err != nil {
		failure(w, 503, "暂时无法读取插件设置")
		return
	}
	if options.Enabled && options.Required {
		var remaining int
		var expires int64
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT remaining_uses,expires_at FROM invitation_codes WHERE code_hash=? FOR UPDATE"), tokenHash(strings.TrimSpace(body.Invitation))).Scan(&remaining, &expires)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (remaining < 1 || expires != 0 && expires <= time.Now().Unix()) {
			failure(w, 400, "邀请码无效、已过期或已用完")
			return
		}
		if err != nil {
			failure(w, 503, "暂时无法注册")
			return
		}
		if _, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE invitation_codes SET remaining_uses=remaining_uses-1,used_at=? WHERE code_hash=?"), time.Now().Unix(), tokenHash(strings.TrimSpace(body.Invitation))); err != nil {
			failure(w, 503, "暂时无法注册")
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt) VALUES(?,?,?,?,?,'ARGON2ID','')"), id, body.Email, strings.ToLower(body.Email), body.Nickname, hash)
	if uniqueViolation(err) {
		failure(w, 409, "此邮箱已注册，请登录或找回密码")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(?,?,?)"), id, false, true)
	}
	if err == nil {
		err = a.issueAccountMail(r.Context(), tx, id, body.Email, "verify")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "注册未完成，邮件可能未发送，请稍后重试")
		return
	}
	respond(w, 201, map[string]string{"message": "账号已创建，验证邮件已提交发送。请验证邮箱后登录。"})
}

func (a *Server) requestAccountMail(w http.ResponseWriter, r *http.Request) {
	if !a.publicWrite(w, r) {
		return
	}
	var body struct{ Email, Purpose string }
	if !decode(w, r, &body) {
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	if !validEmail(body.Email) || body.Purpose != "verify" && body.Purpose != "reset" {
		failure(w, 400, "邮箱或邮件用途无效")
		return
	}
	if !a.mailAllowance(w, r, body.Email) || !a.acquireMail(w, r) {
		return
	}
	defer func() { <-a.mailSlots }()
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法发送邮件")
		return
	}
	defer tx.Rollback()
	var id int64
	var email string
	var banned bool
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT id,email,banned FROM users WHERE email_key=? FOR UPDATE"), strings.ToLower(body.Email)).Scan(&id, &email, &banned)
	if errors.Is(err, sql.ErrNoRows) || err == nil && banned {
		respond(w, 202, map[string]string{"message": "如有符合条件的账号，邮件已提交发送，请检查邮箱。"})
		return
	}
	if err == nil && body.Purpose == "verify" {
		var verified bool
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT email_verified FROM account_states WHERE user_id=?"), id).Scan(&verified)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		if err == nil && verified {
			respond(w, 202, map[string]string{"message": "如有符合条件的账号，邮件已提交发送，请检查邮箱。"})
			return
		}
	}
	if err == nil {
		err = a.issueAccountMail(r.Context(), tx, id, email, body.Purpose)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法发送邮件")
		return
	}
	respond(w, 202, map[string]string{"message": "如有符合条件的账号，邮件已提交发送，请检查邮箱。"})
}

func (a *Server) tokenUser(r *http.Request, token, purpose string) (int64, error) {
	if len(token) != 64 {
		return 0, errCredentials
	}
	var id int64
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT user_id FROM account_tokens WHERE token_hash=? AND purpose=? AND expires_at>?"), tokenHash(token), purpose, time.Now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = errCredentials
	}
	return id, err
}

func (a *Server) lockMailToken(r *http.Request, tx *sql.Tx, id int64, token, purpose string) error {
	var banned bool
	if err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT banned FROM users WHERE id=? FOR UPDATE"), id).Scan(&banned); err != nil {
		return err
	}
	if banned {
		return errCredentials
	}
	var count int
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM account_tokens WHERE user_id=? AND token_hash=? AND purpose=? AND expires_at>?"), id, tokenHash(token), purpose, time.Now().Unix()).Scan(&count)
	if err == nil && count != 1 {
		return errCredentials
	}
	return err
}

func accountTokenError(w http.ResponseWriter, err error) {
	if errors.Is(err, errCredentials) || errors.Is(err, sql.ErrNoRows) {
		failure(w, 400, "链接无效或已过期，请重新申请邮件")
		return
	}
	failure(w, 503, "账号操作暂不可用，请重试")
}

func (a *Server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	if !a.publicWrite(w, r) {
		return
	}
	var body struct{ Token string }
	if !decode(w, r, &body) {
		return
	}
	id, err := a.tokenUser(r, body.Token, "verify")
	if err != nil {
		accountTokenError(w, err)
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		accountTokenError(w, err)
		return
	}
	defer tx.Rollback()
	err = a.lockMailToken(r, tx, id, body.Token, "verify")
	query := "INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(?,TRUE,FALSE) ON CONFLICT(user_id) DO UPDATE SET email_verified=TRUE"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(?,TRUE,FALSE) ON DUPLICATE KEY UPDATE email_verified=TRUE"
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind(query), id)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM account_tokens WHERE user_id=? AND purpose='verify'"), id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		accountTokenError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	if !a.publicWrite(w, r) {
		return
	}
	if !a.passwordLimit.allow(r) {
		failure(w, 429, "修改密码尝试过于频繁，请稍后重试")
		return
	}
	var body struct{ Token, NewPassword string }
	if !decode(w, r, &body) {
		return
	}
	if !credential.ValidNewPassword(body.NewPassword) {
		failure(w, 400, "新密码须为 8–128 个字符，且同时包含英文字母、数字和符号")
		return
	}
	id, err := a.tokenUser(r, body.Token, "reset")
	if err != nil {
		accountTokenError(w, err)
		return
	}
	select {
	case a.hashes <- struct{}{}:
		defer func() { <-a.hashes }()
	default:
		failure(w, 429, "密码处理繁忙，请稍后重试")
		return
	}
	hash, err := credential.Hash(body.NewPassword)
	if err != nil {
		accountTokenError(w, err)
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		accountTokenError(w, err)
		return
	}
	defer tx.Rollback()
	err = a.lockMailToken(r, tx, id, body.Token, "reset")
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE users SET password_hash=?,password_method='ARGON2ID',password_salt='' WHERE id=?"), hash, id)
	}
	if err == nil {
		err = a.revokeAccountCredentials(r.Context(), tx, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		accountTokenError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *Server) revokeAccountCredentials(ctx context.Context, tx *sql.Tx, id int64) error {
	for _, table := range []string{"web_sessions", "game_tokens", "account_tokens"} {
		if _, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM "+table+" WHERE user_id=?"), id); err != nil {
			return err
		}
	}
	return nil
}
