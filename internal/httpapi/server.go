package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"wishingskin/internal/credential"
	"wishingskin/internal/mailer"
	pluginregistry "wishingskin/internal/plugins"
	"wishingskin/internal/runtimeplugin"
	"wishingskin/internal/store"
	sdk "wishingskin/pkg/plugin"
	blob "wishingskin/pkg/storage"
)

type Config struct {
	PublicURL, TextureDir string
	PluginDir             string
	Assets                fs.FS
	MailSender            MailSender
	Challenge             ChallengeVerifier
	TurnstileSiteKey      string
	TrustedProxies        []netip.Prefix
	SettingsKey           []byte
	SMTP                  *mailer.SMTP
	TurnstileSecret       string
	OIDC                  OIDCSettings
	CommunityPlugins      []sdk.Plugin
}
type Server struct {
	communityRuntime   communityRuntime
	dynamic            *runtimeplugin.Manager
	dynamicMu          sync.Mutex
	ops                operationsRuntime
	s                  *store.Store
	cfg                Config
	mux                *http.ServeMux
	hashes             chan struct{}
	uploads            chan struct{}
	uploadAccountLimit limiter
	uploadIPLimit      limiter
	loginLimit         limiter
	passwordLimit      limiter
	registrationLimit  limiter
	mailLimit          limiter
	mailCooldown       limiter
	loginFailures      limiter
	mailSlots          chan struct{}
	dummy              string
	pointsNow          func() time.Time
	totpNow            func() time.Time
	totpLimit          limiter
	plugins            *pluginregistry.Registry
	pluginEvents       *pluginregistry.EventBus
	dynamicCalls       sync.Map
	storageMu          sync.Mutex
	storageConfigHash  string
	remoteStorage      blob.BlobStore
	oidcMu             sync.Mutex
	oidcProvider       *oidc.Provider
	oidcConfigHash     string
}
type User struct {
	ID             int64  `json:"id"`
	Email          string `json:"email"`
	Nickname       string `json:"nickname"`
	IsAdmin        bool   `json:"isAdmin"`
	EmailVerified  bool   `json:"emailVerified"`
	LegacyAccount  bool   `json:"legacyAccount"`
	credentialHash string
}
type Character struct {
	ID       int64  `json:"id"`
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	SkinHash string `json:"skinHash"`
	CapeHash string `json:"capeHash"`
	Model    string `json:"model"`
}

