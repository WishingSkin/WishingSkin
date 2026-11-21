package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type oidcFlow struct {
	ProviderID     string
	Purpose        string
	BrowserHash    string
	Nonce          string
	Verifier       string
	ConfigHash     string
	UserID         int64
	SessionHash    string
	CredentialHash string
	Next           string
	Issuer         string
	Subject        string
	Name           string
	Username       string
	Email          string
	EmailVerified  bool
}

const oidcSignupBindingCookie = "ws_oidc_signup_bind"

func oidcSignupBindingKey(userID int64) string {
	return tokenHash("wishingskin-oidc-signup:" + strconv.FormatInt(userID, 10))
}

func (a *Server) stageOIDCSignupBinding(ctx context.Context, tx *sql.Tx, userID int64, cookie, passwordHash string, flow oidcFlow) error {
	flow.Purpose, flow.BrowserHash = "signup_bind", tokenHash(cookie)
	flow.UserID, flow.CredentialHash = userID, passwordHash
	sealed, err := a.sealOIDC(flow, "flow")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO oidc_flows(flow_hash,value,expires_at) VALUES(?,?,?)"), tokenHash(oidcSignupBindingKey(userID)), sealed, time.Now().Add(24*time.Hour).Unix())
	return err
}

func (a *Server) dropOIDCSignupBinding(ctx context.Context, tx *sql.Tx, userID int64) error {
	_, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM oidc_flows WHERE flow_hash=?"), tokenHash(oidcSignupBindingKey(userID)))
	return err
}

func (a *Server) finishOIDCSignupBinding(r *http.Request, tx *sql.Tx, userID int64) error {
	ctx := r.Context()
	if cookie, err := r.Cookie(oidcSignupBindingCookie); err == nil && len(cookie.Value) == 64 {
		flow, err := a.readOIDCFlow(ctx, tx, oidcSignupBindingKey(userID), "signup_bind", true)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && flow.UserID == userID && flow.BrowserHash == tokenHash(cookie.Value) {
			var credential string
			if err := tx.QueryRowContext(ctx, a.s.Bind("SELECT password_hash FROM users WHERE id=?"), userID).Scan(&credential); err != nil {
				return err
			}
			config, err := a.findOIDCSettings(ctx, tx, flow.ProviderID)
			if err != nil && !errors.Is(err, errCredentials) {
				return err
			}
			if err == nil && config.Enabled && flow.ConfigHash == oidcFingerprint(config) && credential == flow.CredentialHash {
				var linked int
				query := "SELECT COUNT(*) FROM oidc_identities WHERE identity_key=? OR (user_id=? AND issuer_key=?)"
				if err := tx.QueryRowContext(ctx, a.s.Bind(query), oidcIdentityKey(flow.Issuer, flow.Subject), userID, tokenHash(flow.Issuer)).Scan(&linked); err != nil {
					return err
				}
				if linked == 0 {
					if err := a.insertOIDCIdentity(ctx, tx, userID, flow); err != nil {
						return err
					}
				}
			}
		}
	}
	return a.dropOIDCSignupBinding(ctx, tx, userID)
}

func (a *Server) oidcCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: age})
}

func (a *Server) saveOIDCFlow(ctx context.Context, token string, value oidcFlow) error {
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := a.lockOIDCConfiguration(ctx, tx, value.ConfigHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM oidc_flows WHERE expires_at<=?"), time.Now().Unix()); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM oidc_flows").Scan(&count); err != nil {
		return err
	}
	if count >= 2000 {
		return errors.New("authorization capacity reached")
	}
	sealed, err := a.sealOIDC(value, "flow")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO oidc_flows(flow_hash,value,expires_at) VALUES(?,?,?)"), tokenHash(token), sealed, time.Now().Add(10*time.Minute).Unix())
	if err == nil {
		err = tx.Commit()
	}
	return err
}

// Use the same lock order as plugin/configuration writes before creating auth state.
func (a *Server) lockOIDCConfiguration(ctx context.Context, tx *sql.Tx, fingerprint string) error {
	if _, err := lockRegistrationMode(ctx, tx); err != nil {
		return err
	}
	values, err := a.readOIDCConfigurations(ctx, tx)
	if err != nil {
		return err
	}
	enabled, err := a.readPluginEnabled(ctx, tx, "wishingskin.oidc")
	if err != nil {
		return err
	}
	for _, v := range values {
		if enabled && v.Enabled && oidcFingerprint(v) == fingerprint {
			return nil
		}
	}
	return errCredentials
}

func (a *Server) readOIDCFlow(ctx context.Context, reader settingReader, token, purpose string, lock bool) (oidcFlow, error) {
	var value oidcFlow
	if len(token) != 64 {
		return value, errCredentials
	}
	query := "SELECT value FROM oidc_flows WHERE flow_hash=? AND expires_at>?"
	if lock {
		query += " FOR UPDATE"
	}
	var sealed string
	err := reader.QueryRowContext(ctx, a.s.Bind(query), tokenHash(token), time.Now().Unix()).Scan(&sealed)
	if err == nil {
		err = a.openOIDC(sealed, &value, "flow")
	}
	if err == nil && value.Purpose != purpose {
		err = errCredentials
	}
	return value, err
}

func (a *Server) consumeOIDCFlow(ctx context.Context, state, browser string) (oidcFlow, error) {
	tx, err := a.s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return oidcFlow{}, err
	}
	defer tx.Rollback()
	flow, err := a.readOIDCFlow(ctx, tx, state, "authorize", true)
	if err != nil || len(browser) != 64 || flow.BrowserHash != tokenHash(browser) {
		return flow, errCredentials
	}
	_, err = tx.ExecContext(ctx, a.s.Bind("DELETE FROM oidc_flows WHERE flow_hash=?"), tokenHash(state))
	if err == nil {
		err = tx.Commit()
	}
	return flow, err
}

func (a *Server) pendingOIDC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	c, err := r.Cookie("ws_oidc_signup")
	if err != nil {
		failure(w, 400, "OIDC 注册已失效，请重新登录")
		return
	}
	flow, err := a.readOIDCFlow(r.Context(), a.s.DB, c.Value, "signup", false)
	cfg, configErr := a.oidcSettingsFor(r.Context(), flow.ProviderID)
	if err != nil || configErr != nil || !cfg.Enabled || flow.ConfigHash != oidcFingerprint(cfg) {
		failure(w, 400, "OIDC 注册已失效，请重新登录")
		return
	}
	respond(w, 200, map[string]string{"email": flow.Email, "name": cfg.Name})
}

func (a *Server) insertOIDCIdentity(ctx context.Context, tx *sql.Tx, userID int64, flow oidcFlow) error {
	_, err := tx.ExecContext(ctx, a.s.Bind("INSERT INTO oidc_identities(identity_key,issuer_key,user_id,issuer,subject,created_at) VALUES(?,?,?,?,?,?)"), oidcIdentityKey(flow.Issuer, flow.Subject), tokenHash(flow.Issuer), userID, flow.Issuer, flow.Subject, time.Now().Unix())
	return err
}
