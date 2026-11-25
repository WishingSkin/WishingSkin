package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"github.com/WishingSkin/WishingSkin/internal/mailer"
	"github.com/WishingSkin/WishingSkin/internal/store"
	"github.com/WishingSkin/WishingSkin/internal/turnstile"
)

type serviceSettings struct {
	NotificationEmailEnabled bool   `json:"notificationEmailEnabled"`
	SMTPEnabled              bool   `json:"smtpEnabled"`
	Address                  string `json:"address"`
	From                     string `json:"from"`
	Username                 string `json:"username"`
	Password                 string `json:"password"`
	TLSMode                  string `json:"tlsMode"`
	TestRecipient            string `json:"testRecipient"`
	TurnstileEnabled         bool   `json:"turnstileEnabled"`
	SiteKey                  string `json:"siteKey"`
	Secret                   string `json:"secret"`
}
type runtimeServices struct {
	Mail      MailSender
	Challenge ChallengeVerifier
	SiteKey   string
}

func (s runtimeServices) challengeConfigured() bool {
	return s.Challenge != nil && s.SiteKey != ""
}

func (a *Server) serviceSettings(ctx context.Context) (serviceSettings, bool, error) {
	return a.readServiceSettings(ctx, a.s.DB)
}

func (a *Server) readServiceSettings(ctx context.Context, reader settingReader) (serviceSettings, bool, error) {
	value := serviceSettings{TLSMode: "starttls"}
	if a.cfg.SMTP != nil {
		s := a.cfg.SMTP
		value.SMTPEnabled = true
		value.Address = s.Address
		value.From = s.From
		value.Username = s.Username
		value.Password = s.Password
		value.TLSMode = s.TLSMode
	}
	value.TurnstileEnabled = a.cfg.Challenge != nil
	value.SiteKey = a.cfg.TurnstileSiteKey
	value.Secret = a.cfg.TurnstileSecret
	var sealed string
	err := reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE setting_key='service_config'").Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return value, false, nil
	}
	if err != nil {
		return value, false, err
	}
	blob, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return value, true, err
	}
	block, err := aes.NewCipher(a.cfg.SettingsKey)
	if err != nil {
		return value, true, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(blob) < gcm.NonceSize() {
		return value, true, errors.New("invalid settings key or ciphertext")
	}
	plain, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], []byte("wishingskin:service_config:v1"))
	if err != nil {
		return value, true, errors.New("settings cannot be decrypted")
	}
	err = json.Unmarshal(plain, &value)
	return value, true, err
}
func (a *Server) services(ctx context.Context) (runtimeServices, error) {
	value, saved, err := a.serviceSettings(ctx)
	if err != nil {
		return runtimeServices{}, err
	}
	if !saved {
		return runtimeServices{a.emailSender(a.cfg.MailSender), a.cfg.Challenge, a.cfg.TurnstileSiteKey}, nil
	}
	result := runtimeServices{}
	if value.SMTPEnabled {
		result.Mail = a.emailSender(&mailer.SMTP{Address: value.Address, From: value.From, Username: value.Username, Password: value.Password, TLSMode: value.TLSMode})
	}
	if value.TurnstileEnabled {
		origin, _ := url.Parse(a.cfg.PublicURL)
		result.Challenge = &turnstile.Client{Secret: value.Secret, Hostname: origin.Hostname()}
		result.SiteKey = value.SiteKey
	}
	return result, nil
}
func serviceView(value serviceSettings, saved bool) map[string]any {
	return map[string]any{"smtpEnabled": value.SMTPEnabled, "notificationEmailEnabled": value.NotificationEmailEnabled, "address": value.Address, "from": value.From, "username": value.Username, "tlsMode": value.TLSMode, "testRecipient": value.TestRecipient, "passwordConfigured": value.Password != "", "turnstileEnabled": value.TurnstileEnabled, "siteKey": value.SiteKey, "secretConfigured": value.Secret != "", "source": map[bool]string{true: "database", false: "environment"}[saved]}
}
func (a *Server) adminServices(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRead(w, r)
	if !ok {
		return
	}
	value, saved, err := a.serviceSettings(r.Context())
	if err != nil {
		failure(w, 503, "无法解密服务设置，请检查本地设置密钥")
		return
	}
	view := serviceView(value, saved)
	origin, _ := url.Parse(a.cfg.PublicURL)
	view["turnstileHostname"] = origin.Hostname()
	view["editable"] = len(a.cfg.SettingsKey) == 32
	if value.TestRecipient == "" {
		view["testRecipient"] = u.Email
	}
	respond(w, 200, view)
}
func (a *Server) updateServices(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	if len(a.cfg.SettingsKey) != 32 {
		failure(w, 503, "本地设置密钥尚不可用")
		return
	}
	var body struct {
		NotificationEmailEnabled *bool   `json:"notificationEmailEnabled"`
		SMTPEnabled              bool    `json:"smtpEnabled"`
		Address                  string  `json:"address"`
		From                     string  `json:"from"`
		Username                 string  `json:"username"`
		TLSMode                  string  `json:"tlsMode"`
		TestRecipient            string  `json:"testRecipient"`
		TurnstileEnabled         bool    `json:"turnstileEnabled"`
		SiteKey                  string  `json:"siteKey"`
		Password                 *string `json:"password"`
		Secret                   *string `json:"secret"`
		ClearPassword            bool    `json:"clearPassword"`
		ClearSecret              bool    `json:"clearSecret"`
	}
	if !decode(w, r, &body) {
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	value, _, err := a.readServiceSettings(r.Context(), tx)
	if err != nil {
		failure(w, 503, "无法解密服务设置，请检查本地设置密钥")
		return
	}
	previous := value
	if body.NotificationEmailEnabled != nil {
		value.NotificationEmailEnabled = *body.NotificationEmailEnabled
	}
	value.SMTPEnabled = body.SMTPEnabled
	value.Address = strings.TrimSpace(body.Address)
	value.From = strings.TrimSpace(body.From)
	value.Username = body.Username
	value.TLSMode = body.TLSMode
	value.TestRecipient = strings.TrimSpace(body.TestRecipient)
	value.TurnstileEnabled = body.TurnstileEnabled
	value.SiteKey = strings.TrimSpace(body.SiteKey)
	if body.Password != nil && *body.Password != "" {
		value.Password = *body.Password
	}
	if body.ClearPassword {
		value.Password = ""
	}
	if body.Secret != nil && *body.Secret != "" {
		value.Secret = *body.Secret
	}
	if body.ClearSecret {
		value.Secret = ""
	}
	if value.SMTPEnabled {
		smtp := &mailer.SMTP{Address: value.Address, From: value.From, Username: value.Username, Password: value.Password, TLSMode: value.TLSMode}
		if smtp.Validate() != nil {
			failure(w, 400, "SMTP 配置无效，请检查地址、发件人、认证和 TLS")
			return
		}
	}
	if value.TestRecipient != "" {
		address, parseErr := mail.ParseAddress(value.TestRecipient)
		if parseErr != nil || address.Address != value.TestRecipient || strings.ContainsAny(value.TestRecipient, "\r\n") {
			failure(w, 400, "测试收件地址无效")
			return
		}
	}
	if value.TurnstileEnabled && (value.SiteKey == "" || value.Secret == "" || len(value.SiteKey) > 256 || len(value.Secret) > 256) {
		failure(w, 400, "Turnstile 的站点密钥和秘密密钥须成对填写")
		return
	}
	a.auditJSON(r.Context(), "services", map[string]any{"smtpEnabled": previous.SMTPEnabled, "address": previous.Address, "tlsMode": previous.TLSMode, "turnstileEnabled": previous.TurnstileEnabled}, map[string]any{"smtpEnabled": value.SMTPEnabled, "address": value.Address, "tlsMode": value.TLSMode, "turnstileEnabled": value.TurnstileEnabled})
	a.auditChange(r.Context(), "notificationEmailEnabled", previous.NotificationEmailEnabled, value.NotificationEmailEnabled)
	if previous.From != value.From {
		store.CaptureAdminChange(r.Context(), "services.from", "", "发件人已更新")
	}
	if previous.Password != value.Password {
		store.CaptureAdminChange(r.Context(), "services.smtpPassword", "", "凭据已更新")
	}
	if previous.Secret != value.Secret {
		store.CaptureAdminChange(r.Context(), "services.turnstileSecret", "", "凭据已更新")
	}
	data, _ := json.Marshal(value)
	block, _ := aes.NewCipher(a.cfg.SettingsKey)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		failure(w, 503, "暂时无法保存服务设置")
		return
	}
	sealed := base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, data, []byte("wishingskin:service_config:v1")))
	query := "INSERT INTO settings(setting_key,value) VALUES('service_config',?) ON CONFLICT(setting_key) DO UPDATE SET value=EXCLUDED.value"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO settings(setting_key,value) VALUES('service_config',?) ON DUPLICATE KEY UPDATE value=VALUES(value)"
	}
	_, err = tx.ExecContext(r.Context(), a.s.Bind(query), sealed)
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.services", "site", "services")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存服务设置")
		return
	}
	respond(w, 200, serviceView(value, true))
}

