package httpapi

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/securebox"
	"github.com/WishingSkin/WishingSkin/internal/store"
	"golang.org/x/oauth2"
)

const (
	minecraftPlugin        = "wishingskin.minecraft"
	minecraftAAD           = "wishingskin:minecraft_verify:v1"
	minecraftFlowAAD       = "wishingskin:minecraft_flow:v1"
	minecraftPath          = "/api/account/minecraft"
	minecraftProfile       = "/user/account/profile"
	minecraftBrowserCookie = "ws_mc_browser"
	minecraftConfirmCookie = "ws_mc_confirm"
	minecraftFlowLifetime  = 10 * time.Minute
)

type minecraftConfig struct {
	Enabled                bool
	ClientID, ClientSecret string
	Revision               string
}

func (cfg minecraftConfig) valid() bool {
	return strings.TrimSpace(cfg.ClientID) != "" && len(cfg.ClientID) <= 128 && strings.TrimSpace(cfg.ClientSecret) != "" && len(cfg.ClientSecret) <= 1024
}

func (cfg minecraftConfig) fingerprint() string {
	return tokenHash(cfg.Revision + "\x00" + cfg.ClientID + "\x00" + cfg.ClientSecret)
}

type minecraftFlow struct {
	UserID                                                                  int64
	Purpose, SessionHash, CredentialHash, ConfigHash, BrowserHash, Verifier string
	UUID, Name, Subject                                                     string
}

func (a *Server) registerMinecraft() {
	a.minecraftClient = newSafeExtensionClient(10 * time.Second)
	a.minecraftEndpoints = minecraftEndpoints{
		"https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize",
		"https://login.microsoftonline.com/consumers/oauth2/v2.0/token",
		"https://user.auth.xboxlive.com/user/authenticate",
		"https://xsts.auth.xboxlive.com/xsts/authorize",
		"https://api.minecraftservices.com/authentication/login_with_xbox",
		"https://api.minecraftservices.com/entitlements/mcstore",
		"https://api.minecraftservices.com/minecraft/profile",
	}
	a.mux.HandleFunc("GET /api/admin/minecraft", a.adminMinecraft)
	a.mux.HandleFunc("PATCH /api/admin/minecraft", a.updateMinecraft)
	a.mux.HandleFunc("GET "+minecraftPath, a.accountMinecraft)
	a.mux.HandleFunc("POST "+minecraftPath+"/start", a.startMinecraft)
	a.mux.HandleFunc("GET "+minecraftPath+"/callback", a.callbackMinecraft)
	a.mux.HandleFunc("GET "+minecraftPath+"/confirmation", a.minecraftConfirmation)
	a.mux.HandleFunc("POST "+minecraftPath+"/confirmation", a.confirmMinecraft)
	a.mux.HandleFunc("DELETE "+minecraftPath, a.unlinkMinecraft)
}

func (a *Server) minecraftSettings(ctx context.Context, reader settingReader) (minecraftConfig, error) {
	var cfg minecraftConfig
	var raw string
	err := reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='minecraft_verify_config'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	plain, err := securebox.Open(a.cfg.SettingsKey, raw, minecraftAAD)
	if err == nil {
		err = json.Unmarshal([]byte(plain), &cfg)
	}
	return cfg, err
}

func (a *Server) minecraftEnabled(ctx context.Context, reader settingReader) (minecraftConfig, bool, error) {
	cfg, err := a.minecraftSettings(ctx, reader)
	if err != nil {
		return cfg, false, err
	}
	enabled, err := a.readPluginEnabled(ctx, reader, minecraftPlugin)
	return cfg, enabled && cfg.Enabled, err
}

func (a *Server) putMinecraftSettings(ctx context.Context, tx *sql.Tx, cfg minecraftConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	sealed, err := securebox.Seal(a.cfg.SettingsKey, string(data), minecraftAAD)
	if err != nil {
		return err
	}
	query := "INSERT INTO settings(setting_key,value) VALUES('minecraft_verify_config',?) ON CONFLICT(setting_key) DO UPDATE SET value=EXCLUDED.value"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO settings(setting_key,value) VALUES('minecraft_verify_config',?) ON DUPLICATE KEY UPDATE value=VALUES(value)"
	}
	_, err = tx.ExecContext(ctx, a.s.Bind(query), sealed)
	return err
}

func (a *Server) minecraftView(cfg minecraftConfig, enabled bool) map[string]any {
	return map[string]any{"enabled": enabled, "clientId": cfg.ClientID, "secretConfigured": cfg.ClientSecret != "", "redirectUri": a.cfg.PublicURL + minecraftPath + "/callback"}
}

