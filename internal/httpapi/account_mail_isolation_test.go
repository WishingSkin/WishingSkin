package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type queuedMail struct {
	token   string
	release chan struct{}
}

type pausedMail chan queuedMail

func (m pausedMail) Send(ctx context.Context, _, _, link string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, token, _ := strings.Cut(link, "=")
	mail := queuedMail{token: token, release: make(chan struct{})}
	select {
	case m <- mail:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-mail.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m pausedMail) next(t *testing.T) queuedMail {
	t.Helper()
	select {
	case mail := <-m:
		return mail
	case <-time.After(10 * time.Second):
		t.Fatal("message did not reach SMTP")
		return queuedMail{}
	}
}

func TestRegistrationRechecksPolicyAfterMailBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, _ := accessApp(t, dialect)
			mailbox := make(pausedMail, 1)
			f.app.cfg.MailSender = mailbox
			f.db.DB.SetMaxOpenConns(1)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "open"}), 204)
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				result <- f.call(0, "POST", "/api/auth/register", registrationBody("wait@blocked.example.test"))
			}()
			mail := mailbox.next(t)
			// The settings write needs the only DB connection while SMTP is blocked.
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "open", "blockedEmailDomains": []string{"blocked.example.test"}}), 204)
			close(mail.release)
			webStatus(t, <-result, 400)
			webStatus(t, f.call(0, "POST", "/api/auth/verify", map[string]string{"token": mail.token}), 400)
			clearMailLimits(f)
			f.app.cfg.MailSender = &testMailbox{}
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]any{"mode": "open", "blockedEmailDomains": []string{}}), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/register", registrationBody("wait@blocked.example.test")), 201)
		})
	}
}

func TestConcurrentMailResendsKeepLastCommittedLinkBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, _ := accessApp(t, dialect)
			mailbox := make(pausedMail, 2)
			f.app.cfg.MailSender = mailbox
			request := func() <-chan *httptest.ResponseRecorder {
				result := make(chan *httptest.ResponseRecorder, 1)
				go func() { result <- mailRequest(f, "1@example.test", "reset") }()
				return result
			}
			firstResult := request()
			first := mailbox.next(t)
			// A second request can arrive after the cooldown while the first is still sending.
			f.app.mailCooldown.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
			secondResult := request()
			second := mailbox.next(t)
			close(first.release)
			webStatus(t, <-firstResult, 202)
			close(second.release)
			webStatus(t, <-secondResult, 202)
			reset := func(mail queuedMail) *httptest.ResponseRecorder {
				return f.call(0, "POST", "/api/auth/reset", map[string]string{"token": mail.token, "newPassword": registrationBody("")["password"]})
			}
			webStatus(t, reset(first), 400)
			webStatus(t, reset(second), 204)
		})
	}
}
