package httpapi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" // Yggdrasil's mandated property-signature algorithm.
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/defaultassets"
)

var validHash = regexp.MustCompile(`^[a-f0-9]{32,128}$`)
var validUUID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var validProfileName = regexp.MustCompile(`^[a-z0-9_]{1,16}$`)

const gameTokenLifetime = 24 * time.Hour
const joinLifetime = 30 * time.Second
const maxGameTokens = 10
const maxProfileQueries = 100

type gameProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type gameToken struct {
	UserID      int64
	ClientToken string
	ProfileUUID string
	ProfileName string
	ExpiresAt   int64
}

type yggQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (a *Server) registerYggdrasil() {
	a.mux.HandleFunc("GET /api/yggdrasil", a.metadata)
	a.mux.HandleFunc("GET /api/yggdrasil/{$}", a.metadata)
	a.mux.HandleFunc("POST /api/yggdrasil/authserver/authenticate", a.gameAuthenticate)
	a.mux.HandleFunc("POST /api/yggdrasil/authserver/refresh", a.gameRefresh)
	a.mux.HandleFunc("POST /api/yggdrasil/authserver/signout", a.gameSignout)
	a.mux.HandleFunc("POST /api/yggdrasil/authserver/validate", a.gameValidate)
	a.mux.HandleFunc("POST /api/yggdrasil/authserver/invalidate", a.gameInvalidate)
	a.mux.HandleFunc("GET /api/yggdrasil/sessionserver/session/minecraft/profile/{uuid}", a.profile)
	a.mux.HandleFunc("POST /api/yggdrasil/sessionserver/session/minecraft/join", a.gameJoin)
	a.mux.HandleFunc("GET /api/yggdrasil/sessionserver/session/minecraft/hasJoined", a.gameHasJoined)
	a.mux.HandleFunc("POST /api/yggdrasil/api/profiles/minecraft", a.profilesByName)
	// api.mojang.com is mapped to API root + "api" by authlib-injector.
	a.mux.HandleFunc("GET /api/yggdrasil/api/users/profiles/minecraft/{name}", a.profileByName)
	a.mux.HandleFunc("GET /api/yggdrasil/users/profiles/minecraft/{name}", a.profileByName)
}

