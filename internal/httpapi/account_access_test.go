package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/bootstrap"
	"github.com/WishingSkin/WishingSkin/internal/testutil"
	"github.com/WishingSkin/WishingSkin/internal/turnstile"
)

type capturedMail struct{ To, Purpose, Link string }
type testMailbox struct {
	mu       sync.Mutex
	messages []capturedMail
	fail     bool
}

func (m *testMailbox) Send(_ context.Context, to, purpose, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("synthetic mail rejection")
	}
	m.messages = append(m.messages, capturedMail{to, purpose, link})
	return nil
}
func (m *testMailbox) token() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Split(m.messages[len(m.messages)-1].Link, "=")[1]
}

type testChallenge struct{}

func (testChallenge) Verify(_ context.Context, token, _, _ string) error {
	if token != "valid" {
		return turnstile.ErrInvalid
	}
	return nil
}
func accessApp(t *testing.T, dialect string) (*managementApp, *testMailbox) {
	f := newManagementApp(t, dialect)
	m := &testMailbox{}
	f.app.cfg.MailSender, f.app.cfg.Challenge, f.app.cfg.TurnstileSiteKey = m, testChallenge{}, "synthetic-site-key"
	return f, m
}
func registrationBody(email string) map[string]string {
	return map[string]string{"email": email, "password": "Register123!", "nickname": "新账号", "turnstileToken": "valid"}
}
func clearMailLimits(f *managementApp) {
	f.app.mailLimit.entries = map[string]bucket{}
	f.app.mailCooldown.entries = map[string]bucket{}
}
func mailRequest(f *managementApp, email, purpose string) *httptest.ResponseRecorder {
	return f.call(0, "POST", "/api/auth/email", map[string]string{"email": email, "purpose": purpose})
}

func TestAccountRegistrationAndVerificationBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			body := registrationBody("New@Example.test")
			webStatus(t, f.call(0, "POST", "/api/auth/register", body), 403)
			webStatus(t, f.call(1, "PATCH", "/api/admin/registration", map[string]string{"mode": "open"}), 403)
			f.app.cfg.Challenge = nil
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "open"}), 400)
			f.app.cfg.Challenge = testChallenge{}
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "open"}), 204)
			body["turnstileToken"] = "invalid"
			webStatus(t, f.call(0, "POST", "/api/auth/register", body), 403)
			body["turnstileToken"] = "valid"
			webStatus(t, f.call(0, "POST", "/api/auth/register", body), 201)
			token := mailbox.token()
			var id, expires int64
			var stored string
			if err := f.db.DB.QueryRow("SELECT u.id,t.token_hash,t.expires_at FROM users u JOIN account_tokens t ON t.user_id=u.id WHERE email_key='new@example.test'").Scan(&id, &stored, &expires); err != nil {
				t.Fatal(err)
			}
			if stored == token || stored != tokenHash(token) || expires < time.Now().Add(23*time.Hour).Unix() || expires > time.Now().Add(24*time.Hour+time.Minute).Unix() {
				t.Fatal("unsafe verification token storage or expiry")
			}
			login := map[string]string{"email": "new@example.test", "password": "Register123!"}
			webStatus(t, f.call(0, "POST", "/api/auth/login", login), 403)
			webStatus(t, f.call(0, "POST", "/api/yggdrasil/authserver/authenticate", map[string]string{"username": "new@example.test", "password": "Register123!"}), 403)
			// Even an injected session cannot bypass the pending account state.
			f.exec("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", tokenHash(strings.Repeat("a", 64)), id, time.Now().Add(time.Hour).Unix())
			r := httptest.NewRequest("GET", "http://example.test/api/characters", nil)
			r.AddCookie(&http.Cookie{Name: "ws_session", Value: strings.Repeat("a", 64)})
			w := httptest.NewRecorder()
			f.app.ServeHTTP(w, r)
			webStatus(t, w, 401)
			f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,expires_at) VALUES(?,?,?,?)", tokenHash("injected-pending-token"), "client", id, time.Now().Add(time.Hour).Unix())
			webStatus(t, f.call(0, "POST", "/api/yggdrasil/authserver/validate", map[string]string{"accessToken": "injected-pending-token"}), 403)
			webStatus(t, f.call(0, "POST", "/api/yggdrasil/authserver/refresh", map[string]string{"accessToken": "injected-pending-token", "clientToken": "client"}), 403)
			webStatus(t, f.call(0, "POST", "/api/yggdrasil/authserver/signout", map[string]string{"username": "new@example.test", "password": "Register123!"}), 403)
			webStatus(t, f.call(0, "GET", "/api/auth/verify?token="+token, nil), 404)
			webStatus(t, f.call(0, "POST", "/api/auth/reset", map[string]string{"token": token, "newPassword": "Changed123!"}), 400)
			webStatus(t, f.call(0, "POST", "/api/auth/verify", map[string]string{"token": token}), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/verify", map[string]string{"token": token}), 400)
			webStatus(t, f.call(0, "POST", "/api/auth/login", login), 200)
			var session struct{ User User }
			if json.Unmarshal(f.call(1, "GET", "/api/auth/me", nil).Body.Bytes(), &session) != nil || session.User.EmailVerified || !session.User.LegacyAccount {
				t.Fatal("legacy account verification was invented")
			}
			clearMailLimits(f)
			webStatus(t, f.call(0, "POST", "/api/auth/register", registrationBody("NEW@example.test")), 409)
		})
	}
}

func TestRecoveryConsumptionAndPendingGateBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			seedPassword(t, f, 1, "old")
			f.exec("INSERT INTO account_states(user_id,email_verified,verification_required) VALUES(1,FALSE,TRUE)")
			webStatus(t, mailRequest(f, "1@example.test", "reset"), 202)
			token := mailbox.token()
			var wg sync.WaitGroup
			results := make(chan int, 2)
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					results <- f.call(0, "POST", "/api/auth/reset", map[string]string{"token": token, "newPassword": "Changed123!"}).Code
				}()
			}
			wg.Wait()
			close(results)
			var successes int
			for code := range results {
				if code == 204 {
					successes++
				} else if code != 400 {
					t.Fatal("reset race", code)
				}
			}
			if successes != 1 {
				t.Fatal("one reset link used more than once", successes)
			}
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "Changed123!"}), 403)
			clearMailLimits(f)
			webStatus(t, mailRequest(f, "1@example.test", "verify"), 202)
			verification := mailbox.token()
			f.exec("UPDATE users SET banned=TRUE WHERE id=1")
			webStatus(t, f.call(0, "POST", "/api/auth/verify", map[string]string{"token": verification}), 400)
			f.exec("UPDATE users SET banned=FALSE WHERE id=1")
			webStatus(t, f.call(0, "POST", "/api/auth/verify", map[string]string{"token": verification}), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "Changed123!"}), 200)
		})
	}
}

func TestInvitationAtomicityAndMailRollbackBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "invitation"}), 204)
			w := f.call(3, "POST", "/api/admin/invitations", map[string]int{"uses": 1, "hours": 24})
			webStatus(t, w, 201)
			var invitation struct{ Code string }
			if json.Unmarshal(w.Body.Bytes(), &invitation) != nil {
				t.Fatal("invite response")
			}
			body := registrationBody("rollback@example.test")
			body["invitation"] = invitation.Code
			mailbox.fail = true
			webStatus(t, f.call(0, "POST", "/api/auth/register", body), 503)
			var users, uses int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM users WHERE email_key='rollback@example.test'").Scan(&users); err != nil || users != 0 {
				t.Fatal("mail failure created account", err)
			}
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT remaining_uses FROM invitation_codes WHERE code_hash=?"), tokenHash(invitation.Code)).Scan(&uses); err != nil || uses != 1 {
				t.Fatal("mail failure consumed invite", err)
			}
			mailbox.fail = false
			clearMailLimits(f)
			var wg sync.WaitGroup
			responses := make(chan int, 2)
			for _, email := range []string{"race1@example.test", "race2@example.test"} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					body := registrationBody(email)
					body["invitation"] = invitation.Code
					responses <- f.call(0, "POST", "/api/auth/register", body).Code
				}()
			}
			wg.Wait()
			close(responses)
			success := 0
			for status := range responses {
				if status == 201 {
					success++
				} else if status != 400 {
					t.Fatal("unexpected invite race", status)
				}
			}
			if success != 1 {
				t.Fatal("last invitation use not atomic", success)
			}
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT remaining_uses FROM invitation_codes WHERE code_hash=?"), tokenHash(invitation.Code)).Scan(&uses); err != nil || uses != 0 {
				t.Fatal("invite remaining", err)
			}
		})
	}
}

func TestMailResendAndRecoveryBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, mailbox := accessApp(t, dialect)
			seedPassword(t, f, 1, "old")
			webStatus(t, mailRequest(f, "1@example.test", "reset"), 202)
			first := mailbox.token()
			webStatus(t, mailRequest(f, "1@example.test", "reset"), 429)
			clearMailLimits(f)
			mailbox.fail = true
			webStatus(t, mailRequest(f, "1@example.test", "reset"), 503)
			var count int
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT COUNT(*) FROM account_tokens WHERE token_hash=?"), tokenHash(first)).Scan(&count); err != nil || count != 1 {
				t.Fatal("failed resend invalidated previous link", err)
			}
			clearMailLimits(f)
			mailbox.fail = false
			webStatus(t, mailRequest(f, "1@example.test", "reset"), 202)
			second := mailbox.token()
			webStatus(t, f.call(0, "POST", "/api/auth/reset", map[string]string{"token": first, "newPassword": "Changed123!"}), 400)
			f.exec("UPDATE account_tokens SET expires_at=? WHERE user_id=1", time.Now().Add(-time.Second).Unix())
			webStatus(t, f.call(0, "POST", "/api/auth/reset", map[string]string{"token": second, "newPassword": "Changed123!"}), 400)
			clearMailLimits(f)
			webStatus(t, mailRequest(f, "1@example.test", "reset"), 202)
			token := mailbox.token()
			var expiry int64
			if err := f.db.DB.QueryRow("SELECT expires_at FROM account_tokens WHERE user_id=1").Scan(&expiry); err != nil || expiry < time.Now().Add(29*time.Minute).Unix() || expiry > time.Now().Add(31*time.Minute).Unix() {
				t.Fatal("recovery lifetime", err)
			}
			webStatus(t, f.call(0, "POST", "/api/auth/reset", map[string]string{"token": token, "newPassword": "weak"}), 400)
			f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,expires_at) VALUES(?,?,?,?)", tokenHash("previous-game-token"), "client", 1, time.Now().Add(time.Hour).Unix())
			f.exec("ALTER TABLE game_tokens RENAME COLUMN user_id TO unavailable_user_id")
			webStatus(t, f.call(0, "POST", "/api/auth/reset", map[string]string{"token": token, "newPassword": "Changed123!"}), 503)
			f.exec("ALTER TABLE game_tokens RENAME COLUMN unavailable_user_id TO user_id")
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 200)
			webStatus(t, f.call(0, "POST", "/api/auth/reset", map[string]string{"token": token, "newPassword": "Changed123!"}), 204)
			webStatus(t, f.call(0, "POST", "/api/auth/reset", map[string]string{"token": token, "newPassword": "Another123!"}), 400)
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
			webStatus(t, f.call(2, "GET", "/api/auth/me", nil), 200)
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "old"}), 401)
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "Changed123!"}), 200)
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM game_tokens WHERE user_id=1").Scan(&count); err != nil || count != 0 {
				t.Fatal("game token survived reset", err)
			}
			clearMailLimits(f)
			webStatus(t, mailRequest(f, "unknown@example.test", "reset"), 202)
		})
	}
}

func TestLoginChallengeAndRecoveryWindowBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, _ := accessApp(t, dialect)
			seedPassword(t, f, 1, "old")
			for i := 0; i < 5; i++ {
				w := f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "wrong"})
				webStatus(t, w, 401)
				if i == 4 && !strings.Contains(w.Body.String(), `"captchaRequired":true`) {
					t.Fatal("fifth failure did not require challenge")
				}
			}
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "old"}), 403)
			f.app.cfg.Challenge = testChallenge{}
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "old", "turnstileToken": "valid"}), 200)
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "old"}), 200)
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "old"}), 200)
		})
	}
}

type unavailableChallenge struct{}

func (unavailableChallenge) Verify(context.Context, string, string, string) error {
	return errors.New("synthetic provider unavailable")
}

