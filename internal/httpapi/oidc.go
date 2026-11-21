package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func (a *Server) registerOIDC() {
	a.mux.HandleFunc("GET /api/admin/oidc/providers", a.adminOIDCProviders)
	a.mux.HandleFunc("PATCH /api/admin/oidc/providers", a.updateOIDCProviders)
	a.mux.HandleFunc("GET /api/admin/oidc", a.adminOIDC)
	a.mux.HandleFunc("PATCH /api/admin/oidc", a.updateOIDC)
	a.mux.HandleFunc("POST /api/auth/oidc/start", a.startOIDC)
	a.mux.HandleFunc("GET /api/auth/oidc/callback", a.callbackOIDC)
	a.mux.HandleFunc("GET /api/auth/oidc/pending", a.pendingOIDC)
	a.mux.HandleFunc("GET /api/auth/oidc/confirmation", a.oidcConfirmationInfo)
	a.mux.HandleFunc("POST /api/auth/oidc/confirmation", a.confirmOIDC)
	a.mux.HandleFunc("GET /api/account/oidc", a.accountOIDC)
	a.mux.HandleFunc("DELETE /api/account/oidc", a.unlinkOIDC)
}

func (a *Server) oidcProviderFor(ctx context.Context, cfg OIDCSettings) (*oidc.Provider, *oauth2.Config, error) {
	if !cfg.Enabled || !validOIDCIssuer(cfg.Issuer) {
		return nil, nil, errors.New("OIDC disabled")
	}
	a.oidcMu.Lock()
	defer a.oidcMu.Unlock()
	fingerprint := oidcFingerprint(cfg)
	if a.oidcProvider == nil || a.oidcConfigHash != fingerprint {
		// The provider retains this client for JWKS refreshes after discovery.
		provider, err := oidc.NewProvider(oidc.ClientContext(ctx, &http.Client{Timeout: 10 * time.Second}), cfg.Issuer)
		if err != nil {
			return nil, nil, err
		}
		// Discovery endpoints must follow the same HTTPS/loopback transport rule.
		endpoint := provider.Endpoint()
		var metadata struct {
			JWKS     string `json:"jwks_uri"`
			UserInfo string `json:"userinfo_endpoint"`
		}
		if err := provider.Claims(&metadata); err != nil {
			return nil, nil, err
		}
		if !validOIDCIssuer(endpoint.AuthURL) || !validOIDCIssuer(endpoint.TokenURL) || !validOIDCIssuer(metadata.JWKS) || !validOIDCIssuer(metadata.UserInfo) {
			return nil, nil, errors.New("unsafe OIDC endpoint")
		}
		a.oidcProvider = provider
		a.oidcConfigHash = fingerprint
	}
	return a.oidcProvider, &oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: a.oidcProvider.Endpoint(), RedirectURL: a.cfg.PublicURL + "/api/auth/oidc/callback", Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}, nil
}

func (a *Server) startOIDC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.publicWrite(w, r) {
		return
	}
	if !a.loginLimit.allow(r) {
		failure(w, 429, "登录尝试过于频繁，请稍后重试")
		return
	}
	var body struct {
		Provider string
		Link     bool
		Password string
		Next     string
	}
	if !decode(w, r, &body) {
		return
	}
	cfg, err := a.oidcSettingsFor(r.Context(), body.Provider)
	if err != nil || !cfg.Enabled {
		failure(w, 503, "OIDC 登录尚未启用")
		return
	}
	flow := oidcFlow{ProviderID: cfg.ID, Purpose: "authorize", ConfigHash: oidcFingerprint(cfg), Next: "/user"}
	if (body.Next == "/user" || strings.HasPrefix(body.Next, "/user/") || body.Next == "/admin" || strings.HasPrefix(body.Next, "/admin/")) && !strings.ContainsAny(body.Next, "\\\r\n") && len(body.Next) < 4096 {
		flow.Next = body.Next
	}
	if body.Link {
		u, token, err := a.current(r)
		if err != nil {
			sessionError(w, err)
			return
		}
		if !a.validWriteRequest(w, r, token) {
			return
		}
		authenticated, err := a.authenticate(r.Context(), u.Email, body.Password)
		if err != nil {
			a.authError(w, err)
			return
		}
		flow.UserID = u.ID
		flow.SessionHash = tokenHash(token)
		flow.CredentialHash = authenticated.credentialHash
	} else if _, _, err := a.current(r); err == nil {
		failure(w, 409, "请从账号设置绑定 OIDC")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, client, err := a.oidcProviderFor(ctx, cfg)
	if err != nil {
		failure(w, 503, "暂时无法连接 OIDC 提供商，请检查配置")
		return
	}
	state, err := secret()
	if err != nil {
		failure(w, 503, "无法开始 OIDC 登录")
		return
	}
	browser, err := secret()
	if err != nil {
		failure(w, 503, "无法开始 OIDC 登录")
		return
	}
	nonce, err := secret()
	if err != nil {
		failure(w, 503, "无法开始 OIDC 登录")
		return
	}
	flow.BrowserHash = tokenHash(browser)
	flow.Nonce = nonce
	flow.Verifier = oauth2.GenerateVerifier()
	if err = a.saveOIDCFlow(ctx, state, flow); err != nil {
		failure(w, 503, "无法开始 OIDC 登录")
		return
	}
	a.oidcCookie(w, "ws_oidc_browser", browser, 600)
	respond(w, 200, map[string]string{"url": client.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(flow.Verifier))})
}

