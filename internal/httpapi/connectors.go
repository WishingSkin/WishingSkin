package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

type connectorConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Enabled        bool   `json:"enabled"`
	Endpoint       string `json:"endpoint"`
	APIKey         string `json:"apiKey"`
	DSN            string `json:"dsn"`
	Table          string `json:"table"`
	IDColumn       string `json:"idColumn"`
	NameColumn     string `json:"nameColumn"`
	PasswordColumn string `json:"passwordColumn"`
}

func (a *Server) registerConnectors() {
	a.mux.HandleFunc("GET /api/admin/connectors", a.adminConnectors)
	a.mux.HandleFunc("PATCH /api/admin/connectors", a.updateConnectors)
	a.mux.HandleFunc("GET /api/account/connectors", a.accountConnectors)
	a.mux.HandleFunc("POST /api/account/connectors/{id}", a.linkConnector)
	a.mux.HandleFunc("DELETE /api/account/connectors/{id}", a.linkConnector)
}
func (a *Server) connectorSettings(ctx context.Context) ([]connectorConfig, error) {
	var sealed string
	ok, err := a.readSetting(ctx, "external_connectors", &sealed)
	values := []connectorConfig{}
	if err == nil && ok {
		err = a.openOIDC(sealed, &values, "connectors")
	}
	return values, err
}
func connectorView(v connectorConfig) map[string]any {
	return map[string]any{"id": v.ID, "name": v.Name, "kind": v.Kind, "enabled": v.Enabled, "endpoint": v.Endpoint, "dsnConfigured": v.DSN != "", "apiKeyConfigured": v.APIKey != "", "table": v.Table, "idColumn": v.IDColumn, "nameColumn": v.NameColumn, "passwordColumn": v.PasswordColumn}
}
func (a *Server) adminConnectors(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	values, err := a.connectorSettings(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取连接器配置")
		return
	}
	items := []map[string]any{}
	for _, v := range values {
		items = append(items, connectorView(v))
	}
	respond(w, 200, map[string]any{"connectors": items})
}

var sqlIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func (a *Server) updateConnectors(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Connectors []connectorConfig `json:"connectors"`
	}
	if !decodeLimit(w, r, &body, 64<<10) {
		return
	}
	if len(body.Connectors) > 12 {
		failure(w, 400, "最多配置 12 个连接器")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	old, err := a.connectorSettings(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取连接器配置")
		return
	}
	seen := map[string]bool{}
	for i := range body.Connectors {
		v := &body.Connectors[i]
		v.Name = strings.TrimSpace(v.Name)
		if seen[v.ID] || !providerIDPattern.MatchString(v.ID) || v.Name == "" || len(v.Name) > 128 {
			failure(w, 400, "连接器标识或名称无效")
			return
		}
		seen[v.ID] = true
		for _, saved := range old {
			if saved.ID == v.ID {
				if v.DSN == "" {
					v.DSN = saved.DSN
				}
				if v.APIKey == "" {
					v.APIKey = saved.APIKey
				}
			}
		}
		if v.Kind == "authme" {
			if v.Table == "" {
				v.Table = "authme"
			}
			if v.IDColumn == "" {
				v.IDColumn = "id"
			}
			if v.NameColumn == "" {
				v.NameColumn = "realname"
			}
			if v.PasswordColumn == "" {
				v.PasswordColumn = "password"
			}
			cfg, e := mysql.ParseDSN(v.DSN)
			if e != nil || cfg.DBName == "" || !sqlIdentifier.MatchString(v.Table) || !sqlIdentifier.MatchString(v.IDColumn) || !sqlIdentifier.MatchString(v.NameColumn) || !sqlIdentifier.MatchString(v.PasswordColumn) {
				failure(w, 400, "AuthMe 需要有效的 MySQL DSN 和字段名称")
				return
			}
			v.Endpoint = ""
			v.APIKey = ""
		} else if v.Kind == "http" {
			if !validOIDCIssuer(v.Endpoint) || len(v.APIKey) > 4096 {
				failure(w, 400, "HTTP 连接器地址无效，须使用 HTTPS 或本地 HTTP")
				return
			}
			v.DSN = ""
		} else {
			failure(w, 400, "连接器类型无效")
			return
		}
	}
	sealed, err := a.sealOIDC(body.Connectors, "connectors")
	if err == nil {
		err = a.putSetting(r.Context(), tx, "external_connectors", sealed)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.connectors", "site", "connectors")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存连接器")
		return
	}
	items := []map[string]any{}
	for _, v := range body.Connectors {
		items = append(items, connectorView(v))
	}
	respond(w, 200, map[string]any{"connectors": items})
}