func New(s *store.Store, cfg Config) (*Server, error) {
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || u.User != nil || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("public URL must be an http(s) origin")
	}
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	dummy, err := credential.Hash("unavailable-account-timing")
	if err != nil {
		return nil, err
	}
	registry, err := pluginregistry.NewRegistry(pluginregistry.Builtins()...)
	if err != nil {
		return nil, err
	}
	a := &Server{s: s, cfg: cfg, mux: http.NewServeMux(), hashes: make(chan struct{}, 2), uploads: make(chan struct{}, 2), dummy: dummy, plugins: registry, loginLimit: limiter{entries: map[string]bucket{}, now: time.Now}, passwordLimit: limiter{entries: map[string]bucket{}, now: time.Now}}
	a.pluginEvents = pluginregistry.NewEventBus()
	a.communityRuntime.pages = map[string]sdk.Page{}
	a.communityRuntime.tasks = map[string]registeredTask{}
	a.communityRuntime.running = map[string]context.CancelFunc{}
	a.communityRuntime.ctx = context.Background()
	for _, p := range cfg.CommunityPlugins {
		m := p.Manifest()
		if strings.HasPrefix(m.ID, "wishingskin.") {
			return nil, errors.New("reserved official plugin namespace")
		}
		if err := a.plugins.Register(m); err != nil {
			return nil, err
		}
		if err := p.Register(&communityHost{a: a, manifest: m, routes: map[string]bool{}}); err != nil {
			return nil, err
		}
	}
	if err := a.initDynamicPlugins(); err != nil {
		return nil, err
	}
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "alive"}) })
	a.mux.HandleFunc("GET /readyz", a.ready)
	a.registrationLimit = limiter{entries: map[string]bucket{}, now: time.Now, max: 5, window: time.Hour}
	a.mailLimit = limiter{entries: map[string]bucket{}, now: time.Now, max: 3, window: time.Hour}
	a.mailCooldown = limiter{entries: map[string]bucket{}, now: time.Now, max: 1}
	a.loginFailures = limiter{entries: map[string]bucket{}, now: time.Now, max: 5, window: 15 * time.Minute}
	a.uploadAccountLimit = limiter{entries: map[string]bucket{}, now: time.Now, max: 10}
	a.uploadIPLimit = limiter{entries: map[string]bucket{}, now: time.Now, max: 30}
	a.mailSlots = make(chan struct{}, 2)
	a.mux.HandleFunc("GET /api/site", a.site)
	a.mux.HandleFunc("GET /api/notices", a.notices)
	a.registerAccountAccess()
	a.totpNow = time.Now
	a.totpLimit = limiter{entries: map[string]bucket{}, now: time.Now, max: 20, window: 5 * time.Minute}
	a.registerTOTP()
	a.registerOIDC()
	a.registerOAuth()
	a.registerConnectors()
	a.registerCustomization()
	a.registerAdministration()
	a.registerModeration()
	a.registerContentReview()
	a.registerOperations()
	a.mux.HandleFunc("GET /cdn/textures/{hash}", a.cdnTexture)
	a.registerResourceAdministration()
	a.registerReports()
	a.registerPlugins()
	a.registerPoints()
	a.pointsNow = time.Now
	a.mux.HandleFunc("POST /api/auth/login", a.login)
	a.mux.HandleFunc("GET /api/auth/me", a.me)
	a.mux.HandleFunc("POST /api/auth/logout", a.logout)
	a.mux.HandleFunc("POST /api/auth/password", a.changePassword)
	a.mux.HandleFunc("GET /api/characters", a.characters)
	a.mux.HandleFunc("POST /api/characters", a.createCharacter)
	a.mux.HandleFunc("PATCH /api/characters/{id}", a.updateCharacter)
	a.mux.HandleFunc("DELETE /api/characters/{id}", a.deleteCharacter)
	a.mux.HandleFunc("PUT /api/characters/{id}/textures/{kind}", a.bindTexture)
	a.mux.HandleFunc("GET /api/textures", a.listTextures)
	a.mux.HandleFunc("POST /api/textures", a.uploadTexture)
	a.mux.HandleFunc("PATCH /api/textures/{id}", a.manageTextureRecord)
	a.mux.HandleFunc("DELETE /api/textures/{id}", a.manageTextureRecord)
	a.mux.HandleFunc("GET /api/library", a.publicLibrary)
	a.mux.HandleFunc("GET /api/library/{id}", a.publicLibrary)
	a.mux.HandleFunc("GET /api/wardrobe", a.wardrobe)
	a.mux.HandleFunc("PUT /api/wardrobe/{id}", a.manageWardrobe)
	a.mux.HandleFunc("PATCH /api/wardrobe/{id}", a.manageWardrobe)
	a.mux.HandleFunc("DELETE /api/wardrobe/{id}", a.manageWardrobe)
	a.mux.HandleFunc("PATCH /api/admin/settings", a.updateAdminSettings)
	a.mux.HandleFunc("GET /textures/{hash}", a.texture)
	a.mux.HandleFunc("GET /csl/textures/{hash}", a.texture)
	a.mux.HandleFunc("GET /defaults/skins/{model}", a.defaultSkin)
	a.mux.HandleFunc("GET /raw/{tid}", a.legacyRawTexture)
	a.mux.HandleFunc("GET /api/integration", a.integration)
	a.mux.HandleFunc("GET /{file}", a.legacyRoot)
	a.mux.HandleFunc("GET /csl/{file}", a.legacyPlayerJSON)
	a.mux.HandleFunc("GET /skin/{file}", a.legacyPlayerTexture)
	a.mux.HandleFunc("GET /cape/{file}", a.legacyPlayerTexture)
	a.mux.HandleFunc("GET /csl/skin/{file}", a.legacyPlayerTexture)
	a.mux.HandleFunc("GET /csl/cape/{file}", a.legacyPlayerTexture)
	a.registerYggdrasil()
	a.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { failure(w, 404, "接口不存在") })
	if cfg.Assets != nil {
		a.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			a.serveAssetOrSPA(w, r)
		})
	}
	return a, nil
}

