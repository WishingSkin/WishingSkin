package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"
)

type pointRules struct {
	Enabled       bool  `json:"enabled"`
	DailyReward   int64 `json:"dailyReward"`
	UploadCost    int64 `json:"uploadCost"`
	CharacterCost int64 `json:"characterCost"`
}

var (
	errPointsInsufficient = errors.New("insufficient points")
	errPointsOverflow     = errors.New("points overflow")
	errPointRulesChanged  = errors.New("points rules changed")
)

func (a *Server) registerPoints() {
	a.mux.HandleFunc("GET /api/points", a.points)
	a.mux.HandleFunc("POST /api/points/checkin", a.checkin)
	a.mux.HandleFunc("PATCH /api/admin/points", a.updatePointRules)
	a.mux.HandleFunc("POST /api/points/redeem", a.redeemPointCode)
	a.mux.HandleFunc("GET /api/admin/points/codes", a.listPointCodes)
	a.mux.HandleFunc("POST /api/admin/points/codes", a.createPointCodes)
	a.mux.HandleFunc("POST /api/admin/points/codes/{id}/disable", a.disablePointCode)
	a.mux.HandleFunc("GET /api/admin/points/codes/export", a.exportPointCodes)
}

func (a *Server) lockWebAccount(ctx context.Context, tx *sql.Tx, user int64, token string) error {
	var banned bool
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT banned FROM users WHERE id=? FOR UPDATE"), user).Scan(&banned)
	if err != nil {
		return err
	}
	if banned {
		return errCredentials
	}
	return a.lockWebSession(ctx, tx, user, token)
}

func readPointRules(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, locked bool) (pointRules, error) {
	query := "SELECT value FROM settings WHERE setting_key='points_rules'"
	if locked {
		query += " FOR UPDATE"
	}
	var raw string
	var rules pointRules
	err := q.QueryRowContext(ctx, query).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &rules)
	}
	if err == nil {
		var moduleEnabled bool
		moduleErr := q.QueryRowContext(ctx, "SELECT enabled FROM plugins WHERE plugin_id='wishingskin.points'").Scan(&moduleEnabled)
		if errors.Is(moduleErr, sql.ErrNoRows) {
			moduleEnabled = true
		} else if moduleErr != nil {
			return rules, moduleErr
		}
		if !moduleEnabled {
			rules.Enabled = false
		}
	}
	if err == nil && !validPointRules(rules) {
		err = errors.New("invalid points rules")
	}
	return rules, err
}

func validPointRules(rules pointRules) bool {
	for _, value := range []int64{rules.DailyReward, rules.UploadCost, rules.CharacterCost} {
		if value < 0 || value > 1_000_000_000 {
			return false
		}
	}
	return true
}

func pointDay(now time.Time) string {
	// The confirmed daily boundary is Beijing midnight (UTC+08:00).
	return now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
}