func yfail(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]string{"error": code, "errorMessage": message})
}
func ydecode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(v); err != nil {
		yfail(w, 400, "IllegalArgumentException", "Invalid request")
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		yfail(w, 400, "IllegalArgumentException", "Invalid request")
		return false
	}
	return true
}
func (a *Server) signingKey(r *http.Request) (*rsa.PrivateKey, error) {
	return a.signingKeyFrom(r.Context(), a.s.DB)
}
func (a *Server) signingKeyFrom(ctx context.Context, q yggQuerier) (*rsa.PrivateKey, error) {
	var value string
	if err := q.QueryRowContext(ctx, a.s.Bind("SELECT value FROM settings WHERE setting_key=?"), "ygg_private_key").Scan(&value); err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("invalid signing key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	var k *rsa.PrivateKey
	if err == nil {
		k, _ = key.(*rsa.PrivateKey)
	} else {
		k, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if err != nil || k == nil || k.N.BitLen() < 2048 {
		return nil, errors.New("invalid RSA signing key")
	}
	return k, k.Validate()
}
func (a *Server) metadata(w http.ResponseWriter, r *http.Request) {
	k, err := a.signingKey(r)
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Signing key not configured")
		return
	}
	pub, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Signing unavailable")
		return
	}
	origin, _ := url.Parse(a.cfg.PublicURL)
	brand, err := a.siteBranding(r.Context())
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Site settings unavailable")
		return
	}
	version := strings.TrimSpace(a.cfg.Version)
	if version == "" {
		version = "0.1.0-dev"
	}
	nonEmailLogin, err := yggNonEmailLogin(r.Context(), a.s.DB)
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Site settings unavailable")
		return
	}
	respond(w, 200, map[string]any{"meta": map[string]any{"serverName": brand.Name, "implementationName": "WishingSkin", "implementationVersion": version, "feature.non_email_login": nonEmailLogin, "feature.username_check": true, "feature.enable_profile_key": false}, "skinDomains": []string{origin.Hostname()}, "signaturePublickey": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))})
}
func (a *Server) gameAuthenticate(w http.ResponseWriter, r *http.Request) {
	if !a.gameLoginLimit.allow(r) {
		yfail(w, 429, "TooManyRequestsException", "Please retry later")
		return
	}
	var body struct {
		Username, Password, ClientToken string
		RequestUser                     bool
	}
	if !ydecode(w, r, &body) {
		return
	}
	if len(body.ClientToken) > 128 {
		yfail(w, 400, "IllegalArgumentException", "Invalid client token")
		return
	}
	u, matchedProfile, err := a.gameAuthenticateUser(r.Context(), body.Username, body.Password)
	if err != nil {
		if errors.Is(err, errBusy) {
			yfail(w, 429, "TooManyRequestsException", "Please retry later")
		} else if errors.Is(err, errCredentials) || errors.Is(err, errUnverified) {
			yfail(w, 403, "ForbiddenOperationException", "Invalid credentials. Invalid username or password.")
		} else {
			yfail(w, 503, "ServiceUnavailableException", "Authentication unavailable")
		}
		return
	}
	token, err := secret()
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Authentication unavailable")
		return
	}
	if body.ClientToken == "" {
		body.ClientToken, err = secret()
		if err != nil {
			yfail(w, 503, "ServiceUnavailableException", "Authentication unavailable")
			return
		}
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Authentication unavailable")
		return
	}
	defer tx.Rollback()
	if err := a.lockAuthenticatedUser(r.Context(), tx, u); err != nil {
		a.gameTokenError(w, err, "Authentication unavailable")
		return
	}
	profiles, err := a.gameProfiles(r.Context(), tx, u.ID)
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Profiles unavailable")
		return
	}
	var selected gameProfile
	if matchedProfile != nil {
		for _, profile := range profiles {
			if profile.ID == matchedProfile.ID {
				selected = profile
				break
			}
		}
		if selected.ID == "" {
			a.gameTokenError(w, errCredentials, "Authentication unavailable")
			return
		}
	} else if len(profiles) == 1 {
		selected = profiles[0]
	}
	err = a.issueGameToken(r.Context(), tx, token, body.ClientToken, u.ID, selected.ID)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		a.gameTokenError(w, err, "Authentication unavailable")
		return
	}
	response := map[string]any{"accessToken": token, "clientToken": body.ClientToken, "availableProfiles": profiles}
	if selected.ID != "" {
		response["selectedProfile"] = selected
	}
	if body.RequestUser {
		response["user"] = gameUser(u.ID)
	}
	respond(w, 200, response)
}
func (a *Server) gameValidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessToken string
		ClientToken *string
	}
	if !ydecode(w, r, &body) {
		return
	}
	var client string
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT g.client_token FROM game_tokens g JOIN users u ON u.id=g.user_id WHERE g.token_hash=? AND g.expires_at>? AND u.banned=? AND NOT EXISTS (SELECT 1 FROM account_states s WHERE s.user_id=u.id AND s.verification_required=TRUE AND s.email_verified=FALSE) AND (g.profile_uuid='' OR EXISTS (SELECT 1 FROM characters c WHERE c.uuid=g.profile_uuid AND c.user_id=g.user_id))"), tokenHash(body.AccessToken), time.Now().Unix(), false).Scan(&client)
	if errors.Is(err, sql.ErrNoRows) || body.AccessToken == "" || (err == nil && body.ClientToken != nil && client != *body.ClientToken) {
		yfail(w, 403, "ForbiddenOperationException", "Invalid token")
		return
	}
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Validation unavailable")
		return
	}
	w.WriteHeader(204)
}
func (a *Server) gameInvalidate(w http.ResponseWriter, r *http.Request) {
	var body struct{ AccessToken, ClientToken string }
	if !ydecode(w, r, &body) {
		return
	}
	// The upstream contract ignores clientToken and makes revocation idempotent.
	_, err := a.s.DB.ExecContext(r.Context(), a.s.Bind("DELETE FROM game_tokens WHERE token_hash=?"), tokenHash(body.AccessToken))
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Revocation unavailable")
		return
	}
	w.WriteHeader(204)
}

