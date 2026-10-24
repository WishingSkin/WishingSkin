package httpapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (a *Server) registerAdministration() {
	a.mux.HandleFunc("GET /api/admin/users", a.adminUsers)
	a.mux.HandleFunc("PATCH /api/admin/users/{id}", a.updateAdminUser)
	a.mux.HandleFunc("GET /api/admin/events", a.adminEvents)
	a.mux.HandleFunc("GET /api/admin/events/export", a.exportAdminEvents)
	a.mux.HandleFunc("PATCH /api/admin/branding", a.updateSiteBranding)
}

func (a *Server) adminRead(w http.ResponseWriter, r *http.Request) (User, bool) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return User{}, false
	}
	if !u.IsAdmin {
		failure(w, 403, "需要管理员权限")
		return User{}, false
	}
	return u, true
}

// Serialize administration before locking actor/target users, then recheck the actor's authority.
// This also gives last-administrator checks a single order across processes.
func (a *Server) beginAdminWrite(w http.ResponseWriter, r *http.Request, u User) (*sql.Tx, bool) {
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "管理操作暂不可用")
		return nil, false
	}
	var mode string
	err = tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode)
	var permission int
	var banned bool
	if err == nil {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT permission,banned FROM users WHERE id=? FOR UPDATE"), u.ID).Scan(&permission, &banned)
	}
	if errors.Is(err, sql.ErrNoRows) || err == nil && (banned || permission < 2) {
		_ = tx.Rollback()
		failure(w, 403, "管理员权限已变更，请重新登录")
		return nil, false
	}
	if err != nil {
		_ = tx.Rollback()
		failure(w, 503, "管理操作暂不可用")
		return nil, false
	}
	return tx, true
}

func adminPagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page, size := 1, 20
	for _, f := range []struct {
		key   string
		value *int
		max   int
	}{{"page", &page, 1000000}, {"pageSize", &size, 60}} {
		if raw := r.URL.Query().Get(f.key); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > f.max {
				failure(w, 400, "分页参数无效")
				return 0, 0, false
			}
			*f.value = value
		}
	}
	return page, size, true
}

type adminUser struct {
	ID                   string `json:"id"`
	Email                string `json:"email"`
	Nickname             string `json:"nickname"`
	IsAdmin              bool   `json:"isAdmin"`
	Banned               bool   `json:"banned"`
	EmailVerified        bool   `json:"emailVerified"`
	VerificationRequired bool   `json:"verificationRequired"`
}

func (a *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
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
		id, _ := strconv.ParseInt(query, 10, 64)
		where = " WHERE u.id=? OR POSITION(? IN LOWER(u.email))>0 OR POSITION(? IN u.nickname)>0"
		args = append(args, id, strings.ToLower(query), query)
	}
	var total int64
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM users u"+where), args...).Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取用户列表")
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT u.id,u.email,u.nickname,u.permission>=2,u.banned,COALESCE(s.email_verified,FALSE),COALESCE(s.verification_required,FALSE) FROM users u LEFT JOIN account_states s ON s.user_id=u.id"+where+" ORDER BY u.id LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取用户列表")
		return
	}
	defer rows.Close()
	items := []adminUser{}
	for rows.Next() {
		var item adminUser
		var id int64
		if err := rows.Scan(&id, &item.Email, &item.Nickname, &item.IsAdmin, &item.Banned, &item.EmailVerified, &item.VerificationRequired); err != nil {
			failure(w, 503, "暂时无法读取用户列表")
			return
		}
		item.ID = strconv.FormatInt(id, 10)
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取用户列表")
		return
	}
	respond(w, 200, map[string]any{"users": items, "total": total, "page": page, "pageSize": size})
}

