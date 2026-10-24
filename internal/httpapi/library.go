package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type libraryRecord struct {
	textureRecord
	OwnerID         string `json:"ownerId"`
	OwnerName       string `json:"ownerName"`
	DisplayName     string `json:"displayName"`
	CollectionCount int64  `json:"collectionCount"`
}

const libraryColumns = "r.record_id,r.file_hash,r.name,r.description,r.kind,r.model,r.is_public,r.owner_id,COALESCE(u.nickname,''),(SELECT COUNT(*) FROM wardrobe c WHERE c.record_id=r.record_id)"
const libraryTables = " FROM texture_records r JOIN textures f ON f.hash=r.file_hash LEFT JOIN users u ON u.id=r.owner_id"

func scanLibraryRecord(row interface{ Scan(...any) error }, item *libraryRecord, wardrobe bool) error {
	var owner sql.NullInt64
	fields := []any{&item.ID, &item.FileHash, &item.Name, &item.Description, &item.Kind, &item.Model, &item.Public, &owner, &item.OwnerName, &item.CollectionCount}
	if wardrobe {
		fields = append(fields, &item.DisplayName)
	}
	if err := row.Scan(fields...); err != nil {
		return err
	}
	if owner.Valid {
		item.OwnerID = strconv.FormatInt(owner.Int64, 10)
	}
	return nil
}

func (a *Server) publicLibrary(w http.ResponseWriter, r *http.Request) {
	where := " WHERE r.deleted=? AND f.deleted=? AND r.is_public=?"
	args := []any{false, false, true}
	if id := r.PathValue("id"); id != "" {
		if !recordIDPattern.MatchString(id) {
			failure(w, 404, "公开材质不存在")
			return
		}
		var item libraryRecord
		err := scanLibraryRecord(a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT "+libraryColumns+libraryTables+where+" AND r.record_id=?"), append(args, id)...), &item, false)
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, 404, "公开材质不存在")
		} else if err != nil {
			failure(w, 503, "暂时无法读取公开材质")
		} else {
			respond(w, 200, item)
		}
		return
	}
	q := r.URL.Query()
	page, size := 1, 24
	for _, field := range []struct {
		key   string
		value *int
		max   int
	}{{"page", &page, 1000000}, {"pageSize", &size, 60}} {
		if raw, exists := q[field.key]; exists {
			n, err := strconv.Atoi(raw[0])
			if err != nil || n < 1 || n > field.max {
				failure(w, 400, "分页参数无效")
				return
			}
			*field.value = n
		}
	}
	if kind := q.Get("kind"); kind != "" {
		if kind != "skin" && kind != "cape" {
			failure(w, 400, "材质类型无效")
			return
		}
		where += " AND r.kind=?"
		args = append(args, kind)
	}
	if owner := q.Get("owner"); owner != "" {
		id, err := strconv.ParseInt(owner, 10, 64)
		if err != nil || id < 1 {
			failure(w, 400, "上传者无效")
			return
		}
		where += " AND r.owner_id=?"
		args = append(args, id)
	}
	if name := strings.TrimSpace(q.Get("q")); name != "" {
		if utf8.RuneCountInString(name) > 255 {
			failure(w, 400, "搜索名称不能超过 255 个字符")
			return
		}
		// POSITION treats percent and underscore as literal characters on both databases.
		where += " AND POSITION(? IN r.name)>0"
		args = append(args, name)
	}
	order := "r.name,r.record_id"
	switch q.Get("sort") {
	case "", "name":
	case "name-desc":
		order = "r.name DESC,r.record_id"
	case "collections":
		order = "(SELECT COUNT(*) FROM wardrobe c WHERE c.record_id=r.record_id) DESC,r.name,r.record_id"
	default:
		failure(w, 400, "排序方式无效")
		return
	}
	var total int64
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*)"+libraryTables+where), args...).Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取公开库")
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT "+libraryColumns+libraryTables+where+" ORDER BY "+order+" LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取公开库")
		return
	}
	defer rows.Close()
	items := []libraryRecord{}
	for rows.Next() {
		var item libraryRecord
		if err := scanLibraryRecord(rows, &item, false); err != nil {
			failure(w, 503, "暂时无法读取公开库")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取公开库")
		return
	}
	respond(w, 200, map[string]any{"textures": items, "total": total, "page": page, "pageSize": size})
}

func (a *Server) wardrobe(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT "+libraryColumns+",c.display_name"+libraryTables+" JOIN wardrobe c ON c.record_id=r.record_id WHERE c.user_id=? AND r.deleted=? AND f.deleted=? ORDER BY c.display_name,r.record_id"), u.ID, false, false)
	if err != nil {
		failure(w, 503, "暂时无法读取衣柜")
		return
	}
	defer rows.Close()
	items := []libraryRecord{}
	for rows.Next() {
		var item libraryRecord
		if err := scanLibraryRecord(rows, &item, true); err != nil {
			failure(w, 503, "暂时无法读取衣柜")
			return
		}
		item.Owned = item.OwnerID == strconv.FormatInt(u.ID, 10)
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取衣柜")
		return
	}
	respond(w, 200, map[string]any{"textures": items})
}

func (a *Server) manageWardrobe(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	id := r.PathValue("id")
	if !recordIDPattern.MatchString(id) {
		failure(w, 404, "材质记录不存在")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if r.Method == http.MethodPatch {
		if !decode(w, r, &body) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" || utf8.RuneCountInString(body.Name) > 255 {
			failure(w, 400, "衣柜名称不能为空且不能超过 255 个字符")
			return
		}
	}
	tx, err := a.s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		failure(w, 503, "暂时无法修改衣柜")
		return
	}
	defer tx.Rollback()
	var owner sql.NullInt64
	var name string
	var public, removed, disabled bool
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT r.owner_id,r.name,r.is_public,r.deleted,f.deleted FROM texture_records r JOIN textures f ON f.hash=r.file_hash WHERE r.record_id=? FOR UPDATE"), id).Scan(&owner, &name, &public, &removed, &disabled)
	if errors.Is(err, sql.ErrNoRows) && r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if errors.Is(err, sql.ErrNoRows) || (r.Method != http.MethodDelete && (removed || disabled)) {
		failure(w, 404, "材质记录不存在")
		return
	}
	if err != nil {
		failure(w, 503, "暂时无法读取材质")
		return
	}
	var collected int
	if err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM wardrobe WHERE user_id=? AND record_id=?"), u.ID, id).Scan(&collected); err != nil {
		failure(w, 503, "暂时无法读取衣柜")
		return
	}
	switch r.Method {
	case http.MethodPut:
		if !public && (!owner.Valid || owner.Int64 != u.ID) && collected == 0 {
			failure(w, 404, "找不到可收藏的材质")
			return
		}
		if collected == 0 {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO wardrobe(user_id,record_id,display_name,collected_at) VALUES(?,?,?,?)"), u.ID, id, name, time.Now().Unix())
		}
	case http.MethodPatch:
		if collected == 0 {
			failure(w, 404, "衣柜中没有这份材质")
			return
		}
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE wardrobe SET display_name=? WHERE user_id=? AND record_id=?"), body.Name, u.ID, id)
	case http.MethodDelete:
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM wardrobe WHERE user_id=? AND record_id=?"), u.ID, id)
	}
	if err != nil {
		failure(w, 503, "暂时无法修改衣柜")
		return
	}
	if tx.Commit() != nil {
		failure(w, 503, "暂时无法修改衣柜")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
