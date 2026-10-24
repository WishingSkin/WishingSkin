package mailer

import (
	"bufio"
	"context"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func smtpFixture(t *testing.T, reject bool) (string, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	messages := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.WriteString(conn, "220 local fixture\r\n")
		r := textproto.NewReader(bufio.NewReader(conn))
		for {
			line, err := r.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				_, _ = io.WriteString(conn, "250 localhost\r\n")
			case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				if reject {
					_, _ = io.WriteString(conn, "550 synthetic rejection\r\n")
				} else {
					_, _ = io.WriteString(conn, "250 ok\r\n")
				}
			case line == "DATA":
				_, _ = io.WriteString(conn, "354 send mail\r\n")
				body, err := io.ReadAll(r.DotReader())
				if err != nil {
					return
				}
				messages <- string(body)
				_, _ = io.WriteString(conn, "250 accepted\r\n")
			case line == "QUIT":
				return // Accepted DATA remains a success even if QUIT disconnects.
			default:
				_, _ = io.WriteString(conn, "502 unavailable\r\n")
			}
		}
	}()
	return listener.Addr().String(), messages
}

func TestLocalDeliveryAndEncoding(t *testing.T) {
	for _, purpose := range []string{"verify", "reset"} {
		t.Run(purpose, func(t *testing.T) {
			address, messages := smtpFixture(t, false)
			s := SMTP{Address: address, From: "site@example.test", TLSMode: "local"}
			link := "https://skin.example.test/#" + purpose + "=synthetic"
			if err := s.Send(context.Background(), "user@example.test", purpose, link); err != nil {
				t.Fatal(err)
			}
			message := <-messages
			parsed, err := mail.ReadMessage(strings.NewReader(message))
			if err != nil {
				t.Fatal(err)
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
			if err != nil || !strings.Contains(subject, "WishingSkin") {
				t.Fatal("subject encoding", err)
			}
			if !strings.Contains(message, link) || !strings.Contains(message, "只能使用一次") {
				t.Fatal("missing one-time link copy")
			}
		})
	}
}

func TestDeliveryRejectsUnsafeOrUnavailableTransport(t *testing.T) {
	for _, test := range []struct {
		mode   string
		reject bool
	}{{"local", true}, {"starttls", false}} {
		t.Run(test.mode, func(t *testing.T) {
			address, _ := smtpFixture(t, test.reject)
			s := SMTP{Address: address, From: "site@example.test", TLSMode: test.mode}
			if s.Send(context.Background(), "user@example.test", "verify", "https://example.test/#verify=synthetic") == nil {
				t.Fatal("unsafe or rejected delivery accepted")
			}
		})
	}
	s := SMTP{Address: "mail.example.test:25", From: "site@example.test", TLSMode: "local"}
	if s.Validate() == nil {
		t.Fatal("cleartext remote allowed")
	}
	s.Address = "127.0.0.1:25"
	s.From = "site@example.test\r\nBcc: hidden@example.test"
	if s.Validate() == nil {
		t.Fatal("header injection allowed")
	}
}

func TestDeliveryCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s := SMTP{Address: listener.Addr().String(), From: "site@example.test", TLSMode: "local"}
	start := time.Now()
	if s.Send(ctx, "user@example.test", "verify", "https://example.test") == nil {
		t.Fatal("cancelled mail accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("context cancellation ignored")
	}
	<-done
}
