package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func (a *Server) registerOAuthProvider() {
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		a.mux.HandleFunc("GET "+path, a.oauthDiscovery)
	}
	a.mux.HandleFunc("GET /oauth/jwks", a.oauthJWKS)
	a.mux.HandleFunc("POST /oauth/introspect", a.oauthIntrospect)
}
func (a *Server) oauthDiscovery(w http.ResponseWriter, r *http.Request) {
	enabled, err := a.storedPluginEnabled(r.Context(), "wishingskin.oauth2")
	if err != nil || !enabled {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	issuer := a.cfg.PublicURL
	respond(w, 200, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/oauth/authorize", "token_endpoint": issuer + "/oauth/token", "revocation_endpoint": issuer + "/oauth/revoke", "introspection_endpoint": issuer + "/oauth/introspect", "userinfo_endpoint": issuer + "/oauth/userinfo", "jwks_uri": issuer + "/oauth/jwks", "response_types_supported": []string{"code"}, "response_modes_supported": []string{"query"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{"openid", "profile", "email", "offline_access"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic", "none"}, "introspection_endpoint_auth_methods_supported": []string{"client_secret_basic"}, "claims_supported": []string{"iss", "sub", "aud", "iat", "exp", "nonce", "auth_time", "at_hash", "name", "email", "email_verified"}})
}
func (a *Server) oauthSigningKey(ctx context.Context, tx *sql.Tx) (*rsa.PrivateKey, error) {
	var encoded, sealed string
	err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='oauth_signing_key'").Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		key, e := rsa.GenerateKey(rand.Reader, 2048)
		if e != nil {
			return nil, e
		}
		sealed, e = a.sealOIDC(x509.MarshalPKCS1PrivateKey(key), "oauth-signing")
		if e != nil {
			return nil, e
		}
		if e = a.putSetting(ctx, tx, "oauth_signing_key", sealed); e != nil {
			return nil, e
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	var data []byte
	if err = json.Unmarshal([]byte(encoded), &sealed); err != nil {
		return nil, err
	}
	if err = a.openOIDC(sealed, &data, "oauth-signing"); err != nil {
		return nil, err
	}
	return x509.ParsePKCS1PrivateKey(data)
}
func oauthKeyID(key *rsa.PrivateKey) string {
	hash := sha256.Sum256(x509.MarshalPKCS1PublicKey(&key.PublicKey))
	return hex.EncodeToString(hash[:16])
}
func (a *Server) oauthJWKS(w http.ResponseWriter, r *http.Request) {
	tx, ok := a.beginOAuthWrite(w, r)
	if !ok {
		return
	}
	defer tx.Rollback()
	key, err := a.oauthSigningKey(r.Context(), tx)
	if err != nil || tx.Commit() != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	respond(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: oauthKeyID(key), Algorithm: "RS256", Use: "sig"}}})
}
func (a *Server) oauthIDToken(ctx context.Context, tx *sql.Tx, client string, u User, access, nonce string, authTime int64) (string, error) {
	key, err := a.oauthSigningKey(ctx, tx)
	if err != nil {
		return "", err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", oauthKeyID(key)))
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	hash := sha256.Sum256([]byte(access))
	claims := map[string]any{"iss": a.cfg.PublicURL, "sub": strconv.FormatInt(u.ID, 10), "aud": client, "iat": now, "exp": now + 3600, "at_hash": base64.RawURLEncoding.EncodeToString(hash[:16])}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	if authTime > 0 {
		claims["auth_time"] = authTime
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return signed.CompactSerialize()
}
func (a *Server) recordWebAuthTime(ctx context.Context, tx *sql.Tx, token string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM web_session_auth WHERE token_hash NOT IN (SELECT token_hash FROM web_sessions)"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, a.s.Bind("INSERT INTO web_session_auth(token_hash,auth_time) VALUES(?,?)"), tokenHash(token), time.Now().Unix())
	return err
}
func (a *Server) revokeOAuthFamily(ctx context.Context, tx *sql.Tx, family string) error {
	for _, stmt := range []string{"DELETE FROM oauth_tokens WHERE token_hash IN (SELECT token_hash FROM oauth_access_families WHERE family=?)", "DELETE FROM oauth_access_families WHERE family=?", "DELETE FROM oauth_refresh_tokens WHERE family=?", "DELETE FROM oauth_refresh_metadata WHERE family=?"} {
		if _, err := tx.ExecContext(ctx, a.s.Bind(stmt), family); err != nil {
			return err
		}
	}
	return nil
}
func (a *Server) oauthIntrospect(w http.ResponseWriter, r *http.Request) {
	if !a.oauthForm(w, r) {
		return
	}
	tx, ok := a.beginOAuthWrite(w, r)
	if !ok {
		return
	}
	defer tx.Rollback()
	client, ok := a.oauthAuthenticateClient(w, r, tx)
	if !ok {
		return
	}
	if client.Public {
		oauthError(w, 401, "invalid_client")
		return
	}
	var uid, exp int64
	var scope, credential string
	err := tx.QueryRowContext(r.Context(), a.s.Bind("SELECT user_id,scope,credential_hash,expires_at FROM oauth_tokens WHERE token_hash=? AND client_id=? AND expires_at>?"), tokenHash(r.PostForm.Get("token")), client.ID, time.Now().Unix()).Scan(&uid, &scope, &credential, &exp)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	if err == nil {
		u, e := a.oidcUser(r.Context(), uid)
		if e == nil && u.credentialHash == credential {
			respond(w, 200, map[string]any{"active": true, "client_id": client.ID, "sub": strconv.FormatInt(uid, 10), "scope": scope, "exp": exp, "token_type": "Bearer"})
			return
		}
	}
	respond(w, 200, map[string]bool{"active": false})
}
