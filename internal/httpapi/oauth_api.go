package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *Server) authenticateOAuthBearer(w http.ResponseWriter, r *http.Request, requiredScope string) (User, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	scheme, bearer, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	var id int64
	var scope, credential string
	ctx := r.Context()
	enabled, err := a.storedPluginEnabled(ctx, "wishingskin.oauth2")
	if err == nil && enabled && strings.EqualFold(scheme, "Bearer") && len(bearer) == 64 {
		err = a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT t.user_id,t.scope,t.credential_hash FROM oauth_tokens t JOIN oauth_clients c ON c.client_id=t.client_id WHERE t.token_hash=? AND t.expires_at>? AND c.enabled=TRUE"), tokenHash(bearer), time.Now().Unix()).Scan(&id, &scope, &credential)
	} else {
		err = errCredentials
	}
	var user User
	if err == nil {
		user, err = a.oidcUser(ctx, id)
	}
	if err != nil || user.credentialHash != credential {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		oauthError(w, 401, "invalid_token")
		return User{}, "", false
	}
	if requiredScope != "" && !hasOAuthScope(scope, requiredScope) {
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+requiredScope+`"`)
		oauthError(w, 403, "insufficient_scope")
		return User{}, "", false
	}
	return user, scope, true
}

func (a *Server) oauthTextureURL(hash string) string {
	if hash == "" {
		return ""
	}
	return a.cfg.PublicURL + "/textures/" + hash
}

func (a *Server) oauthCharacters(w http.ResponseWriter, r *http.Request) {
	user, _, ok := a.authenticateOAuthBearer(w, r, "characters:read")
	if !ok {
		return
	}
	characters, err := a.userCharacters(r.Context(), user.ID)
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	limit, err := effectiveCharacterLimit(r.Context(), a.s.DB, a.s.Bind, user.ID)
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	items := make([]map[string]string, 0, len(characters))
	for _, item := range characters {
		items = append(items, map[string]string{"uuid": item.UUID, "name": item.Name, "model": item.Model, "skinHash": item.SkinHash, "capeHash": item.CapeHash, "skinUrl": a.oauthTextureURL(item.SkinHash), "capeUrl": a.oauthTextureURL(item.CapeHash)})
	}
	respond(w, 200, map[string]any{"characters": items, "characterLimit": limit})
}

func (a *Server) oauthTextures(w http.ResponseWriter, r *http.Request) {
	user, _, ok := a.authenticateOAuthBearer(w, r, "textures:read")
	if !ok {
		return
	}
	page, size, ok := pagination(w, r, 50)
	if !ok {
		return
	}
	ctx := r.Context()
	var total int
	if err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM texture_records WHERE owner_id=? AND deleted=FALSE"), user.ID).Scan(&total); err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	rows, err := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT r.record_id,r.name,r.description,r.kind,r.model,r.file_hash,r.is_public,COALESCE(m.category,''),COALESCE(m.tags,'[]'),COALESCE(m.review_state,'approved'),r.created_at FROM texture_records r LEFT JOIN texture_metadata m ON m.record_id=r.record_id WHERE r.owner_id=? AND r.deleted=FALSE ORDER BY r.created_at DESC,r.record_id DESC LIMIT ? OFFSET ?"), user.ID, size, (page-1)*size)
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	defer rows.Close()
	type texture struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Kind        string   `json:"kind"`
		Model       string   `json:"model"`
		FileHash    string   `json:"fileHash"`
		URL         string   `json:"url"`
		Public      bool     `json:"public"`
		Category    string   `json:"category"`
		Tags        []string `json:"tags"`
		ReviewState string   `json:"reviewState"`
		CreatedAt   int64    `json:"createdAt"`
	}
	items := []texture{}
	for rows.Next() {
		var item texture
		var tags string
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Kind, &item.Model, &item.FileHash, &item.Public, &item.Category, &tags, &item.ReviewState, &item.CreatedAt); err != nil || json.Unmarshal([]byte(tags), &item.Tags) != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
		item.URL = a.oauthTextureURL(item.FileHash)
		items = append(items, item)
	}
	if rows.Err() != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	respond(w, 200, map[string]any{"textures": items, "total": total, "page": page, "pageSize": size})
}

func (a *Server) oauthPoints(w http.ResponseWriter, r *http.Request) {
	user, _, ok := a.authenticateOAuthBearer(w, r, "points:read")
	if !ok {
		return
	}
	page, size, ok := pagination(w, r, 50)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	defer tx.Rollback()
	rules, err := readPointRules(ctx, tx, false)
	var balance int64
	if err == nil {
		err = tx.QueryRowContext(ctx, a.s.Bind("SELECT score FROM users WHERE id=?"), user.ID).Scan(&balance)
	}
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	entries, total, err := a.pointEntries(ctx, tx, user.ID, page, size)
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	respond(w, 200, map[string]any{"enabled": rules.Enabled, "balance": strconv.FormatInt(balance, 10), "entries": entries, "total": total, "page": page, "pageSize": size})
}

func (a *Server) oauthMinecraft(w http.ResponseWriter, r *http.Request) {
	user, _, ok := a.authenticateOAuthBearer(w, r, "minecraft:read")
	if !ok {
		return
	}
	var uuid, name string
	var at int64
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT mc_uuid,mc_name,verified_at FROM minecraft_verifications WHERE user_id=?"), user.ID).Scan(&uuid, &name, &at)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	respond(w, 200, map[string]any{"verified": err == nil, "mcUuid": minecraftUUID(uuid), "mcName": name, "verifiedAt": at})
}