func connectorNamespace(v connectorConfig) string {
	if v.Kind == "http" {
		return v.ID + ":" + v.Endpoint
	}
	cfg, err := mysql.ParseDSN(v.DSN)
	if err != nil {
		return ""
	}
	return strings.Join([]string{v.ID, cfg.Net, cfg.Addr, cfg.DBName, v.Table, v.IDColumn}, ":")
}
func verifyAuthMe(password, hash string) bool {
	if len(password) > 256 || len(hash) > 512 {
		return false
	}
	if strings.HasPrefix(hash, "$SHA$") {
		parts := strings.Split(hash, "$")
		if len(parts) != 4 || len(parts[2]) > 256 || len(parts[3]) != 64 {
			return false
		}
		expected := "$SHA$" + parts[2] + "$" + tokenHash(tokenHash(password)+parts[2])
		return subtle.ConstantTimeCompare([]byte(expected), []byte(hash)) == 1
	}
	if strings.HasPrefix(hash, "$2") {
		cost, err := bcrypt.Cost([]byte(hash))
		return err == nil && cost <= 14 && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return false
}
func verifyConnector(ctx context.Context, v connectorConfig, username, password string) (string, string, error) {
	if len(username) < 1 || len(username) > 255 || len(password) < 1 || len(password) > 256 {
		return "", "", errCredentials
	}
	if v.Kind == "authme" {
		cfg, err := mysql.ParseDSN(v.DSN)
		if err != nil {
			return "", "", err
		}
		cfg.MultiStatements = false
		cfg.Timeout = 5 * time.Second
		cfg.ReadTimeout = 5 * time.Second
		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return "", "", err
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		query := fmt.Sprintf("SELECT `%s`,`%s`,`%s` FROM `%s` WHERE LOWER(`%s`)=? LIMIT 2", v.IDColumn, v.NameColumn, v.PasswordColumn, v.Table, v.NameColumn)
		rows, err := db.QueryContext(ctx, query, strings.ToLower(username))
		if err != nil {
			return "", "", err
		}
		defer rows.Close()
		var subject, name, hash string
		if !rows.Next() {
			return "", "", errCredentials
		}
		if err = rows.Scan(&subject, &name, &hash); err != nil {
			return "", "", err
		}
		if rows.Next() || rows.Err() != nil || subject == "" || len(subject) > 255 || !verifyAuthMe(password, hash) {
			return "", "", errCredentials
		}
		return subject, name, nil
	}
	data, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, err := http.NewRequestWithContext(ctx, "POST", v.Endpoint, bytes.NewReader(data))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if v.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.APIKey)
	}
	client := http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", errCredentials
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return "", "", errCredentials
	}
	var result struct {
		Authenticated bool   `json:"authenticated"`
		Subject       string `json:"subject"`
		Username      string `json:"username"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Authenticated || result.Subject == "" || len(result.Subject) > 255 || result.Username == "" || len(result.Username) > 255 {
		return "", "", errCredentials
	}
	return result.Subject, result.Username, nil
}
func (a *Server) accountConnectors(w http.ResponseWriter, r *http.Request) {
	u, _, err := a.current(r)
	if err != nil {
		sessionError(w, err)
		return
	}
	values, err := a.connectorSettings(r.Context())
	enabled, e := a.storedPluginEnabled(r.Context(), "wishingskin.connectors")
	if err != nil || e != nil {
		failure(w, 503, "暂时无法读取连接器")
		return
	}
	items := []map[string]any{}
	for _, v := range values {
		var username string
		err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT username FROM external_identities WHERE connector_id=? AND user_id=?"), v.ID, u.ID).Scan(&username)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			failure(w, 503, "暂时无法读取绑定")
			return
		}
		items = append(items, map[string]any{"id": v.ID, "name": v.Name, "kind": v.Kind, "enabled": enabled && v.Enabled, "username": username, "linked": err == nil})
	}
	respond(w, 200, map[string]any{"connectors": items})
}
func (a *Server) linkConnector(w http.ResponseWriter, r *http.Request) {
	u, token, ok := a.totpRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		Username        string `json:"username"`
		Password        string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	u, ok = a.totpPassword(w, r, u, body.CurrentPassword)
	if !ok {
		return
	}
	values, err := a.connectorSettings(r.Context())
	var cfg connectorConfig
	for _, v := range values {
		if v.ID == r.PathValue("id") {
			cfg = v
		}
	}
	enabled, e := a.storedPluginEnabled(r.Context(), "wishingskin.connectors")
	if err != nil || e != nil || cfg.ID == "" || r.Method == "POST" && (!enabled || !cfg.Enabled) {
		failure(w, 400, "连接器尚未启用")
		return
	}
	var subject, name string
	if r.Method == "POST" {
		select {
		case a.hashes <- struct{}{}:
		default:
			failure(w, 429, "账号验证繁忙，请稍后重试")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		subject, name, err = verifyConnector(ctx, cfg, strings.TrimSpace(body.Username), body.Password)
		cancel()
		<-a.hashes
		if err != nil {
			failure(w, 400, "外部账号验证失败，请检查账号、密码和连接器配置")
			return
		}
	}
	tx, err := a.s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		failure(w, 503, "暂时无法保存绑定")
		return
	}
	defer tx.Rollback()
	var mode string
	err = tx.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE setting_key='registration_mode' FOR UPDATE").Scan(&mode)
	if err == nil {
		var current []connectorConfig
		current, err = a.connectorSettings(r.Context())
		matched := false
		for _, v := range current {
			if oidcConnectorFingerprint(v) == oidcConnectorFingerprint(cfg) {
				matched = true
			}
		}
		on, e := a.storedPluginEnabled(r.Context(), "wishingskin.connectors")
		if e != nil || !matched || r.Method == "POST" && !on {
			err = errCredentials
		}
	}
	if err == nil {
		err = a.lockWebAccount(r.Context(), tx, u.ID, token)
	}
	if err == nil {
		err = a.lockAuthenticatedUser(r.Context(), tx, u)
	}
	if err == nil {
		if r.Method == "DELETE" {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM external_identities WHERE connector_id=? AND user_id=?"), cfg.ID, u.ID)
		} else {
			_, err = tx.ExecContext(r.Context(), a.s.Bind("INSERT INTO external_identities(identity_key,connector_id,subject,username,user_id,created_at) VALUES(?,?,?,?,?,?)"), oidcIdentityKey(connectorNamespace(cfg), subject), cfg.ID, subject, name, u.ID, time.Now().Unix())
		}
	}
	if uniqueViolation(err) {
		failure(w, 409, "该外部账号已绑定，或当前连接器已有绑定")
		return
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "connector."+strings.ToLower(r.Method), "account", cfg.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存绑定")
		return
	}
	w.WriteHeader(204)
}
func oidcConnectorFingerprint(v connectorConfig) string {
	raw, _ := json.Marshal(v)
	return tokenHash(string(raw))
}
