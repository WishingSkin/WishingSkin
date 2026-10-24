package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"time"
)

type siteNotice struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Format    string `json:"format"`
	CreatedAt int64  `json:"createdAt"`
}

type noticeQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (a *Server) noticeHistory(ctx context.Context, q noticeQuery) ([]siteNotice, error) {
	rows, err := q.QueryContext(ctx, "SELECT notice_id,content,format,created_at FROM site_notices ORDER BY created_at,notice_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []siteNotice{}
	for rows.Next() {
		var item siteNotice
		if err := rows.Scan(&item.ID, &item.Content, &item.Format, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func legacyNotice(brand siteBranding) siteNotice {
	sum := sha256.Sum256([]byte(normalizedNoticeFormat(brand.NoticeFormat) + "\n" + brand.Notice))
	return siteNotice{ID: hex.EncodeToString(sum[:]), Content: brand.Notice, Format: brand.NoticeFormat}
}

func normalizedNoticeFormat(value string) string {
	if value == "markdown" {
		return "markdown"
	}
	return "plain"
}

func (a *Server) saveNoticeHistory(ctx context.Context, tx *sql.Tx, body siteBranding) error {
	items, err := a.noticeHistory(ctx, tx)
	if err != nil {
		return err
	}
	var previous siteBranding
	for _, field := range []struct {
		key   string
		value *string
	}{{"site_notice", &previous.Notice}, {"site_notice_format", &previous.NoticeFormat}} {
		err = tx.QueryRowContext(ctx, a.s.Bind("SELECT value FROM settings WHERE setting_key=?"), field.key).Scan(field.value)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if len(items) == 0 && previous.Notice != "" {
		item := legacyNotice(previous)
		if err := a.insertNotice(ctx, tx, item); err != nil {
			return err
		}
		items = append(items, item)
	}
	if body.Notice != "" && (body.Notice != previous.Notice || normalizedNoticeFormat(body.NoticeFormat) != normalizedNoticeFormat(previous.NoticeFormat)) {
		id, err := randomUUID()
		if err != nil {
			return err
		}
		created := time.Now().UnixMilli()
		if len(items) > 0 && created <= items[len(items)-1].CreatedAt {
			created = items[len(items)-1].CreatedAt + 1
		}
		return a.insertNotice(ctx, tx, siteNotice{ID: id, Content: body.Notice, Format: body.NoticeFormat, CreatedAt: created})
	}
	return nil
}

func (a *Server) insertNotice(ctx context.Context, tx *sql.Tx, item siteNotice) error {
	_, err := tx.ExecContext(ctx, a.s.Bind("INSERT INTO site_notices(notice_id,content,format,created_at) VALUES(?,?,?,?)"), item.ID, item.Content, item.Format, item.CreatedAt)
	return err
}

func (a *Server) notices(w http.ResponseWriter, r *http.Request) {
	brand, err := a.siteBranding(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取公告")
		return
	}
	items, err := a.noticeHistory(r.Context(), a.s.DB)
	if err != nil {
		failure(w, 503, "暂时无法读取公告")
		return
	}
	if len(items) == 0 && brand.Notice != "" {
		items = append(items, legacyNotice(brand))
	}
	type publicNotice struct {
		ID        string `json:"id"`
		HTML      string `json:"html"`
		CreatedAt int64  `json:"createdAt"`
	}
	result := []publicNotice{}
	currentID := ""
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		result = append(result, publicNotice{item.ID, renderNotice(item.Content, item.Format), item.CreatedAt})
		if currentID == "" && brand.Notice != "" && item.Content == brand.Notice && normalizedNoticeFormat(item.Format) == normalizedNoticeFormat(brand.NoticeFormat) {
			currentID = item.ID
		}
	}
	respond(w, 200, map[string]any{"notices": result, "currentId": currentID})
}
