package mailer

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
			mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
			if err != nil || mediaType != "multipart/alternative" {
				t.Fatalf("expected multipart alternative, got %q: %v", parsed.Header.Get("Content-Type"), err)
			}
			reader := multipart.NewReader(parsed.Body, params["boundary"])
			var plain, htmlPart string
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				content, err := io.ReadAll(part)
				if err != nil {
					t.Fatal(err)
				}
				switch {
				case strings.HasPrefix(part.Header.Get("Content-Type"), "text/plain"):
					plain = string(content)
				case strings.HasPrefix(part.Header.Get("Content-Type"), "text/html"):
					htmlPart = string(content)
				}
			}
			if !strings.Contains(plain, link) || !strings.Contains(plain, "只能使用一次") {
				t.Fatal("missing one-time link copy in plain part")
			}
			button := "确认邮箱"
			if purpose == "reset" {
				button = "设置新密码"
			}
			if !strings.Contains(htmlPart, "<html") || !strings.Contains(htmlPart, button) || !strings.Contains(htmlPart, `href="`+link+`"`) {
				t.Fatal("missing HTML email CTA")
			}
		})
	}
}

func TestHTMLMailEscapesLinksAndTestTemplateHasNoCTA(t *testing.T) {
	address, messages := smtpFixture(t, false)
	s := SMTP{Address: address, From: "site@example.test", TLSMode: "local"}
	unsafeLink := `https://skin.example.test/#verify=synthetic" onmouseover="alert(1)`
	if err := s.Send(context.Background(), "user@example.test", "verify", unsafeLink); err != nil {
		t.Fatal(err)
	}
	message := <-messages
	parsed, err := mail.ReadMessage(strings.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	var htmlPart string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(part)
		if strings.HasPrefix(part.Header.Get("Content-Type"), "text/html") {
			htmlPart = string(content)
		}
	}
	if strings.Contains(htmlPart, unsafeLink) || !strings.Contains(htmlPart, "&#34; onmouseover=&#34;") {
		t.Fatal("HTML link was not escaped")
	}

	address, messages = smtpFixture(t, false)
	s.Address = address
	if err := s.Send(context.Background(), "user@example.test", "test", ""); err != nil {
		t.Fatal(err)
	}
	message = <-messages
	parsed, err = mail.ReadMessage(strings.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("expected multipart test message, got %q: %v", parsed.Header.Get("Content-Type"), err)
	}
	reader = multipart.NewReader(parsed.Body, params["boundary"])
	var testPlain, testHTML string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(part)
		if strings.HasPrefix(part.Header.Get("Content-Type"), "text/plain") {
			testPlain = string(content)
		} else if strings.HasPrefix(part.Header.Get("Content-Type"), "text/html") {
			testHTML = string(content)
		}
	}
	if !strings.Contains(testPlain, "SMTP 测试邮件") || !strings.Contains(testPlain, "SMTP test message") || !strings.Contains(testHTML, "SMTP 测试") || strings.Contains(testHTML, "href=") {
		t.Fatal("test template should have status without CTA")
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
