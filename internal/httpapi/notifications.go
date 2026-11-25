package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/mailer"
)

type notification struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Link      string `json:"link"`
	ReadAt    int64  `json:"readAt"`
	CreatedAt int64  `json:"createdAt"`
}

type notificationDelivery struct {
	userID  int64
	message notification
}

func (a *Server) registerNotifications() {
	a.mux.HandleFunc("GET /api/notifications", a.notifications)
	a.mux.HandleFunc("POST /api/notifications/{id}/read", a.readNotification)
	a.mux.HandleFunc("POST /api/notifications/read-all", a.readNotification)
}

func (a *Server) unreadNotifications(ctx context.Context, reader settingReader, userID int64) (int, error) {
	var count int
	err := reader.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM user_notifications WHERE user_id=? AND read_at=0"), userID).Scan(&count)
	return count, err
}

func (a *Server) notifications(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	query := r.URL.Query()
	limit := 20
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 {
			failure(w, 400, "通知分页数量无效")
			return
		}
	}
	where := " WHERE user_id=?"
	args := []any{user.ID}
	switch query.Get("unreadOnly") {
	case "true":
		where += " AND read_at=0"
	case "", "false":
	default:
		failure(w, 400, "通知筛选条件无效")
		return
	}
	if cursor := query.Get("cursor"); cursor != "" {
		created, id, found := strings.Cut(cursor, ":")
		seconds, err := strconv.ParseInt(created, 10, 64)
		if !found || err != nil || seconds < 0 || !recordIDPattern.MatchString(id) {
			failure(w, 400, "通知分页游标无效")
			return
		}
		where += " AND (created_at<? OR (created_at=? AND notification_id<?))"
		args = append(args, seconds, seconds, id)
	}
	ctx := r.Context()
	items, err := a.listNotifications(ctx, where, append(args, limit+1)...)
	if err != nil {
		failure(w, 503, "暂时无法读取通知")
		return
	}
	count, err := a.unreadNotifications(ctx, a.s.DB, user.ID)
	if err != nil {
		failure(w, 503, "暂时无法读取通知")
		return
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = strconv.FormatInt(last.CreatedAt, 10) + ":" + last.ID
	}
	respond(w, 200, map[string]any{"notifications": items, "unreadCount": count, "nextCursor": next})
}

func (a *Server) listNotifications(ctx context.Context, where string, args ...any) ([]notification, error) {
	rows, err := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT notification_id,kind,title,body,link,read_at,created_at FROM user_notifications"+where+" ORDER BY created_at DESC,notification_id DESC LIMIT ?"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []notification{}
	for rows.Next() {
		var item notification
		if err := rows.Scan(&item.ID, &item.Kind, &item.Title, &item.Body, &item.Link, &item.ReadAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, session, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, session) {
		return
	}
	id := r.PathValue("id")
	if id != "" && !recordIDPattern.MatchString(id) {
		failure(w, 404, "通知不存在")
		return
	}
	ctx := r.Context()
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法标记通知")
		return
	}
	defer tx.Rollback()
	if err := a.lockWebAccount(ctx, tx, user.ID, session); err != nil {
		sessionError(w, err)
		return
	}
	suffix := ""
	args := []any{time.Now().Unix(), user.ID}
	if id != "" {
		var existing string
		err = tx.QueryRowContext(ctx, a.s.Bind("SELECT notification_id FROM user_notifications WHERE user_id=? AND notification_id=? FOR UPDATE"), user.ID, id).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, 404, "通知不存在")
			return
		}
		suffix = " AND notification_id=?"
		args = append(args, id)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, a.s.Bind("UPDATE user_notifications SET read_at=? WHERE user_id=? AND read_at=0"+suffix), args...)
	}
	var count int
	if err == nil && id != "" {
		count, err = a.unreadNotifications(ctx, tx, user.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法标记通知")
		return
	}
	respond(w, 200, map[string]int{"unreadCount": count})
}

func (a *Server) addNotification(ctx context.Context, tx *sql.Tx, deliveries *[]notificationDelivery, userID int64, kind, title, body, link string) error {
	id, err := randomUUID()
	if err != nil {
		return err
	}
	if chars := []rune(body); len(chars) > 600 {
		body = string(chars[:599]) + "…"
	}
	created := time.Now().Unix()
	_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO user_notifications(notification_id,user_id,kind,title,body,link,created_at) VALUES(?,?,?,?,?,?,?)"), id, userID, kind, title, body, link, created)
	if err == nil {
		*deliveries = append(*deliveries, notificationDelivery{userID, notification{ID: id, Kind: kind, Title: title, Body: body, Link: link, CreatedAt: created}})
	}
	return err
}

func (a *Server) deliverNotifications(deliveries []notificationDelivery) {
	if len(deliveries) == 0 {
		return
	}
	select {
	case a.mailSlots <- struct{}{}:
	default:
		return
	}
	a.ops.jobs.Add(1)
	go func() {
		defer a.ops.jobs.Done()
		defer func() { <-a.mailSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		settings, _, err := a.serviceSettings(ctx)
		if err != nil || !settings.NotificationEmailEnabled || !settings.SMTPEnabled {
			return
		}
		sender := siteMailSender{a, &mailer.SMTP{Address: settings.Address, From: settings.From, Username: settings.Username, Password: settings.Password, TLSMode: settings.TLSMode}}
		for _, delivery := range deliveries {
			if ctx.Err() != nil {
				return
			}
			var email string
			err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT u.email FROM users u JOIN account_states s ON s.user_id=u.id WHERE u.id=? AND s.email_verified=TRUE"), delivery.userID).Scan(&email)
			if err != nil {
				continue
			}
			message := delivery.message
			link := ""
			if message.Link != "" {
				link = a.cfg.PublicURL + message.Link
			}
			_ = sender.SendNotification(ctx, email, message.Title, message.Body, link)
		}
	}()
}