func gameUser(id int64) map[string]any {
	// BS has numeric user IDs, independent of the preserved character UUIDs.
	return map[string]any{"id": fmt.Sprintf("%032x", id), "properties": []any{}}
}

func yggNonEmailLogin(ctx context.Context, reader settingReader) (bool, error) {
	var raw string
	err := reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='ygg_non_email_login'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return raw == "true", err
}

func (a *Server) gameAuthenticateUser(ctx context.Context, username, password string) (User, *gameProfile, error) {
	trimmed := strings.TrimSpace(username)
	account := tokenHash(strings.ToLower(trimmed))
	if !a.loginLimit.allowKey("account:" + account) {
		return User{}, nil, errBusy
	}
	if trimmed != "" && !strings.Contains(trimmed, "@") {
		nonEmail, err := yggNonEmailLogin(ctx, a.s.DB)
		if err != nil {
			return User{}, nil, err
		}
		if nonEmail {
			u, profile, err := a.authenticateCharacter(ctx, trimmed, password)
			if err != nil {
				return User{}, nil, err
			}
			return u, &profile, nil
		}
	}
	u, err := a.authenticate(ctx, username, password)
	return u, nil, err
}

func (a *Server) gameTokenError(w http.ResponseWriter, err error, unavailable string) {
	if errors.Is(err, errCredentials) || errors.Is(err, sql.ErrNoRows) {
		yfail(w, 403, "ForbiddenOperationException", "Invalid token.")
		return
	}
	yfail(w, 503, "ServiceUnavailableException", unavailable)
}

func (a *Server) lockGameUser(ctx context.Context, tx *sql.Tx, id int64) error {
	var banned bool
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT banned FROM users WHERE id=? FOR UPDATE"), id).Scan(&banned)
	if errors.Is(err, sql.ErrNoRows) || banned {
		return errCredentials
	}
	if err == nil {
		var blocked int
		err = tx.QueryRowContext(ctx, a.s.Bind("SELECT COUNT(*) FROM account_states WHERE user_id=? AND verification_required=TRUE AND email_verified=FALSE"), id).Scan(&blocked)
		if err == nil && blocked != 0 {
			return errCredentials
		}
	}
	return err
}

func (a *Server) ownedGameProfile(ctx context.Context, q yggQuerier, id int64, uuid string) (gameProfile, error) {
	var profile gameProfile
	err := q.QueryRowContext(ctx, a.s.Bind("SELECT uuid,name FROM characters WHERE user_id=? AND uuid=? FOR UPDATE"), id, uuid).Scan(&profile.ID, &profile.Name)
	return profile, err
}