func (a *Server) updateAdminUser(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		failure(w, 400, "用户编号无效")
		return
	}
	var body struct{ Banned, IsAdmin *bool }
	if !decode(w, r, &body) {
		return
	}
	if body.Banned == nil && body.IsAdmin == nil {
		failure(w, 400, "请选择账号状态或权限变更")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	var banned bool
	var permission int
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT banned,permission FROM users WHERE id=? FOR UPDATE"), id).Scan(&banned, &permission)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "用户不存在")
		return
	}
	if err != nil {
		failure(w, 503, "管理操作暂不可用")
		return
	}
	nextBanned, nextPermission := banned, permission
	if body.Banned != nil {
		nextBanned = *body.Banned
	}
	if body.IsAdmin != nil {
		nextPermission = 0
		if *body.IsAdmin {
			nextPermission = 2
		}
	}
	if permission >= 2 && !banned && (nextPermission < 2 || nextBanned) {
		remaining, err := a.hasOtherAvailableAdmin(r.Context(), tx, id)
		if err != nil {
			failure(w, 503, "管理操作暂不可用")
			return
		}
		if !remaining {
			failure(w, 409, "必须保留至少一个可用管理员")
			return
		}
	}
	if nextBanned == banned && nextPermission == permission {
		w.WriteHeader(204)
		return
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE users SET banned=?,permission=? WHERE id=?"), nextBanned, nextPermission, id)
	if err == nil {
		err = a.revokeAccountCredentials(r.Context(), tx, id)
	}
	if err == nil && nextBanned != banned {
		action := "user.unban"
		if nextBanned {
			action = "user.ban"
		}
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, action, "user", strconv.FormatInt(id, 10))
	}
	if err == nil && nextPermission != permission {
		action := "user.demote"
		if nextPermission >= 2 {
			action = "user.promote"
		}
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, action, "user", strconv.FormatInt(id, 10))
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

func (a *Server) hasOtherAvailableAdmin(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM users u LEFT JOIN account_states s ON s.user_id=u.id WHERE u.id<>? AND u.permission>=2 AND u.banned=FALSE AND (COALESCE(s.verification_required,FALSE)=FALSE OR s.email_verified=TRUE)"), id).Scan(&count)
	return count > 0, err
}

type adminEvent struct {
	ID         string `json:"id"`
	ActorID    string `json:"actorId"`
	Action     string `json:"action"`
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	CreatedAt  int64  `json:"createdAt"`
}

func adminEventFilter(w http.ResponseWriter, r *http.Request) (string, []any, bool) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 255 {
		failure(w, 400, "搜索内容不能超过 255 个字符")
		return "", nil, false
	}
	if query == "" {
		return "", nil, true
	}
	where := " WHERE POSITION(? IN action)>0 OR POSITION(? IN target_type)>0 OR POSITION(? IN target_id)>0"
	args := []any{query, query, query}
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		where += " OR actor_id=?"
		args = append(args, id)
	}
	return where, args, true
}

func (a *Server) adminEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	page, size, ok := adminPagination(w, r)
	if !ok {
		return
	}
	where, args, ok := adminEventFilter(w, r)
	if !ok {
		return
	}
	var total int64
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM admin_events"+where), args...).Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取管理记录")
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT event_id,actor_id,action,target_type,target_id,created_at FROM admin_events"+where+" ORDER BY created_at DESC,event_id DESC LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取管理记录")
		return
	}
	defer rows.Close()
	items := []adminEvent{}
	for rows.Next() {
		var item adminEvent
		var actor int64
		if err := rows.Scan(&item.ID, &actor, &item.Action, &item.TargetType, &item.TargetID, &item.CreatedAt); err != nil {
			failure(w, 503, "暂时无法读取管理记录")
			return
		}
		item.ActorID = strconv.FormatInt(actor, 10)
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取管理记录")
		return
	}
	respond(w, 200, map[string]any{"events": items, "total": total, "page": page, "pageSize": size})
}

func csvCell(value string) string {
	if value != "" && strings.ContainsRune("=+-@", []rune(value)[0]) {
		return "'" + value
	}
	return value
}

func (a *Server) exportAdminEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	where, args, ok := adminEventFilter(w, r)
	if !ok {
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT event_id,actor_id,action,target_type,target_id,created_at FROM admin_events"+where+" ORDER BY created_at DESC,event_id DESC"), args...)
	if err != nil {
		failure(w, 503, "暂时无法导出管理记录")
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="wishingskin-admin-events.csv"`)
	writer := csv.NewWriter(w)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	if err := writer.Write([]string{"id", "actor_id", "action", "target_type", "target_id", "created_at"}); err != nil {
		return
	}
	for rows.Next() {
		var id, action, targetType, targetID string
		var actor, created int64
		if err := rows.Scan(&id, &actor, &action, &targetType, &targetID, &created); err != nil {
			return
		}
		if err := writer.Write([]string{csvCell(id), fmt.Sprintf("%d", actor), csvCell(action), csvCell(targetType), csvCell(targetID), fmt.Sprintf("%d", created)}); err != nil {
			return
		}
	}
	writer.Flush()
}