func (a *Server) oidcFailure(w http.ResponseWriter, r *http.Request, code string) {
	a.oidcCookie(w, "ws_oidc_browser", "", -1)
	a.oidcCookie(w, "ws_oidc_confirm", "", -1)
	path := "/login"
	if _, _, err := a.current(r); err == nil {
		path = "/user/account/profile"
	}
	http.Redirect(w, r, a.cfg.PublicURL+path+"?oidcError="+code, http.StatusSeeOther)
}

func (a *Server) callbackOIDC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	cookie, err := r.Cookie("ws_oidc_browser")
	if err != nil {
		a.oidcFailure(w, r, "expired")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	flow, err := a.consumeOIDCFlow(ctx, r.URL.Query().Get("state"), cookie.Value)
	if err != nil {
		a.oidcFailure(w, r, "expired")
		return
	}
	a.oidcCookie(w, "ws_oidc_browser", "", -1)
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		a.oidcFailure(w, r, "cancelled")
		return
	}
	cfg, err := a.oidcSettingsFor(ctx, flow.ProviderID)
	if err != nil || !cfg.Enabled || oidcFingerprint(cfg) != flow.ConfigHash {
		a.oidcFailure(w, r, "expired")
		return
	}
	provider, client, err := a.oidcProviderFor(ctx, cfg)
	if err != nil {
		a.oidcFailure(w, r, "failed")
		return
	}
	token, err := client.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		a.oidcFailure(w, r, "failed")
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		a.oidcFailure(w, r, "failed")
		return
	}
	id, err := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(ctx, raw)
	if err != nil || id.Nonce != flow.Nonce || id.Subject == "" || len(id.Subject) > 255 {
		a.oidcFailure(w, r, "failed")
		return
	}
	flow.Issuer = cfg.Issuer
	flow.Subject = id.Subject
	var idClaims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
	}
	_ = id.Claims(&idClaims)
	info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil || info.Subject != id.Subject {
		a.oidcFailure(w, r, "failed")
		return
	}
	var profile struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		EmailVerified     *bool  `json:"email_verified"`
	}
	if err := info.Claims(&profile); err != nil {
		a.oidcFailure(w, r, "failed")
		return
	}
	flow.Name = profile.Name
	if flow.Name == "" {
		flow.Name = idClaims.Name
	}
	flow.Username = profile.PreferredUsername
	if flow.Username == "" {
		flow.Username = idClaims.PreferredUsername
	}
	flow.Email = info.Email
	if flow.Email == "" {
		flow.Email = idClaims.Email
	}
	flow.EmailVerified = idClaims.EmailVerified
	if profile.EmailVerified != nil {
		flow.EmailVerified = *profile.EmailVerified
	}
	if flow.UserID > 0 {
		a.beginOIDCConfirmation(w, r, flow, "confirm_link")
		return
	}
	// A login flow cannot replace a session opened in another tab.
	if _, _, err := a.current(r); err == nil {
		a.oidcFailure(w, r, "session")
		return
	}
	var userID int64
	err = a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT user_id FROM oidc_identities WHERE identity_key=?"), oidcIdentityKey(flow.Issuer, flow.Subject)).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		mode, err := a.registrationMode(ctx)
		if err != nil || mode == "closed" {
			a.oidcFailure(w, r, "registration")
			return
		}
		flow.Purpose = "signup"
		flow.Nonce = ""
		flow.Verifier = ""
		flow.BrowserHash = ""
		pending, err := secret()
		if err == nil {
			err = a.saveOIDCFlow(ctx, pending, flow)
		}
		if err != nil {
			a.oidcFailure(w, r, "failed")
			return
		}
		a.oidcCookie(w, "ws_oidc_signup", pending, 600)
		http.Redirect(w, r, a.cfg.PublicURL+"/login?mode=register&oidc=1", http.StatusSeeOther)
		return
	}
	if err != nil {
		a.oidcFailure(w, r, "failed")
		return
	}
	u, err := a.oidcUser(ctx, userID)
	if err != nil {
		a.oidcFailure(w, r, "account")
		return
	}
	flow.UserID = u.ID
	a.beginOIDCConfirmation(w, r, flow, "confirm_login")
}