func (a *Server) adminMinecraft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	cfg, enabled, err := a.minecraftEnabled(r.Context(), a.s.DB)
	if err != nil {
		failure(w, 503, "暂时无法读取正版验证设置")
		return
	}
	respond(w, 200, a.minecraftView(cfg, enabled))
}

func (a *Server) updateMinecraft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled      *bool
		ClientID     *string `json:"clientId"`
		ClientSecret string
		ClearSecret  bool
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.ClientSecret) > 1024 || body.ClientID != nil && len(*body.ClientID) > 128 || body.ClearSecret && strings.TrimSpace(body.ClientSecret) != "" {
		failure(w, 400, "正版验证设置无效")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	ctx := r.Context()
	before, enabled, err := a.minecraftEnabled(ctx, tx)
	if err != nil {
		failure(w, 503, "暂时无法读取正版验证设置")
		return
	}
	cfg := before
	cfg.Enabled = enabled
	if body.Enabled != nil {
		cfg.Enabled = *body.Enabled
	}
	if body.ClientID != nil {
		cfg.ClientID = strings.TrimSpace(*body.ClientID)
	}
	if body.ClearSecret {
		cfg.ClientSecret = ""
	} else if value := strings.TrimSpace(body.ClientSecret); value != "" {
		cfg.ClientSecret = value
	}
	if cfg.Enabled && !cfg.valid() {
		failure(w, 409, "请先保存有效的微软应用配置")
		return
	}
	changes := []store.AdminChange{}
	if enabled != cfg.Enabled {
		changes = append(changes, store.AdminChange{Field: "enabled", Before: strconv.FormatBool(enabled), After: strconv.FormatBool(cfg.Enabled)})
	}
	if before.ClientID != cfg.ClientID {
		changes = append(changes, store.AdminChange{Field: "clientId", Before: before.ClientID, After: cfg.ClientID})
	}
	if before.ClientSecret != cfg.ClientSecret {
		changes = append(changes, store.AdminChange{Field: "clientSecret", After: secretChangeLabel(cfg.ClientSecret)})
	}
	cfg.Revision, err = secret()
	if err == nil {
		err = a.putMinecraftSettings(ctx, tx, cfg)
	}
	if err == nil {
		err = a.putPluginEnabled(ctx, tx, minecraftPlugin, "1.0.0", cfg.Enabled)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM minecraft_flows")
	}
	if err == nil {
		err = a.s.RecordAdminEvent(ctx, tx, user.ID, "settings.minecraft", "site", minecraftPlugin, changes...)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存正版验证设置")
		return
	}
	respond(w, 200, a.minecraftView(cfg, cfg.Enabled))
}

func secretChangeLabel(secret string) string {
	if secret == "" {
		return "已清除"
	}
	return "已更新"
}

func minecraftUUID(uuid string) string {
	if len(uuid) != 32 {
		return ""
	}
	return uuid[:8] + "-" + uuid[8:12] + "-" + uuid[12:16] + "-" + uuid[16:20] + "-" + uuid[20:]
}

func isHexID(value string, size int) bool {
	if len(value) != size {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (a *Server) accountMinecraft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	_, enabled, err := a.minecraftEnabled(r.Context(), a.s.DB)
	var name, uuid string
	var verifiedAt int64
	linked := false
	if err == nil {
		err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT mc_uuid,mc_name,verified_at FROM minecraft_verifications WHERE user_id=?"), user.ID).Scan(&uuid, &name, &verifiedAt)
		linked = err == nil
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
	}
	if err != nil {
		failure(w, 503, "暂时无法读取正版验证状态")
		return
	}
	respond(w, 200, map[string]any{"enabled": enabled, "linked": linked, "name": name, "uuid": minecraftUUID(uuid), "verifiedAt": verifiedAt})
}

func (a *Server) minecraftAccountWrite(w http.ResponseWriter, r *http.Request, rate *limiter) (User, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	user, session, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return User{}, "", false
	}
	if !a.validWriteRequest(w, r, session) {
		return User{}, "", false
	}
	if !rate.allow(r) || !rate.allowKey("minecraft:"+strconv.FormatInt(user.ID, 10)) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "操作过于频繁，请稍后重试")
		return User{}, "", false
	}
	return user, session, true
}

