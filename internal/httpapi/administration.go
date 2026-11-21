package httpapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
	"github.com/WishingSkin/WishingSkin/internal/store"
)

func (a *Server) registerAdministration() {
	a.mux.HandleFunc("GET /api/admin/overview", a.adminOverview)
	a.mux.HandleFunc("GET /api/admin/users", a.adminUsers)
	a.mux.HandleFunc("PATCH /api/admin/users/{id}", a.updateAdminUser)
	a.mux.HandleFunc("GET /api/admin/events", a.adminEvents)
	a.mux.HandleFunc("GET /api/admin/events/export", a.exportAdminEvents)
	a.mux.HandleFunc("PATCH /api/admin/branding", a.updateSiteBranding)
	a.mux.HandleFunc("GET /api/admin/email-template", a.adminMailTemplate)
	a.mux.HandleFunc("PATCH /api/admin/email-template", a.updateMailTemplate)
	a.mux.HandleFunc("POST /api/admin/email-template/preview", a.previewMailTemplate)
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
	*r = *r.WithContext(store.WithAdminChanges(r.Context()))
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

func (a *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	ctx := r.Context()
	var userTotal, characterTotal, textureTotal, pendingReviews, pendingReports int64
	if err := a.s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&userTotal); err != nil {
		failure(w, 503, "暂时无法读取站点概览")
		return
	}
	if err := a.s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM characters").Scan(&characterTotal); err != nil {
		failure(w, 503, "暂时无法读取站点概览")
		return
	}
	if err := a.s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM texture_records WHERE deleted=FALSE").Scan(&textureTotal); err != nil {
		failure(w, 503, "暂时无法读取站点概览")
		return
	}
	if err := a.s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM texture_records r JOIN texture_metadata m ON m.record_id=r.record_id WHERE r.deleted=FALSE AND m.review_state='pending'").Scan(&pendingReviews); err != nil {
		failure(w, 503, "暂时无法读取站点概览")
		return
	}
	if err := a.s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM reports WHERE status='pending'").Scan(&pendingReports); err != nil {
		failure(w, 503, "暂时无法读取站点概览")
		return
	}
	siteUsed, err := a.textureUsage(
		ctx,
		a.s.DB,
		" FROM textures t LEFT JOIN texture_file_integrity i ON i.file_hash=t.hash WHERE TRUE",
		"t.hash",
	)
	if err != nil {
		failure(w, 503, "暂时无法读取站点概览")
		return
	}
	states := a.plugins.States()
	enabledPlugins := 0
	for _, state := range states {
		on, err := a.storedPluginEnabled(ctx, state.Manifest.ID)
		if err != nil {
			failure(w, 503, "暂时无法读取站点概览")
			return
		}
		if on && state.Enabled {
			enabledPlugins++
		}
	}
	backupInfo := map[string]any{"configured": false, "count": 0}
	if dir, err := a.backupDirectory(ctx); err == nil {
		backupInfo["configured"] = true
		if entries, err := readBackupEntries(dir); err == nil {
			backupInfo["count"] = len(entries)
			if len(entries) > 0 {
				backupInfo["latestId"] = entries[0].ID
				backupInfo["latestCreatedAt"] = entries[0].CreatedAt
				backupInfo["latestState"] = entries[0].State
				backupInfo["latestUnverifiedFiles"] = entries[0].UnverifiedFiles
			}
		}
	}
	respond(w, 200, map[string]any{
		"users":          userTotal,
		"characters":     characterTotal,
		"textures":       textureTotal,
		"pendingReviews": pendingReviews,
		"pendingReports": pendingReports,
		"storage": map[string]any{
			"siteUsedBytes":     siteUsed,
			"siteLimitBytes":    a.cfg.TextureSiteLimitBytes,
			"accountLimitBytes": a.cfg.TextureAccountLimitBytes,
		},
		"plugins": map[string]any{
			"enabled": enabledPlugins,
			"total":   len(states),
		},
		"backup": backupInfo,
	})
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
	var body struct{ Banned, IsAdmin, EmailVerified *bool }
	if !decode(w, r, &body) {
		return
	}
	if body.Banned == nil && body.IsAdmin == nil && body.EmailVerified == nil {
		failure(w, 400, "请选择用户变更")
		return
	}
	if body.EmailVerified != nil && !*body.EmailVerified {
		failure(w, 400, "请选择完成邮箱验证")
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
	var verified bool
	if body.EmailVerified != nil {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COALESCE((SELECT email_verified FROM account_states WHERE user_id=?),FALSE)"), id).Scan(&verified)
		if err != nil {
			failure(w, 503, "管理操作暂不可用")
			return
		}
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
	if nextBanned == banned && nextPermission == permission && (body.EmailVerified == nil || verified) {
		w.WriteHeader(204)
		return
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE users SET banned=?,permission=? WHERE id=?"), nextBanned, nextPermission, id)
	if err == nil && (nextBanned != banned || nextPermission != permission) {
		err = a.revokeAccountCredentials(r.Context(), tx, id)
	}
	if err == nil && body.EmailVerified != nil && !verified {
		query := "INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(?,TRUE,FALSE) ON CONFLICT(user_id) DO UPDATE SET email_verified=TRUE,verification_required=FALSE"
		if a.s.Dialect == "mysql" {
			query = "INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(?,TRUE,FALSE) ON DUPLICATE KEY UPDATE email_verified=TRUE,verification_required=FALSE"
		}
		_, err = tx.ExecContext(r.Context(), a.s.Bind(query), id)
		if err == nil {
			err = a.dropOIDCSignupBinding(r.Context(), tx, id)
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM account_tokens WHERE user_id=? AND purpose='verify'"), id)
		}
		if err == nil {
			err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "user.verify_email", "user", strconv.FormatInt(id, 10), store.AdminChange{Field: "emailVerified", Before: "false", After: "true"})
		}
	}
	if err == nil && nextBanned != banned {
		action := "user.unban"
		if nextBanned {
			action = "user.ban"
		}
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, action, "user", strconv.FormatInt(id, 10), store.AdminChange{Field: "banned", Before: strconv.FormatBool(banned), After: strconv.FormatBool(nextBanned)})
	}
	if err == nil && nextPermission != permission {
		action := "user.demote"
		if nextPermission >= 2 {
			action = "user.promote"
		}
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, action, "user", strconv.FormatInt(id, 10), store.AdminChange{Field: "permission", Before: strconv.Itoa(permission), After: strconv.Itoa(nextPermission)})
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
	Changes    []store.AdminChange `json:"changes,omitempty"`
	ID         string              `json:"id"`
	ActorID    string              `json:"actorId"`
	Action     string              `json:"action"`
	TargetType string              `json:"targetType"`
	TargetID   string              `json:"targetId"`
	CreatedAt  int64               `json:"createdAt"`
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
	identifierOnly := strings.HasPrefix(query, "#")
	query = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "#")))
	if query == "" {
		return " WHERE 1=0", nil, true
	}
	where := " WHERE POSITION(? IN LOWER(target_id))>0"
	args := []any{query}
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		where += " OR actor_id=?"
		args = append(args, id)
	}
	if !identifierOnly {
		where += " OR POSITION(? IN LOWER(action))>0 OR POSITION(? IN LOWER(target_type))>0"
		args = append(args, query, query)
		for _, group := range []struct{ name, column string }{{"actions", "action"}, {"targets", "target_type"}, {"settings", "target_id"}} {
			for _, key := range adminEventLabelMatches(group.name, query) {
				if group.name == "settings" {
					where += " OR (target_type='site' AND target_id=?)"
				} else {
					where += " OR " + group.column + "=?"
				}
				args = append(args, key)
			}
		}
		if strings.Contains("本机初始化", query) || strings.Contains("local initialization", query) {
			where += " OR actor_id=0"
		}
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
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind(adminEventSelect+where+" ORDER BY created_at DESC,event_id DESC LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取管理记录")
		return
	}
	defer rows.Close()
	items := []adminEvent{}
	for rows.Next() {
		var item adminEvent
		var actor int64
		var details string
		if err := rows.Scan(&item.ID, &actor, &item.Action, &item.TargetType, &item.TargetID, &item.CreatedAt, &details); err != nil {
			failure(w, 503, "暂时无法读取管理记录")
			return
		}
		item.ActorID = strconv.FormatInt(actor, 10)
		if err := json.Unmarshal([]byte(details), &item.Changes); err != nil {
			failure(w, 503, "暂时无法读取管理记录")
			return
		}
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

const adminEventSelect = "SELECT event_id,actor_id,action,target_type,target_id,created_at,COALESCE((SELECT details_json FROM admin_event_changes c WHERE c.event_id=admin_events.event_id),'[]') FROM admin_events"

func (a *Server) exportAdminEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	where, args, ok := adminEventFilter(w, r)
	if !ok {
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind(adminEventSelect+where+" ORDER BY created_at DESC,event_id DESC"), args...)
	if err != nil {
		failure(w, 503, "暂时无法导出管理记录")
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="wishingskin-admin-events.csv"`)
	writer := csv.NewWriter(w)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	if err := writer.Write([]string{"id", "actor_id", "action", "target_type", "target_id", "created_at", "changes"}); err != nil {
		return
	}
	for rows.Next() {
		var id, action, targetType, targetID, details string
		var actor, created int64
		if err := rows.Scan(&id, &actor, &action, &targetType, &targetID, &created, &details); err != nil {
			return
		}
		if err := writer.Write([]string{csvCell(id), fmt.Sprintf("%d", actor), csvCell(action), csvCell(targetType), csvCell(targetID), fmt.Sprintf("%d", created), csvCell(details)}); err != nil {
			return
		}
	}
	writer.Flush()
}
