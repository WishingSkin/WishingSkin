package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

type SMTP struct{ Address, From, Username, Password, TLSMode string }

type messageContent struct {
	subject string
	plain   string
	html    string
}

func mailLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return raw
}

func renderHTMLMail(title, summary, details, button, link string) string {
	cta := ""
	if link != "" && button != "" {
		cta = fmt.Sprintf(`<p style="margin:28px 0"><a href="%s" style="display:inline-block;background:#2563eb;color:#fff;text-decoration:none;border-radius:8px;padding:12px 20px;font-weight:600">%s</a></p>`, html.EscapeString(link), html.EscapeString(button))
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head><meta charset="UTF-8"><meta name="color-scheme" content="light dark"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;background:#f4f6fb;color:#172033;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;line-height:1.6">
<table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="background:#f4f6fb;padding:32px 12px"><tr><td align="center">
<table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="max-width:560px;background:#fff;border:1px solid #e5e7eb;border-radius:14px;overflow:hidden">
<tr><td style="padding:28px 32px 10px"><div style="font-size:14px;font-weight:700;letter-spacing:.08em;color:#2563eb">WISHINGSKIN</div><h1 style="margin:18px 0 10px;font-size:24px;line-height:1.3;color:#111827">%s</h1><p style="margin:0;color:#4b5563">%s</p></td></tr>
<tr><td style="padding:0 32px 28px"><p style="margin:20px 0 0">%s</p>%s<p style="margin:24px 0 0;color:#6b7280;font-size:13px">若非本人操作，请忽略此邮件。<br>If you did not request this, ignore this email.</p></td></tr>
</table><p style="margin:16px 0 0;color:#9ca3af;font-size:12px">WishingSkin</p>
</td></tr></table>
</body></html>`, html.EscapeString(title), html.EscapeString(summary), html.EscapeString(details), cta)
}

func messageFor(purpose, rawLink string) messageContent {
	link := mailLink(rawLink)
	switch purpose {
	case "reset":
		plain := "通过以下链接设置新密码。链接有效期为 30 分钟，只能使用一次。\r\nSet a new password using the link below. It expires in 30 minutes and can be used once."
		if link != "" {
			plain += "\r\n\r\n" + link
		}
		plain += "\r\n\r\n若非本人操作，请忽略此邮件。If you did not request this, ignore this email."
		return messageContent{
			subject: "WishingSkin - Reset your password / 找回密码",
			plain:   plain,
			html:    renderHTMLMail("找回密码 / Reset your password", "你可以使用下面的按钮设置新密码。", "链接有效期为 30 分钟，只能使用一次。 / It expires in 30 minutes and can be used once.", "设置新密码 / Set a new password", link),
		}
	case "test":
		plain := "这是一封 WishingSkin SMTP 测试邮件，说明当前配置可以提交邮件。\r\nThis is a WishingSkin SMTP test message. The current configuration accepted the message for delivery.\r\n\r\n若非本人操作，请忽略此邮件。If you did not request this, ignore this email."
		return messageContent{
			subject: "WishingSkin - SMTP test / SMTP 测试",
			plain:   plain,
			html:    renderHTMLMail("SMTP 测试 / SMTP test", "当前配置已接受这封测试邮件。", "这封邮件用于确认 SMTP 可以提交邮件。 / This message confirms that SMTP accepted the message for delivery.", "", ""),
		}
	default:
		plain := "请确认此邮箱属于你。链接有效期为 24 小时，只能使用一次。\r\nConfirm your email. This link expires in 24 hours and can be used once."
		if link != "" {
			plain += "\r\n\r\n" + link
		}
		plain += "\r\n\r\n若非本人操作，请忽略此邮件。If you did not request this, ignore this email."
		return messageContent{
			subject: "WishingSkin - Verify your email / 邮箱验证",
			plain:   plain,
			html:    renderHTMLMail("邮箱验证 / Verify your email", "请确认此邮箱属于你。", "链接有效期为 24 小时，只能使用一次。 / This link expires in 24 hours and can be used once.", "确认邮箱 / Confirm email", link),
		}
	}
}

func composeMessage(from, to string, content messageContent) ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	contentType := fmt.Sprintf("multipart/alternative; boundary=%q", writer.Boundary())
	if _, err := fmt.Fprintf(&body, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\n\r\n", from, to, mime.QEncoding.Encode("UTF-8", content.subject), contentType); err != nil {
		return nil, err
	}
	for _, part := range []struct {
		contentType string
		body        string
	}{{"text/plain; charset=UTF-8", content.plain}, {"text/html; charset=UTF-8", content.html}} {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", part.contentType)
		header.Set("Content-Transfer-Encoding", "8bit")
		partWriter, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		if _, err := partWriter.Write([]byte(part.body)); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

func (s *SMTP) Validate() error {
	host, _, err := net.SplitHostPort(s.Address)
	if err != nil || host == "" {
		return errors.New("SMTP address must be host:port")
	}
	from, err := mail.ParseAddress(s.From)
	if err != nil || from.Address != s.From || strings.ContainsAny(s.From, "\r\n") {
		return errors.New("SMTP from must be a plain email address")
	}
	if (s.Username == "") != (s.Password == "") {
		return errors.New("SMTP username and password must be configured together")
	}
	if s.TLSMode != "starttls" && s.TLSMode != "tls" && s.TLSMode != "local" {
		return errors.New("SMTP TLS mode must be starttls, tls or local")
	}
	if s.TLSMode == "local" && host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) {
		return errors.New("unencrypted SMTP is limited to loopback")
	}
	return nil
}

func (s *SMTP) Send(ctx context.Context, to, purpose, link string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	address, err := mail.ParseAddress(to)
	if err != nil || address.Address != to || strings.ContainsAny(to, "\r\n") {
		return errors.New("invalid recipient")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	host, _, _ := net.SplitHostPort(s.Address)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.Address)
	if err != nil {
		return errors.New("mail delivery unavailable")
	}
	defer conn.Close()
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if s.TLSMode == "tls" {
		secured := tls.Client(conn, tlsConfig)
		if err := secured.HandshakeContext(ctx); err != nil {
			return errors.New("mail TLS unavailable")
		}
		conn = secured
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return errors.New("mail delivery unavailable")
	}
	defer client.Close()
	if s.TLSMode == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return errors.New("mail TLS unavailable")
		}
	}
	if s.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return errors.New("mail authentication unavailable")
		}
	}
	content := messageFor(purpose, link)
	message, err := composeMessage(s.From, to, content)
	if err != nil {
		return errors.New("mail message unavailable")
	}
	if client.Mail(s.From) != nil || client.Rcpt(to) != nil {
		return errors.New("mail delivery rejected")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("mail delivery rejected")
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return errors.New("mail delivery incomplete")
	}
	if err := writer.Close(); err != nil {
		return errors.New("mail delivery incomplete")
	}
	// DATA acceptance is the delivery boundary; a failed QUIT must not report an accepted message as unsent.
	_ = client.Quit()
	return nil
}