func (a *Server) minecraftPassword(w http.ResponseWriter, r *http.Request, user User) (User, bool) {
	var body struct{ Password string }
	if !decode(w, r, &body) {
		return User{}, false
	}
	authenticated, err := a.authenticate(r.Context(), user.Email, body.Password)
	if err == nil && authenticated.ID != user.ID {
		err = errCredentials
	}
	if err != nil {
		a.authError(w, err)
		return User{}, false
	}
	return authenticated, true
}

func (a *Server) startMinecraft(w http.ResponseWriter, r *http.Request) {
	user, session, ok := a.minecraftAccountWrite(w, r, &a.oidcLimit)
	if !ok {
		return
	}
	user, ok = a.minecraftPassword(w, r, user)
	if !ok {
		return
	}
	ctx := r.Context()
	cfg, enabled, err := a.minecraftEnabled(ctx, a.s.DB)
	if err != nil {
		failure(w, 503, "暂时无法读取正版验证设置")
		return
	}
	if !enabled || !cfg.valid() {
		failure(w, 409, "正版验证尚未启用")
		return
	}
	state, stateErr := secret()
	browser, browserErr := secret()
	if stateErr != nil || browserErr != nil {
		failure(w, 503, "暂时无法开始正版验证")
		return
	}
	flow := minecraftFlow{UserID: user.ID, Purpose: "authorize", SessionHash: tokenHash(session), CredentialHash: tokenHash(user.credentialHash), ConfigHash: cfg.fingerprint(), BrowserHash: tokenHash(browser), Verifier: oauth2.GenerateVerifier()}
	if err := a.saveMinecraftFlow(ctx, state, flow, session); err != nil {
		minecraftWriteError(w, err)
		return
	}
	client := a.minecraftOAuth(cfg)
	a.oidcCookie(w, minecraftBrowserCookie, browser, int(minecraftFlowLifetime.Seconds()))
	respond(w, 200, map[string]string{"url": client.AuthCodeURL(state, oauth2.S256ChallengeOption(flow.Verifier))})
}

var errMinecraftLinked = errors.New("Minecraft account already linked")

func minecraftWriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errMinecraftLinked), uniqueViolation(err):
		failure(w, 409, "Minecraft 账号已绑定")
	case errors.Is(err, errCredentials), errors.Is(err, sql.ErrNoRows):
		failure(w, 409, "正版验证流程已失效")
	default:
		failure(w, 503, "暂时无法处理正版验证")
	}
}

func (a *Server) callbackMinecraft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	redirect := func(reason string) {
		http.Redirect(w, r, minecraftProfile+"?minecraftError="+reason, 303)
	}
	query := r.URL.Query()
	browser, err := r.Cookie(minecraftBrowserCookie)
	if err != nil {
		redirect("expired")
		return
	}
	a.oidcCookie(w, minecraftBrowserCookie, "", -1)
	ctx := r.Context()
	flow, err := a.consumeMinecraftFlow(ctx, query.Get("state"), browser.Value)
	if err != nil {
		redirect("expired")
		return
	}
	user, session, err := a.current(r)
	if err != nil || a.matchMinecraftSession(ctx, a.s.DB, user.ID, session, flow) != nil {
		redirect("session")
		return
	}
	code := query.Get("code")
	if query.Get("error") != "" || code == "" || len(code) > 8192 {
		redirect("denied")
		return
	}
	cfg, enabled, err := a.minecraftEnabled(ctx, a.s.DB)
	if err != nil || !enabled || flow.ConfigHash != cfg.fingerprint() {
		redirect("expired")
		return
	}
	flow.UUID, flow.Name, flow.Subject, code = a.exchangeMinecraft(ctx, cfg, code, flow.Verifier)
	if code != "" {
		redirect(code)
		return
	}
	confirm, err := secret()
	if err != nil {
		redirect("denied")
		return
	}
	flow.Purpose, flow.Verifier, flow.BrowserHash = "confirm", "", ""
	if err := a.saveMinecraftFlow(ctx, confirm, flow, session); err != nil {
		if errors.Is(err, errMinecraftLinked) {
			redirect("already_linked")
		} else {
			redirect("expired")
		}
		return
	}
	a.oidcCookie(w, minecraftConfirmCookie, confirm, int(minecraftFlowLifetime.Seconds()))
	http.Redirect(w, r, minecraftProfile+"?minecraftConfirm=1", 303)
}

