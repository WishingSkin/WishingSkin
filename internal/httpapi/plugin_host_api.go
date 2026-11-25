package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/WishingSkin/WishingSkin/internal/runtimeplugin"
	"github.com/WishingSkin/WishingSkin/internal/textureimport"
)

type pluginHostAuth struct {
	mu       sync.RWMutex
	byPlugin map[string]string
	byToken  map[string]string
}

func newPluginHostAuth() *pluginHostAuth {
	return &pluginHostAuth{
		byPlugin: map[string]string{},
		byToken:  map[string]string{},
	}
}

func (h *pluginHostAuth) tokenFor(pluginID string) string {
	if h == nil || pluginID == "" {
		return ""
	}
	h.mu.RLock()
	tok := h.byPlugin[pluginID]
	h.mu.RUnlock()
	if tok != "" {
		return tok
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	tok = hex.EncodeToString(b[:])
	h.mu.Lock()
	defer h.mu.Unlock()
	if existing := h.byPlugin[pluginID]; existing != "" {
		return existing
	}
	h.byPlugin[pluginID] = tok
	h.byToken[tok] = pluginID
	return tok
}

func (h *pluginHostAuth) rotate(pluginID string) {
	if h == nil || pluginID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.byPlugin[pluginID]; old != "" {
		delete(h.byToken, old)
		delete(h.byPlugin, pluginID)
	}
}

func (h *pluginHostAuth) lookup(token string) (string, bool) {
	if h == nil || len(token) != 64 {
		return "", false
	}
	h.mu.RLock()
	id, ok := h.byToken[token]
	h.mu.RUnlock()
	return id, ok
}

func (a *Server) officialPluginPublicKeys() []ed25519.PublicKey {
	builtin := runtimeplugin.BuiltInOfficialPublicKeys()
	keys := make([]ed25519.PublicKey, 0, len(builtin)+len(a.cfg.OfficialPluginPublicKeys))
	keys = append(keys, builtin...)
	keys = append(keys, a.cfg.OfficialPluginPublicKeys...)
	return keys
}

func (a *Server) startPluginHostAPI() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/site", a.hostAPISite)
	mux.HandleFunc("GET /v1/characters/by-name", a.hostAPICharacterByName)
	mux.HandleFunc("GET /v1/textures/{hash}/bytes", a.hostAPITextureBytes)
	mux.HandleFunc("GET /v1/users/{id}/summary", a.hostAPIUserSummary)
	mux.HandleFunc("GET /v1/users/{id}/textures", a.hostAPIUserTextures)
	mux.HandleFunc("POST /v1/users/{id}/textures", a.hostAPIUploadTexture)
	mux.HandleFunc("PUT /v1/users/{id}/characters/{charId}/textures/{kind}", a.hostAPIBindCharacterTexture)
	mux.HandleFunc("POST /v1/users/{id}/points/spend", a.hostAPISpendPoints)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	a.hostAPIListener = ln
	a.hostAPI = srv
	a.hostAPIURL = "http://" + ln.Addr().String()
	go func() { _ = srv.Serve(ln) }()
	return nil
}

func (a *Server) authenticateHostPlugin(w http.ResponseWriter, r *http.Request, requiredGrant string) (runtimeplugin.Installed, bool) {
	scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	if !strings.EqualFold(scheme, "Bearer") {
		failure(w, 401, "unauthorized")
		return runtimeplugin.Installed{}, false
	}
	id, ok := a.hostAuth.lookup(strings.TrimSpace(token))
	if !ok || a.dynamic == nil || !a.availableCommunity(r.Context(), id) {
		failure(w, 401, "unauthorized")
		return runtimeplugin.Installed{}, false
	}
	item, exists := a.dynamic.Get(id)
	if !exists {
		failure(w, 401, "unauthorized")
		return runtimeplugin.Installed{}, false
	}
	if requiredGrant != "" && !slices.Contains(item.Grants, requiredGrant) {
		failure(w, 403, "missing grant")
		return runtimeplugin.Installed{}, false
	}
	return item, true
}