func TestLoginChallengeFreshPageAndIPWindowBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, _ := accessApp(t, dialect)
			seedPassword(t, f, 1, "old")
			seedPassword(t, f, 2, "other")
			for i := 0; i < 5; i++ {
				w := f.call(0, "POST", "/api/auth/login", map[string]string{"email": " 1@EXAMPLE.TEST ", "password": "wrong"})
				webStatus(t, w, 401)
				if strings.Contains(w.Body.String(), `"captchaRequired":true`) != (i == 4) {
					t.Fatal("incorrect challenge threshold")
				}
			}
			f.app.cfg.Challenge = unavailableChallenge{}
			w := f.call(0, "POST", "/api/auth/login", map[string]string{"email": "2@example.test", "password": "other"})
			webStatus(t, w, 503)
			if !strings.Contains(w.Body.String(), `"captchaRequired":true`) || len(w.Result().Cookies()) != 0 {
				t.Fatal("fresh page lost challenge or bypassed IP gate")
			}
			f.app.cfg.Challenge = testChallenge{}
			f.app.loginLimit.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
			f.app.loginFailures.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
			webStatus(t, f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "old"}), 200)
		})
	}
}

func TestLoginWithoutTurnstileUsesRetryWindowBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			seedPassword(t, f, 1, "old")
			now := time.Now()
			f.app.loginFailures.now = func() time.Time { return now }
			f.app.loginLimit.now = func() time.Time { return now }
			for range 5 {
				w := f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "wrong"})
				webStatus(t, w, 401)
				if strings.Contains(w.Body.String(), `"captchaRequired":true`) {
					t.Fatal("requested an unconfigured challenge")
				}
			}
			now = now.Add(10 * time.Minute)
			login := map[string]string{"email": "1@example.test", "password": "old"}
			w := f.call(0, "POST", "/api/auth/login", login)
			webStatus(t, w, 429)
			if w.Header().Get("Retry-After") != "300" || strings.Contains(w.Body.String(), `"captchaRequired":true`) {
				t.Fatal("login did not report the remaining retry window")
			}
			now = now.Add(5 * time.Minute)
			webStatus(t, f.call(0, "POST", "/api/auth/login", login), 200)
		})
	}
}

func TestRegistrationAdministrationAuditBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, _ := accessApp(t, dialect)
			// Enable the module without an audit event so this case isolates audit rollback.
			f.exec("INSERT INTO settings(setting_key,value) VALUES('official_invitation',?)", "{\"enabled\":true,\"required\":true}")
			f.exec("UPDATE plugins SET enabled=TRUE WHERE plugin_id='wishingskin.invitations'")
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "open"}), 503)
			webStatus(t, f.call(3, "POST", "/api/admin/invitations", map[string]int{"uses": 1, "hours": 24}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			var mode string
			var count int
			if err := f.db.DB.QueryRow("SELECT value FROM settings WHERE setting_key='registration_mode'").Scan(&mode); err != nil || mode != "closed" {
				t.Fatal("unaudited registration change committed", err)
			}
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM invitation_codes").Scan(&count); err != nil || count != 0 {
				t.Fatal("unaudited invitation committed", err)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/registration", map[string]string{"mode": "invitation"}), 204)
			response := f.call(3, "POST", "/api/admin/invitations", map[string]int{"uses": 1, "hours": 24})
			webStatus(t, response, 201)
			var invitation struct{ Code string }
			if err := json.Unmarshal(response.Body.Bytes(), &invitation); err != nil {
				t.Fatal(err)
			}
			var action, kind, target string
			if err := f.db.DB.QueryRow("SELECT action,target_type,target_id FROM admin_events WHERE action='invitation.create' AND actor_id=3").Scan(&action, &kind, &target); err != nil || kind != "invitation" || target != tokenHash(invitation.Code) || target == invitation.Code {
				t.Fatal("invitation audit leaked code or lost target", err)
			}
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_events WHERE actor_id=3").Scan(&count); err != nil || count != 2 {
				t.Fatal("audit count", err)
			}
		})
	}
}

func TestLocalAdministratorAccessBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			if _, err := bootstrap.CreateAdmin(context.Background(), db, "admin@example.test", "Local admin", "Initial123!"); err != nil {
				t.Fatal(err)
			}
			app, err := New(db, Config{PublicURL: "http://example.test", TextureDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			f := &managementApp{t: t, db: db, app: app}
			response := f.call(0, "POST", "/api/auth/login", map[string]string{"email": "admin@example.test", "password": "Initial123!"})
			webStatus(t, response, 200)
			var result struct{ User User }
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result.User.IsAdmin || result.User.EmailVerified || result.User.LegacyAccount {
				t.Fatal("local administrator state", err)
			}
			webStatus(t, f.call(0, "POST", "/api/yggdrasil/authserver/authenticate", map[string]string{"username": "admin@example.test", "password": "Initial123!"}), 200)
		})
	}
}
