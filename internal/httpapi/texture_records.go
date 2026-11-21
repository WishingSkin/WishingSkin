package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

func (a *Server) manageTextureRecord(w http.ResponseWriter, r *http.Request) {
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
		Category    *string   `json:"category"`
		Tags        *[]string `json:"tags"`
		Name        string    `json:"name"`
		Description string    `json:"description"`
		Public      *bool     `json:"isPublic"`
	}
	if r.Method == http.MethodPatch {
		if !decode(w, r, &body) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		body.Description = strings.TrimSpace(body.Description)
		if body.Name == "" || utf8.RuneCountInString(body.Name) > 255 {
			failure(w, 400, "材质名称不能为空且不能超过 255 个字符")
			return
		}
		if utf8.RuneCountInString(body.Description) > maxDescription {
			failure(w, 400, "材质描述不能超过 200 个字符")
			return
		}
		if body.Public == nil {
			failure(w, 400, "请选择公开或未公开")
			return
		}
	}
	tx, err := a.s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		failure(w, 503, "暂时无法修改材质")
		return
	}
	defer tx.Rollback()
	if r.Method == http.MethodPatch {
		var mode string
		if err := tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode); err != nil {
			failure(w, 503, "暂时无法修改材质")
			return
		}
	}
	var removed, disabled bool
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT r.deleted,f.deleted FROM texture_records r JOIN textures f ON f.hash=r.file_hash WHERE r.record_id=? AND r.owner_id=? FOR UPDATE"), id, u.ID).Scan(&removed, &disabled)
	if errors.Is(err, sql.ErrNoRows) || (r.Method == http.MethodPatch && (removed || disabled)) {
		failure(w, 404, "材质记录不存在或不可修改")
		return
	}
	if err != nil {
		failure(w, 503, "暂时无法读取材质")
		return
	}
	if r.Method == http.MethodDelete {
		// Keep shared files and existing character bindings. Raw serving checks remaining references.
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE texture_records SET deleted=? WHERE record_id=?"), true, id)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM texture_likes WHERE record_id=?"), id)
		}
	} else {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE texture_records SET name=?,description=?,is_public=? WHERE record_id=?"), body.Name, body.Description, *body.Public, id)
	}
	if err == nil && r.Method == http.MethodPatch {
		var category, raw, state, reason string
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COALESCE(m.category,''),COALESCE(m.tags,'[]'),COALESCE(m.review_state,'approved'),COALESCE(m.review_reason,'') FROM texture_records r LEFT JOIN texture_metadata m ON m.record_id=r.record_id WHERE r.record_id=?"), id).Scan(&category, &raw, &state, &reason)
		var tags []string
		if err == nil {
			err = json.Unmarshal([]byte(raw), &tags)
		}
		policy, e := readContentPolicy(r.Context(), tx)
		if e != nil {
			err = e
		}
		if body.Category != nil {
			category = strings.TrimSpace(*body.Category)
			if category != "" && !slices.Contains(policy.Categories, category) {
				failure(w, 400, "请选择有效分类")
				return
			}
		}
		if body.Tags != nil {
			tags, e = normalizeTextureTags(*body.Tags)
			if e != nil {
				failure(w, 400, "标签格式无效")
				return
			}
		}
		if policy.ReviewUploads {
			state = "pending"
			reason = ""
		}
		if err == nil {
			err = a.putTextureMetadata(r.Context(), tx, id, category, tags, state, reason)
		}
	}
	if err != nil {
		failure(w, 503, "暂时无法修改材质")
		return
	}
	if err := tx.Commit(); err != nil {
		failure(w, 503, "暂时无法修改材质")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