func (a *Server) updatePointRules(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled                                *bool
		DailyReward, UploadCost, CharacterCost *int64
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Enabled == nil || body.DailyReward == nil || body.UploadCost == nil || body.CharacterCost == nil {
		failure(w, 400, "请填写全部积分规则")
		return
	}
	rules := pointRules{*body.Enabled, *body.DailyReward, *body.UploadCost, *body.CharacterCost}
	if !validPointRules(rules) {
		failure(w, 400, "积分规则须为 0–1000000000 的整数")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	old, err := readPointRules(r.Context(), tx, true)
	if err == nil && old == rules {
		w.WriteHeader(204)
		return
	}
	if err == nil {
		a.auditJSON(r.Context(), "points", map[string]any{"enabled": old.Enabled, "dailyReward": old.DailyReward, "uploadCost": old.UploadCost, "characterCost": old.CharacterCost}, map[string]any{"enabled": rules.Enabled, "dailyReward": rules.DailyReward, "uploadCost": rules.UploadCost, "characterCost": rules.CharacterCost})
		raw, _ := json.Marshal(rules)
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE settings SET value=? WHERE setting_key='points_rules'"), string(raw))
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.points", "site", "points")
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

type pointEntry struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	ReferenceID  string `json:"referenceId"`
	Note         string `json:"note,omitempty"`
	Delta        string `json:"delta"`
	BalanceAfter string `json:"balanceAfter"`
	CreatedAt    int64  `json:"createdAt"`
}

func (a *Server) points(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	page, size, ok := adminPagination(w, r)
	if !ok {
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		failure(w, 503, "暂时无法读取积分")
		return
	}
	defer tx.Rollback()
	rules, err := readPointRules(r.Context(), tx, false)
	var balance int64
	if err == nil {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT score FROM users WHERE id=?"), u.ID).Scan(&balance)
	}
	day := pointDay(a.pointsNow())
	var checked int
	if err == nil {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM point_entries WHERE user_id=? AND kind='checkin' AND reference_id=?"), u.ID, day).Scan(&checked)
	}
	if err != nil {
		failure(w, 503, "暂时无法读取积分")
		return
	}
	items, total, err := a.pointEntries(r.Context(), tx, u.ID, page, size)
	if err != nil {
		failure(w, 503, "暂时无法读取积分")
		return
	}
	respond(w, 200, map[string]any{"rules": rules, "balance": strconv.FormatInt(balance, 10), "day": day, "timeZone": "Asia/Shanghai", "checkedIn": checked > 0, "entries": items, "total": total, "page": page, "pageSize": size})
}

func (a *Server) pointEntries(ctx context.Context, tx *sql.Tx, userID int64, page, size int) ([]pointEntry, int, error) {
	var total int
	if err := tx.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM point_entries WHERE user_id=?"), userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, a.s.Bind("SELECT e.entry_id,e.kind,e.reference_id,e.delta,e.balance_after,e.created_at,COALESCE(pc.note,'') FROM point_entries e LEFT JOIN point_codes pc ON e.kind='redeem' AND pc.code_hash=e.reference_id WHERE e.user_id=? ORDER BY e.created_at DESC,e.entry_id DESC LIMIT ? OFFSET ?"), userID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []pointEntry{}
	for rows.Next() {
		var item pointEntry
		var delta, after int64
		if err := rows.Scan(&item.ID, &item.Kind, &item.ReferenceID, &delta, &after, &item.CreatedAt, &item.Note); err != nil {
			return nil, 0, err
		}
		item.Delta = strconv.FormatInt(delta, 10)
		item.BalanceAfter = strconv.FormatInt(after, 10)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// Callers lock the account before the rules row. Rules and balance stay locked until the business operation commits.
func (a *Server) chargePoints(ctx context.Context, tx *sql.Tx, user int64, kind, reference string, expected *int64) error {
	rules, err := readPointRules(ctx, tx, true)
	if err != nil {
		return err
	}
	cost := rules.UploadCost
	if kind == "character" {
		cost = rules.CharacterCost
	}
	if !rules.Enabled {
		cost = 0
	}
	if expected != nil && *expected != cost {
		return errPointRulesChanged
	}
	if cost == 0 {
		return nil
	}
	return a.recordPointChange(ctx, tx, user, -cost, kind, reference)
}

func (a *Server) recordPointChange(ctx context.Context, tx *sql.Tx, user, delta int64, kind, reference string) error {
	var balance int64
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT score FROM users WHERE id=? FOR UPDATE"), user).Scan(&balance)
	if err != nil {
		return err
	}
	if delta < 0 && balance < -delta {
		return errPointsInsufficient
	}
	if delta > 0 && balance > math.MaxInt64-delta {
		return errPointsOverflow
	}
	next := balance + delta
	id, err := randomUUID()
	if err == nil {
		_, err = tx.ExecContext(ctx, a.s.Bind("UPDATE users SET score=? WHERE id=?"), next, user)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO point_entries(entry_id,user_id,kind,reference_id,delta,balance_after,created_at) VALUES(?,?,?,?,?,?,?)"), id, user, kind, reference, delta, next, a.pointsNow().Unix())
	}
	return err
}

func pointError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPointRulesChanged):
		respond(w, 409, map[string]any{"error": "积分规则已变化，请核对更新后的费用再提交", "pointsRulesChanged": true})
	case errors.Is(err, errPointsInsufficient):
		failure(w, 402, "积分不足，无法完成操作")
	case errors.Is(err, errPointsOverflow):
		failure(w, 409, "积分余额已达上限")
	default:
		failure(w, 503, "积分操作暂不可用，请重试")
	}
}

func (a *Server) checkin(w http.ResponseWriter, r *http.Request) {
	u, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		pointError(w, err)
		return
	}
	defer tx.Rollback()
	if err = a.lockWebAccount(r.Context(), tx, u.ID, token); err != nil {
		sessionError(w, err)
		return
	}
	rules, err := readPointRules(r.Context(), tx, true)
	if err != nil {
		pointError(w, err)
		return
	}
	if !rules.Enabled {
		failure(w, 409, "积分与签到尚未启用")
		return
	}
	day := pointDay(a.pointsNow())
	var count int
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM point_entries WHERE user_id=? AND kind='checkin' AND reference_id=?"), u.ID, day).Scan(&count)
	if err == nil && count > 0 {
		failure(w, 409, "今天已经签到")
		return
	}
	if err == nil {
		err = a.recordPointChange(r.Context(), tx, u.ID, rules.DailyReward, "checkin", day)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		pointError(w, err)
		return
	}
	w.WriteHeader(204)
}
