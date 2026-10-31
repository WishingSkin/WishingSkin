package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"wishingskin/internal/store"
)

type oauthClient struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Redirects  []string `json:"redirectUris"`
	SecretHash string   `json:"-"`
	Public     bool     `json:"public"`
	Enabled    bool     `json:"enabled"`
}

func (a *Server) registerOAuth() {
	a.mux.HandleFunc("GET /api/admin/oauth/clients", a.oauthClients)
	a.mux.HandleFunc("POST /api/admin/oauth/clients", a.saveOAuthClient)
	a.mux.HandleFunc("PATCH /api/admin/oauth/clients/{id}", a.saveOAuthClient)
	a.mux.HandleFunc("GET /oauth/authorize", a.oauthAuthorize)
	a.mux.HandleFunc("GET /user/oauth/authorize", a.oauthAuthorize)
	a.mux.HandleFunc("POST /oauth/authorize", a.oauthAuthorize)
	a.mux.HandleFunc("POST /oauth/token", a.oauthToken)
	a.mux.HandleFunc("POST /oauth/revoke", a.oauthRevoke)
	a.mux.HandleFunc("GET /oauth/userinfo", a.oauthUserinfo)
	a.mux.HandleFunc("GET /api/account/oauth", a.accountOAuth)
	a.mux.HandleFunc("DELETE /api/account/oauth/{id}", a.revokeAccountOAuth)
	a.mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"issuer": a.cfg.PublicURL, "authorization_endpoint": a.cfg.PublicURL + "/oauth/authorize", "token_endpoint": a.cfg.PublicURL + "/oauth/token", "revocation_endpoint": a.cfg.PublicURL + "/oauth/revoke", "userinfo_endpoint": a.cfg.PublicURL + "/oauth/userinfo", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{"profile", "email"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic", "none"}})
	})
}