func (a *Server) minecraftConfirmation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, session, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	ctx := r.Context()
	cookie, err := r.Cookie(minecraftConfirmCookie)
	if err != nil {
		minecraftWriteError(w, errCredentials)
		return
	}
	flow, err := a.readMinecraftFlow(ctx, a.s.DB, cookie.Value, "confirm", false)
	if err == nil {
		err = a.matchMinecraftSession(ctx, a.s.DB, user.ID, session, flow)
	}
	if err == nil {
		err = a.checkMinecraftConfig(ctx, a.s.DB, flow.ConfigHash)
	}
	if err == nil {
		err = a.checkMinecraftAvailable(ctx, a.s.DB, user.ID, flow.UUID)
	}
	if err != nil {
		minecraftWriteError(w, err)
		return
	}
	respond(w, 200, map[string]string{"name": flow.Name, "uuid": minecraftUUID(flow.UUID), "accountName": user.Nickname, "accountEmail": user.Email})
}

func (a *Server) confirmMinecraft(w http.ResponseWriter, r *http.Request) {
	user, session, ok := a.minecraftAccountWrite(w, r, &a.oidcLimit)
	if !ok {
		return
	}
	var body struct{ Action string }
	if !decode(w, r, &body) {
		return
	}
	if body.Action != "confirm" && body.Action != "cancel" {
		failure(w, 400, "确认操作无效")
		return
	}
	cookie, err := r.Cookie(minecraftConfirmCookie)
	if err != nil {
		if body.Action == "cancel" {
			respond(w, 200, map[string]string{"next": minecraftProfile})
		} else {
			minecraftWriteError(w, errCredentials)
		}
		return
	}
	ctx := r.Context()
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		minecraftWriteError(w, err)
		return
	}
	defer tx.Rollback()
	_, err = lockRegistrationMode(ctx, tx)
	if err == nil {
		err = a.lockWebAccount(ctx, tx, user.ID, session)
	}
	var flow minecraftFlow
	if err == nil {
		flow, err = a.readMinecraftFlow(ctx, tx, cookie.Value, "confirm", true)
	}
	if err == nil {
		err = a.matchMinecraftSession(ctx, tx, user.ID, session, flow)
	}
	if err == nil && body.Action == "confirm" {
		err = a.checkMinecraftConfig(ctx, tx, flow.ConfigHash)
		if err == nil {
			err = tx.QueryRowContext(ctx, a.s.Bind("SELECT password_hash FROM users WHERE id=?"), user.ID).Scan(&user.credentialHash)
		}
		if err == nil {
			err = a.lockAuthenticatedUser(ctx, tx, user)
		}
	}
	if err == nil {
		err = a.deleteMinecraftFlow(ctx, tx, cookie.Value)
	}
	next := minecraftProfile
	if err == nil && body.Action == "confirm" {
		err = a.checkMinecraftAvailable(ctx, tx, user.ID, flow.UUID)
		if err == nil {
			_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO minecraft_verifications(user_id,mc_uuid,mc_name,ms_subject,verified_at) VALUES(?,?,?,?,?)"), user.ID, flow.UUID, flow.Name, flow.Subject, time.Now().Unix())
		}
		if err == nil {
			err = a.s.RecordAdminEvent(ctx, tx, user.ID, "minecraft.link", "account", strconv.FormatInt(user.ID, 10), store.AdminChange{Field: "mcUUID", After: flow.UUID})
		}
		next += "?minecraftLinked=1"
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if body.Action != "cancel" || !errors.Is(err, sql.ErrNoRows) {
			minecraftWriteError(w, err)
			return
		}
	}
	a.oidcCookie(w, minecraftConfirmCookie, "", -1)
	if body.Action == "confirm" {
		a.publishPluginEvent(r.Context(), "minecraft.link", user.ID, flow.UUID)
	}
	respond(w, 200, map[string]string{"next": next})
}

func (a *Server) unlinkMinecraft(w http.ResponseWriter, r *http.Request) {
	user, session, ok := a.minecraftAccountWrite(w, r, &a.passwordLimit)
	if !ok {
		return
	}
	user, ok = a.minecraftPassword(w, r, user)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		minecraftWriteError(w, err)
		return
	}
	defer tx.Rollback()
	var linkedUUID string
	err = a.lockWebAccount(ctx, tx, user.ID, session)
	if err == nil {
		err = a.lockAuthenticatedUser(ctx, tx, user)
	}
	if err == nil {
		_ = tx.QueryRowContext(ctx, a.s.Bind("SELECT mc_uuid FROM minecraft_verifications WHERE user_id=?"), user.ID).Scan(&linkedUUID)
		_, err = tx.ExecContext(ctx, a.s.Bind("DELETE FROM minecraft_verifications WHERE user_id=?"), user.ID)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(ctx, tx, user.ID, "minecraft.unlink", "account", strconv.FormatInt(user.ID, 10))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		minecraftWriteError(w, err)
		return
	}
	resourceID := linkedUUID
	if resourceID == "" {
		resourceID = strconv.FormatInt(user.ID, 10)
	}
	a.publishPluginEvent(r.Context(), "minecraft.unlink", user.ID, resourceID)
	w.WriteHeader(204)
}

