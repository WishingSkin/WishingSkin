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

func (a *Server) registerReports() {
	a.mux.HandleFunc("POST /api/reports", a.createReport)
	a.mux.HandleFunc("GET /api/reports", func(w http.ResponseWriter, r *http.Request) { a.listReports(w, r, false) })
	a.mux.HandleFunc("GET /api/admin/reports", func(w http.ResponseWriter, r *http.Request) { a.listReports(w, r, true) })
	a.mux.HandleFunc("PATCH /api/admin/reports/{id}", a.resolveReport)
	a.mux.HandleFunc("GET /api/admin/legacy-reports", a.listLegacyReports)
}

type reportItem struct {
	ID            string `json:"id"`
	RecordID      string `json:"recordId"`
	Name          string `json:"name"`
	FileHash      string `json:"fileHash"`
	Reason        string `json:"reason"`
	Status        string `json:"status"`
	Resolution    string `json:"resolution"`
	Reply         string `json:"reply"`
	CreatedAt     int64  `json:"createdAt"`
	ResolvedAt    int64  `json:"resolvedAt"`
	Disabled      bool   `json:"disabled"`
	ReporterID    string `json:"reporterId,omitempty"`
	ReporterEmail string `json:"reporterEmail,omitempty"`
	OwnerID       string `json:"ownerId,omitempty"`
	OwnerEmail    string `json:"ownerEmail,omitempty"`
}

func (a *Server) listReports(w http.ResponseWriter, r *http.Request, admin bool) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if admin && !u.IsAdmin {
		failure(w, 403, "需要管理员权限")
		return
	}
	columns := "p.report_id,p.record_id,p.texture_name,r.file_hash,p.reason,p.status,p.resolution,p.reply,p.created_at,p.resolved_at,f.deleted"
	from := " FROM reports p JOIN texture_records r ON r.record_id=p.record_id JOIN textures f ON f.hash=r.file_hash"
	if admin {
		columns += ",p.reporter_id,reporter.email,r.owner_id,COALESCE(owner.email,'')"
		from += " JOIN users reporter ON reporter.id=p.reporter_id LEFT JOIN users owner ON owner.id=r.owner_id"
	}
	page, size, ok := adminPagination(w, r)
	if !ok {
		return
	}
	where := " WHERE 1=1"
	args := []any{}
	if !admin {
		where += " AND p.reporter_id=?"
		args = append(args, u.ID)
	}
	status := r.URL.Query().Get("status")
	if status != "" {
		if status != "pending" && status != "resolved" && status != "dismissed" {
			failure(w, 400, "举报状态无效")
			return
		}
		where += " AND p.status=?"
		args = append(args, status)
	}
	if query := strings.TrimSpace(r.URL.Query().Get("q")); query != "" {
		if utf8.RuneCountInString(query) > 255 {
			failure(w, 400, "搜索内容不能超过 255 个字符")
			return
		}
		id, _ := strconv.ParseInt(query, 10, 64)
		where += " AND (POSITION(? IN p.texture_name)>0 OR POSITION(? IN p.reason)>0"
		args = append(args, query, query)
		if admin {
			where += " OR POSITION(? IN r.file_hash)>0 OR p.reporter_id=? OR r.owner_id=? OR POSITION(? IN LOWER(reporter.email))>0 OR POSITION(? IN LOWER(COALESCE(owner.email,'')))>0"
			args = append(args, strings.ToLower(query), id, id, strings.ToLower(query), strings.ToLower(query))
		}
		where += ")"
	}
	var total int
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*)"+from+where), args...).Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取举报")
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT "+columns+from+where+" ORDER BY p.created_at DESC,p.report_id DESC LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取举报")
		return
	}
	defer rows.Close()
	items := []reportItem{}
	for rows.Next() {
		var item reportItem
		var reporter int64
		var owner sql.NullInt64
		fields := []any{&item.ID, &item.RecordID, &item.Name, &item.FileHash, &item.Reason, &item.Status, &item.Resolution, &item.Reply, &item.CreatedAt, &item.ResolvedAt, &item.Disabled}
		if admin {
			fields = append(fields, &reporter, &item.ReporterEmail, &owner, &item.OwnerEmail)
		}
		if err := rows.Scan(fields...); err != nil {
			failure(w, 503, "暂时无法读取举报")
			return
		}
		if admin {
			item.ReporterID = strconv.FormatInt(reporter, 10)
			if owner.Valid {
				item.OwnerID = strconv.FormatInt(owner.Int64, 10)
			}
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取举报")
		return
	}
	respond(w, 200, map[string]any{"reports": items, "total": total, "page": page, "pageSize": size})
}

