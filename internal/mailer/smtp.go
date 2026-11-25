package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

type SMTP struct {
	Address, From, Username, Password, TLSMode string
	Template                                   *Template
}

func composeMessage(from, to string, content Content) ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	contentType := fmt.Sprintf("multipart/alternative; boundary=%q", writer.Boundary())
	if _, err := fmt.Fprintf(&body, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\n\r\n", from, to, mime.QEncoding.Encode("UTF-8", content.Subject), contentType); err != nil {
		return nil, err
	}
	for _, part := range []struct {
		contentType string
		body        string
	}{{"text/plain; charset=UTF-8", content.Text}, {"text/html; charset=UTF-8", content.HTML}} {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", part.contentType)
		header.Set("Content-Transfer-Encoding", "quoted-printable")
		partWriter, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		encoded := quotedprintable.NewWriter(partWriter)
		if _, err := encoded.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := encoded.Close(); err != nil {
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
	content, err := s.template().Render(purpose, link)
	if err != nil {
		return err
	}
	return s.send(ctx, to, content)
}

func (s *SMTP) SendNotification(ctx context.Context, to, title, body, link string) error {
	content, err := s.template().RenderNotification(title, body, link)
	if err != nil {
		return err
	}
	return s.send(ctx, to, content)
}

func (s *SMTP) template() Template {
	if s.Template != nil {
		return *s.Template
	}
	return DefaultTemplate("WishingSkin")
}

func (s *SMTP) send(ctx context.Context, to string, content Content) error {
	if err := s.Validate(); err != nil {
		return err
	}
	address, err := mail.ParseAddress(to)
	if err != nil || address.Address != to || strings.ContainsAny(to, "\r\n") {
		return errors.New("invalid recipient")
	}
	message, err := composeMessage(s.From, to, content)
	if err != nil {
		return errors.New("mail message unavailable")
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
