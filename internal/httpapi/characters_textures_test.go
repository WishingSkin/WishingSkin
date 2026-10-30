package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"wishingskin/internal/store"
	"wishingskin/internal/testutil"
)

type managementApp struct {
	t   *testing.T
	db  *store.Store
	app *Server
}

func newManagementApp(t *testing.T, dialect string) *managementApp {
	t.Helper()
	db := testutil.Database(t, dialect)
	f := &managementApp{t: t, db: db}
	// Existing closed-site scenarios choose their policy explicitly.
	f.exec("UPDATE settings SET value='closed' WHERE setting_key='registration_mode'")
	for id, permission := range map[int]int{1: 0, 2: 1, 3: 2} {
		f.exec("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt,score,permission,banned) VALUES(?,?,?,?,?,?,?,?,?,?)", id, fmt.Sprintf("%d@example.test", id), fmt.Sprintf("%d@example.test", id), "fixture", "unused", "ARGON2ID", "", 0, permission, false)
		f.exec("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", tokenHash(fmt.Sprintf("%064d", id)), id, time.Now().Add(time.Hour).Unix())
	}
	var err error
	f.app, err = New(db, Config{PublicURL: "http://example.test", TextureDir: t.TempDir(), SettingsKey: bytes.Repeat([]byte{9}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *managementApp) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.DB.Exec(f.db.Bind(query), args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *managementApp) raw(user int, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://example.test"+path, bytes.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Origin", "http://example.test")
	if user > 0 {
		token := fmt.Sprintf("%064d", user)
		r.AddCookie(&http.Cookie{Name: "ws_session", Value: token})
		r.Header.Set("X-CSRF-Token", csrf(token))
	}
	w := httptest.NewRecorder()
	f.app.ServeHTTP(w, r)
	return w
}

func (f *managementApp) call(user int, method, path string, body any) *httptest.ResponseRecorder {
	data, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.raw(user, method, path, "application/json", data)
}

func webStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d want %d: %s", w.Code, want, w.Body)
	}
}

func webCharacter(t *testing.T, w *httptest.ResponseRecorder) Character {
	t.Helper()
	webStatus(t, w, 201)
	var c Character
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID < 1 || c.ID > (1<<53)-1 || len(c.UUID) != 32 {
		t.Fatalf("unsafe character identity: %+v", c)
	}
	return c
}

func TestCharacterManagementBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			webStatus(t, f.call(0, "POST", "/api/characters", map[string]string{"name": "Player"}), 401)
			for _, name := range []string{"ab", "0123456789abcdefg", "玩家名", "invalid-name"} {
				webStatus(t, f.call(1, "POST", "/api/characters", map[string]string{"name": name}), 400)
			}
			r := httptest.NewRequest("POST", "http://example.test/api/characters", strings.NewReader(`{"name":"Player"}`))
			r.AddCookie(&http.Cookie{Name: "ws_session", Value: fmt.Sprintf("%064d", 1)})
			w := httptest.NewRecorder()
			f.app.ServeHTTP(w, r)
			webStatus(t, w, 403)
			var wg sync.WaitGroup
			responses := make(chan *httptest.ResponseRecorder, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					responses <- f.call(1, "POST", "/api/characters", map[string]string{"name": fmt.Sprintf("Concurrent_%d", i)})
				}(i)
			}
			wg.Wait()
			close(responses)
			var c Character
			created := 0
			for w := range responses {
				if w.Code == 201 {
					created++
					c = webCharacter(t, w)
				} else {
					webStatus(t, w, 409)
				}
			}
			if created != 1 {
				t.Fatalf("default limit created %d characters", created)
			}
			path := fmt.Sprintf("/api/characters/%d", c.ID)
			for _, method := range []string{"PATCH", "DELETE"} {
				webStatus(t, f.call(2, method, path, map[string]string{"name": "StolenName"}), 404)
			}
			for _, name := range []string{"Renamed_1", "Renamed_1", "renamed_1"} {
				webStatus(t, f.call(1, "PATCH", path, map[string]string{"name": name}), 200)
			}
			var uuid, name string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT uuid,name FROM characters WHERE id=?"), c.ID).Scan(&uuid, &name); err != nil || uuid != c.UUID || name != "renamed_1" {
				t.Fatal("rename changed identity", uuid, name, err)
			}
			for _, id := range []int{1, 2} {
				webStatus(t, f.call(id, "PATCH", "/api/admin/settings", map[string]int{"characterLimit": 2}), 403)
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/settings", map[string]int{"characterLimit": 0}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/settings", map[string]int{"characterLimit": 2}), 200)
			webStatus(t, f.call(2, "POST", "/api/characters", map[string]string{"name": "RENAMED_1"}), 409)
			webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "SecondPlayer"}))
			webStatus(t, f.call(3, "PATCH", "/api/admin/settings", map[string]int{"characterLimit": 1}), 200)
			webStatus(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "ThirdPlayer"}), 409)
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM characters WHERE user_id=1").Scan(&count); err != nil || count != 2 {
				t.Fatal("lowered limit removed existing characters", count, err)
			}
			gameToken := strings.Repeat("a", 64)
			f.exec("INSERT INTO game_tokens(token_hash,client_token,user_id,profile_uuid,expires_at) VALUES(?,?,?,?,?)", tokenHash(gameToken), "client", 1, c.UUID, time.Now().Add(time.Hour).Unix())
			f.exec("INSERT INTO ygg_join_sessions(token_hash,server_id,profile_uuid,client_ip,expires_at) VALUES(?,?,?,?,?)", tokenHash(gameToken), "server", c.UUID, "127.0.0.1", time.Now().Add(time.Minute).Unix())
			webStatus(t, f.call(1, "DELETE", path, nil), 204)
			webStatus(t, f.call(1, "POST", "/api/yggdrasil/authserver/validate", map[string]string{"accessToken": gameToken}), 403)
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM ygg_join_sessions").Scan(&count); err != nil || count != 0 {
				t.Fatal("deleted character retained joins", count, err)
			}
		})
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (f *managementApp) upload(user int, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	defaults := map[string]string{"name": "Test texture", "kind": "skin", "model": "default"}
	for key, value := range fields {
		defaults[key] = value
	}
	for key, value := range defaults {
		if err := m.WriteField(key, value); err != nil {
			f.t.Fatal(err)
		}
	}
	part, err := m.CreateFormFile("file", "skin.png")
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		f.t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		f.t.Fatal(err)
	}
	return f.raw(user, "POST", "/api/textures", m.FormDataContentType(), b.Bytes())
}

