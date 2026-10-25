package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

type OIDCSettings struct {
	Enabled      bool   `json:"enabled"`
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

func (a *Server) sealOIDC(value any, purpose string) (string, error) {
	if len(a.cfg.SettingsKey) != 32 {
		return "", errors.New("settings key unavailable")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(a.cfg.SettingsKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, data, []byte("wishingskin:oidc:"+purpose+":v1"))), nil
}

func (a *Server) openOIDC(sealed string, value any, purpose string) error {
	blob, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(a.cfg.SettingsKey)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(blob) < gcm.NonceSize() {
		return errors.New("invalid ciphertext")
	}
	data, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], []byte("wishingskin:oidc:"+purpose+":v1"))
	if err != nil {
		return errors.New("OIDC settings cannot be decrypted")
	}
	return json.Unmarshal(data, value)
}

func validOIDCIssuer(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "https" || u.Scheme == "http" && loopbackOriginHost(u.Hostname()))
}

func (a *Server) oidcSettings(ctx context.Context) (OIDCSettings, error) {
	value := a.cfg.OIDC
	if value.Name == "" {
		value.Name = "OIDC"
	}
	var sealed string
	saved, err := a.readSetting(ctx, "oidc_config", &sealed)
	if err == nil && !saved {
		return value, nil
	}
	if err == nil {
		err = a.openOIDC(sealed, &value, "config")
	}
	return value, err
}

func oidcFingerprint(value OIDCSettings) string {
	data, _ := json.Marshal(value)
	return tokenHash(string(data))
}
func oidcIdentityKey(issuer, subject string) string { return tokenHash(issuer + "\x00" + subject) }
func (a *Server) oidcView(value OIDCSettings) map[string]any {
	return map[string]any{"enabled": value.Enabled, "name": value.Name, "issuer": value.Issuer, "clientId": value.ClientID, "secretConfigured": value.ClientSecret != "", "redirectUri": a.cfg.PublicURL + "/api/auth/oidc/callback"}
}

func (a *Server) adminOIDC(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	value, err := a.oidcSettings(r.Context())
	if err != nil {
		failure(w, 503, "无法读取 OIDC 设置，请检查设置密钥")
		return
	}
	respond(w, 200, a.oidcView(value))
}

func (a *Server) updateOIDC(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled      bool    `json:"enabled"`
		Name         string  `json:"name"`
		Issuer       string  `json:"issuer"`
		ClientID     string  `json:"clientId"`
		ClientSecret *string `json:"clientSecret"`
		ClearSecret  bool    `json:"clearSecret"`
	}
	if !decode(w, r, &body) {
		return
	}
	value, err := a.oidcSettings(r.Context())
	if err != nil {
		failure(w, 503, "无法读取 OIDC 设置，请检查设置密钥")
		return
	}
	value.Enabled = body.Enabled
	value.Name = strings.TrimSpace(body.Name)
	value.Issuer = strings.TrimRight(strings.TrimSpace(body.Issuer), "/")
	value.ClientID = strings.TrimSpace(body.ClientID)
	if body.ClientSecret != nil && *body.ClientSecret != "" {
		value.ClientSecret = *body.ClientSecret
	}
	if body.ClearSecret {
		value.ClientSecret = ""
	}
	if value.Name == "" {
		value.Name = "OIDC"
	}
	if utf8.RuneCountInString(value.Name) > 64 || strings.ContainsAny(value.Name, "\r\n") || len(value.ClientID) > 256 || len(value.ClientSecret) > 4096 || (value.Issuer != "" && !validOIDCIssuer(value.Issuer)) || value.Enabled && (value.Issuer == "" || value.ClientID == "" || value.ClientSecret == "") {
		failure(w, 400, "OIDC 配置无效，请检查名称、Issuer、应用 ID 和密钥")
		return
	}
	sealed, err := a.sealOIDC(value, "config")
	if err != nil {
		failure(w, 503, "本地设置密钥尚不可用")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err = a.putSetting(r.Context(), tx, "oidc_config", sealed)
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.oidc", "site", "oidc")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存 OIDC 设置")
		return
	}
	respond(w, 200, a.oidcView(value))
}
