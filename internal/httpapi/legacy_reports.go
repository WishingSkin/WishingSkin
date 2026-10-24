package httpapi

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type legacyReportItem struct {
	ID         string  `json:"id"`
	TextureID  string  `json:"textureId"`
	UploaderID string  `json:"uploaderId"`
	ReporterID string  `json:"reporterId"`
	Reason     string  `json:"reason"`
	Status     int     `json:"status"`
	ReportAt   *string `json:"reportAt"`
}

func (a *Server) listLegacyReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	page, size, ok := adminPagination(w, r)
	if !ok {
		return
	}
	where := ""
	args := []any{}
	if status := r.URL.Query().Get("status"); status != "" {
		if status != "0" && status != "1" && status != "2" {
			failure(w, 400, "举报状态无效")
			return
		}
		value, _ := strconv.Atoi(status)
		where = " WHERE status=?"
		args = append(args, value)
	}
	if query := strings.TrimSpace(r.URL.Query().Get("q")); query != "" {
		if utf8.RuneCountInString(query) > 255 {
			failure(w, 400, "搜索内容不能超过 255 个字符")
			return
		}
		where += " AND (POSITION(? IN reason)>0"
		args = append(args, query)
		if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
			where += " OR id=? OR tid=? OR uploader=? OR reporter=?"
			args = append(args, id, id, id, id)
		}
		where += ")"
	}
	var total int
	if err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM legacy_reports"+where), args...).Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取历史举报")
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), a.s.Bind("SELECT id,tid,uploader,reporter,reason,status,report_at FROM legacy_reports"+where+" ORDER BY id DESC LIMIT ? OFFSET ?"), append(args, size, (page-1)*size)...)
	if err != nil {
		failure(w, 503, "暂时无法读取历史举报")
		return
	}
	defer rows.Close()
	items := []legacyReportItem{}
	for rows.Next() {
		var item legacyReportItem
		var id, tid, uploader, reporter int64
		var at sql.NullString
		if err := rows.Scan(&id, &tid, &uploader, &reporter, &item.Reason, &item.Status, &at); err != nil {
			failure(w, 503, "暂时无法读取历史举报")
			return
		}
		item.ID, item.TextureID = strconv.FormatInt(id, 10), strconv.FormatInt(tid, 10)
		item.UploaderID, item.ReporterID = strconv.FormatInt(uploader, 10), strconv.FormatInt(reporter, 10)
		if at.Valid {
			item.ReportAt = &at.String
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		failure(w, 503, "暂时无法读取历史举报")
		return
	}
	respond(w, 200, map[string]any{"reports": items, "total": total, "page": page, "pageSize": size})
}