func (a *Server) oauthClient(r *http.Request, reader settingReader, id string) (oauthClient, error) {
	v := oauthClient{}
	var redirects string
	err := reader.QueryRowContext(r.Context(), a.s.Bind("SELECT client_id,name,redirect_uris,secret_hash,is_public,enabled FROM oauth_clients WHERE client_id=?"), id).Scan(&v.ID, &v.Name, &redirects, &v.SecretHash, &v.Public, &v.Enabled)
	if err == nil {
		err = json.Unmarshal([]byte(redirects), &v.Redirects)
	}
	return v, err
}
func (a *Server) oauthClients(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), "SELECT client_id,name,redirect_uris,is_public,enabled FROM oauth_clients ORDER BY client_id")
	if err != nil {
		failure(w, 503, "暂时无法读取应用")
		return
	}
	defer rows.Close()
	items := []oauthClient{}
	for rows.Next() {
		var v oauthClient
		var raw string
		if err = rows.Scan(&v.ID, &v.Name, &raw, &v.Public, &v.Enabled); err == nil {
			err = json.Unmarshal([]byte(raw), &v.Redirects)
		}
		if err != nil {
			break
		}
		items = append(items, v)
	}
	if err != nil || rows.Err() != nil {
		failure(w, 503, "暂时无法读取应用")
		return
	}
	respond(w, 200, map[string]any{"clients": items})
}
func validOAuthRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || u.Scheme == "http" && loopbackOriginHost(u.Hostname()))
}
func (a *Server) saveOAuthClient(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var v oauthClient
	if !decode(w, r, &v) {
		return
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || len(v.Name) > 128 || len(v.Redirects) < 1 || len(v.Redirects) > 10 {
		failure(w, 400, "应用名称或回调地址无效")
		return
	}
	for _, uri := range v.Redirects {
		if !validOAuthRedirect(uri) {
			failure(w, 400, "回调须为 HTTPS 完整地址，本地测试可使用 HTTP")
			return
		}
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	fresh := id == ""
	issued := ""
	var previous oauthClient
	var err error
	if fresh {
		id, err = randomUUID()
		if err == nil && !v.Public {
			issued, err = secret()
			v.SecretHash = tokenHash(issued)
		}
	} else {
		old, e := a.oauthClient(r, tx, id)
		if e != nil {
			failure(w, 404, "应用不存在")
			return
		}
		previous = old
		v.Public = old.Public
		v.SecretHash = old.SecretHash
	}
	v.ID = id
	a.auditJSON(r.Context(), "oauth", map[string]any{"name": previous.Name, "enabled": previous.Enabled, "public": previous.Public}, map[string]any{"name": v.Name, "enabled": v.Enabled, "public": v.Public})
	if fmt.Sprint(previous.Redirects) != fmt.Sprint(v.Redirects) {
		store.CaptureAdminChange(r.Context(), "oauth.redirects", "", "回调地址已更新")
	}
	raw, _ := json.Marshal(v.Redirects)
	if err == nil {
		if fresh {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_clients(client_id,name,redirect_uris,secret_hash,is_public,enabled) VALUES(?,?,?,?,?,?)"), id, v.Name, string(raw), v.SecretHash, v.Public, v.Enabled)
		} else {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE oauth_clients SET name=?,redirect_uris=?,enabled=? WHERE client_id=?"), v.Name, string(raw), v.Enabled, id)
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_codes WHERE client_id=?"), id)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE client_id=?"), id)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "oauth.client", "site", id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存应用")
		return
	}
	respond(w, 200, map[string]any{"client": v, "clientSecret": issued})
}

var pkceChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var pkceVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func oauthScope(raw string) (string, bool) {
	fields := strings.Fields(raw)
	slices.Sort(fields)
	fields = slices.Compact(fields)
	if len(fields) == 0 {
		fields = []string{"profile"}
	}
	for _, scope := range fields {
		if scope != "profile" && scope != "email" {
			return "", false
		}
	}
	return strings.Join(fields, " "), true
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html><html lang="{{if .English}}en{{else}}zh-CN{{end}}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{if .English}}Application authorization{{else}}应用授权{{end}}</title><style>body{font:16px/1.6 system-ui,sans-serif;background:#f5f5f5;color:#202020;margin:0;padding:48px 20px}main{max-width:480px;margin:auto;padding:32px;background:#fff;border:1px solid #ddd;border-radius:16px}h1{font-size:24px}p{overflow-wrap:anywhere}button{font:inherit;padding:10px 20px;border-radius:8px;border:1px solid #ccc;cursor:pointer}button[value=allow]{background:#333;color:white}small{color:#666} @media(prefers-color-scheme:dark){body{background:#111;color:#eee}main{background:#191919;border-color:#444}small{color:#bbb}}</style><main><h1>{{if .English}}Authorize {{.Name}}{{else}}授权给 {{.Name}}{{end}}</h1><p>{{.Email}}</p><p>{{if .English}}This application will read {{if .Profile}}your user ID and nickname{{end}}{{if and .Profile .EmailScope}} and {{end}}{{if .EmailScope}}your email and verification status{{end}}.{{else}}此应用将读取{{if .Profile}}你的用户 ID 和昵称{{end}}{{if and .Profile .EmailScope}}，以及{{end}}{{if .EmailScope}}你的邮箱和验证状态{{end}}。{{end}}</p><small>{{if .English}}Redirect URL: {{else}}回调地址：{{end}}{{.Redirect}}</small><form method="post" action="{{.Action}}"><input type="hidden" name="csrf" value="{{.CSRF}}"><button name="decision" value="allow">{{if .English}}Authorize{{else}}同意授权{{end}}</button> <button name="decision" value="deny">{{if .English}}Cancel{{else}}取消{{end}}</button></form></main></html>`))

func oauthEnglish(r *http.Request) bool {
	if cookie, err := r.Cookie("ws_locale"); err == nil {
		return cookie.Value == "en"
	}
	language := r.URL.Query().Get("ui_locales")
	if language == "" {
		language = r.Header.Get("Accept-Language")
	}
	return strings.HasPrefix(strings.ToLower(language), "en")
}

func (a *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	enabled, err := a.storedPluginEnabled(r.Context(), "wishingskin.oauth2")
	if err != nil || !enabled {
		failure(w, 503, "授权服务尚未启用")
		return
	}
	q := r.URL.Query()
	for _, values := range q {
		if len(values) != 1 {
			failure(w, 400, "授权请求包含重复参数")
			return
		}
	}
	client, err := a.oauthClient(r, a.s.DB, q.Get("client_id"))
	redirect := q.Get("redirect_uri")
	scope, valid := oauthScope(q.Get("scope"))
	if err != nil || !client.Enabled || !slices.Contains(client.Redirects, redirect) || q.Get("response_type") != "code" || !valid || q.Get("code_challenge_method") != "S256" || !pkceChallenge.MatchString(q.Get("code_challenge")) || len(q.Get("state")) > 1024 {
		failure(w, 400, "授权请求无效")
		return
	}
	destination, _ := url.Parse(redirect)
	if destination == nil || strings.ContainsAny(destination.Host, ";'\"\\ \t\r\n") {
		failure(w, 400, "回调地址无效")
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' "+destination.Scheme+"://"+destination.Host+"; frame-ancestors 'none'; base-uri 'none'")
	u, token, err := a.current(r)
	if err != nil {
		next := "/user/oauth/authorize?" + q.Encode()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), 303)
		return
	}
	if r.Method == "GET" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = consentPage.Execute(w, map[string]any{"English": oauthEnglish(r), "Name": client.Name, "Email": u.Email, "Profile": strings.Contains(scope, "profile"), "EmailScope": strings.Contains(scope, "email"), "Redirect": redirect, "Action": "/oauth/authorize?" + q.Encode(), "CSRF": csrf(token)})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		failure(w, 400, "请求无效")
		return
	}
	for _, values := range r.PostForm {
		if len(values) != 1 {
			failure(w, 400, "授权请求包含重复参数")
			return
		}
	}
	r.Header.Set("X-CSRF-Token", r.PostForm.Get("csrf"))
	if !a.validWriteRequest(w, r, token) {
		return
	}
	result := destination.Query()
	result.Set("state", q.Get("state"))
	if r.PostForm.Get("decision") != "allow" {
		result.Set("error", "access_denied")
		destination.RawQuery = result.Encode()
		http.Redirect(w, r, destination.String(), 303)
		return
	}
	u, err = a.oidcUser(r.Context(), u.ID)
	if err != nil {
		sessionError(w, err)
		return
	}
	tx, ok := a.beginOAuthWrite(w, r)
	if !ok {
		return
	}
	defer tx.Rollback()
	current, err := a.oauthClient(r, tx, client.ID)
	if err == nil && (!current.Enabled || !slices.Contains(current.Redirects, redirect)) {
		err = errCredentials
	}
	if err == nil {
		err = a.lockWebAccount(r.Context(), tx, u.ID, token)
	}
	if err == nil {
		err = a.lockAuthenticatedUser(r.Context(), tx, u)
	}
	var code string
	if err == nil {
		code, err = secret()
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_codes WHERE expires_at<=?"), time.Now().Unix())
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_codes(code_hash,client_id,user_id,redirect_uri,scope,challenge,credential_hash,expires_at) VALUES(?,?,?,?,?,?,?,?)"), tokenHash(code), client.ID, u.ID, redirect, scope, q.Get("code_challenge"), u.credentialHash, time.Now().Add(time.Minute).Unix())
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法授权，请重试")
		return
	}
	result.Set("code", code)
	destination.RawQuery = result.Encode()
	http.Redirect(w, r, destination.String(), 303)
}