func isSPAPath(path string) bool {
	if strings.HasSuffix(path, ".json") {
		return false
	}
	return path == "/" || path == "/login" || path == "/library" || path == "/user" || strings.HasPrefix(path, "/user/") || path == "/admin" || strings.HasPrefix(path, "/admin/")
}

func (a *Server) serveAssetOrSPA(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Assets == nil {
		http.NotFound(w, r)
		return
	}
	if isSPAPath(r.URL.Path) {
		clone := r.Clone(r.Context())
		// Serve the root directory so FileServerFS can select index.html without
		// redirecting the SPA route back to ./.
		clone.URL.Path = "/"
		clone.URL.RawPath = ""
		http.FileServerFS(a.cfg.Assets).ServeHTTP(w, clone)
		return
	}
	http.FileServerFS(a.cfg.Assets).ServeHTTP(w, r)
}

func (a *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = a.proxyRequest(r)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Authlib-Injector-API-Location", a.cfg.PublicURL+"/api/yggdrasil/")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/oauth/") || strings.HasPrefix(r.URL.Path, "/cdn/") || strings.HasPrefix(r.URL.Path, "/textures/") || strings.HasPrefix(r.URL.Path, "/csl/") || strings.HasPrefix(r.URL.Path, "/skin/") || strings.HasPrefix(r.URL.Path, "/cape/") || strings.HasPrefix(r.URL.Path, "/raw/") || strings.HasSuffix(r.URL.Path, ".json") {
		var status string
		err := a.s.DB.QueryRowContext(r.Context(), "SELECT status FROM migration_state WHERE id=1").Scan(&status)
		if (err != nil && !errors.Is(err, sql.ErrNoRows)) || status == "pending" {
			if strings.HasPrefix(r.URL.Path, "/api/yggdrasil") {
				yfail(w, 503, "ServiceUnavailableException", "Migration pending")
			} else {
				failure(w, 503, "站点正在迁移，暂不可用")
			}
			return
		}
	}
	a.mux.ServeHTTP(w, r)
}

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeLimit(w, r, v, 8192)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(v); err != nil {
		failure(w, 400, "请求内容无效")
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		failure(w, 400, "请求内容无效")
		return false
	}
	return true
}
func secret() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func tokenHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func csrf(s string) string      { return tokenHash("wishingskin-csrf:" + s) }

func (a *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.s.DB.PingContext(ctx); err != nil {
		failure(w, 503, "数据库暂不可用")
		return
	}
	var status string
	err := a.s.DB.QueryRowContext(ctx, "SELECT status FROM migration_state WHERE id=1").Scan(&status)
	if (err != nil && !errors.Is(err, sql.ErrNoRows)) || status == "pending" {
		failure(w, 503, "站点迁移尚未完成")
		return
	}
	if _, _, err := a.serviceSettings(ctx); err != nil {
		failure(w, 503, "服务设置不可用，请检查本地设置密钥")
		return
	}
	if _, err := a.oidcSettings(ctx); err != nil {
		failure(w, 503, "OIDC 设置不可用，请检查本地设置密钥")
		return
	}
	respond(w, 200, map[string]string{"status": "ready"})
}

func (a *Server) authenticate(ctx context.Context, email, password string) (User, error) {
	var u User
	var hash, method, salt string
	var banned bool
	if len(email) > 254 || len(password) > 1024 || password == "" {
		return u, errCredentials
	}
	select {
	case a.hashes <- struct{}{}:
		defer func() { <-a.hashes }()
	default:
		return u, errBusy
	}
	var permission int
	var verificationRequired bool
	err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT u.id,u.email,u.nickname,u.password_hash,u.password_method,u.password_salt,u.banned,u.permission,COALESCE(s.email_verified,FALSE),s.user_id IS NULL,COALESCE(s.verification_required,FALSE) FROM users u LEFT JOIN account_states s ON s.user_id=u.id WHERE u.email_key=?"), strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.Nickname, &hash, &method, &salt, &banned, &permission, &u.EmailVerified, &u.LegacyAccount, &verificationRequired)
	if errors.Is(err, sql.ErrNoRows) {
		_, _ = credential.Verify("ARGON2ID", a.dummy, "", password)
		return User{}, errCredentials
	}
	if err != nil {
		return User{}, err
	}
	u.IsAdmin = permission >= 2
	ok, err := credential.Verify(method, hash, salt, password)
	if err != nil || !ok || banned {
		return User{}, errCredentials
	}
	if !u.EmailVerified && verificationRequired {
		return User{}, errUnverified
	}
	u.credentialHash = hash
	if method != "ARGON2ID" || !strings.HasPrefix(hash, "$argon2id$") {
		updated, err := credential.Hash(password)
		if err != nil {
			return User{}, err
		}
		result, err := a.s.DB.ExecContext(ctx, a.s.Bind("UPDATE users SET password_hash=?,password_method='ARGON2ID',password_salt='' WHERE id=? AND password_hash=? AND banned=?"), updated, u.ID, hash, false)
		if err != nil {
			return User{}, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return User{}, err
		}
		if rows != 1 {
			return User{}, errCredentials
		}
		u.credentialHash = updated
	}
	return u, nil
}