func (a *Server) createReport(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	var body struct {
		RecordID string `json:"recordId"`
		Reason   string `json:"reason"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if !recordIDPattern.MatchString(body.RecordID) {
		failure(w, 404, "材质记录不存在")
		return
	}
	if body.Reason == "" || utf8.RuneCountInString(body.Reason) > 1000 {
		failure(w, 400, "举报原因须为 1–1000 个字符")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "举报未提交，请重试")
		return
	}
	defer tx.Rollback()
	// Serialize same-account submissions, and do not admit a concurrently banned reporter.
	var banned bool
	if err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT banned FROM users WHERE id=? FOR UPDATE"), u.ID).Scan(&banned); err != nil {
		failure(w, 503, "举报未提交，请重试")
		return
	}
	if banned {
		failure(w, 401, "请先登录")
		return
	}
	var name string
	var owner sql.NullInt64
	var public, removed, disabled bool
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT r.name,r.owner_id,r.is_public,r.deleted,f.deleted FROM texture_records r JOIN textures f ON f.hash=r.file_hash WHERE r.record_id=? FOR UPDATE"), body.RecordID).Scan(&name, &owner, &public, &removed, &disabled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (removed || disabled) {
		failure(w, 404, "材质记录不存在")
		return
	}
	if err != nil {
		failure(w, 503, "举报未提交，请重试")
		return
	}
	if !public && (!owner.Valid || owner.Int64 != u.ID) {
		var collected int
		if err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM wardrobe WHERE user_id=? AND record_id=?"), u.ID, body.RecordID).Scan(&collected); err != nil {
			failure(w, 503, "举报未提交，请重试")
			return
		}
		if collected == 0 {
			failure(w, 404, "材质记录不存在")
			return
		}
	}
	id, err := randomUUID()
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO reports(report_id,reporter_id,record_id,texture_name,reason,reply,created_at) VALUES(?,?,?,?,?,'',?)"), id, u.ID, body.RecordID, name, body.Reason, time.Now().Unix())
	}
	if uniqueViolation(err) {
		failure(w, 409, "你已举报过这份材质，请在账号页查看进度")
		return
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "举报未提交，请重试")
		return
	}
	respond(w, 201, map[string]string{"id": id})
}

func (a *Server) resolveReport(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !recordIDPattern.MatchString(id) {
		failure(w, 404, "举报不存在")
		return
	}
	var body struct {
		Action string `json:"action"`
		Reply  string `json:"reply"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Reply = strings.TrimSpace(body.Reply)
	if body.Action != "dismiss" && body.Action != "disable" && body.Action != "ban" {
		failure(w, 400, "请选择举报处理方式")
		return
	}
	if utf8.RuneCountInString(body.Reply) > 1000 {
		failure(w, 400, "处理说明不能超过 1000 个字符")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	var status, record string
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT status,record_id FROM reports WHERE report_id=? FOR UPDATE"), id).Scan(&status, &record)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "举报不存在")
		return
	}
	if err != nil {
		failure(w, 503, "管理操作暂不可用")
		return
	}
	if status != "pending" {
		failure(w, 409, "举报已处理，请刷新列表")
		return
	}
	var hash string
	var owner sql.NullInt64
	// Record ownership and file identity are immutable, including after soft deletion.
	if err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT file_hash,owner_id FROM texture_records WHERE record_id=?"), record).Scan(&hash, &owner); err != nil {
		failure(w, 503, "管理操作暂不可用")
		return
	}
	if body.Action == "disable" {
		err = a.disableTexture(r.Context(), tx, u.ID, hash)
	}
	if body.Action == "ban" {
		if !owner.Valid {
			failure(w, 409, "该记录归属未知，无法封禁上传者")
			return
		}
		var banned bool
		var permission int
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT banned,permission FROM users WHERE id=? FOR UPDATE"), owner.Int64).Scan(&banned, &permission)
		if err == nil && permission >= 2 && !banned {
			var remaining bool
			remaining, err = a.hasOtherAvailableAdmin(r.Context(), tx, owner.Int64)
			if err == nil && !remaining {
				failure(w, 409, "必须保留至少一个可用管理员")
				return
			}
		}
		if err == nil && !banned {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE users SET banned=TRUE WHERE id=?"), owner.Int64)
			if err == nil {
				err = a.revokeAccountCredentials(r.Context(), tx, owner.Int64)
			}
			if err == nil {
				err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "user.ban", "user", strconv.FormatInt(owner.Int64, 10))
			}
		}
	}
	next := "resolved"
	if body.Action == "dismiss" {
		next = "dismissed"
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE reports SET status=?,resolution=?,reply=?,reviewer_id=?,resolved_at=? WHERE report_id=?"), next, body.Action, body.Reply, u.ID, time.Now().Unix(), id)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "report."+body.Action, "report", id)
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
