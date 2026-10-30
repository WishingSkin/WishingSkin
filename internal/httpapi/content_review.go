package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Missing metadata denotes an existing, approved record.
const approvedRecordSQL = "NOT EXISTS (SELECT 1 FROM texture_metadata review_meta WHERE review_meta.record_id=r.record_id AND review_meta.review_state<>'approved')"
const availableTextureSQL = "(EXISTS (SELECT 1 FROM texture_records r WHERE r.file_hash=f.hash AND r.deleted=FALSE AND " + approvedRecordSQL + ") OR (EXISTS (SELECT 1 FROM characters c WHERE c.skin_hash=f.hash OR c.cape_hash=f.hash) AND NOT EXISTS (SELECT 1 FROM texture_records r JOIN texture_metadata m ON m.record_id=r.record_id WHERE r.file_hash=f.hash AND m.review_state<>'approved')))"

type contentPolicy struct {
	ReviewUploads bool     `json:"reviewUploads"`
	Categories    []string `json:"categories"`
}

func readContentPolicy(ctx context.Context, reader settingReader) (contentPolicy, error) {
	v := contentPolicy{Categories: []string{}}
	var raw string
	err := reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='content_policy'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return v, err
}
func (a *Server) registerContentReview() {
	a.mux.HandleFunc("GET /api/content-policy", func(w http.ResponseWriter, r *http.Request) {
		v, err := readContentPolicy(r.Context(), a.s.DB)
		if err != nil {
			failure(w, 503, "暂时无法读取内容设置")
			return
		}
		respond(w, 200, v)
	})
	a.mux.HandleFunc("PATCH /api/admin/content-policy", a.updateContentPolicy)
	a.mux.HandleFunc("GET /api/admin/review", a.reviewQueue)
	a.mux.HandleFunc("PATCH /api/admin/review/{id}", a.reviewTexture)
	a.mux.HandleFunc("GET /api/admin/review/{id}/image", a.reviewImage)
}
func (a *Server) updateContentPolicy(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var v contentPolicy
	if !decode(w, r, &v) {
		return
	}
	if len(v.Categories) > 50 {
		failure(w, 400, "分类不能超过 50 个")
		return
	}
	seen := map[string]bool{}
	for i, c := range v.Categories {
		c = strings.TrimSpace(c)
		if c == "" || utf8.RuneCountInString(c) > 32 || strings.ContainsAny(c, "\r\n") || seen[c] {
			failure(w, 400, "分类名称无效或重复")
			return
		}
		v.Categories[i] = c
		seen[c] = true
	}
	if v.Categories == nil {
		v.Categories = []string{}
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err := a.putSetting(r.Context(), tx, "content_policy", v)
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.content", "site", "content")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存内容设置")
		return
	}
	respond(w, 200, v)
}

var tagPattern = regexp.MustCompile(`^[\pL\pN][\pL\pN _-]{0,23}$`)

func normalizeTextureTags(raw []string) ([]string, error) {
	if len(raw) > 10 {
		return nil, errors.New("too many tags")
	}
	tags := []string{}
	for _, tag := range raw {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if !tagPattern.MatchString(tag) {
			return nil, errors.New("invalid tag")
		}
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}
func (a *Server) putTextureMetadata(ctx context.Context, tx *sql.Tx, id, category string, tags []string, state, reason string) error {
	raw, _ := json.Marshal(tags)
	q := "INSERT INTO texture_metadata(record_id,category,tags,review_state,review_reason) VALUES(?,?,?,?,?) ON CONFLICT(record_id) DO UPDATE SET category=EXCLUDED.category,tags=EXCLUDED.tags,review_state=EXCLUDED.review_state,review_reason=EXCLUDED.review_reason"
	if a.s.Dialect == "mysql" {
		q = "INSERT INTO texture_metadata(record_id,category,tags,review_state,review_reason) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE category=VALUES(category),tags=VALUES(tags),review_state=VALUES(review_state),review_reason=VALUES(review_reason)"
	}
	_, err := tx.ExecContext(ctx, a.s.Bind(q), id, category, string(raw), state, reason)
	return err
}
func (a *Server) reviewQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	page, size, ok := adminPagination(w, r)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "pending"
	}
	if state != "pending" && state != "approved" && state != "rejected" {
		failure(w, 400, "审核状态无效")
		return
	}
	where := " WHERE r.deleted=FALSE AND f.deleted=FALSE AND COALESCE(m.review_state,'approved')=?"
	var total int
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*)"+libraryTables+where), state).Scan(&total)
	if err != nil {
		failure(w, 503, "暂时无法读取审核队列")
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT "+libraryColumns+libraryTables+where+" ORDER BY r.record_id LIMIT ? OFFSET ?"), state, size, (page-1)*size)
	if err != nil {
		failure(w, 503, "暂时无法读取审核队列")
		return
	}
	defer rows.Close()
	items := []libraryRecord{}
	for rows.Next() {
		var item libraryRecord
		if err = scanLibraryRecord(rows, &item, false); err != nil {
			break
		}
		items = append(items, item)
	}
	if err != nil || rows.Err() != nil {
		failure(w, 503, "暂时无法读取审核队列")
		return
	}
	respond(w, 200, map[string]any{"textures": items, "total": total, "page": page, "pageSize": size})
}
func (a *Server) reviewTexture(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.State != "approved" && body.State != "rejected" || utf8.RuneCountInString(body.Reason) > 200 {
		failure(w, 400, "审核决定或理由无效")
		return
	}
	id := r.PathValue("id")
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	var hash, category, tags, previousState, previousReason string
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT file_hash FROM texture_records WHERE record_id=? AND deleted=FALSE FOR UPDATE"), id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "材质不存在")
		return
	}
	if err == nil {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COALESCE(m.category,''),COALESCE(m.tags,'[]'),COALESCE(m.review_state,'approved'),COALESCE(m.review_reason,'') FROM texture_records r LEFT JOIN texture_metadata m ON m.record_id=r.record_id WHERE r.record_id=?"), id).Scan(&category, &tags, &previousState, &previousReason)
	}
	var labels []string
	if err == nil {
		err = json.Unmarshal([]byte(tags), &labels)
	}
	if err == nil {
		a.auditChange(r.Context(), "review_state", previousState, body.State)
		a.auditChange(r.Context(), "review_reason", previousReason, strings.TrimSpace(body.Reason))
		err = a.putTextureMetadata(r.Context(), tx, id, category, labels, body.State, strings.TrimSpace(body.Reason))
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "texture.review."+body.State, "texture", id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存审核结果")
		return
	}
	w.WriteHeader(204)
}
func (a *Server) reviewImage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	var hash string
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT r.file_hash FROM texture_records r JOIN textures f ON f.hash=r.file_hash WHERE r.record_id=? AND r.deleted=FALSE AND f.deleted=FALSE"), r.PathValue("id")).Scan(&hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := a.openTextureFile(r.Context(), hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	http.ServeContent(w, r, hash, time.Time{}, f)
}