func (a *Server) gameProfiles(ctx context.Context, tx *sql.Tx, id int64) ([]gameProfile, error) {
	rows, err := tx.QueryContext(ctx, a.s.Bind("SELECT uuid,name FROM characters WHERE user_id=? ORDER BY id FOR UPDATE"), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := []gameProfile{}
	for rows.Next() {
		var profile gameProfile
		if err := rows.Scan(&profile.ID, &profile.Name); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

// All game writes lock user, token, profile, then join record in that order.
func (a *Server) lockGameToken(ctx context.Context, tx *sql.Tx, hash string) (gameToken, error) {
	var token gameToken
	if err := tx.QueryRowContext(ctx, a.s.Bind("SELECT user_id FROM game_tokens WHERE token_hash=?"), hash).Scan(&token.UserID); err != nil {
		return token, err
	}
	if err := a.lockGameUser(ctx, tx, token.UserID); err != nil {
		return token, err
	}
	if err := tx.QueryRowContext(ctx, a.s.Bind("SELECT client_token,profile_uuid,expires_at FROM game_tokens WHERE token_hash=? AND user_id=? FOR UPDATE"), hash, token.UserID).Scan(&token.ClientToken, &token.ProfileUUID, &token.ExpiresAt); err != nil {
		return token, err
	}
	if token.ExpiresAt <= time.Now().Unix() {
		return token, errCredentials
	}
	if token.ProfileUUID != "" {
		profile, err := a.ownedGameProfile(ctx, tx, token.UserID, token.ProfileUUID)
		if err != nil {
			return token, err
		}
		token.ProfileName = profile.Name
	}
	return token, nil
}

// The caller holds the user row lock, so simultaneous issuances share one cap.
func (a *Server) issueGameToken(ctx context.Context, tx *sql.Tx, access, client string, id int64, profile string) error {
	if _, err := tx.ExecContext(ctx, a.s.Bind("DELETE FROM game_tokens WHERE user_id=? AND expires_at<=?"), id, time.Now().Unix()); err != nil {
		return err
	}
	var cutoffHash string
	var cutoffExpiry int64
	err := tx.QueryRowContext(ctx, a.s.Bind("SELECT token_hash,expires_at FROM game_tokens WHERE user_id=? ORDER BY expires_at DESC,token_hash DESC LIMIT 1 OFFSET ?"), id, maxGameTokens-2).Scan(&cutoffHash, &cutoffExpiry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, a.s.Bind("DELETE FROM game_tokens WHERE user_id=? AND (expires_at<? OR (expires_at=? AND token_hash<?))"), id, cutoffExpiry, cutoffExpiry, cutoffHash)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, a.s.Bind("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?,?,?,?,?)"), tokenHash(access), client, id, profile, time.Now().Add(gameTokenLifetime).Unix())
	return err
}

func (a *Server) gameRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessToken     string
		ClientToken     *string
		RequestUser     bool
		SelectedProfile *gameProfile
	}
	if !ydecode(w, r, &body) {
		return
	}
	if body.AccessToken == "" {
		a.gameTokenError(w, errCredentials, "Refresh unavailable")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		a.gameTokenError(w, err, "Refresh unavailable")
		return
	}
	defer tx.Rollback()
	old, err := a.lockGameToken(r.Context(), tx, tokenHash(body.AccessToken))
	if err == nil && body.ClientToken != nil && *body.ClientToken != old.ClientToken {
		err = errCredentials
	}
	if err != nil {
		a.gameTokenError(w, err, "Refresh unavailable")
		return
	}
	selected := old.ProfileUUID
	if body.SelectedProfile != nil {
		if old.ProfileUUID != "" {
			yfail(w, 400, "IllegalArgumentException", "Access token already has a profile assigned.")
			return
		}
		selected = strings.ToLower(body.SelectedProfile.ID)
		if !validUUID.MatchString(selected) {
			yfail(w, 400, "IllegalArgumentException", "Invalid profile UUID")
			return
		}
	}
	response := map[string]any{"clientToken": old.ClientToken}
	if selected != "" {
		profile, err := a.ownedGameProfile(r.Context(), tx, old.UserID, selected)
		if err == nil && body.SelectedProfile != nil && body.SelectedProfile.Name != "" && strings.ToLower(strings.TrimSpace(body.SelectedProfile.Name)) != strings.ToLower(profile.Name) {
			err = errCredentials
		}
		if err != nil {
			a.gameTokenError(w, err, "Refresh unavailable")
			return
		}
		response["selectedProfile"] = profile
	}
	access, err := secret()
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM game_tokens WHERE token_hash=?"), tokenHash(body.AccessToken))
	}
	if err == nil {
		err = a.issueGameToken(r.Context(), tx, access, old.ClientToken, old.UserID, selected)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		a.gameTokenError(w, err, "Refresh unavailable")
		return
	}
	response["accessToken"] = access
	if body.RequestUser {
		response["user"] = gameUser(old.UserID)
	}
	respond(w, 200, response)
}

