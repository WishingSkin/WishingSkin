package httpapi

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestLegacyPublicCompatibilityBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			character := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "LegacyPlayer"}))
			record := uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), map[string]string{"model": "slim", "name": "Legacy skin"}))
			webStatus(t, f.call(1, "PUT", "/api/characters/"+strconv.FormatInt(character.ID, 10)+"/textures/skin", map[string]string{"recordId": record.ID}), 204)
			for _, path := range []string{"/LegacyPlayer.json", "/legacyplayer.json", "/csl/LegacyPlayer.json"} {
				w := f.call(0, "GET", path, nil)
				webStatus(t, w, 200)
				var profile struct {
					Username string         `json:"username"`
					Skins    map[string]any `json:"skins"`
					Cape     any            `json:"cape"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || profile.Username != "LegacyPlayer" || profile.Skins["slim"] != record.FileHash || profile.Cape != nil {
					t.Fatal("legacy profile shape", w.Body.String(), err)
				}
			}
			for _, path := range []string{"/textures/" + record.FileHash, "/csl/textures/" + record.FileHash, "/skin/LegacyPlayer.png", "/csl/skin/LegacyPlayer.png"} {
				w := f.call(0, "GET", path, nil)
				webStatus(t, w, 200)
				if !bytes.Equal(w.Body.Bytes(), testPNG(t, 64, 64)) || w.Header().Get("Content-Type") != "image/png" {
					t.Fatal("legacy bytes changed", path)
				}
				head := f.call(0, "HEAD", path, nil)
				webStatus(t, head, 200)
				if head.Body.Len() != 0 {
					t.Fatal("HEAD returned bytes")
				}
			}
			// Source tid maps to the stable imported record, independent of file hash.
			sum := md5.Sum([]byte("bs-texture-record:11"))
			legacyID := hex.EncodeToString(sum[:])
			f.exec("UPDATE texture_records SET record_id=? WHERE record_id=?", legacyID, record.ID)
			webStatus(t, f.call(0, "GET", "/raw/11", nil), 200)
			webStatus(t, f.call(0, "GET", "/raw/12", nil), 404)
			for _, path := range []string{"/cape/LegacyPlayer.png", "/csl/cape/LegacyPlayer.png", "/missing.json"} {
				webStatus(t, f.call(0, "GET", path, nil), 404)
			}
			f.exec("UPDATE users SET banned=? WHERE id=?", true, 1)
			for _, path := range []string{"/LegacyPlayer.json", "/skin/LegacyPlayer.png"} {
				webStatus(t, f.call(0, "GET", path, nil), 403)
			}
			f.exec("UPDATE users SET banned=? WHERE id=?", false, 1)
			f.exec("UPDATE textures SET deleted=? WHERE hash=?", true, record.FileHash)
			for _, path := range []string{"/skin/LegacyPlayer.png", "/raw/11", "/csl/textures/" + record.FileHash} {
				r := httptest.NewRequest("GET", "http://example.test"+path, nil)
				r.Header.Set("If-None-Match", `"`+record.FileHash+`"`)
				w := httptest.NewRecorder()
				f.app.ServeHTTP(w, r)
				webStatus(t, w, 404)
			}
			profile := f.call(0, "GET", "/csl/LegacyPlayer.json", nil)
			webStatus(t, profile, 200)
			if bytes.Contains(profile.Body.Bytes(), []byte(record.FileHash)) || !bytes.Contains(profile.Body.Bytes(), []byte(`"default":null`)) {
				t.Fatal("disabled image leaked in legacy profile", profile.Body.String())
			}
			f.exec("INSERT INTO migration_state(id,source_fingerprint,status,report) VALUES(1,?,'pending','{}')", "fixture")
			for _, path := range []string{"/LegacyPlayer.json", "/raw/11", "/skin/LegacyPlayer.png", "/csl/textures/" + record.FileHash} {
				webStatus(t, f.call(0, "GET", path, nil), 503)
			}
		})
	}
}

func TestIntegrationConfigUsesPublicOriginBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			r := httptest.NewRequest("GET", "http://forged.test/api/integration", nil)
			r.Header.Set("X-Forwarded-Host", "forged.test")
			r.Header.Set("X-Forwarded-Proto", "https")
			w := httptest.NewRecorder()
			f.app.ServeHTTP(w, r)
			webStatus(t, w, 200)
			var data map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || data["authlibInjector"] != "http://example.test/api/yggdrasil/" || data["cslRoot"] != "http://example.test/csl/" || bytes.Contains(w.Body.Bytes(), []byte("forged.test")) {
				t.Fatal("generated config trusted request host", err)
			}
			if len(data) != 5 || bytes.Contains(w.Body.Bytes(), []byte("password")) {
				t.Fatal("unsafe configuration projection")
			}
		})
	}
}
