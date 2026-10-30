package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
	"wishingskin/internal/store"
)

func (a *Server) registerResourceAdministration() {
	a.mux.HandleFunc("GET /api/admin/characters", a.adminCharacters)
	a.mux.HandleFunc("PATCH /api/admin/characters/{id}", a.updateAdminCharacter)
	a.mux.HandleFunc("DELETE /api/admin/characters/{id}", a.deleteAdminCharacter)
	a.mux.HandleFunc("GET /api/admin/wardrobe", a.adminWardrobe)
	a.mux.HandleFunc("PATCH /api/admin/wardrobe/{userID}/{recordID}", a.updateAdminWardrobe)
	a.mux.HandleFunc("DELETE /api/admin/wardrobe/{userID}/{recordID}", a.updateAdminWardrobe)
}

type managedResource struct {
	ID           string `json:"id"`
	OwnerID      string `json:"ownerId"`
	OwnerEmail   string `json:"ownerEmail"`
	Name         string `json:"name"`
	Kind         string `json:"kind,omitempty"`
	Model        string `json:"model"`
	UUID         string `json:"uuid,omitempty"`
	SkinHash     string `json:"skinHash,omitempty"`
	CapeHash     string `json:"capeHash,omitempty"`
	RecordID     string `json:"recordId,omitempty"`
	FileHash     string `json:"fileHash,omitempty"`
	RecordPublic bool   `json:"recordPublic,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
}

func resourceQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 255 {
		failure(w, 400, "搜索内容不能超过 255 个字符")
		return "", false
	}
	return query, true
}

func (a *Server) adminCharacters(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	page, size, ok := adminPagination(w, r)
	if !ok {
		return
	}
	query, ok := resourceQuery(w, r)
	if !ok {
		return
	}
	where := ""
	args := []any{}
	if query != "" {
		id, _ := strconv.ParseInt(query, 10, 64)
		where = " WHERE c.id=? OR c.user_id=? OR c.uuid=? OR POSITION(? IN LOWER(c.name))>0 OR POSITION(? IN LOWER(u.email))>0"
		args = append(args, id, id, strings.ReplaceAll(strings.ToLower(query), "-", ""), strings.ToLower(query), strings.ToLower(query))
	}
	from := " FROM characters c JOIN users u ON u.id=c.user_id"
	var total int64
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*)"+from+where), args...).Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取管理角色")
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT c.id,c.user_id,u.email,c.name,c.uuid,c.skin_hash,c.cape_hash,c.model"+from+where+" ORDER BY c.id LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取管理角色")
		return
	}
	defer rows.Close()
	items := []managedResource{}
	for rows.Next() {
		var item managedResource
		var id, owner int64
		if err := rows.Scan(&id, &owner, &item.OwnerEmail, &item.Name, &item.UUID, &item.SkinHash, &item.CapeHash, &item.Model); err != nil {
			failure(w, 503, "暂时无法读取管理角色")
			return
		}
		item.ID = strconv.FormatInt(id, 10)
		item.OwnerID = strconv.FormatInt(owner, 10)
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取管理角色")
		return
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (a *Server) updateAdminCharacter(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Name                 *string
		ClearSkin, ClearCape bool
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Name != nil {
		*body.Name = strings.TrimSpace(*body.Name)
		if !characterNamePattern.MatchString(*body.Name) {
			failure(w, 400, "角色名须为 3–16 位英文字母、数字或下划线")
			return
		}
	}
	if body.Name == nil && !body.ClearSkin && !body.ClearCape {
		failure(w, 400, "请选择角色变更")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	var name, skin, cape string
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT name,skin_hash,cape_hash FROM characters WHERE id=? FOR UPDATE"), id).Scan(&name, &skin, &cape)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "角色不存在")
		return
	}
	if err != nil {
		failure(w, 503, "管理操作暂不可用")
		return
	}
	oldName, oldSkin, oldCape := name, skin, cape
	var actions []string
	if body.Name != nil && *body.Name != name {
		name = *body.Name
		actions = append(actions, "character.rename")
	}
	if body.ClearSkin && skin != "" {
		skin = ""
		actions = append(actions, "character.clear_skin")
	}
	if body.ClearCape && cape != "" {
		cape = ""
		actions = append(actions, "character.clear_cape")
	}
	if len(actions) == 0 {
		w.WriteHeader(204)
		return
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE characters SET name=?,name_key=?,skin_hash=?,cape_hash=?,model=CASE WHEN ? THEN 'default' ELSE model END WHERE id=?"), name, strings.ToLower(name), skin, cape, body.ClearSkin, id)
	if uniqueViolation(err) {
		failure(w, 409, "这个角色名已被使用")
		return
	}
	for _, action := range actions {
		if err != nil {
			break
		}
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, action, "character", strconv.FormatInt(id, 10), map[string]store.AdminChange{
			"character.rename":     {Field: "name", Before: oldName, After: name},
			"character.clear_skin": {Field: "skin", Before: oldSkin, After: skin},
			"character.clear_cape": {Field: "cape", Before: oldCape, After: cape},
		}[action])
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

func (a *Server) deleteAdminCharacter(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		ConfirmUUID string `json:"confirmUUID"`
	}
	if !decode(w, r, &body) {
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	var owner int64
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT user_id FROM characters WHERE id=?"), id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "角色不存在")
		return
	}
	// Game writes lock the owner before tokens and profiles. Match that order here.
	if err == nil {
		err = lockUser(r.Context(), tx, a.s.Bind("SELECT id FROM users WHERE id=? FOR UPDATE"), owner)
	}
	var uuid string
	if err == nil {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT uuid FROM characters WHERE id=? FOR UPDATE"), id).Scan(&uuid)
	}
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "角色不存在")
		return
	}
	if err != nil {
		failure(w, 503, "管理操作暂不可用")
		return
	}
	if body.ConfirmUUID != uuid {
		failure(w, 409, "角色 UUID 不匹配，请刷新后确认")
		return
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM game_tokens WHERE user_id=? AND profile_uuid=?"), owner, uuid)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM characters WHERE id=?"), id)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "character.delete", "character", strconv.FormatInt(id, 10)+":"+uuid)
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

func (a *Server) adminWardrobe(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	page, size, ok := adminPagination(w, r)
	if !ok {
		return
	}
	query, ok := resourceQuery(w, r)
	if !ok {
		return
	}
	where := ""
	args := []any{}
	if query != "" {
		id, _ := strconv.ParseInt(query, 10, 64)
		where = " WHERE w.user_id=? OR w.record_id=? OR r.file_hash=? OR POSITION(? IN w.display_name)>0 OR POSITION(? IN LOWER(u.email))>0"
		args = append(args, id, query, strings.ToLower(query), query, strings.ToLower(query))
	}
	from := " FROM wardrobe w JOIN users u ON u.id=w.user_id JOIN texture_records r ON r.record_id=w.record_id JOIN textures f ON f.hash=r.file_hash"
	var total int64
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*)"+from+where), args...).Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取管理衣柜")
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT w.user_id,u.email,w.display_name,w.record_id,r.file_hash,r.is_public,(r.deleted OR f.deleted),r.kind,r.model"+from+where+" ORDER BY w.user_id,w.record_id LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取管理衣柜")
		return
	}
	defer rows.Close()
	items := []managedResource{}
	for rows.Next() {
		var item managedResource
		var owner int64
		if err := rows.Scan(&owner, &item.OwnerEmail, &item.Name, &item.RecordID, &item.FileHash, &item.RecordPublic, &item.Disabled, &item.Kind, &item.Model); err != nil {
			failure(w, 503, "暂时无法读取管理衣柜")
			return
		}
		item.OwnerID = strconv.FormatInt(owner, 10)
		item.ID = item.OwnerID + ":" + item.RecordID
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取管理衣柜")
		return
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (a *Server) updateAdminWardrobe(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	owner, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	record := r.PathValue("recordID")
	if err != nil || owner < 1 || !recordIDPattern.MatchString(record) {
		failure(w, 400, "衣柜对象无效")
		return
	}
	var body struct{ Name string }
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
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	// Match user wardrobe writes: serialize on the record before its collection row.
	var locked string
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT record_id FROM texture_records WHERE record_id=? FOR UPDATE"), record).Scan(&locked)
	var name string
	if err == nil {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT display_name FROM wardrobe WHERE user_id=? AND record_id=? FOR UPDATE"), owner, record).Scan(&name)
	}
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "衣柜中没有这份材质")
		return
	}
	if err != nil {
		failure(w, 503, "管理操作暂不可用")
		return
	}
	action := "wardrobe.rename"
	if r.Method == http.MethodDelete {
		action = "wardrobe.remove"
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM wardrobe WHERE user_id=? AND record_id=?"), owner, record)
	} else {
		if name == body.Name {
			w.WriteHeader(204)
			return
		}
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE wardrobe SET display_name=? WHERE user_id=? AND record_id=?"), body.Name, owner, record)
	}
	if err == nil {
		a.auditChange(r.Context(), "name", name, func() string {
			if r.Method == http.MethodDelete {
				return ""
			}
			return body.Name
		}())
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, action, "wardrobe", strconv.FormatInt(owner, 10)+":"+record)
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