func (a *Server) gameSignout(w http.ResponseWriter, r *http.Request) {
	if !a.gameLoginLimit.allow(r) {
		yfail(w, 429, "TooManyRequestsException", "Please retry later")
		return
	}
	var body struct{ Username, Password string }
	if !ydecode(w, r, &body) {
		return
	}
	u, _, err := a.gameAuthenticateUser(r.Context(), body.Username, body.Password)
	if err != nil {
		if errors.Is(err, errBusy) {
			yfail(w, 429, "TooManyRequestsException", "Please retry later")
		} else if errors.Is(err, errCredentials) || errors.Is(err, errUnverified) {
			yfail(w, 403, "ForbiddenOperationException", "Invalid credentials. Invalid username or password.")
		} else {
			yfail(w, 503, "ServiceUnavailableException", "Signout unavailable")
		}
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		a.gameTokenError(w, err, "Signout unavailable")
		return
	}
	defer tx.Rollback()
	err = a.lockAuthenticatedUser(r.Context(), tx, u)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM game_tokens WHERE user_id=?"), u.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		a.gameTokenError(w, err, "Signout unavailable")
		return
	}
	w.WriteHeader(204)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return ""
}

func (a *Server) gameJoin(w http.ResponseWriter, r *http.Request) {
	var body struct{ AccessToken, SelectedProfile, ServerID string }
	if !ydecode(w, r, &body) {
		return
	}
	if body.ServerID == "" || len(body.ServerID) > 255 {
		yfail(w, 400, "IllegalArgumentException", "Invalid server ID")
		return
	}
	profile := strings.ToLower(body.SelectedProfile)
	if body.AccessToken == "" || !validUUID.MatchString(profile) {
		a.gameTokenError(w, errCredentials, "Join unavailable")
		return
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		a.gameTokenError(w, err, "Join unavailable")
		return
	}
	defer tx.Rollback()
	hash := tokenHash(body.AccessToken)
	token, err := a.lockGameToken(r.Context(), tx, hash)
	if err == nil && token.ProfileUUID != profile {
		err = errCredentials
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM ygg_join_sessions WHERE token_hash=? OR (server_id=? AND profile_uuid=?)"), hash, body.ServerID, profile)
	}
	expires := time.Now().Add(joinLifetime).Unix()
	if token.ExpiresAt < expires {
		expires = token.ExpiresAt
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO ygg_join_sessions(token_hash,server_id,profile_uuid,client_ip,expires_at) VALUES(?,?,?,?,?)"), hash, body.ServerID, profile, clientIP(r), expires)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		a.gameTokenError(w, err, "Join unavailable")
		return
	}
	w.WriteHeader(204)
}

func (a *Server) gameHasJoined(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	name, ok := normalizedProfileName(username)
	server := r.URL.Query().Get("serverId")
	if !ok || server == "" || len(server) > 255 {
		w.WriteHeader(204)
		return
	}
	ip := r.URL.Query().Get("ip")
	if ip != "" {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			w.WriteHeader(204)
			return
		}
		ip = parsed.String()
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Session verification unavailable")
		return
	}
	defer tx.Rollback()
	var hash string
	err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT j.token_hash FROM ygg_join_sessions j JOIN characters c ON c.uuid=j.profile_uuid WHERE j.server_id=? AND c.name_key=?"), server, name).Scan(&hash)
	var token gameToken
	if err == nil {
		token, err = a.lockGameToken(r.Context(), tx, hash)
	}
	var profile, storedIP string
	var expires int64
	if err == nil {
		err = tx.QueryRowContext(r.Context(), a.s.Bind("SELECT profile_uuid,client_ip,expires_at FROM ygg_join_sessions WHERE token_hash=? AND server_id=? FOR UPDATE"), hash, server).Scan(&profile, &storedIP, &expires)
	}
	if err == nil && (profile != token.ProfileUUID || username != token.ProfileName || expires <= time.Now().Unix() || (ip != "" && ip != storedIP)) {
		err = errCredentials
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errCredentials) {
		w.WriteHeader(204)
		return
	}
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Session verification unavailable")
		return
	}
	result, err := a.profileResponse(r.Context(), tx, profile, true)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM ygg_join_sessions WHERE token_hash=?"), hash)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Session verification unavailable")
		return
	}
	respond(w, 200, result)
}

func normalizedProfileName(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	return name, validProfileName.MatchString(name)
}