func (a *Server) beginOAuthWrite(w http.ResponseWriter, r *http.Request) (*sql.Tx, bool) {
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "授权服务暂不可用")
		return nil, false
	}
	var mode string
	err = tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode)
	if err == nil {
		var enabled bool
		err = tx.QueryRowContext(r.Context(), "SELECT enabled FROM plugins WHERE plugin_id='wishingskin.oauth2'").Scan(&enabled)
		if err == nil && !enabled {
			err = errCredentials
		}
	}
	if err != nil {
		tx.Rollback()
		failure(w, 503, "授权服务暂不可用")
		return nil, false
	}
	return tx, true
}
func oauthError(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]string{"error": code})
}
func (a *Server) oauthForm(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if !a.loginLimit.allow(r) {
		oauthError(w, 429, "temporarily_unavailable")
		return false
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") || r.ParseForm() != nil {
		oauthError(w, 400, "invalid_request")
		return false
	}
	for _, values := range r.PostForm {
		if len(values) != 1 {
			oauthError(w, 400, "invalid_request")
			return false
		}
	}
	return true
}
func (a *Server) oauthAuthenticateClient(w http.ResponseWriter, r *http.Request, reader settingReader) (oauthClient, bool) {
	id, password, basic := r.BasicAuth()
	if basic {
		var err error
		id, err = url.QueryUnescape(id)
		if err != nil {
			oauthError(w, 401, "invalid_client")
			return oauthClient{}, false
		}
		password, err = url.QueryUnescape(password)
		if err != nil {
			oauthError(w, 401, "invalid_client")
			return oauthClient{}, false
		}
	} else {
		id = r.PostForm.Get("client_id")
	}
	v, err := a.oauthClient(r, reader, id)
	if err != nil || !v.Enabled || !v.Public && (!basic || subtle.ConstantTimeCompare([]byte(tokenHash(password)), []byte(v.SecretHash)) != 1) || v.Public && basic {
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth"`)
		oauthError(w, 401, "invalid_client")
		return v, false
	}
	return v, true
}
func (a *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	if !a.oauthForm(w, r) {
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		oauthError(w, 400, "unsupported_grant_type")
		return
	}
	tx, ok := a.beginOAuthWrite(w, r)
	if !ok {
		return
	}
	defer tx.Rollback()
	client, ok := a.oauthAuthenticateClient(w, r, tx)
	if !ok {
		return
	}
	var userID, expires int64
	var redirect, scope, challenge, credential string
	code := r.PostForm.Get("code")
	verifier := r.PostForm.Get("code_verifier")
	hash := sha256.Sum256([]byte(verifier))
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT user_id,redirect_uri,scope,challenge,credential_hash,expires_at FROM oauth_codes WHERE code_hash=? AND client_id=? FOR UPDATE"), tokenHash(code), client.ID).Scan(&userID, &redirect, &scope, &challenge, &credential, &expires)
	if err != nil || len(code) != 64 || expires <= time.Now().Unix() || redirect != r.PostForm.Get("redirect_uri") || !slices.Contains(client.Redirects, redirect) || !pkceVerifier.MatchString(verifier) || subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(hash[:])), []byte(challenge)) != 1 {
		oauthError(w, 400, "invalid_grant")
		return
	}
	u, err := a.oidcUser(r.Context(), userID)
	if err == nil {
		err = a.lockAuthenticatedUser(r.Context(), tx, u)
	}
	if err != nil || u.credentialHash != credential {
		oauthError(w, 400, "invalid_grant")
		return
	}
	access, err := secret()
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_codes WHERE code_hash=?"), tokenHash(code))
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE expires_at<=? OR (client_id=? AND user_id=?)"), time.Now().Unix(), client.ID, u.ID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_tokens(token_hash,client_id,user_id,scope,credential_hash,expires_at) VALUES(?,?,?,?,?,?)"), tokenHash(access), client.ID, userID, scope, credential, time.Now().Add(time.Hour).Unix())
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	respond(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "scope": scope})
}
func (a *Server) oauthUserinfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var id int64
	var scope, credential string
	enabled, err := a.storedPluginEnabled(r.Context(), "wishingskin.oauth2")
	if err == nil && enabled && len(bearer) == 64 {
		err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT t.user_id,t.scope,t.credential_hash FROM oauth_tokens t JOIN oauth_clients c ON c.client_id=t.client_id WHERE t.token_hash=? AND t.expires_at>? AND c.enabled=TRUE"), tokenHash(bearer), time.Now().Unix()).Scan(&id, &scope, &credential)
	} else {
		err = errCredentials
	}
	var u User
	if err == nil {
		u, err = a.oidcUser(r.Context(), id)
	}
	if err != nil || u.credentialHash != credential {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		oauthError(w, 401, "invalid_token")
		return
	}
	result := map[string]any{"sub": strconv.FormatInt(id, 10)}
	if strings.Contains(scope, "profile") {
		result["name"] = u.Nickname
	}
	if strings.Contains(scope, "email") {
		result["email"] = u.Email
		result["email_verified"] = u.EmailVerified
	}
	respond(w, 200, result)
}
func (a *Server) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if !a.oauthForm(w, r) {
		return
	}
	client, ok := a.oauthAuthenticateClient(w, r, a.s.DB)
	if !ok {
		return
	}
	_, err := a.s.DB.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE token_hash=? AND client_id=?"), tokenHash(r.PostForm.Get("token")), client.ID)
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	w.WriteHeader(200)
}
func (a *Server) accountOAuth(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT c.client_id,c.name,t.scope,t.expires_at FROM oauth_tokens t JOIN oauth_clients c ON c.client_id=t.client_id WHERE t.user_id=? AND t.expires_at>? ORDER BY c.name"), u.ID, time.Now().Unix())
	if err != nil {
		failure(w, 503, "暂时无法读取授权")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, scope string
		var expires int64
		if err = rows.Scan(&id, &name, &scope, &expires); err != nil {
			break
		}
		items = append(items, map[string]any{"id": id, "name": name, "scope": scope, "expiresAt": expires})
	}
	if err != nil || rows.Err() != nil {
		failure(w, 503, "暂时无法读取授权")
		return
	}
	respond(w, 200, map[string]any{"grants": items})
}
func (a *Server) revokeAccountOAuth(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	_, err = a.s.DB.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE client_id=? AND user_id=?"), r.PathValue("id"), u.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		failure(w, 503, "暂时无法撤销授权")
		return
	}
	w.WriteHeader(204)
}
