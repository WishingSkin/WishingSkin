package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"github.com/WishingSkin/WishingSkin/internal/securebox"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

var extensionEvents = []string{"character.create", "character.rename", "character.delete", "texture.upload", "minecraft.link", "minecraft.unlink"}

type webhookSubscription struct {
	ID       string   `json:"id"`
	PluginID string   `json:"pluginId"`
	URL      string   `json:"url"`
	Events   []string `json:"events"`
	Enabled  bool     `json:"enabled"`
}
type extensionEvent struct {
	ID        string    `json:"id"`
	CreatedAt int64     `json:"createdAt"`
	Event     sdk.Event `json:"event"`
}

func (a *Server) registerWebhooks() {
	a.mux.HandleFunc("GET /api/admin/webhooks", a.listWebhooks)
	a.mux.HandleFunc("POST /api/admin/webhooks", a.saveWebhook)
	a.mux.HandleFunc("PUT /api/admin/webhooks/{id}", a.saveWebhook)
	a.mux.HandleFunc("DELETE /api/admin/webhooks/{id}", a.deleteWebhook)
	a.mux.HandleFunc("POST /api/admin/webhook-deliveries/{id}/retry", a.retryWebhook)
}
func (a *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	rows, err := a.s.DB.QueryContext(r.Context(), "SELECT id,plugin_id,url,events_json,enabled FROM webhook_subscriptions ORDER BY id")
	if err != nil {
		failure(w, 503, "暂时无法读取 Webhook")
		return
	}
	subscriptions := []webhookSubscription{}
	for rows.Next() {
		var v webhookSubscription
		var raw string
		if err = rows.Scan(&v.ID, &v.PluginID, &v.URL, &raw, &v.Enabled); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &v.Events); err != nil {
			break
		}
		subscriptions = append(subscriptions, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		failure(w, 503, "暂时无法读取 Webhook")
		return
	}
	rows, err = a.s.DB.QueryContext(r.Context(), "SELECT id,subscription_id,attempts,state,status,created_at,next_at FROM webhook_deliveries ORDER BY created_at DESC,id LIMIT 50")
	if err != nil {
		failure(w, 503, "暂时无法读取投递记录")
		return
	}
	defer rows.Close()
	deliveries := []map[string]any{}
	for rows.Next() {
		var id, sub, state string
		var attempts, status int
		var created, next int64
		if err = rows.Scan(&id, &sub, &attempts, &state, &status, &created, &next); err != nil {
			break
		}
		deliveries = append(deliveries, map[string]any{"id": id, "subscriptionId": sub, "attempts": attempts, "state": state, "status": status, "createdAt": created, "nextAt": next})
	}
	if err != nil || rows.Err() != nil {
		failure(w, 503, "暂时无法读取投递记录")
		return
	}
	respond(w, 200, map[string]any{"subscriptions": subscriptions, "deliveries": deliveries, "events": extensionEvents})
}
func (a *Server) saveWebhook(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		webhookSubscription
		RotateSecret bool `json:"rotateSecret"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !validExtensionURL(body.URL) || len(body.Events) == 0 || len(body.Events) > len(extensionEvents) {
		failure(w, 400, "请填写有效地址和事件")
		return
	}
	seen := map[string]bool{}
	for _, event := range body.Events {
		if !slices.Contains(extensionEvents, event) || seen[event] {
			failure(w, 400, "事件无效")
			return
		}
		seen[event] = true
	}
	if body.PluginID != "" {
		state, exists := a.plugins.State(body.PluginID)
		if !exists || !slices.Contains(state.Manifest.Permissions, "webhooks") {
			failure(w, 400, "插件未声明 Webhook 权限")
			return
		}
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	var sealed, plain string
	var err error
	if id == "" {
		var count int
		err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM webhook_subscriptions").Scan(&count)
		if err != nil || count >= 32 {
			failure(w, 409, "Webhook 数量已达上限")
			return
		}
		id, err = secret()
	} else {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT secret_json FROM webhook_subscriptions WHERE id=?"), id).Scan(&sealed)
	}
	if err != nil {
		failure(w, 404, "Webhook 不存在")
		return
	}
	if sealed == "" || body.RotateSecret {
		plain, err = secret()
		if err == nil {
			sealed, err = securebox.Seal(a.cfg.SettingsKey, plain, "wishingskin:webhook:"+id)
		}
	}
	raw, _ := json.Marshal(body.Events)
	if err == nil {
		query := "INSERT INTO webhook_subscriptions(id,plugin_id,url,events_json,secret_json,enabled) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET plugin_id=EXCLUDED.plugin_id,url=EXCLUDED.url,events_json=EXCLUDED.events_json,secret_json=EXCLUDED.secret_json,enabled=EXCLUDED.enabled"
		if a.s.Dialect == "mysql" {
			query = "INSERT INTO webhook_subscriptions(id,plugin_id,url,events_json,secret_json,enabled) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE plugin_id=VALUES(plugin_id),url=VALUES(url),events_json=VALUES(events_json),secret_json=VALUES(secret_json),enabled=VALUES(enabled)"
		}
		_, err = tx.ExecContext(r.Context(), a.s.Bind(query), id, body.PluginID, body.URL, string(raw), sealed, body.Enabled)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "webhook.save", "webhook", id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "Webhook 保存失败")
		return
	}
	respond(w, 200, map[string]any{"id": id, "secret": plain})
}
func (a *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	_, err := tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM webhook_subscriptions WHERE id=?"), id)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM webhook_deliveries WHERE subscription_id=?"), id)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "webhook.delete", "webhook", id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "Webhook 删除失败")
		return
	}
	respond(w, 200, map[string]bool{"deleted": true})
}
func (a *Server) retryWebhook(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	result, err := a.s.DB.ExecContext(r.Context(), a.s.Bind("UPDATE webhook_deliveries SET attempts=0,state='pending',next_at=? WHERE id=? AND state='failed'"), time.Now().Unix(), r.PathValue("id"))
	if err != nil {
		failure(w, 503, "重试失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		failure(w, 409, "该投递记录无需重试")
		return
	}
	respond(w, 200, map[string]bool{"queued": true})
}
func (a *Server) queueExtensionEvent(ctx context.Context, event sdk.Event) error {
	id, err := secret()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	payload, err := json.Marshal(extensionEvent{ID: id, CreatedAt: now, Event: event})
	if err != nil {
		return err
	}
	tx, err := a.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id,events_json FROM webhook_subscriptions WHERE enabled=TRUE FOR UPDATE")
	if err != nil {
		return err
	}
	subs := []string{}
	for rows.Next() {
		var sub, raw string
		if err = rows.Scan(&sub, &raw); err != nil {
			break
		}
		var events []string
		if json.Unmarshal([]byte(raw), &events) == nil && slices.Contains(events, event.Name) {
			subs = append(subs, sub)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, sub := range subs {
		delivery, e := secret()
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO webhook_deliveries(id,subscription_id,payload_json,attempts,next_at,state,status,created_at) VALUES(?,?,?,0,?,'pending',0,?)"), delivery, sub, string(payload), now, now); err != nil {
			return err
		}
	}
	for _, item := range a.dynamic.Items() {
		pluginID := item.Package.Manifest.ID
		if !slices.Contains(item.Package.Manifest.Events, event.Name) {
			continue
		}
		on, e := a.readCommunityEnabled(ctx, tx, pluginID)
		if e != nil {
			return e
		}
		if !on {
			continue
		}
		delivery, e := secret()
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO plugin_event_deliveries(id,plugin_id,payload_json,attempts,next_at,state,created_at) VALUES(?,?,?,0,?,'pending',?)"), delivery, pluginID, string(payload), now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *Server) webhookPluginEnabled(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return true, nil
	}
	state, ok := a.plugins.State(id)
	if !ok || !slices.Contains(state.Manifest.Permissions, "webhooks") {
		return false, nil
	}
	return a.communityEnabled(ctx, id)
}

func (a *Server) deliveryTargets(ctx context.Context, dynamic bool) ([]string, error) {
	var targets []string
	if dynamic {
		for _, item := range a.dynamic.Items() {
			if len(item.Package.Manifest.Events) == 0 {
				continue
			}
			id := item.Package.Manifest.ID
			if a.availableCommunity(ctx, id) {
				targets = append(targets, id)
			}
		}
		return targets, nil
	}
	rows, err := a.s.DB.QueryContext(ctx, "SELECT id,plugin_id FROM webhook_subscriptions WHERE enabled=TRUE")
	if err != nil {
		return nil, err
	}
	type subscription struct{ id, plugin string }
	var subscriptions []subscription
	for rows.Next() {
		var sub subscription
		if err = rows.Scan(&sub.id, &sub.plugin); err != nil {
			rows.Close()
			return nil, err
		}
		subscriptions = append(subscriptions, sub)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, sub := range subscriptions {
		if enabled, err := a.webhookPluginEnabled(ctx, sub.plugin); err == nil && enabled {
			targets = append(targets, sub.id)
		}
	}
	return targets, nil
}

func (a *Server) runExtensionDeliveries(ctx context.Context, now time.Time) {
	release, err := a.s.LeasePluginTask(ctx, "wishingskin.events", "dispatch")
	if err != nil {
		return
	}
	defer release()
	for _, dynamic := range []bool{false, true} {
		table, target := "webhook_deliveries", "subscription_id"
		if dynamic {
			table, target = "plugin_event_deliveries", "plugin_id"
		}
		_, _ = a.s.DB.ExecContext(ctx, a.s.Bind("DELETE FROM "+table+" WHERE state<>'pending' AND created_at<?"), now.Add(-30*24*time.Hour).Unix())
		targets, err := a.deliveryTargets(ctx, dynamic)
		if err != nil || len(targets) == 0 {
			continue
		}
		args := []any{now.Unix()}
		for _, id := range targets {
			args = append(args, id)
		}
		placeholders := strings.Repeat("?,", len(targets)-1) + "?"
		rows, err := a.s.DB.QueryContext(ctx, a.s.Bind("SELECT id,"+target+",payload_json,attempts FROM "+table+" WHERE state='pending' AND next_at<=? AND "+target+" IN ("+placeholders+") ORDER BY created_at,id LIMIT 4"), args...)
		if err != nil {
			continue
		}
		type delivery struct {
			id, target, payload string
			attempts            int
		}
		batch := []delivery{}
		for rows.Next() {
			var d delivery
			if rows.Scan(&d.id, &d.target, &d.payload, &d.attempts) == nil {
				batch = append(batch, d)
			}
		}
		rows.Close()
		for _, d := range batch {
			if ctx.Err() != nil {
				return
			}
			status := 0
			var sendErr error
			if dynamic {
				var event extensionEvent
				sendErr = json.Unmarshal([]byte(d.payload), &event)
				if sendErr == nil {
					eventCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_, sendErr = a.callDynamic(eventCtx, d.target, sdk.RuntimeRequest{Kind: "event", ID: event.ID, Event: &event.Event})
					cancel()
				}
			} else {
				status, sendErr = a.sendWebhook(ctx, d.target, d.id, d.payload)
			}
			if errors.Is(sendErr, errPluginUnavailable) {
				continue
			}
			state := "pending"
			attempts := d.attempts + 1
			if sendErr == nil {
				state = "delivered"
			} else if attempts >= 5 {
				state = "failed"
			}
			next := now.Add(time.Duration(1<<min(attempts, 8)) * time.Minute).Unix()
			query := "UPDATE " + table + " SET attempts=?,state=?,next_at=?"
			args := []any{attempts, state, next}
			if !dynamic {
				query += ",status=?"
				args = append(args, status)
			}
			query += " WHERE id=?"
			args = append(args, d.id)
			_, _ = a.s.DB.ExecContext(ctx, a.s.Bind(query), args...)
		}
	}
}
func (a *Server) sendWebhook(ctx context.Context, sub, id, payload string) (int, error) {
	var endpoint, sealed, pluginID string
	var enabled bool
	err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT url,secret_json,plugin_id,enabled FROM webhook_subscriptions WHERE id=?"), sub).Scan(&endpoint, &sealed, &pluginID, &enabled)
	if err != nil {
		return 0, err
	}
	if !enabled {
		return 0, errPluginUnavailable
	}
	on, err := a.webhookPluginEnabled(ctx, pluginID)
	if err != nil {
		return 0, err
	}
	if !on {
		return 0, errPluginUnavailable
	}
	key, err := securebox.Open(a.cfg.SettingsKey, sealed, "wishingskin:webhook:"+sub)
	if err != nil {
		return 0, err
	}
	if !validExtensionURL(endpoint) {
		return 0, errors.New("invalid webhook URL")
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = io.WriteString(mac, stamp+"."+id+"."+payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBufferString(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WishingSkin-Delivery", id)
	req.Header.Set("X-WishingSkin-Timestamp", stamp)
	req.Header.Set("X-WishingSkin-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	client := newSafeExtensionClient(5 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, errors.New("webhook rejected")
	}
	return resp.StatusCode, nil
}