func uploadedRecord(t *testing.T, w *httptest.ResponseRecorder) textureRecord {
	t.Helper()
	webStatus(t, w, 201)
	var record textureRecord
	if err := json.Unmarshal(w.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestTextureManagementBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			// Exercise all PNG validation cases independently of the upload window.
			// Admission quotas are covered by TestUploadRateLimitsBothDatabases.
			f.app.uploadAccountLimit.max = 100
			c := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "TexturePlayer"}))
			path := fmt.Sprintf("/api/characters/%d/textures/skin", c.ID)
			data := testPNG(t, 64, 64)
			first := uploadedRecord(t, f.upload(1, data, map[string]string{"model": "slim", "description": strings.Repeat("😀", 200)}))
			second := uploadedRecord(t, f.upload(2, data, map[string]string{"name": "Other private upload"}))
			if first.ID == second.ID || first.FileHash != second.FileHash || first.Public || second.Public {
				t.Fatal("record/file identity or default visibility incorrect")
			}
			bind := func(user int, recordID string, want int) {
				t.Helper()
				webStatus(t, f.call(user, "PUT", path, map[string]string{"recordId": recordID}), want)
			}
			bind(1, first.ID, 204)
			for _, model := range []string{"default", "slim"} {
				webStatus(t, f.call(1, "PUT", path, map[string]string{"recordId": first.ID, "model": model}), 204)
				var applied, original string
				if err := f.db.DB.QueryRow(f.db.Bind("SELECT model FROM characters WHERE id=?"), c.ID).Scan(&applied); err != nil || applied != model {
					t.Fatal("character model override not applied", applied, err)
				}
				if err := f.db.DB.QueryRow(f.db.Bind("SELECT model FROM texture_records WHERE record_id=?"), first.ID).Scan(&original); err != nil || original != "slim" {
					t.Fatal("upload model changed", original, err)
				}
			}
			webStatus(t, f.call(1, "PUT", path, map[string]string{"recordId": first.ID, "model": "invalid"}), 400)
			webStatus(t, f.call(1, "PUT", path, map[string]string{"recordId": "", "model": "slim"}), 400)
			bind(1, first.ID, 204)
			bind(1, second.ID, 404)
			bind(2, first.ID, 404)
			w := f.call(1, "GET", "/api/textures", nil)
			webStatus(t, w, 200)
			if bytes.Contains(w.Body.Bytes(), []byte(second.ID)) {
				t.Fatal("private metadata leaked through shared image hash")
			}
			third := uploadedRecord(t, f.upload(2, data, map[string]string{"name": "Shared texture", "isPublic": "true"}))
			bind(1, third.ID, 204)
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM textures").Scan(&count); err != nil || count != 1 {
				t.Fatal("bytes were not deduplicated", count, err)
			}
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM texture_records").Scan(&count); err != nil || count != 3 {
				t.Fatal("independent uploads collapsed", count, err)
			}
			webStatus(t, f.call(0, "GET", "/textures/"+first.FileHash, nil), 200)
			for _, fields := range []map[string]string{{"description": strings.Repeat("😀", 201)}, {"kind": "invalid"}, {"model": "invalid"}, {"isPublic": "yes"}, {"name": " "}} {
				webStatus(t, f.upload(1, data, fields), 400)
			}
			boundary := make([]byte, maxTextureBytes)
			copy(boundary, data)
			uploadedRecord(t, f.upload(1, boundary, nil))
			for _, bad := range [][]byte{[]byte("not PNG"), data[:len(data)/2], testPNG(t, 63, 64), testPNG(t, 2048, 1024), make([]byte, maxTextureBytes+1)} {
				webStatus(t, f.upload(1, bad, nil), 400)
			}
			webStatus(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"kind": "cape"}), 400)
			cape := uploadedRecord(t, f.upload(1, testPNG(t, 64, 32), map[string]string{"kind": "cape"}))
			bind(1, cape.ID, 404)
			webStatus(t, f.call(1, "PUT", fmt.Sprintf("/api/characters/%d/textures/cape", c.ID), map[string]string{"recordId": cape.ID}), 204)
			bind(1, "", 204)
			bind(1, "", 204)
			bind(1, first.ID, 204)
			f.exec("UPDATE textures SET deleted=? WHERE hash=?", true, first.FileHash)
			bind(1, third.ID, 404)
			webStatus(t, f.upload(1, data, nil), 409)
			webStatus(t, f.call(0, "GET", "/textures/"+first.FileHash, nil), 404)
			var hash string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT skin_hash FROM characters WHERE id=?"), c.ID).Scan(&hash); err != nil || hash != first.FileHash {
				t.Fatal("failed bind changed appearance", hash, err)
			}
			f.app.uploads <- struct{}{}
			f.app.uploads <- struct{}{}
			webStatus(t, f.upload(1, data, nil), 429)
			<-f.app.uploads
			<-f.app.uploads
		})
	}
}