func (a *Server) lookupProfileName(ctx context.Context, name string) (gameProfile, error) {
	key, ok := normalizedProfileName(name)
	if !ok {
		return gameProfile{}, sql.ErrNoRows
	}
	var profile gameProfile
	err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT c.uuid,c.name FROM characters c JOIN users u ON u.id=c.user_id WHERE c.name_key=? AND u.banned=?"), key, false).Scan(&profile.ID, &profile.Name)
	return profile, err
}

func (a *Server) profileByName(w http.ResponseWriter, r *http.Request) {
	profile, err := a.lookupProfileName(r.Context(), r.PathValue("name"))
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(204)
		return
	}
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Profile lookup unavailable")
		return
	}
	respond(w, 200, profile)
}

func (a *Server) profilesByName(w http.ResponseWriter, r *http.Request) {
	var names []string
	if !ydecode(w, r, &names) {
		return
	}
	if names == nil || len(names) > maxProfileQueries {
		yfail(w, 400, "IllegalArgumentException", "Expected at most 100 profile names")
		return
	}
	profiles := []gameProfile{}
	seen := map[string]bool{}
	for _, name := range names {
		key, ok := normalizedProfileName(name)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		profile, err := a.lookupProfileName(r.Context(), key)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			yfail(w, 503, "ServiceUnavailableException", "Profile lookup unavailable")
			return
		}
		profiles = append(profiles, profile)
	}
	respond(w, 200, profiles)
}

func (a *Server) profile(w http.ResponseWriter, r *http.Request) {
	uuid := strings.ToLower(strings.ReplaceAll(r.PathValue("uuid"), "-", ""))
	if !validUUID.MatchString(uuid) {
		yfail(w, 400, "IllegalArgumentException", "Invalid UUID")
		return
	}
	result, err := a.profileResponse(r.Context(), a.s.DB, uuid, r.URL.Query().Get("unsigned") == "false")
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(204)
		return
	}
	if err != nil {
		yfail(w, 503, "ServiceUnavailableException", "Profile unavailable")
		return
	}
	respond(w, 200, result)
}

func (a *Server) profileResponse(ctx context.Context, q yggQuerier, uuid string, signed bool) (map[string]any, error) {
	var name, skin, cape, model string
	if err := q.QueryRowContext(ctx, a.s.Bind("SELECT c.name,c.skin_hash,c.cape_hash,c.model FROM characters c JOIN users u ON u.id=c.user_id WHERE c.uuid=? AND u.banned=?"), uuid, false).Scan(&name, &skin, &cape, &model); err != nil {
		return nil, err
	}
	textures := map[string]any{}
	for _, item := range []struct{ kind, hash string }{{"SKIN", skin}, {"CAPE", cape}} {
		if item.kind == "SKIN" && item.hash == "" {
			defaultModel := "default"
			if model == "slim" {
				defaultModel = "slim"
			}
			texture := map[string]any{"url": a.cfg.PublicURL + "/defaults/skins/" + defaultModel}
			if defaultModel == "slim" {
				texture["metadata"] = map[string]string{"model": "slim"}
			}
			textures[item.kind] = texture
			continue
		}
		if item.hash == "" {
			continue
		}
		var available int
		err := q.QueryRowContext(ctx, a.s.Bind("SELECT 1 FROM textures f WHERE f.hash=? AND "+downloadableTextureSQL), item.hash).Scan(&available)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		t := map[string]any{"url": a.cfg.PublicURL + "/textures/" + item.hash}
		if item.kind == "SKIN" && model == "slim" {
			t["metadata"] = map[string]string{"model": "slim"}
		}
		textures[item.kind] = t
	}
	payload := map[string]any{"timestamp": time.Now().UnixMilli(), "profileId": uuid, "profileName": name, "textures": textures}
	if signed {
		payload["signatureRequired"] = true
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	value := base64.StdEncoding.EncodeToString(data)
	property := map[string]string{"name": "textures", "value": value}
	if signed {
		k, err := a.signingKeyFrom(ctx, q)
		if err != nil {
			return nil, err
		}
		sum := sha1.Sum([]byte(value))
		signature, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA1, sum[:])
		if err != nil {
			return nil, err
		}
		property["signature"] = base64.StdEncoding.EncodeToString(signature)
	}
	return map[string]any{"id": uuid, "name": name, "properties": []any{property}}, nil
}

