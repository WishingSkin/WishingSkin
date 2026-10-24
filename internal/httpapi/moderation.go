package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (a *Server) registerModeration() {
	a.mux.HandleFunc("GET /api/admin/textures", a.adminTextures)
	a.mux.HandleFunc("POST /api/admin/textures/{hash}/disable", a.disableAdminTexture)
}

type adminTexture struct {
	ID              string `json:"id"`
	FileHash        string `json:"fileHash"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Model           string `json:"model"`
	OwnerID         string `json:"ownerId"`
	OwnerEmail      string `json:"ownerEmail"`
	Public          bool   `json:"public"`
	RecordDeleted   bool   `json:"recordDeleted"`
	Disabled        bool   `json:"disabled"`
	RecordCount     int64  `json:"recordCount"`
	CharacterCount  int64  `json:"characterCount"`
	CollectionCount int64  `json:"collectionCount"`
}

func (a *Server) adminTextures(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	page, size, ok := adminPagination(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 255 {
		failure(w, 400, "搜索内容不能超过 255 个字符")
		return
	}
	where := ""
	args := []any{}
	if query != "" {
		owner, _ := strconv.ParseInt(query, 10, 64)
		where = " WHERE r.record_id=? OR r.file_hash=? OR r.owner_id=? OR POSITION(? IN r.name)>0 OR POSITION(? IN LOWER(u.email))>0"
		args = append(args, query, strings.ToLower(query), owner, query, strings.ToLower(query))
	}
	from := " FROM texture_records r JOIN textures f ON f.hash=r.file_hash LEFT JOIN users u ON u.id=r.owner_id"
	var total int64
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*)"+from+where), args...).Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取管理材质")
		return
	}
	columns := "r.record_id,r.file_hash,r.name,r.kind,r.model,r.owner_id,COALESCE(u.email,''),r.is_public,r.deleted,f.deleted,(SELECT COUNT(*) FROM texture_records x WHERE x.file_hash=f.hash),(SELECT COUNT(*) FROM characters c WHERE c.skin_hash=f.hash OR c.cape_hash=f.hash),(SELECT COUNT(*) FROM wardrobe w JOIN texture_records x ON x.record_id=w.record_id WHERE x.file_hash=f.hash)"
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT "+columns+from+where+" ORDER BY r.record_id LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取管理材质")
		return
	}
	defer rows.Close()
	items := []adminTexture{}
	for rows.Next() {
		var item adminTexture
		var owner sql.NullInt64
		if err := rows.Scan(&item.ID, &item.FileHash, &item.Name, &item.Kind, &item.Model, &owner, &item.OwnerEmail, &item.Public, &item.RecordDeleted, &item.Disabled, &item.RecordCount, &item.CharacterCount, &item.CollectionCount); err != nil {
			failure(w, 503, "暂时无法读取管理材质")
			return
		}
		if owner.Valid {
			item.OwnerID = strconv.FormatInt(owner.Int64, 10)
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取管理材质")
		return
	}
	respond(w, 200, map[string]any{"textures": items, "total": total, "page": page, "pageSize": size})
}

func (a *Server) disableAdminTexture(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	hash := r.PathValue("hash")
	if !validHash.MatchString(hash) {
		failure(w, 404, "材质记录不存在")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err := a.disableTexture(r.Context(), tx, u.ID, hash)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "材质记录不存在")
		return
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "管理操作未完成，请重试")
		return
	}
	w.WriteHeader(204)
}

func (a *Server) disableTexture(ctx context.Context, tx *sql.Tx, actor int64, hash string) error {
	var disabled bool
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT deleted FROM textures WHERE hash=? FOR UPDATE"), hash).Scan(&disabled)
	if err != nil {
		return err
	}
	if disabled {
		return nil
	}
	_, err = tx.ExecContext(ctx, a.s.Bind("UPDATE textures SET deleted=TRUE WHERE hash=?"), hash)
	if err == nil {
		err = a.s.RecordAdminEvent(ctx, tx, actor, "texture.disable", "texture", hash)
	}
	return err
}