func (a *Server) oidcUser(ctx context.Context, id int64) (User, error) {
	return a.readOIDCUser(ctx, a.s.DB, id)
}

func (a *Server) readOIDCUser(ctx context.Context, reader settingReader, id int64) (User, error) {
	var u User
	var permission int
	var banned, required bool
	err := reader.QueryRowContext(ctx, a.s.Bind("SELECT u.id,u.email,u.nickname,u.permission,u.banned,u.password_hash,COALESCE(s.email_verified,FALSE),s.user_id IS NULL,COALESCE(s.verification_required,FALSE) FROM users u LEFT JOIN account_states s ON s.user_id=u.id WHERE u.id=?"), id).Scan(&u.ID, &u.Email, &u.Nickname, &permission, &banned, &u.credentialHash, &u.EmailVerified, &u.LegacyAccount, &required)
	u.IsAdmin = permission >= 2
	if err == nil && (banned || required && !u.EmailVerified) {
		err = errCredentials
	}
	return u, err
}

func (a *Server) accountOIDC(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	cfg, err := a.oidcSettings(r.Context())
	if err != nil {
		failure(w, 503, "无法读取 OIDC 设置，请检查设置密钥")
		return
	}
	values, err := a.oidcConfigurations(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取绑定状态")
		return
	}
	enabled, err := a.storedPluginEnabled(r.Context(), "wishingskin.oidc")
	items := []map[string]any{}
	for _, v := range values {
		var linked int
		err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM oidc_identities WHERE user_id=? AND issuer_key=?"), u.ID, tokenHash(v.Issuer)).Scan(&linked)
		if err != nil {
			failure(w, 503, "暂时无法读取绑定状态")
			return
		}
		items = append(items, map[string]any{"id": v.ID, "name": v.Name, "enabled": v.Enabled && enabled, "linked": linked > 0})
	}
	result := map[string]any{"enabled": items[0]["enabled"], "linked": items[0]["linked"], "providers": items, "name": cfg.Name}
	respond(w, 200, result)
}

func (a *Server) unlinkOIDC(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	if !a.passwordLimit.allow(r) {
		failure(w, 429, "操作过于频繁，请稍后重试")
		return
	}
	var body struct {
		Password string
		Provider string
	}
	if !decode(w, r, &body) {
		return
	}
	u, err = a.authenticate(r.Context(), u.Email, body.Password)
	if err != nil {
		a.authError(w, err)
		return
	}
	cfg, err := a.oidcSettingsFor(r.Context(), body.Provider)
	if err != nil {
		failure(w, 503, "无法读取 OIDC 设置，请检查设置密钥")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法解除绑定")
		return
	}
	defer tx.Rollback()
	err = a.lockAuthenticatedUser(r.Context(), tx, u)
	if err == nil {
		var active int
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM web_sessions WHERE token_hash=? AND user_id=? AND expires_at>?"), tokenHash(token), u.ID, time.Now().Unix()).Scan(&active)
		if err == nil && active != 1 {
			err = errCredentials
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oidc_identities WHERE user_id=? AND issuer_key=?"), u.ID, tokenHash(cfg.Issuer))
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM web_sessions WHERE user_id=?"), u.ID)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "oidc.unlink", "account", "oidc")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法解除绑定")
		return
	}
	a.oidcCookie(w, "ws_session", "", -1)
	w.WriteHeader(204)
}