var errCredentials = errors.New("invalid credentials")
var errBusy = errors.New("authentication busy")

func (a *Server) login(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		failure(w, 403, "请求来源无效")
		return
	}
	if !a.loginLimit.allow(r) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "登录尝试过于频繁，请稍后重试")
		return
	}
	var body struct {
		Email          string `json:"email"`
		Password       string `json:"password"`
		TurnstileToken string `json:"turnstileToken"`
		TOTPCode       string `json:"totpCode"`
	}
	if !decode(w, r, &body) {
		return
	}
	accountKey := "account:" + tokenHash(strings.ToLower(strings.TrimSpace(body.Email)))
	ipKey := "ip:" + connectionIP(r)
	if !a.loginLimit.allowKey(accountKey) {
		a.authError(w, errBusy)
		return
	}
	challenged := a.loginFailures.blocked(accountKey) || a.loginFailures.blocked(ipKey)
	if challenged && !a.challenge(w, r, body.TurnstileToken, "login") {
		return
	}
	u, err := a.authenticate(r.Context(), body.Email, body.Password)
	if err != nil {
		if errors.Is(err, errCredentials) {
			a.loginFailures.allowKey(accountKey)
			a.loginFailures.allowKey(ipKey)
			respond(w, 401, map[string]any{"error": "邮箱或密码不正确", "captchaRequired": a.loginFailures.blocked(accountKey) || a.loginFailures.blocked(ipKey)})
			return
		}
		a.authError(w, err)
		return
	}
	// A valid password and any required challenge clear password guessing state.
	// Second-factor failures are governed by the separate TOTP limiter below.
	a.loginFailures.reset(accountKey)
	if challenged {
		a.loginFailures.reset(ipKey)
	}
	token, err := secret()
	if err != nil {
		failure(w, 500, "无法创建会话")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		a.authError(w, err)
		return
	}
	defer tx.Rollback()
	err = a.lockAuthenticatedUser(r.Context(), tx, u)
	if err == nil {
		var totpEnabled bool
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT enabled FROM user_totp WHERE user_id=?"), u.ID).Scan(&totpEnabled)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		} else if err == nil && totpEnabled {
			if !a.totpLimit.allow(r) || !a.totpLimit.allowKey("account:"+strconv.FormatInt(u.ID, 10)) {
				w.Header().Set("Retry-After", "300")
				respond(w, 429, map[string]any{"error": "验证尝试过于频繁，请五分钟后重试", "totpRequired": true, "captchaRequired": false})
				return
			}
		}
	}
	if err == nil {
		err = a.consumeTOTP(r.Context(), tx, u.ID, body.TOTPCode)
	}
	if errors.Is(err, errTOTPRequired) {
		respond(w, 401, map[string]any{"error": "此账号已开启双重验证，请输入验证码或恢复码", "totpRequired": true, "captchaRequired": false})
		return
	}
	if errors.Is(err, errTOTP) {
		respond(w, 401, map[string]any{"error": "验证码或恢复码无效，请重试", "totpRequired": true, "captchaRequired": false})
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)"), tokenHash(token), u.ID, time.Now().Add(24*time.Hour).Unix())
		if err == nil {
			err = a.recordWebAuthTime(r.Context(), tx, token)
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		a.authError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "ws_session", Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 86400})
	respond(w, 200, map[string]any{"user": u, "csrfToken": csrf(token)})
}
func (a *Server) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" || o == a.cfg.PublicURL {
		return true
	}
	// Local previews are commonly opened once as localhost and once as
	// 127.0.0.1. Keep production origins strict while allowing only equivalent
	// loopback hosts on the same scheme and port.
	expected, expectedErr := url.Parse(a.cfg.PublicURL)
	actual, actualErr := url.Parse(o)
	if expectedErr != nil || actualErr != nil || expected.Scheme != actual.Scheme || expected.Port() != actual.Port() || expected.User != nil || actual.User != nil || expected.Path != "" && expected.Path != "/" || actual.Path != "" && actual.Path != "/" {
		return false
	}
	return loopbackOriginHost(expected.Hostname()) && loopbackOriginHost(actual.Hostname())
}

func loopbackOriginHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}
func (a *Server) authError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBusy):
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "登录繁忙，请稍后重试")
	case errors.Is(err, errCredentials):
		failure(w, 401, "邮箱或密码不正确")
	case errors.Is(err, errUnverified):
		respond(w, 403, map[string]any{"error": "请先验证邮箱后登录", "emailVerificationRequired": true})
	default:
		failure(w, 503, "暂时无法登录")
	}
}
func (a *Server) current(r *http.Request) (User, string, error) {
	c, err := r.Cookie("ws_session")
	if err != nil || len(c.Value) != 64 {
		return User{}, "", errCredentials
	}
	var u User
	var permission int
	err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT u.id,u.email,u.nickname,u.permission,COALESCE(a.email_verified,FALSE),a.user_id IS NULL FROM web_sessions s JOIN users u ON u.id=s.user_id LEFT JOIN account_states a ON a.user_id=u.id WHERE s.token_hash=? AND s.expires_at>? AND u.banned=? AND (a.user_id IS NULL OR a.email_verified=TRUE OR a.verification_required=FALSE)"), tokenHash(c.Value), time.Now().Unix(), false).Scan(&u.ID, &u.Email, &u.Nickname, &permission, &u.EmailVerified, &u.LegacyAccount)
	u.IsAdmin = permission >= 2
	return u, c.Value, err
}
func sessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, errCredentials) || errors.Is(err, sql.ErrNoRows) {
		failure(w, 401, "请先登录")
		return
	}
	failure(w, 503, "暂时无法验证会话")
}
func (a *Server) me(w http.ResponseWriter, r *http.Request) {
	u, t, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	respond(w, 200, map[string]any{"user": u, "csrfToken": csrf(t)})
}
func (a *Server) logout(w http.ResponseWriter, r *http.Request) {
	_, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.sameOrigin(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf(token))) != 1 {
		failure(w, 403, "请求校验失败")
		return
	}
	if _, err = a.s.DB.ExecContext(r.Context(), a.s.Bind("DELETE FROM web_sessions WHERE token_hash=?"), tokenHash(token)); err != nil {
		failure(w, 503, "暂时无法退出")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "ws_session", Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(204)
}
func (a *Server) userCharacters(ctx context.Context, id int64) ([]Character, error) {
	rows, err := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT c.id,c.uuid,c.name,CASE WHEN EXISTS (SELECT 1 FROM textures f WHERE f.hash=c.skin_hash AND (f.deleted=TRUE OR NOT "+availableTextureSQL+")) THEN '' ELSE c.skin_hash END,CASE WHEN EXISTS (SELECT 1 FROM textures f WHERE f.hash=c.cape_hash AND (f.deleted=TRUE OR NOT "+availableTextureSQL+")) THEN '' ELSE c.cape_hash END,c.model FROM characters c WHERE c.user_id=? ORDER BY c.id"), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Character{}
	for rows.Next() {
		var c Character
		if err := rows.Scan(&c.ID, &c.UUID, &c.Name, &c.SkinHash, &c.CapeHash, &c.Model); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (a *Server) characters(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	out, err := a.userCharacters(r.Context(), u.ID)
	if err != nil {
		failure(w, 503, "暂时无法读取角色")
		return
	}
	limit, err := characterLimit(r.Context(), a.s.DB, a.s.Bind)
	if err != nil {
		failure(w, 503, "暂时无法读取站点设置")
		return
	}
	respond(w, 200, map[string]any{"characters": out, "characterLimit": limit})
}