func TestTextureDatabaseFailureAndRetry(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			if dialect == "mysql" {
				f.exec("CREATE TRIGGER ws_upload_fail BEFORE INSERT ON texture_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected upload failure'")
			} else {
				f.exec("CREATE FUNCTION ws_upload_fail_fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected upload failure'; END $$")
				f.exec("CREATE TRIGGER ws_upload_fail BEFORE INSERT ON texture_records FOR EACH ROW EXECUTE FUNCTION ws_upload_fail_fn()")
			}
			data := testPNG(t, 64, 64)
			sum := sha256.Sum256(data)
			hash := hex.EncodeToString(sum[:])
			webStatus(t, f.upload(1, data, nil), 503)
			webStatus(t, f.call(0, "GET", "/textures/"+hash, nil), 404)
			if actual, err := os.ReadFile(filepath.Join(f.app.cfg.TextureDir, hash)); err != nil || !bytes.Equal(actual, data) {
				t.Fatal("failed transaction left incomplete file", err)
			}
			if dialect == "mysql" {
				f.exec("DROP TRIGGER ws_upload_fail")
			} else {
				f.exec("DROP TRIGGER ws_upload_fail ON texture_records")
				f.exec("DROP FUNCTION ws_upload_fail_fn()")
			}
			uploadedRecord(t, f.upload(1, data, nil))
			webStatus(t, f.call(0, "GET", "/textures/"+hash, nil), 200)
			// Concurrent requests share bytes but must each commit a separate owned record.
			var wg sync.WaitGroup
			responses := make(chan *httptest.ResponseRecorder, 2)
			for user := 1; user <= 2; user++ {
				wg.Add(1)
				go func(user int) { defer wg.Done(); responses <- f.upload(user, data, nil) }(user)
			}
			wg.Wait()
			close(responses)
			for response := range responses {
				uploadedRecord(t, response)
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM texture_records").Scan(&count); err != nil || count != 3 {
				t.Fatal("concurrent uploads lost records", count, err)
			}
		})
	}
}

func TestTextureFilePublication(t *testing.T) {
	dir := t.TempDir()
	data := testPNG(t, 64, 64)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- saveTexture(dir, hash, data) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != hash {
		t.Fatal("staging files left behind", files, err)
	}
	if err := saveTexture(dir, hash, []byte("different bytes")); err == nil {
		t.Fatal("existing hash file was overwritten")
	}
	actual, err := os.ReadFile(filepath.Join(dir, hash))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("published file changed", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "blocked"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveTexture(dir, "blocked", data); err == nil {
		t.Fatal("directory accepted as existing image")
	}
}
