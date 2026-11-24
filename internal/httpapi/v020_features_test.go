package httpapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/credential"
)

func TestYggdrasilNonEmailLoginBothDatabases(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := credential.Hash("PlayerPass!123")
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))

	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.exec("INSERT INTO settings(setting_key,value) VALUES(?,?)", "ygg_private_key", pemKey)
			f.exec("UPDATE users SET password_hash=?,password_method='ARGON2ID',password_salt='' WHERE id=1", hash)
			f.exec("UPDATE settings SET value='3' WHERE setting_key='character_limit'")
			alpha := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "AlphaHero"}))
			beta := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "BetaKnight"}))

			yggCall := func(path string, body any) *httptest.ResponseRecorder {
				y := &yggTestApp{t: t, db: f.db, app: f.app, key: key}
				return y.call("POST", path, body)
			}
			getMeta := func() bool {
				y := &yggTestApp{t: t, db: f.db, app: f.app, key: key}
				w := y.call("GET", "", nil)
				yggStatus(t, w, 200)
				var resp struct{ Meta map[string]any }
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatal(err)
				}
				v, _ := resp.Meta["feature.non_email_login"].(bool)
				return v
			}

			if getMeta() {
				t.Fatal("expected feature.non_email_login=false by default")
			}
			yggStatus(t, yggCall("/authserver/authenticate", map[string]any{"username": "AlphaHero", "password": "PlayerPass!123"}), 403)

			// Only admins may toggle Yggdrasil non-email login.
			webStatus(t, f.call(1, "PATCH", "/api/admin/settings", map[string]any{"yggNonEmailLogin": true}), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/settings", map[string]any{}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/settings", map[string]any{"yggNonEmailLogin": true}), 200)

			if !getMeta() {
				t.Fatal("expected feature.non_email_login=true after enabling")
			}
			siteResp := f.call(0, "GET", "/api/site", nil)
			webStatus(t, siteResp, 200)
			var sitePayload struct {
				YggNonEmailLogin bool `json:"yggNonEmailLogin"`
			}
			if err := json.Unmarshal(siteResp.Body.Bytes(), &sitePayload); err != nil || !sitePayload.YggNonEmailLogin {
				t.Fatalf("expected /api/site yggNonEmailLogin=true, got %s err=%v", siteResp.Body.String(), err)
			}

			// Character-name login auto-selects the matched profile even when the user owns multiple profiles.
			charLogin := readYggToken(t, yggCall("/authserver/authenticate", map[string]any{
				"username":    "  betaknight ",
				"password":    "PlayerPass!123",
				"clientToken": "launcher-1",
				"requestUser": true,
			}))
			if len(charLogin.AvailableProfiles) != 2 || charLogin.SelectedProfile == nil || charLogin.SelectedProfile.ID != beta.UUID || charLogin.SelectedProfile.Name != "BetaKnight" {
				t.Fatalf("unexpected character-name login response: %+v", charLogin)
			}

			// Wrong password with valid character name is rejected.
			yggStatus(t, yggCall("/authserver/authenticate", map[string]any{"username": alpha.Name, "password": "wrong"}), 403)

			// Email login still works and keeps multi-profile selection deferred.
			emailLogin := readYggToken(t, yggCall("/authserver/authenticate", map[string]any{
				"username": "1@example.test",
				"password": "PlayerPass!123",
			}))
			if emailLogin.SelectedProfile != nil || len(emailLogin.AvailableProfiles) != 2 {
				t.Fatalf("expected multi-profile email login to leave selectedProfile unset: %+v", emailLogin)
			}

			// Signout via character name revokes all game tokens for the account.
			yggStatus(t, yggCall("/authserver/signout", map[string]any{"username": alpha.Name, "password": "PlayerPass!123"}), 204)
			yggStatus(t, yggCall("/authserver/validate", map[string]any{"accessToken": charLogin.AccessToken}), 403)
			yggStatus(t, yggCall("/authserver/validate", map[string]any{"accessToken": emailLogin.AccessToken}), 403)
		})
	}
}

func TestLibraryDetailAndNewestSortBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			older := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "AlphaOld", "isPublic": "true"}))
			newer := uploadedRecord(t, f.upload(2, testPNG(t, 64, 64), map[string]string{"name": "BetaNew", "isPublic": "true"}))
			hidden := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"name": "SecretSkin"}))

			f.exec("UPDATE texture_records SET created_at=? WHERE record_id=?", int64(1700001000), older.ID)
			f.exec("UPDATE texture_records SET created_at=? WHERE record_id=?", int64(1700002000), newer.ID)

			w := f.call(0, "GET", "/api/library?sort=newest", nil)
			webStatus(t, w, 200)
			var list struct {
				Textures []libraryRecord `json:"textures"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Textures) != 2 {
				t.Fatalf("unexpected newest list: %v %s", err, w.Body.String())
			}
			if list.Textures[0].ID != newer.ID || list.Textures[0].CreatedAt != 1700002000 || list.Textures[1].ID != older.ID || list.Textures[1].CreatedAt != 1700001000 {
				t.Fatalf("unexpected newest order/timestamps: %+v", list.Textures)
			}

			detailResp := f.call(0, "GET", "/api/library/"+newer.ID, nil)
			webStatus(t, detailResp, 200)
			var detail libraryRecord
			if err := json.Unmarshal(detailResp.Body.Bytes(), &detail); err != nil || detail.ID != newer.ID || detail.CreatedAt != 1700002000 {
				t.Fatalf("unexpected library detail: %v %+v", err, detail)
			}
			webStatus(t, f.call(0, "GET", "/api/library/"+hidden.ID, nil), 404)

			if !isSPAPath("/library/" + newer.ID) || isSPAPath("/library/not-a-valid-hex-id") {
				t.Fatal("SPA route matcher did not match shareable library URL contract")
			}
		})
	}
}

func TestAdminUserControlsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "SlotOne"}))
			webStatus(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "SlotTwo"}), 409)

			// GET /api/admin/users includes score, characterLimit (null by default), and characterCount.
			listResp := f.call(3, "GET", "/api/admin/users", nil)
			webStatus(t, listResp, 200)
			var userList struct {
				Users []adminUser `json:"users"`
			}
			if err := json.Unmarshal(listResp.Body.Bytes(), &userList); err != nil || len(userList.Users) < 3 {
				t.Fatalf("unexpected admin user list: %v %s", err, listResp.Body.String())
			}
			if userList.Users[0].Score != "0" || userList.Users[0].CharacterLimit != nil || userList.Users[0].CharacterCount != 1 {
				t.Fatalf("unexpected initial admin user summary: %+v", userList.Users[0])
			}

			// Authorization & input guards.
			webStatus(t, f.call(2, "PATCH", "/api/admin/users/1", map[string]any{"score": 100}), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"score": -1}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"score": 10, "scoreReason": "bad\nline"}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"characterLimit": -1}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"newPassword": "weak"}), 400)

			// Adjust points up and down and verify point_entries.
			now := time.Unix(1700001000, 0)
			f.app.pointsNow = func() time.Time { return now }
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"score": 150, "scoreReason": "活动奖励"}), 204)
			now = now.Add(time.Second)
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"score": 120}), 204)
			ptsResp := f.call(1, "GET", "/api/points", nil)
			webStatus(t, ptsResp, 200)
			var pts struct {
				Balance string       `json:"balance"`
				Entries []pointEntry `json:"entries"`
			}
			if err := json.Unmarshal(ptsResp.Body.Bytes(), &pts); err != nil || pts.Balance != "120" || len(pts.Entries) != 2 || pts.Entries[0].Kind != "admin" || pts.Entries[0].Delta != "-30" || pts.Entries[1].Delta != "150" {
				t.Fatalf("unexpected points after admin adjustments: %v %+v", err, pts)
			}

			// Per-user character limit override allows exceeding global limit of 1, then clearing restores default.
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"characterLimit": 3}), 204)
			charsResp := f.call(1, "GET", "/api/characters", nil)
			webStatus(t, charsResp, 200)
			var charsPayload struct {
				CharacterLimit int64 `json:"characterLimit"`
			}
			if err := json.Unmarshal(charsResp.Body.Bytes(), &charsPayload); err != nil || charsPayload.CharacterLimit != 3 {
				t.Fatalf("expected effective character limit 3, got %+v err=%v", charsPayload, err)
			}
			webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "SlotTwo"}))
			webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "SlotThree"}))
			webStatus(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "SlotFour"}), 409)

			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"clearCharacterLimit": true}), 204)
			webStatus(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "SlotFour"}), 409)

			// Seed game token and reset password; verify all sessions & game tokens are revoked and new password works.
			f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?,?,?,?,?)",
				strings.Repeat("a", 64), "client", 1, "", time.Now().Add(time.Hour).Unix())
			webStatus(t, f.call(3, "PATCH", "/api/admin/users/1", map[string]any{"newPassword": "ResetPass!2026"}), 204)
			webStatus(t, f.call(1, "GET", "/api/auth/me", nil), 401)
			var remainingGameTokens int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM game_tokens WHERE user_id=1").Scan(&remainingGameTokens); err != nil || remainingGameTokens != 0 {
				t.Fatalf("expected game tokens revoked after password reset, got count=%d err=%v", remainingGameTokens, err)
			}
			loginBody, _ := json.Marshal(map[string]string{"email": "1@example.test", "password": "ResetPass!2026"})
			loginResp := f.raw(0, "POST", "/api/auth/login", "application/json", loginBody)
			if loginResp.Code != http.StatusOK {
				t.Fatalf("expected login with reset password to succeed, got %d: %s", loginResp.Code, loginResp.Body.String())
			}

			// Verify audit log entries and confirm password value is never leaked in audit trail.
			evResp := f.call(3, "GET", "/api/admin/events", nil)
			webStatus(t, evResp, 200)
			if strings.Contains(evResp.Body.String(), "ResetPass!2026") {
				t.Fatal("password reset leaked secret value in audit log")
			}
			for _, action := range []string{"user.adjust_points", "user.character_limit", "user.reset_password"} {
				if !strings.Contains(evResp.Body.String(), action) {
					t.Fatalf("expected %s in admin events: %s", action, evResp.Body.String())
				}
			}
		})
	}
}
