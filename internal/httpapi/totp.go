package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"

	"strings"
	"time"

	"wishingskin/internal/securebox"
)

const totpPurpose = "wishingskin:totp:v1"

var errTOTP = errors.New("invalid totp code")
var errTOTPRequired = errors.New("totp required")

type totpStatus struct {
	Enabled       bool     `json:"enabled"`
	Configured    bool     `json:"configured"`
	RecoveryCodes []string `json:"recoveryCodes,omitempty"`
}

func (a *Server) registerTOTP() {
	a.mux.HandleFunc("GET /api/account/totp", a.accountTOTP)
	a.mux.HandleFunc("POST /api/account/totp/setup", a.setupTOTP)
	a.mux.HandleFunc("POST /api/account/totp/enable", a.enableTOTP)
	a.mux.HandleFunc("POST /api/account/totp/disable", a.disableTOTP)
	a.mux.HandleFunc("POST /api/account/totp/recovery", a.regenerateTOTPRecovery)
	a.mux.HandleFunc("POST /api/auth/totp", a.completeOIDCTOTP)
}

func (a *Server) readTOTP(ctx context.Context, userID int64) (secret string, enabled bool, configured bool, err error) {
	var sealed string
	err = a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT secret_encrypted,enabled FROM user_totp WHERE user_id=?"), userID).Scan(&sealed, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	if len(a.cfg.SettingsKey) != 32 {
		return "", false, true, errors.New("settings key unavailable")
	}
	secret, err = securebox.Open(a.cfg.SettingsKey, sealed, totpPurpose)
	return secret, enabled, true, err
}

func (a *Server) accountTOTP(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	_, enabled, configured, err := a.readTOTP(r.Context(), u.ID)
	if err != nil {
		failure(w, 503, "暂时无法读取双重验证设置")
		return
	}
	respond(w, 200, totpStatus{Enabled: enabled, Configured: configured})
}

func newTOTPSecret() (string, error) {
	data := make([]byte, 20)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(data), "="), nil
}

func validTOTP(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	if strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, false
	}
	base := now.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		step := base + offset
		if step < 0 || !hmac.Equal([]byte(totpCode(secret, step)), []byte(code)) {
			continue
		}
		return step, true
	}
	return 0, false
}

func totpCode(secret string, step int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	h := hmac.New(sha1.New, key)
	_, _ = h.Write(counter[:])
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1000000)
}

func (a *Server) consumeTOTP(ctx context.Context, tx *sql.Tx, userID int64, code string) error {
	var sealed string
	var enabled bool
	var last int64
	if err := tx.QueryRowContext(ctx, a.s.Bind("SELECT secret_encrypted,enabled,last_used_step FROM user_totp WHERE user_id=? FOR UPDATE"), userID).Scan(&sealed, &enabled, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if !enabled {
		return nil
	}
	if len(a.cfg.SettingsKey) != 32 {
		return errors.New("settings key unavailable")
	}
	secret, err := securebox.Open(a.cfg.SettingsKey, sealed, totpPurpose)
	if err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return errTOTPRequired
	}
	if len(code) != 6 {
		if len(code) != 21 {
			return errTOTP
		}
		result, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM user_totp_recovery WHERE user_id=? AND code_hash=?"), userID, totpRecoveryHash(userID, code))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err == nil && n != 1 {
			return errTOTP
		}
		return err
	}
	step, ok := validTOTP(secret, code, a.totpNow())
	if !ok || step <= last {
		return errTOTP
	}
	_, err = tx.ExecContext(ctx, a.s.Bind("UPDATE user_totp SET last_used_step=? WHERE user_id=?"), step, userID)
	return err
}
