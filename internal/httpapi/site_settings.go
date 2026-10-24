package httpapi

import (
	"context"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

type siteBranding struct {
	Name         string `json:"name"`
	Tagline      string `json:"tagline"`
	Footer       string `json:"footer"`
	Notice       string `json:"notice"`
	NoticeFormat string `json:"noticeFormat,omitempty"`
}

func (a *Server) siteBranding(ctx context.Context) (siteBranding, error) {
	brand := siteBranding{Name: "WishingSkin"}
	rows, err := a.s.DB.QueryContext(ctx, "SELECT setting_key,value FROM settings WHERE setting_key IN ('site_name','site_tagline','site_footer','site_notice','site_notice_format')")
	if err != nil {
		return brand, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return brand, err
		}
		switch key {
		case "site_name":
			if value != "" {
				brand.Name = value
			}
		case "site_tagline":
			brand.Tagline = value
		case "site_footer":
			brand.Footer = value
		case "site_notice_format":
			brand.NoticeFormat = value
		case "site_notice":
			brand.Notice = value
		}
	}
	return brand, rows.Err()
}

func (a *Server) updateSiteBranding(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body siteBranding
	if !decode(w, r, &body) {
		return
	}
	if body.NoticeFormat != "" && body.NoticeFormat != "plain" && body.NoticeFormat != "markdown" {
		failure(w, 400, "公告格式无效")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Tagline = strings.TrimSpace(body.Tagline)
	body.Footer = strings.TrimSpace(body.Footer)
	body.Notice = strings.TrimSpace(body.Notice)
	if body.Name == "" || utf8.RuneCountInString(body.Name) > 64 || strings.ContainsAny(body.Name, "\r\n") {
		failure(w, 400, "站点名称须为 1–64 个字符且不能换行")
		return
	}
	if utf8.RuneCountInString(body.Tagline) > 200 || strings.ContainsAny(body.Tagline, "\r\n") {
		failure(w, 400, "站点简介最多 200 个字符且不能换行")
		return
	}
	if utf8.RuneCountInString(body.Footer) > 200 || strings.ContainsAny(body.Footer, "\r\n") {
		failure(w, 400, "页脚信息最多 200 个字符且不能换行")
		return
	}
	if utf8.RuneCountInString(body.Notice) > 1000 {
		failure(w, 400, "站点公告最多 1000 个字符")
		return
	}
	for _, value := range []string{body.Name, body.Tagline, body.Footer, body.Notice} {
		for _, char := range value {
			if unicode.IsControl(char) && char != '\n' && char != '\r' && char != '\t' {
				failure(w, 400, "站点文案不能包含控制字符")
				return
			}
		}
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	query := "INSERT INTO settings(setting_key,value) VALUES(?,?) ON CONFLICT(setting_key) DO UPDATE SET value=EXCLUDED.value"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO settings(setting_key,value) VALUES(?,?) ON DUPLICATE KEY UPDATE value=VALUES(value)"
	}
	err := a.saveNoticeHistory(r.Context(), tx, body)
	for _, field := range []struct{ key, value string }{{"site_name", body.Name}, {"site_tagline", body.Tagline}, {"site_footer", body.Footer}, {"site_notice", body.Notice}, {"site_notice_format", body.NoticeFormat}} {
		if err != nil {
			break
		}
		if _, err = tx.ExecContext(r.Context(), a.s.Bind(query), field.key, field.value); err != nil {
			break
		}
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.branding", "site", "branding")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存站点设置")
		return
	}
	respond(w, 200, body)
}
