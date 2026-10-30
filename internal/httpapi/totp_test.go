package httpapi

import (
	"bytes"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/url"
	"strings"
	"testing"
	"time"
	"wishingskin/internal/credential"
)

func TestTOTPReferenceVectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, v := range []struct {
		at   int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		if got := totpCode(secret, v.at/30); got != v.code {
			t.Fatalf("RFC 6238 mismatch at %d", v.at)
		}
	}
	for _, code := range []string{"", "00000", "abcdef", "+12345", "1234567"} {
		if _, ok := validTOTP(secret, code, time.Unix(59, 0)); ok {
			t.Fatal("invalid format accepted")
		}
	}
}

func TestTOTPExpiryAndRecoveryBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			hash, err := credential.Hash("Recovery-test-123!")
			if err != nil {
				t.Fatal(err)
			}
			f.exec("UPDATE users SET password_hash=? WHERE id=1", hash)
			now := time.Now()
			f.app.totpNow = func() time.Time { return now }
			setup := func() string {
				t.Helper()
				w := f.call(1, "POST", "/api/account/totp/setup", map[string]string{"currentPassword": "Recovery-test-123!"})
				totpStatusOnly(t, w, 200)
				var v struct{ Secret, QRCode, OTPAuthURL string }
				if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Secret == "" {
					t.Fatal("missing setup secret")
				}
				uri, err := url.Parse(v.OTPAuthURL)
				if err != nil || uri.Scheme != "otpauth" || uri.Query().Get("secret") != v.Secret || uri.Query().Get("digits") != "6" {
					t.Fatal("invalid authenticator URI")
				}
				data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v.QRCode, "data:image/png;base64,"))
				if err != nil {
					t.Fatal("invalid QR image encoding")
				}
				image, err := png.Decode(bytes.NewReader(data))
				if err != nil || image.Bounds().Dx() != 320 || image.Bounds().Dy() != 320 {
					t.Fatal("invalid QR image")
				}
				return v.Secret
			}
			secret := setup()
			now = now.Add(11 * time.Minute)
			totpStatusOnly(t, f.call(1, "POST", "/api/account/totp/enable", map[string]string{"code": totpCode(secret, now.Unix()/30)}), 400)
			secret = setup()
			w := f.call(1, "POST", "/api/account/totp/enable", map[string]string{"code": totpCode(secret, now.Unix()/30)})
			totpStatusOnly(t, w, 200)
			var old totpStatus
			if json.Unmarshal(w.Body.Bytes(), &old) != nil || len(old.RecoveryCodes) != 10 {
				t.Fatal("missing recovery codes")
			}
			body := map[string]string{"currentPassword": "Recovery-test-123!", "code": old.RecoveryCodes[0]}
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			totpStatusOnly(t, f.call(1, "POST", "/api/account/totp/recovery", body), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			w = f.call(1, "POST", "/api/account/totp/recovery", body)
			totpStatusOnly(t, w, 200)
			var next totpStatus
			if json.Unmarshal(w.Body.Bytes(), &next) != nil || len(next.RecoveryCodes) != 10 || next.RecoveryCodes[0] == old.RecoveryCodes[0] {
				t.Fatal("codes not regenerated")
			}
			login := map[string]string{"email": "1@example.test", "password": "Recovery-test-123!", "totpCode": old.RecoveryCodes[1]}
			totpStatusOnly(t, f.call(0, "POST", "/api/auth/login", login), 401)
			login["totpCode"] = next.RecoveryCodes[0]
			totpStatusOnly(t, f.call(0, "POST", "/api/auth/login", login), 200)
			totpStatusOnly(t, f.call(0, "POST", "/api/auth/login", login), 401)
			for range 21 {
				w = f.call(1, "POST", "/api/account/totp/disable", map[string]string{"currentPassword": "", "code": ""})
			}
			totpStatusOnly(t, w, 429)
		})
	}
}

func TestTOTPLifecycleBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f, _ := accessApp(t, dialect)
			f.app.loginLimit.max = 50
			hash, err := credential.Hash("TOTP-test-password-42!")
			if err != nil {
				t.Fatal(err)
			}
			f.exec("UPDATE users SET password_hash=? WHERE id=1", hash)
			webStatus(t, f.call(0, "POST", "/api/account/totp/setup", nil), 401)
			webStatus(t, f.call(1, "POST", "/api/account/totp/setup", map[string]string{}), 400)
			setup := f.call(1, "POST", "/api/account/totp/setup", map[string]string{"currentPassword": "TOTP-test-password-42!"})
			totpStatusOnly(t, setup, 200)
			var data struct {
				Secret     string
				OtpauthURL string
			}
			if json.Unmarshal(setup.Body.Bytes(), &data) != nil || data.Secret == "" {
				t.Fatal("missing setup")
			}
			var sealed string
			if err := f.db.DB.QueryRow("SELECT secret_encrypted FROM user_totp WHERE user_id=1").Scan(&sealed); err != nil || strings.Contains(sealed, data.Secret) {
				t.Fatal("plaintext TOTP secret")
			}
			code := totpCode(data.Secret, time.Now().Unix()/30)
			webStatus(t, f.call(1, "POST", "/api/account/totp/enable", map[string]string{"code": "invalid"}), 400)
			enabled := f.call(1, "POST", "/api/account/totp/enable", map[string]string{"code": code})
			totpStatusOnly(t, enabled, 200)
			var recovery struct {
				RecoveryCodes []string `json:"recoveryCodes"`
			}
			if json.Unmarshal(enabled.Body.Bytes(), &recovery) != nil || len(recovery.RecoveryCodes) != 10 {
				t.Fatal("missing one-time recovery codes")
			}
			webStatus(t, f.call(1, "POST", "/api/account/totp/setup", map[string]string{"currentPassword": "TOTP-test-password-42!"}), 409)
			login := map[string]string{"email": "1@example.test", "password": "TOTP-test-password-42!"}
			before := 0
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM web_sessions WHERE user_id=1").Scan(&before)
			w := f.call(0, "POST", "/api/auth/login", login)
			webStatus(t, w, 401)
			if !strings.Contains(w.Body.String(), `"totpRequired":true`) {
				t.Fatal("missing second factor challenge")
			}
			if !strings.Contains(w.Body.String(), "此账号已开启双重验证") {
				t.Fatal("missing second factor guidance")
			}
			login["totpCode"] = code
			webStatus(t, f.call(0, "POST", "/api/auth/login", login), 401)
			after := 0
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM web_sessions WHERE user_id=1").Scan(&after)
			if before != after {
				t.Fatal("session issued before second factor")
			}
			for i := 0; i < 5; i++ {
				w = f.call(0, "POST", "/api/auth/login", map[string]string{"email": "1@example.test", "password": "wrong"})
				webStatus(t, w, 401)
			}
			delete(login, "totpCode")
			login["turnstileToken"] = "valid"
			w = f.call(0, "POST", "/api/auth/login", login)
			webStatus(t, w, 401)
			if !strings.Contains(w.Body.String(), `"totpRequired":true`) || !strings.Contains(w.Body.String(), `"captchaRequired":false`) {
				t.Fatal("valid password and challenge did not clear password failures before TOTP")
			}
			delete(login, "turnstileToken")
			login["totpCode"] = recovery.RecoveryCodes[0]
			webStatus(t, f.call(0, "POST", "/api/auth/login", login), 200)
			for i := 0; i < 5; i++ {
				w = f.call(0, "POST", "/api/auth/login", login)
				webStatus(t, w, 401)
				if strings.Contains(w.Body.String(), `"captchaRequired":true`) {
					t.Fatal("reused recovery code triggered password captcha")
				}
			}
			future := time.Now().Add(2 * time.Minute)
			f.app.totpNow = func() time.Time { return future }
			for i := 0; i < 20; i++ {
				login["totpCode"] = "invalid"
				w = f.call(0, "POST", "/api/auth/login", login)
			}
			webStatus(t, w, 429)
			if !strings.Contains(w.Body.String(), `"totpRequired":true`) || !strings.Contains(w.Body.String(), `"captchaRequired":false`) {
				t.Fatal("TOTP rate limit hid the second factor input")
			}
			f.app.totpLimit.now = func() time.Time { return future.Add(6 * time.Minute) }
			login["totpCode"] = totpCode(data.Secret, future.Unix()/30)
			webStatus(t, f.call(0, "POST", "/api/auth/login", login), 200)
			webStatus(t, f.call(1, "POST", "/api/account/totp/disable", map[string]string{"currentPassword": "wrong", "code": recovery.RecoveryCodes[1]}), 400)
			f.exec("ALTER TABLE admin_events RENAME COLUMN action TO unavailable_action")
			webStatus(t, f.call(1, "POST", "/api/account/totp/disable", map[string]string{"currentPassword": "TOTP-test-password-42!", "code": recovery.RecoveryCodes[1]}), 503)
			f.exec("ALTER TABLE admin_events RENAME COLUMN unavailable_action TO action")
			webStatus(t, f.call(1, "POST", "/api/account/totp/disable", map[string]string{"currentPassword": "TOTP-test-password-42!", "code": recovery.RecoveryCodes[1]}), 200)
		})
	}
}