func (a *Server) hostAPISite(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticateHostPlugin(w, r, "host.public.read"); !ok {
		return
	}
	ctx := r.Context()
	siteName := "WishingSkin"
	var storedName string
	if err := a.s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='site_name'").Scan(&storedName); err == nil && storedName != "" {
		siteName = storedName
	}
	rules, err := readPointRules(ctx, a.s.DB, false)
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	policy, err := readContentPolicy(ctx, a.s.DB)
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	respond(w, 200, map[string]any{
		"name":          siteName,
		"publicUrl":     a.cfg.PublicURL,
		"pointsEnabled": rules.Enabled,
		"contentPolicy": policy,
	})
}

func (a *Server) hostAPICharacterByName(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticateHostPlugin(w, r, "host.public.read"); !ok {
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if !characterNamePattern.MatchString(name) {
		failure(w, 400, "invalid character name")
		return
	}
	var c Character
	var mcUUID, mcName string
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind(
		"SELECT c.id,c.uuid,c.name,c.model,"+
			"CASE WHEN EXISTS (SELECT 1 FROM textures f WHERE f.hash=c.skin_hash AND (f.deleted=TRUE OR NOT "+availableTextureSQL+")) THEN '' ELSE c.skin_hash END,"+
			"CASE WHEN EXISTS (SELECT 1 FROM textures f WHERE f.hash=c.cape_hash AND (f.deleted=TRUE OR NOT "+availableTextureSQL+")) THEN '' ELSE c.cape_hash END,"+
			"COALESCE(mv.mc_uuid,''),COALESCE(mv.mc_name,'') "+
			"FROM characters c LEFT JOIN minecraft_verifications mv ON mv.user_id=c.user_id "+
			"WHERE c.name_key=?"), strings.ToLower(name)).
		Scan(&c.ID, &c.UUID, &c.Name, &c.Model, &c.SkinHash, &c.CapeHash, &mcUUID, &mcName)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "character not found")
		return
	}
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	respond(w, 200, map[string]any{
		"id":                c.ID,
		"uuid":              c.UUID,
		"name":              c.Name,
		"model":             c.Model,
		"skinHash":          c.SkinHash,
		"capeHash":          c.CapeHash,
		"verifiedMinecraft": mcUUID != "",
		"mcName":            mcName,
	})
}

func (a *Server) hostAPITextureBytes(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !validHash.MatchString(hash) {
		failure(w, 404, "not found")
		return
	}
	rawUID := strings.TrimSpace(r.URL.Query().Get("userId"))
	ctx := r.Context()
	if rawUID != "" {
		if _, ok := a.authenticateHostPlugin(w, r, "host.textures.read"); !ok {
			return
		}
		uid, err := strconv.ParseInt(rawUID, 10, 64)
		if err != nil || uid < 1 {
			failure(w, 400, "invalid userId")
			return
		}
		var exists int
		err = a.s.DB.QueryRowContext(ctx, a.s.Bind(
			"SELECT 1 FROM textures f WHERE f.hash=? AND f.deleted=FALSE AND ("+
				downloadableTextureSQL+" OR "+
				"EXISTS (SELECT 1 FROM texture_records r WHERE r.file_hash=f.hash AND r.owner_id=? AND r.deleted=FALSE)"+
				")"), hash, uid).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, 404, "not found")
			return
		}
		if err != nil {
			failure(w, 503, "unavailable")
			return
		}
	} else {
		if _, ok := a.authenticateHostPlugin(w, r, "host.public.read"); !ok {
			return
		}
		var exists int
		err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT 1 FROM textures f WHERE f.hash=? AND "+downloadableTextureSQL), hash).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, 404, "not found")
			return
		}
		if err != nil {
			failure(w, 503, "unavailable")
			return
		}
	}
	data, err := a.textureFileBytes(ctx, hash)
	if err != nil {
		failure(w, 404, "not found")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func (a *Server) hostAPIUserSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticateHostPlugin(w, r, "host.users.read"); !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || uid < 1 {
		failure(w, 400, "invalid user id")
		return
	}
	ctx := r.Context()
	var nickname string
	var permission int
	var banned, emailVerified bool
	var score int64
	var mcUUID, mcName string
	err = a.s.DB.QueryRowContext(ctx, a.s.Bind(
		"SELECT u.nickname,u.permission,u.banned,u.score,COALESCE(a.email_verified,FALSE),COALESCE(mv.mc_uuid,''),COALESCE(mv.mc_name,'') "+
			"FROM users u LEFT JOIN account_states a ON a.user_id=u.id LEFT JOIN minecraft_verifications mv ON mv.user_id=u.id WHERE u.id=?"), uid).
		Scan(&nickname, &permission, &banned, &score, &emailVerified, &mcUUID, &mcName)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "user not found")
		return
	}
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	chars, err := a.userCharacters(ctx, uid)
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	respond(w, 200, map[string]any{
		"id":                uid,
		"nickname":          nickname,
		"isAdmin":           permission >= 2,
		"banned":            banned,
		"emailVerified":     emailVerified,
		"score":             strconv.FormatInt(score, 10),
		"verifiedMinecraft": mcUUID != "",
		"mcName":            mcName,
		"mcUuid":            minecraftUUID(mcUUID),
		"characters":        chars,
	})
}

