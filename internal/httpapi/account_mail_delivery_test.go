package httpapi

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/mailer"
)

func accountSMTPFixture(t *testing.T) (string, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	messages := make(chan string, 8)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = io.WriteString(conn, "220 account fixture\r\n")
				r := textproto.NewReader(bufio.NewReader(conn))
				for {
					line, err := r.ReadLine()
					if err != nil {
						return
					}
					switch {
					case line == "DATA":
						_, _ = io.WriteString(conn, "354 send\r\n")
						body, err := io.ReadAll(r.DotReader())
						if err != nil {
							return
						}
						messages <- string(body)
						_, _ = io.WriteString(conn, "250 accepted\r\n")
					case line == "QUIT":
						_, _ = io.WriteString(conn, "221 bye\r\n")
						return
					default:
						_, _ = io.WriteString(conn, "250 ok\r\n")
					}
				}
			}()
		}
	}()
	return listener.Addr().String(), messages
}

func accountMailParts(t *testing.T, messages <-chan string) (string, string) {
	t.Helper()
	var raw string
	select {
	case raw = <-messages:
	case <-time.After(5 * time.Second):
		t.Fatal("no SMTP message received")
	}
	m, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	kind, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/alternative" {
		t.Fatal("missing email alternatives", err)
	}
	reader := multipart.NewReader(m.Body, params["boundary"])
	parts := make(map[string]string)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		media, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		parts[media] = string(data)
	}
	if parts["text/plain"] == "" || parts["text/html"] == "" {
		t.Fatal("missing plain or HTML part")
	}
	return parts["text/plain"], parts["text/html"]
}

func TestAccountHTMLDeliveryAndConsumptionBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			address, messages := accountSMTPFixture(t)
			f.app.cfg.MailSender = &mailer.SMTP{Address: address, From: "sender@example.test", TLSMode: "local"}
			value := mailer.DefaultTemplate("自定义站点")
			value.Verify.Body = "Custom verification delivery"
			value.Reset.Body = "Custom password recovery delivery"
			webStatus(t, f.call(3, "PATCH", "/api/admin/email-template", value), 200)
			for _, kind := range []string{"verify", "reset"} {
				clearMailLimits(f)
				webStatus(t, mailRequest(f, "1@example.test", kind), 202)
				plain, html := accountMailParts(t, messages)
				copy := value.Verify.Body
				if kind == "reset" {
					copy = value.Reset.Body
				}
				if !strings.Contains(plain, copy) || !strings.Contains(html, copy) {
					t.Fatal("account mail did not use saved body in both alternatives")
				}
				_, tail, ok := strings.Cut(plain, "http://example.test/#"+kind+"=")
				if !ok || len(tail) < 64 {
					t.Fatal("missing real account link")
				}
				token := tail[:64]
				if !strings.Contains(html, "href=\"http://example.test/#"+kind+"="+token+"\"") || !strings.Contains(html, "自定义站点") || strings.Contains(html, "样例邮件") {
					t.Fatal("account mail did not use saved template and live action")
				}
				body := map[string]string{"token": token, "newPassword": "Updated123!"}
				webStatus(t, f.call(0, "POST", "/api/auth/"+kind, body), 204)
				webStatus(t, f.call(0, "POST", "/api/auth/"+kind, body), 400)
			}
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "Updated123!"}), 200)
			var verified bool
			if err := f.db.DB.QueryRowContext(context.Background(), "SELECT email_verified FROM account_states WHERE user_id=1").Scan(&verified); err != nil || !verified {
				t.Fatal("email verification did not persist", err)
			}
		})
	}
}
