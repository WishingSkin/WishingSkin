package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type SMTP struct{ Address, From, Username, Password, TLSMode string }

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
	subject := "WishingSkin - Verify your email / 邮箱验证"
	text := "请确认此邮箱属于你。链接有效期为 24 小时，只能使用一次。\r\nConfirm your email. This link expires in 24 hours and can be used once."
	if purpose == "reset" {
		subject = "WishingSkin - Reset your password / 找回密码"
		text = "通过以下链接设置新密码。链接有效期为 30 分钟，只能使用一次。\r\nSet a new password using the link below. It expires in 30 minutes and can be used once."
	} else if purpose == "test" {
		subject = "WishingSkin - SMTP test / SMTP 测试"
		text = "这是一封 WishingSkin SMTP 测试邮件，说明当前配置可以提交邮件。\r\nThis is a WishingSkin SMTP test message. The current configuration accepted the message for delivery."
		link = ""
	}
	// Both languages keep a forwarded link understandable without guessing the recipient's locale.
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n\r\n%s\r\n\r\n若非本人操作，请忽略此邮件。If you did not request this, ignore this email.\r\n", s.From, to, mime.QEncoding.Encode("UTF-8", subject), text, link)
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