func (a *Server) testServices(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	services, err := a.services(r.Context())
	if err != nil {
		failure(w, 503, "无法读取服务设置")
		return
	}
	if services.Mail == nil {
		failure(w, 400, "请先启用并保存 SMTP 设置")
		return
	}
	var body struct {
		Recipient string `json:"recipient"`
		Purpose   string `json:"purpose"`
	}
	if !decode(w, r, &body) {
		return
	}
	purpose := "test"
	if body.Purpose == "verify" || body.Purpose == "reset" {
		purpose = "preview-" + body.Purpose
	} else if body.Purpose != "" && body.Purpose != "test" {
		failure(w, 400, "邮件用途无效")
		return
	}
	recipient := strings.TrimSpace(body.Recipient)
	if recipient == "" {
		recipient = u.Email
	}
	address, parseErr := mail.ParseAddress(recipient)
	if parseErr != nil || address.Address != recipient || strings.ContainsAny(recipient, "\r\n") {
		failure(w, 400, "测试收件地址无效")
		return
	}
	select {
	case a.mailSlots <- struct{}{}:
		defer func() { <-a.mailSlots }()
	default:
		failure(w, 429, "邮件处理繁忙，请稍后重试")
		return
	}
	if err := services.Mail.Send(r.Context(), recipient, purpose, ""); err != nil {
		failure(w, 503, "测试邮件发送失败，请检查 SMTP 配置")
		return
	}
	respond(w, 200, map[string]string{"message": "测试邮件已提交", "recipient": recipient})
}

func (a *Server) testTurnstile(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	var body struct {
		TurnstileToken string `json:"turnstileToken"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !a.challenge(w, r, body.TurnstileToken, "admin-test") {
		return
	}
	respond(w, 200, map[string]string{"message": "Turnstile 测试通过，当前站点密钥和 action 有效"})
}