func (a *Server) checkMinecraftConfig(ctx context.Context, reader settingReader, fingerprint string) error {
	cfg, enabled, err := a.minecraftEnabled(ctx, reader)
	if err == nil && (!enabled || !cfg.valid() || cfg.fingerprint() != fingerprint) {
		return errCredentials
	}
	return err
}

func (a *Server) checkMinecraftAvailable(ctx context.Context, reader settingReader, userID int64, uuid string) error {
	var linked int
	err := reader.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM minecraft_verifications WHERE user_id=? OR mc_uuid=?"), userID, uuid).Scan(&linked)
	if err == nil && linked != 0 {
		return errMinecraftLinked
	}
	return err
}

func (a *Server) matchMinecraftSession(ctx context.Context, reader settingReader, userID int64, session string, flow minecraftFlow) error {
	if flow.UserID != userID || flow.SessionHash != tokenHash(session) {
		return errCredentials
	}
	var credentialHash string
	err := reader.QueryRowContext(ctx, a.s.Bind("SELECT password_hash FROM users WHERE id=?"), userID).Scan(&credentialHash)
	if err == nil && tokenHash(credentialHash) != flow.CredentialHash {
		return errCredentials
	}
	return err
}

func (a *Server) saveMinecraftFlow(ctx context.Context, token string, flow minecraftFlow, session string) error {
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = lockRegistrationMode(ctx, tx); err != nil {
		return err
	}
	if err := a.checkMinecraftConfig(ctx, tx, flow.ConfigHash); err != nil {
		return err
	}
	if err := a.lockWebAccount(ctx, tx, flow.UserID, session); err != nil {
		return err
	}
	if err := a.matchMinecraftSession(ctx, tx, flow.UserID, session, flow); err != nil {
		return err
	}
	if err := a.checkMinecraftAvailable(ctx, tx, flow.UserID, flow.UUID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM minecraft_flows WHERE expires_at<=?"), time.Now().Unix()); err != nil {
		return err
	}
	data, err := json.Marshal(flow)
	if err != nil {
		return err
	}
	sealed, err := securebox.Seal(a.cfg.SettingsKey, string(data), minecraftFlowAAD)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, a.s.Bind("INSERT INTO minecraft_flows(flow_hash,value,expires_at) VALUES(?,?,?)"), tokenHash(token), sealed, time.Now().Add(minecraftFlowLifetime).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *Server) readMinecraftFlow(ctx context.Context, reader settingReader, token, purpose string, lock bool) (minecraftFlow, error) {
	var flow minecraftFlow
	if !isHexID(token, 64) {
		return flow, errCredentials
	}
	query := "SELECT value FROM minecraft_flows WHERE flow_hash=? AND expires_at>?"
	if lock {
		query += " FOR UPDATE"
	}
	var sealed string
	err := reader.QueryRowContext(ctx, a.s.Bind(query), tokenHash(token), time.Now().Unix()).Scan(&sealed)
	if err != nil {
		return flow, err
	}
	plain, err := securebox.Open(a.cfg.SettingsKey, sealed, minecraftFlowAAD)
	if err == nil {
		err = json.Unmarshal([]byte(plain), &flow)
	}
	if err == nil && flow.Purpose != purpose {
		err = errCredentials
	}
	return flow, err
}

func (a *Server) deleteMinecraftFlow(ctx context.Context, tx *sql.Tx, token string) error {
	_, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM minecraft_flows WHERE flow_hash=?"), tokenHash(token))
	return err
}

func (a *Server) consumeMinecraftFlow(ctx context.Context, state, browser string) (minecraftFlow, error) {
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return minecraftFlow{}, err
	}
	defer tx.Rollback()
	flow, err := a.readMinecraftFlow(ctx, tx, state, "authorize", true)
	if err != nil || !isHexID(browser, 64) || flow.BrowserHash != tokenHash(browser) {
		return minecraftFlow{}, errCredentials
	}
	if err := a.deleteMinecraftFlow(ctx, tx, state); err != nil {
		return minecraftFlow{}, err
	}
	return flow, tx.Commit()
}
