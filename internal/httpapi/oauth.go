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

	"github.com/WishingSkin/WishingSkin/internal/store"
)

var oauthCanonicalScopes = []string{"openid", "profile", "email", "offline_access", "characters:read", "textures:read", "points:read", "minecraft:read"}

type oauthClient struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Redirects     []string `json:"redirectUris"`
	SecretHash    string   `json:"-"`
	Public        bool     `json:"public"`
	Enabled       bool     `json:"enabled"`
	AllowedScopes []string `json:"allowedScopes"`
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
	a.registerOAuthProvider()
	a.mux.HandleFunc("GET /oauth/api/characters", a.oauthCharacters)
	a.mux.HandleFunc("GET /oauth/api/textures", a.oauthTextures)
	a.mux.HandleFunc("GET /oauth/api/points", a.oauthPoints)
	a.mux.HandleFunc("GET /oauth/api/minecraft", a.oauthMinecraft)
}

func (a *Server) oauthClient(r *http.Request, reader settingReader, id string) (oauthClient, error) {
	v := oauthClient{}
	var redirects, scopes string
	err := reader.QueryRowContext(r.Context(), a.s.Bind("SELECT client_id,name,redirect_uris,secret_hash,is_public,enabled,allowed_scopes FROM oauth_clients WHERE client_id=?"), id).Scan(&v.ID, &v.Name, &redirects, &v.SecretHash, &v.Public, &v.Enabled, &scopes)
	if err == nil {
		err = json.Unmarshal([]byte(redirects), &v.Redirects)
		v.AllowedScopes = oauthAllowedScopes(scopes)
	}
	return v, err
}
func (a *Server) oauthClients(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), "SELECT client_id,name,redirect_uris,is_public,enabled,allowed_scopes FROM oauth_clients ORDER BY client_id")
	if err != nil {
		failure(w, 503, "暂时无法读取应用")
		return
	}
	defer rows.Close()
	items := []oauthClient{}
	for rows.Next() {
		var v oauthClient
		var raw, scopes string
		if err = rows.Scan(&v.ID, &v.Name, &raw, &v.Public, &v.Enabled, &scopes); err == nil {
			err = json.Unmarshal([]byte(raw), &v.Redirects)
			v.AllowedScopes = oauthAllowedScopes(scopes)
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
	respond(w, 200, map[string]any{"clients": items, "availableScopes": oauthCanonicalScopes})
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
	if v.AllowedScopes != nil {
		raw := strings.Join(v.AllowedScopes, " ")
		normalized, valid := oauthScope(raw)
		if !valid || len(strings.Fields(raw)) == 0 {
			failure(w, 400, "请选择至少一项有效的授权权限")
			return
		}
		v.AllowedScopes = strings.Fields(normalized)
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
	if v.AllowedScopes == nil {
		v.AllowedScopes = previous.AllowedScopes
		if fresh {
			v.AllowedScopes = oauthAllowedScopes("")
		}
	}
	v.ID = id
	a.auditJSON(r.Context(), "oauth", map[string]any{"name": previous.Name, "enabled": previous.Enabled, "public": previous.Public}, map[string]any{"name": v.Name, "enabled": v.Enabled, "public": v.Public})
	a.auditJSON(r.Context(), "oauth.allowedScopes", strings.Join(previous.AllowedScopes, " "), strings.Join(v.AllowedScopes, " "))
	if fmt.Sprint(previous.Redirects) != fmt.Sprint(v.Redirects) {
		store.CaptureAdminChange(r.Context(), "oauth.redirects", "", "回调地址已更新")
	}
	raw, _ := json.Marshal(v.Redirects)
	if err == nil {
		if fresh {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_clients(client_id,name,redirect_uris,secret_hash,is_public,enabled,allowed_scopes) VALUES(?,?,?,?,?,?,?)"), id, v.Name, string(raw), v.SecretHash, v.Public, v.Enabled, strings.Join(v.AllowedScopes, " "))
		} else {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE oauth_clients SET name=?,redirect_uris=?,enabled=?,allowed_scopes=? WHERE client_id=?"), v.Name, string(raw), v.Enabled, strings.Join(v.AllowedScopes, " "), id)
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_codes WHERE client_id=?"), id)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE client_id=?"), id)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_refresh_tokens WHERE client_id=?"), id)
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
		if !slices.Contains(oauthCanonicalScopes, scope) {
			return "", false
		}
	}
	return strings.Join(fields, " "), true
}

func oauthAllowedScopes(raw string) []string {
	if raw == "" {
		return slices.Clone(oauthCanonicalScopes[:4])
	}
	return strings.Fields(raw)
}

func hasOAuthScope(scope, name string) bool {
	return slices.Contains(strings.Fields(scope), name)
}

func oauthScopesAllowed(scope string, allowed []string) bool {
	for _, name := range strings.Fields(scope) {
		if !slices.Contains(allowed, name) {
			return false
		}
	}
	return true
}

type oauthScopeItem struct{ Title, Detail string }

func oauthConsentPermissions(scope string, english bool) ([]oauthScopeItem, []oauthScopeItem) {
	item := func(title, detail, englishTitle, englishDetail string) oauthScopeItem {
		if english {
			return oauthScopeItem{englishTitle, englishDetail}
		}
		return oauthScopeItem{title, detail}
	}
	permissions := []oauthScopeItem{item("读取你的用户 ID", "用于识别你的本站账号。", "Read your user ID", "Identifies your account on this site.")}
	var readOnly []oauthScopeItem
	for _, name := range oauthCanonicalScopes {
		if !hasOAuthScope(scope, name) {
			continue
		}
		switch name {
		case "profile":
			permissions = append(permissions, item("读取昵称", "查看你的公开昵称。", "Read your nickname", "View your public nickname."))
		case "email":
			permissions = append(permissions, item("读取邮箱", "查看邮箱地址和验证状态。", "Read your email", "View your email address and verification status."))
		case "offline_access":
			permissions = append(permissions, item("续期授权", "允许应用在 30 天内续期授权。", "Renew access", "Allow this application to renew access for up to 30 days."))
		case "characters:read":
			readOnly = append(readOnly, item("角色", "查看你的角色和绑定的材质。", "Characters", "View your characters and their textures."))
		case "textures:read":
			readOnly = append(readOnly, item("材质", "查看你上传的材质记录。", "Textures", "View your uploaded texture records."))
		case "points:read":
			readOnly = append(readOnly, item("积分", "查看你的积分余额和明细。", "Points", "View your points balance and history."))
		case "minecraft:read":
			readOnly = append(readOnly, item("Minecraft 验证", "查看你验证的 Minecraft 账号。", "Minecraft verification", "View your verified Minecraft account."))
		}
	}
	return permissions, readOnly
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="{{if .English}}en{{else}}zh-CN{{end}}"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark">
<title>{{if .English}}Application authorization{{else}}应用授权{{end}}</title>
<style>
body{font:16px/1.5 system-ui,sans-serif;background:Canvas;color:CanvasText;margin:0;padding:40px 16px}
main{max-width:480px;margin:auto;padding:24px;border:1px solid GrayText;border-radius:8px;overflow-wrap:anywhere}
h1{font-size:24px;margin-top:0} h2{font-size:16px} ul{padding-left:24px} li+li{margin-top:8px}
small{display:block;font-size:14px;color:GrayText;overflow-wrap:anywhere}
form{display:flex;justify-content:flex-end;gap:12px;margin-top:24px}
button{font:inherit;padding:8px 16px;border-radius:6px;border:1px solid GrayText;background:transparent;color:CanvasText;cursor:pointer}
button[value=allow]{background:Highlight;color:HighlightText;border-color:Highlight}
</style>
<main><h1>{{if .English}}Authorize {{.Name}}{{else}}授权给 {{.Name}}{{end}}</h1><p>{{.Email}}</p>
<ul>{{range .Permissions}}<li><strong>{{.Title}}</strong><small>{{.Detail}}</small></li>{{end}}</ul>
{{if .ReadOnlyPermissions}}<h2>{{if .English}}Read-only data{{else}}只读数据{{end}}</h2><ul>{{range .ReadOnlyPermissions}}<li><strong>{{.Title}}</strong><small>{{.Detail}}</small></li>{{end}}</ul>{{end}}
<p>{{if .English}}Returns to{{else}}返回至{{end}} <strong>{{.Host}}</strong><small>{{.Redirect}}</small></p>
<form method="post" action="{{.Action}}"><input type="hidden" name="csrf" value="{{.CSRF}}">
<button name="decision" value="deny">{{if .English}}Cancel{{else}}取消{{end}}</button>
<button name="decision" value="allow">{{if .English}}Authorize{{else}}同意授权{{end}}</button></form></main></html>`))

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
	if err != nil || !client.Enabled || !slices.Contains(client.Redirects, redirect) || q.Get("response_type") != "code" || !valid || !oauthScopesAllowed(scope, client.AllowedScopes) || q.Get("code_challenge_method") != "S256" || !pkceChallenge.MatchString(q.Get("code_challenge")) || len(q.Get("state")) > 1024 || len(q.Get("nonce")) > 512 || q.Get("max_age") != "" || q.Get("prompt") != "" && q.Get("prompt") != "consent" && q.Get("prompt") != "none" {
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
	if q.Get("prompt") == "none" {
		result := destination.Query()
		result.Set("state", q.Get("state"))
		code := "consent_required"
		if err != nil {
			code = "login_required"
		}
		result.Set("error", code)
		destination.RawQuery = result.Encode()
		http.Redirect(w, r, destination.String(), 303)
		return
	}
	if err != nil {
		next := "/user/oauth/authorize?" + q.Encode()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), 303)
		return
	}
	if r.Method == "GET" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		english := oauthEnglish(r)
		permissions, readOnly := oauthConsentPermissions(scope, english)
		_ = consentPage.Execute(w, map[string]any{"English": english, "Name": client.Name, "Email": u.Email, "Permissions": permissions, "ReadOnlyPermissions": readOnly, "Host": destination.Host, "Redirect": redirect, "Action": "/oauth/authorize?" + q.Encode(), "CSRF": csrf(token)})
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
	if err == nil && !oauthScopesAllowed(scope, current.AllowedScopes) {
		failure(w, 400, "授权请求无效")
		return
	}
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
		var authTime int64
		e := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT auth_time FROM web_session_auth WHERE token_hash=?"), tokenHash(token)).Scan(&authTime)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			err = e
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_code_metadata(code_hash,nonce,auth_time) VALUES(?,?,?)"), tokenHash(code), q.Get("nonce"), authTime)
		}
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
	_, err = lockRegistrationMode(r.Context(), tx)
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
func (a *Server) oauthForm(w http.ResponseWriter, r *http.Request, limit *limiter) bool {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if !limit.allow(r) {
		w.Header().Set("Retry-After", "60")
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
	if !a.oauthForm(w, r, &a.oauthLimit) {
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
	grant := r.PostForm.Get("grant_type")
	var userID, authTime, familyExpires int64
	var scope, credential, family, nonce string
	if grant == "authorization_code" {
		var expires int64
		var redirect, challenge string
		code, verifier := r.PostForm.Get("code"), r.PostForm.Get("code_verifier")
		hash := sha256.Sum256([]byte(verifier))
		err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT user_id,redirect_uri,scope,challenge,credential_hash,expires_at FROM oauth_codes WHERE code_hash=? AND client_id=? FOR UPDATE"), tokenHash(code), client.ID).Scan(&userID, &redirect, &scope, &challenge, &credential, &expires)
		if err != nil || len(code) != 64 || expires <= time.Now().Unix() || redirect != r.PostForm.Get("redirect_uri") || !slices.Contains(client.Redirects, redirect) || !pkceVerifier.MatchString(verifier) || subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(hash[:])), []byte(challenge)) != 1 {
			oauthError(w, 400, "invalid_grant")
			return
		}
		if !oauthScopesAllowed(scope, client.AllowedScopes) {
			oauthError(w, 400, "invalid_scope")
			return
		}
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT nonce,auth_time FROM oauth_code_metadata WHERE code_hash=?"), tokenHash(code)).Scan(&nonce, &authTime)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
		if _, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_codes WHERE code_hash=?"), tokenHash(code)); err != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
		if _, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_code_metadata WHERE code_hash=?"), tokenHash(code)); err != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
	} else if grant == "refresh_token" {
		old := r.PostForm.Get("refresh_token")
		if len(old) != 64 {
			oauthError(w, 400, "invalid_grant")
			return
		}
		var used bool
		err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT family,user_id,scope,credential_hash,expires_at,used FROM oauth_refresh_tokens WHERE token_hash=? AND client_id=? FOR UPDATE"), tokenHash(old), client.ID).Scan(&family, &userID, &scope, &credential, &familyExpires, &used)
		if err != nil {
			oauthError(w, 400, "invalid_grant")
			return
		}
		if familyExpires <= time.Now().Unix() || used {
			if a.revokeOAuthFamily(r.Context(), tx, family) != nil || tx.Commit() != nil {
				oauthError(w, 503, "temporarily_unavailable")
				return
			}
			oauthError(w, 400, "invalid_grant")
			return
		}
		if requested := r.PostForm.Get("scope"); requested != "" {
			narrowed, valid := oauthScope(requested)
			if !valid || !oauthScopesAllowed(narrowed, strings.Fields(scope)) {
				oauthError(w, 400, "invalid_scope")
				return
			}
			scope = narrowed
		}
		if !oauthScopesAllowed(scope, client.AllowedScopes) {
			oauthError(w, 400, "invalid_scope")
			return
		}
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT nonce,auth_time FROM oauth_refresh_metadata WHERE family=?"), family).Scan(&nonce, &authTime)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
		if _, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE oauth_refresh_tokens SET used=TRUE WHERE token_hash=?"), tokenHash(old)); err != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
	} else {
		oauthError(w, 400, "unsupported_grant_type")
		return
	}
	u, err := a.readOIDCUser(r.Context(), tx, userID)
	if err == nil {
		err = a.lockAuthenticatedUser(r.Context(), tx, u)
	}
	if err != nil || u.credentialHash != credential {
		oauthError(w, 400, "invalid_grant")
		return
	}
	if _, err := tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_refresh_tokens WHERE expires_at<=?"), time.Now().Unix()); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	access, err := secret()
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	if _, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE expires_at<=?"), time.Now().Unix()); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	if _, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_tokens(token_hash,client_id,user_id,scope,credential_hash,expires_at) VALUES(?,?,?,?,?,?)"), tokenHash(access), client.ID, userID, scope, credential, time.Now().Add(time.Hour).Unix()); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	result := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "scope": scope}
	if hasOAuthScope(scope, "openid") {
		signed, err := a.oauthIDToken(r.Context(), tx, client.ID, u, access, nonce, authTime)
		if err != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
		result["id_token"] = signed
	}
	if hasOAuthScope(scope, "offline_access") {
		if family == "" {
			family, err = secret()
			familyExpires = time.Now().Add(30 * 24 * time.Hour).Unix()
			if err == nil {
				_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_refresh_metadata(family,nonce,auth_time) VALUES(?,?,?)"), family, nonce, authTime)
			}
		}
		var refresh string
		if err == nil {
			refresh, err = secret()
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_refresh_tokens(token_hash,family,client_id,user_id,scope,credential_hash,expires_at,used) VALUES(?,?,?,?,?,?,?,FALSE)"), tokenHash(refresh), family, client.ID, userID, scope, credential, familyExpires)
		}
		if err != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
		result["refresh_token"] = refresh
	}
	if family != "" {
		if _, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO oauth_access_families(token_hash,family) VALUES(?,?)"), tokenHash(access), family); err != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
	}
	for _, statement := range []string{
		"DELETE FROM oauth_code_metadata WHERE code_hash NOT IN (SELECT code_hash FROM oauth_codes)",
		"DELETE FROM oauth_access_families WHERE token_hash NOT IN (SELECT token_hash FROM oauth_tokens)",
		"DELETE FROM oauth_refresh_metadata WHERE family NOT IN (SELECT family FROM oauth_refresh_tokens)",
	} {
		if _, err = tx.ExecContext(r.Context(), statement); err != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	respond(w, 200, result)
}
func (a *Server) oauthUserinfo(w http.ResponseWriter, r *http.Request) {
	u, scope, ok := a.authenticateOAuthBearer(w, r, "")
	if !ok {
		return
	}
	result := map[string]any{"sub": strconv.FormatInt(u.ID, 10)}
	if hasOAuthScope(scope, "profile") {
		result["name"] = u.Nickname
	}
	if hasOAuthScope(scope, "email") {
		result["email"] = u.Email
		result["email_verified"] = u.EmailVerified
	}
	respond(w, 200, result)
}
func (a *Server) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if !a.oauthForm(w, r, &a.oauthLimit) {
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
	hash := tokenHash(r.PostForm.Get("token"))
	var family string
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT family FROM oauth_refresh_tokens WHERE token_hash=? AND client_id=?"), hash, client.ID).Scan(&family)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	if family != "" {
		err = a.revokeOAuthFamily(r.Context(), tx, family)
	} else {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE token_hash=? AND client_id=?"), hash, client.ID)
	}
	if err != nil || tx.Commit() != nil {
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
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT c.client_id,c.name,t.scope,t.expires_at FROM (SELECT client_id,scope,expires_at FROM oauth_tokens WHERE user_id=? AND expires_at>? UNION ALL SELECT client_id,scope,expires_at FROM oauth_refresh_tokens WHERE user_id=? AND expires_at>? AND used=FALSE) t JOIN oauth_clients c ON c.client_id=t.client_id WHERE c.enabled=TRUE ORDER BY c.name,t.expires_at DESC"), u.ID, time.Now().Unix(), u.ID, time.Now().Unix())
	if err != nil {
		failure(w, 503, "暂时无法读取授权")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	positions := map[string]int{}
	for rows.Next() {
		var id, name, scope string
		var expires int64
		if err = rows.Scan(&id, &name, &scope, &expires); err != nil {
			break
		}
		if index, exists := positions[id]; exists {
			items[index]["scope"], _ = oauthScope(items[index]["scope"].(string) + " " + scope)
		} else {
			positions[id] = len(items)
			items = append(items, map[string]any{"id": id, "name": name, "scope": scope, "expiresAt": expires})
		}
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
	tx, ok := a.beginOAuthWrite(w, r)
	if !ok {
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_codes WHERE client_id=? AND user_id=?"), r.PathValue("id"), u.ID)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_tokens WHERE client_id=? AND user_id=?"), r.PathValue("id"), u.ID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM oauth_refresh_tokens WHERE client_id=? AND user_id=?"), r.PathValue("id"), u.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法撤销授权")
		return
	}
	w.WriteHeader(204)
}