func (a *Server) defaultSkin(w http.ResponseWriter, r *http.Request) {
	model := r.PathValue("model")
	if model != "default" && model != "slim" {
		http.NotFound(w, r)
		return
	}
	file, ok := defaultassets.Skin(model)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"default-`+model+`"`)
	http.ServeContent(w, r, model+".png", time.Unix(0, 0), file)
}

func (a *Server) texture(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !validHash.MatchString(hash) {
		http.NotFound(w, r)
		return
	}
	a.serveTextureHash(w, r, hash)
}

func (a *Server) serveTextureHash(w http.ResponseWriter, r *http.Request, hash string) {
	if !validHash.MatchString(hash) {
		http.NotFound(w, r)
		return
	}
	var available int
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT 1 FROM textures f WHERE f.hash=? AND "+downloadableTextureSQL), hash).Scan(&available)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		failure(w, 503, "材质暂不可用")
		return
	}
	if a.redirectCDN(w, r, hash) {
		return
	}
	f, err := a.openTextureFile(r.Context(), hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	w.Header().Set("ETag", `"`+hash+`"`)
	http.ServeContent(w, r, hash, time.Unix(0, 0), f)
}

func (a *Server) legacyRoot(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.PathValue("file"), ".json") {
		a.legacyPlayerJSON(w, r)
	} else if a.cfg.Assets != nil {
		a.serveAssetOrSPA(w, r)
	} else {
		http.NotFound(w, r)
	}
}

func (a *Server) legacyPlayerJSON(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if !strings.HasSuffix(file, ".json") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSuffix(file, ".json")
	key, ok := normalizedProfileName(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var username, model, skin, cape string
	var banned bool
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind(
		"SELECT c.name,c.model,"+
			"CASE WHEN EXISTS (SELECT 1 FROM textures f WHERE f.hash=c.skin_hash AND "+downloadableTextureSQL+") THEN c.skin_hash ELSE '' END,"+
			"CASE WHEN EXISTS (SELECT 1 FROM textures f WHERE f.hash=c.cape_hash AND "+downloadableTextureSQL+") THEN c.cape_hash ELSE '' END,"+
			"u.banned FROM characters c JOIN users u ON u.id=c.user_id WHERE c.name_key=?"), key).Scan(&username, &model, &skin, &cape, &banned)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		failure(w, 503, "材质资料暂不可用")
		return
	}
	if banned {
		failure(w, 403, "角色所属账号已被封禁")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// CustomSkinAPI R1 returns hashes; clients concatenate the configured root
	// with /textures/{hash}. Empty slots are represented as JSON null.
	skins := map[string]any{"default": nil}
	if skin == "" {
		model = "default"
	}
	if model == "slim" {
		skins = map[string]any{"slim": nil}
	}
	if skin != "" {
		skins[model] = skin
	}
	var capeValue any
	if cape != "" {
		capeValue = cape
	}
	respond(w, 200, map[string]any{"username": username, "skins": skins, "cape": capeValue})
}

func (a *Server) legacyPlayerTexture(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if !strings.HasSuffix(file, ".png") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSuffix(file, ".png")
	key, ok := normalizedProfileName(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	wantKind := "skin"
	if strings.Contains(r.URL.Path, "/cape/") {
		wantKind = "cape"
	}
	var hash string
	var banned bool
	column := "c.skin_hash"
	if wantKind == "cape" {
		column = "c.cape_hash"
	}
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT "+column+",u.banned FROM characters c JOIN users u ON u.id=c.user_id WHERE c.name_key=?"), key).Scan(&hash, &banned)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		failure(w, 503, "材质暂不可用")
		return
	}
	if banned {
		failure(w, 403, "角色所属账号已被封禁")
		return
	}
	if hash == "" {
		http.NotFound(w, r)
		return
	}
	a.serveTextureHash(w, r, hash)
}