func (a *Server) hostAPIUserTextures(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticateHostPlugin(w, r, "host.textures.read"); !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || uid < 1 {
		failure(w, 400, "invalid user id")
		return
	}
	ctx := r.Context()
	var exists int
	if err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT 1 FROM users WHERE id=?"), uid).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "user not found")
		return
	} else if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	items, err := a.listUserTextures(ctx, uid, strings.TrimSpace(r.URL.Query().Get("kind")))
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	respond(w, 200, map[string]any{"textures": items})
}

func (a *Server) hostAPIUploadTexture(w http.ResponseWriter, r *http.Request) {
	item, ok := a.authenticateHostPlugin(w, r, "host.textures.write")
	if !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || uid < 1 {
		failure(w, 400, "invalid user id")
		return
	}
	var body struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		Kind         string   `json:"kind"`
		Model        string   `json:"model"`
		IsPublic     bool     `json:"isPublic"`
		Category     string   `json:"category"`
		Tags         []string `json:"tags"`
		PNGBase64    string   `json:"pngBase64"`
		ExpectedCost *int64   `json:"expectedCost"`
	}
	if !decodeLimit(w, r, &body, 12<<20) {
		return
	}
	pngBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.PNGBase64))
	if err != nil || len(pngBytes) == 0 || len(pngBytes) > maxTextureBytes {
		failure(w, 400, "PNG 图像数据无效")
		return
	}
	hookCtx := withHookSkipPlugin(r.Context(), item.Package.Manifest.ID)
	a.createTextureUpload(w, r.WithContext(hookCtx), uid, "", textureUploadInput{
		Kind:         body.Kind,
		Model:        body.Model,
		Name:         body.Name,
		Description:  body.Description,
		Category:     body.Category,
		Tags:         body.Tags,
		IsPublic:     body.IsPublic,
		ExpectedCost: body.ExpectedCost,
		RawFile:      pngBytes,
	})
}

func (a *Server) hostAPIBindCharacterTexture(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticateHostPlugin(w, r, "host.characters.write"); !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || uid < 1 {
		failure(w, 400, "invalid user id")
		return
	}
	charID, err := strconv.ParseInt(r.PathValue("charId"), 10, 64)
	if err != nil || charID < 1 {
		failure(w, 400, "invalid character id")
		return
	}
	kind := r.PathValue("kind")
	var body struct {
		RecordID string  `json:"recordId"`
		Model    *string `json:"model"`
	}
	if !decode(w, r, &body) {
		return
	}
	a.bindCharacterTexture(w, r, uid, "", charID, kind, body.RecordID, body.Model)
}

