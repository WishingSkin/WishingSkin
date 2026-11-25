package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/WishingSkin/WishingSkin/internal/securebox"
)

const pointCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
const pointCodePurpose = "wishingskin:point_code:v1"

func normalizePointCode(raw string) (string, bool) {
	var code strings.Builder
	for _, char := range raw {
		if char == '-' || unicode.IsSpace(char) {
			continue
		}
		if char >= 'a' && char <= 'z' {
			char -= 'a' - 'A'
		}
		if !strings.ContainsRune(pointCodeAlphabet, char) || code.Len() == 20 {
			return "", false
		}
		code.WriteByte(byte(char))
	}
	return code.String(), code.Len() == 20
}

type pointCode struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Points        int64  `json:"points"`
	MaxUses       int    `json:"maxUses"`
	RemainingUses int    `json:"remainingUses"`
	ExpiresAt     int64  `json:"expiresAt"`
	Note          string `json:"note"`
	Disabled      bool   `json:"disabled"`
	CreatedAt     int64  `json:"createdAt"`
	Status        string `json:"status"`
}

func (a *Server) pointCodeKeyAvailable(w http.ResponseWriter) bool {
	if len(a.cfg.SettingsKey) != 32 {
		failure(w, 503, "本地设置密钥尚不可用")
		return false
	}
	return true
}

type pointCodeReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (a *Server) readPointCodes(ctx context.Context, reader pointCodeReader, now int64, suffix string, args ...any) ([]pointCode, error) {
	rows, err := reader.QueryContext(ctx, a.s.Bind("SELECT code_hash,encrypted_code,points,max_uses,remaining_uses,expires_at,note,disabled,created_at FROM point_codes"+suffix), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []pointCode{}
	for rows.Next() {
		var item pointCode
		var sealed string
		if err := rows.Scan(&item.ID, &sealed, &item.Points, &item.MaxUses, &item.RemainingUses, &item.ExpiresAt, &item.Note, &item.Disabled, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Code, err = securebox.Open(a.cfg.SettingsKey, sealed, pointCodePurpose)
		if err != nil {
			return nil, err
		}
		switch {
		case item.Disabled:
			item.Status = "disabled"
		case item.RemainingUses <= 0:
			item.Status = "exhausted"
		case item.ExpiresAt > 0 && item.ExpiresAt <= now:
			item.Status = "expired"
		default:
			item.Status = "available"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *Server) listPointCodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok || !a.pointCodeKeyAvailable(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			failure(w, 400, "列表数量无效")
			return
		}
		limit = min(50, max(1, value))
	}
	ctx := r.Context()
	var total int
	if err := a.s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM point_codes").Scan(&total); err != nil {
		failure(w, 503, "暂时无法读取积分码")
		return
	}
	codes, err := a.readPointCodes(ctx, a.s.DB, a.pointsNow().Unix(), " ORDER BY created_at DESC,code_hash DESC LIMIT ?", limit)
	if err != nil {
		failure(w, 503, "暂时无法读取积分码")
		return
	}
	respond(w, 200, map[string]any{"codes": codes, "total": total})
}

func (a *Server) createPointCodes(w http.ResponseWriter, r *http.Request) {
	user, ok := a.adminRequest(w, r)
	if !ok || !a.pointCodeKeyAvailable(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	body := struct {
		Count   int    `json:"count"`
		Points  int64  `json:"points"`
		MaxUses int    `json:"maxUses"`
		Hours   int    `json:"hours"`
		Note    string `json:"note"`
	}{MaxUses: 1}
	if !decode(w, r, &body) {
		return
	}
	note := strings.TrimSpace(body.Note)
	if body.Count < 1 || body.Count > 100 || body.Points < 1 || body.Points > 1_000_000_000 || body.MaxUses < 1 || body.MaxUses > 10000 || body.Hours < 0 || body.Hours > 8760 || utf8.RuneCountInString(note) > 48 || strings.ContainsAny(body.Note, "\r\n") {
		failure(w, 400, "积分码数量、积分、次数、有效期或备注无效")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	ctx := r.Context()
	rules, err := readPointRules(ctx, tx, true)
	if err != nil {
		pointError(w, err)
		return
	}
	if !rules.Enabled {
		failure(w, 409, "请先启用积分与签到")
		return
	}
	now := a.pointsNow()
	var expires int64
	if body.Hours > 0 {
		expires = now.Add(time.Duration(body.Hours) * time.Hour).Unix()
	}
	codes := make([]string, 0, body.Count)
	for range body.Count {
		var raw [20]byte
		if _, err := rand.Read(raw[:]); err != nil {
			failure(w, 503, "暂时无法生成积分码")
			return
		}
		for index, value := range raw {
			raw[index] = pointCodeAlphabet[int(value)%len(pointCodeAlphabet)]
		}
		normalized := string(raw[:])
		code := strings.Join([]string{normalized[:4], normalized[4:8], normalized[8:12], normalized[12:16], normalized[16:]}, "-")
		sealed, err := securebox.Seal(a.cfg.SettingsKey, code, pointCodePurpose)
		if err == nil {
			_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO point_codes(code_hash,encrypted_code,points,max_uses,remaining_uses,expires_at,note,created_at) VALUES(?,?,?,?,?,?,?,?)"), tokenHash(normalized), sealed, body.Points, body.MaxUses, body.MaxUses, expires, note, now.Unix())
		}
		if err != nil {
			failure(w, 503, "暂时无法生成积分码")
			return
		}
		codes = append(codes, code)
	}
	a.auditJSON(ctx, "pointCode", nil, map[string]any{"count": body.Count, "points": body.Points, "maxUses": body.MaxUses, "hours": body.Hours, "note": note})
	if err := a.s.RecordAdminEvent(ctx, tx, user.ID, "points.code.create", "point_code", "batch"); err != nil || tx.Commit() != nil {
		failure(w, 503, "暂时无法保存积分码")
		return
	}
	respond(w, 201, map[string]any{"codes": codes, "points": body.Points, "maxUses": body.MaxUses, "expiresAt": expires, "note": note})
}

func (a *Server) disablePointCode(w http.ResponseWriter, r *http.Request) {
	user, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !isHexID(id, 64) {
		failure(w, 404, "积分码不存在")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	ctx := r.Context()
	var disabled bool
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT disabled FROM point_codes WHERE code_hash=? FOR UPDATE"), id).Scan(&disabled)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "积分码不存在")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, a.s.Bind("UPDATE point_codes SET disabled=TRUE WHERE code_hash=?"), id)
	}
	if err == nil {
		a.auditJSON(ctx, "pointCode.disabled", disabled, true)
		err = a.s.RecordAdminEvent(ctx, tx, user.ID, "points.code.disable", "point_code", id[:16])
	}
	if err != nil || tx.Commit() != nil {
		failure(w, 503, "暂时无法停用积分码")
		return
	}
	w.WriteHeader(204)
}

func (a *Server) exportPointCodes(w http.ResponseWriter, r *http.Request) {
	user, ok := a.adminRequest(w, r)
	if !ok || !a.pointCodeKeyAvailable(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	filter, format := r.URL.Query().Get("status"), r.URL.Query().Get("format")
	if filter == "" {
		filter = "available"
	}
	if filter != "available" && filter != "all" || format != "txt" && format != "csv" {
		failure(w, 400, "积分码导出筛选条件或格式无效")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	ctx := r.Context()
	now := a.pointsNow().Unix()
	var args []any
	where := ""
	if filter == "available" {
		where = " WHERE disabled=FALSE AND remaining_uses>0 AND (expires_at=0 OR expires_at>?)"
		args = append(args, now)
	}
	codes, err := a.readPointCodes(ctx, tx, now, where+" ORDER BY created_at DESC,code_hash DESC", args...)
	if err != nil {
		failure(w, 503, "暂时无法读取积分码")
		return
	}
	if err := a.s.RecordAdminEvent(ctx, tx, user.ID, "points.code.export", "point_code", fmt.Sprintf("status=%s,count=%d", filter, len(codes))); err != nil || tx.Commit() != nil {
		failure(w, 503, "无法记录积分码导出操作")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="wishingskin-point-codes.`+format+`"`)
	if format == "txt" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, item := range codes {
			_, _ = fmt.Fprintln(w, item.Code)
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"code", "status", "points", "remaining_uses", "max_uses", "expires_at", "note", "created_at"})
	for _, item := range codes {
		expires := ""
		if item.ExpiresAt != 0 {
			expires = strconv.FormatInt(item.ExpiresAt, 10)
		}
		_ = writer.Write([]string{item.Code, item.Status, strconv.FormatInt(item.Points, 10), strconv.Itoa(item.RemainingUses), strconv.Itoa(item.MaxUses), expires, csvCell(item.Note), strconv.FormatInt(item.CreatedAt, 10)})
	}
	writer.Flush()
}

func (a *Server) redeemPointCode(w http.ResponseWriter, r *http.Request) {
	user, token, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	if !a.validWriteRequest(w, r, token) {
		return
	}
	key := strconv.FormatInt(user.ID, 10)
	if !a.pointRedeemLimit.allowKey(key) {
		seconds := (a.pointRedeemLimit.retryAfter(key) + time.Second - 1) / time.Second
		w.Header().Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
		failure(w, 429, "兑换尝试过于频繁，请稍后再试")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	normalized, ok := normalizePointCode(body.Code)
	if !ok {
		failure(w, 404, "积分码无效")
		return
	}
	hash := tokenHash(normalized)
	ctx := r.Context()
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		pointError(w, err)
		return
	}
	defer tx.Rollback()
	if err := a.lockWebAccount(ctx, tx, user.ID, token); err != nil {
		sessionError(w, err)
		return
	}
	rules, err := readPointRules(ctx, tx, true)
	if err != nil {
		pointError(w, err)
		return
	}
	if !rules.Enabled {
		failure(w, 409, "积分与签到尚未启用")
		return
	}
	var item pointCode
	err = tx.QueryRowContext(ctx, a.s.Bind("SELECT points,remaining_uses,expires_at,note,disabled FROM point_codes WHERE code_hash=? FOR UPDATE"), hash).Scan(&item.Points, &item.RemainingUses, &item.ExpiresAt, &item.Note, &item.Disabled)
	now := a.pointsNow().Unix()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		failure(w, 404, "积分码无效")
		return
	case err != nil:
		pointError(w, err)
		return
	case item.Disabled:
		failure(w, 409, "该积分码已停用")
		return
	case item.ExpiresAt > 0 && item.ExpiresAt <= now:
		failure(w, 409, "该积分码已过期")
		return
	case item.RemainingUses <= 0:
		failure(w, 409, "该积分码已领完")
		return
	}
	var redeemed bool
	err = tx.QueryRowContext(ctx, a.s.Bind("SELECT EXISTS (SELECT 1 FROM point_code_redemptions WHERE code_hash=? AND user_id=?)"), hash, user.ID).Scan(&redeemed)
	if err == nil && redeemed {
		failure(w, 409, "你已经兑换过此积分码")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO point_code_redemptions(code_hash,user_id,redeemed_at) VALUES(?,?,?)"), hash, user.ID, now)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, a.s.Bind("UPDATE point_codes SET remaining_uses=remaining_uses-1 WHERE code_hash=?"), hash)
	}
	if err == nil {
		err = a.recordPointChange(ctx, tx, user.ID, item.Points, "redeem", hash)
	}
	var balance int64
	if err == nil {
		err = tx.QueryRowContext(ctx, a.s.Bind("SELECT score FROM users WHERE id=?"), user.ID).Scan(&balance)
	}
	var notifications []notificationDelivery
	if err == nil {
		message := fmt.Sprintf("你已获得 %d 积分。", item.Points)
		if item.Note != "" {
			message += "\n备注：" + item.Note
		}
		err = a.addNotification(ctx, tx, &notifications, user.ID, "points.redeem", "活动积分已到账", message, "/user/account/points")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		pointError(w, err)
		return
	}
	a.deliverNotifications(notifications)
	respond(w, 200, map[string]string{"points": strconv.FormatInt(item.Points, 10), "balance": strconv.FormatInt(balance, 10), "note": item.Note})
}