func (a *Server) hostAPISpendPoints(w http.ResponseWriter, r *http.Request) {
	item, ok := a.authenticateHostPlugin(w, r, "host.points.spend")
	if !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || uid < 1 {
		failure(w, 400, "invalid user id")
		return
	}
	var body struct {
		Amount      int64  `json:"amount"`
		ReferenceID string `json:"referenceId"`
	}
	if !decode(w, r, &body) {
		return
	}
	ref := strings.TrimSpace(body.ReferenceID)
	if body.Amount <= 0 || body.Amount > 1_000_000_000 || ref == "" || utf8.RuneCountInString(ref) > 64 {
		failure(w, 400, "invalid points spend request")
		return
	}
	sum := sha256.Sum256([]byte("plugin:" + item.Package.Manifest.ID + "\x00" + ref))
	entryRef := hex.EncodeToString(sum[:])

	ctx := r.Context()
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	defer tx.Rollback()

	var banned bool
	var score int64
	err = tx.QueryRowContext(ctx, a.s.Bind("SELECT banned,score FROM users WHERE id=? FOR UPDATE"), uid).Scan(&banned, &score)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "user not found")
		return
	}
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	if banned {
		failure(w, 403, "user is banned")
		return
	}
	rules, err := readPointRules(ctx, tx, true)
	if err != nil {
		failure(w, 503, "unavailable")
		return
	}
	if !rules.Enabled {
		failure(w, 409, "积分与签到尚未启用")
		return
	}

	var prevDelta, prevAfter int64
	err = tx.QueryRowContext(ctx, a.s.Bind("SELECT delta,balance_after FROM point_entries WHERE user_id=? AND kind='plugin' AND reference_id=?"), uid, entryRef).Scan(&prevDelta, &prevAfter)
	if err == nil {
		if prevDelta != -body.Amount {
			failure(w, 409, "referenceId already used with different amount")
			return
		}
		if err = tx.Commit(); err != nil {
			failure(w, 503, "unavailable")
			return
		}
		respond(w, 200, map[string]any{"balance": strconv.FormatInt(score, 10), "deducted": false})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		failure(w, 503, "unavailable")
		return
	}

	if err := a.recordPointChange(ctx, tx, uid, -body.Amount, "plugin", entryRef); err != nil {
		if errors.Is(err, errPointsInsufficient) {
			failure(w, 409, "积分不足，无法完成操作")
			return
		}
		failure(w, 503, "unavailable")
		return
	}
	var after int64
	if err := tx.QueryRowContext(ctx, a.s.Bind("SELECT score FROM users WHERE id=?"), uid).Scan(&after); err != nil {
		failure(w, 503, "unavailable")
		return
	}
	if err := tx.Commit(); err != nil {
		failure(w, 503, "unavailable")
		return
	}
	respond(w, 200, map[string]any{"balance": strconv.FormatInt(after, 10), "deducted": true})
}

type textureUploadInput struct {
	Kind         string
	Model        string
	Name         string
	Description  string
	Category     string
	Tags         []string
	IsPublic     bool
	ExpectedCost *int64
	RawFile      []byte
}

func (a *Server) listUserTextures(ctx context.Context, userID int64, kind string) ([]textureRecord, error) {
	where := " WHERE r.deleted=? AND f.deleted=? AND (r.owner_id=? OR (r.owner_id IS NULL AND r.file_hash IN (SELECT skin_hash FROM characters WHERE user_id=? UNION SELECT cape_hash FROM characters WHERE user_id=?)))"
	args := []any{false, false, userID, userID, userID}
	if kind == "skin" || kind == "cape" {
		where += " AND r.kind=?"
		args = append(args, kind)
	}
	rows, err := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT r.record_id,r.file_hash,r.name,r.description,r.kind,r.model,r.is_public,r.owner_id,COALESCE(m.category,''),COALESCE(m.tags,'[]'),COALESCE(m.review_state,'approved'),COALESCE(m.review_reason,''),r.created_at FROM texture_records r JOIN textures f ON f.hash=r.file_hash LEFT JOIN texture_metadata m ON m.record_id=r.record_id"+where+" ORDER BY r.name,r.record_id"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []textureRecord{}
	for rows.Next() {
		var item textureRecord
		var owner sql.NullInt64
		var tags string
		if err := rows.Scan(&item.ID, &item.FileHash, &item.Name, &item.Description, &item.Kind, &item.Model, &item.Public, &owner, &item.Category, &tags, &item.ReviewState, &item.ReviewReason, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := jsonUnmarshaler(tags, &item.Tags); err != nil {
			return nil, err
		}
		item.Owned = owner.Valid && owner.Int64 == userID
		items = append(items, item)
	}
	return items, rows.Err()
}

// Keep json decoding explicit without shadowing httpapi.decode.
func jsonUnmarshaler(raw string, target any) error {
	return textureimport.DecodeJSONString(raw, target)
}
