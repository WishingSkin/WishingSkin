package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type cdnSettings struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"`
	MaxAge  int    `json:"maxAge"`
}

func (a *Server) cdnSignature(hash, expires string) string {
	mac := hmac.New(sha256.New, a.cfg.SettingsKey)
	mac.Write([]byte("wishingskin:cdn:v1:" + hash + ":" + expires))
	return hex.EncodeToString(mac.Sum(nil))
}
func (a *Server) publicTextureHash(r *http.Request, hash string) bool {
	var count int
	err := a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT COUNT(*) FROM texture_records r JOIN textures f ON f.hash=r.file_hash WHERE r.file_hash=? AND r.deleted=FALSE AND f.deleted=FALSE AND r.is_public=TRUE AND "+approvedRecordSQL), hash).Scan(&count)
	return err == nil && count > 0
}
func (a *Server) redirectCDN(w http.ResponseWriter, r *http.Request, hash string) bool {
	var cfg cdnSettings
	if _, err := a.readSetting(r.Context(), "cdn_config", &cfg); err != nil || !cfg.Enabled || len(a.cfg.SettingsKey) != 32 || !a.publicTextureHash(r, hash) {
		return false
	}
	expires := strconv.FormatInt(time.Now().Add(time.Duration(cfg.MaxAge)*time.Second).Unix(), 10)
	q := url.Values{"expires": {expires}, "sig": {a.cdnSignature(hash, expires)}}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, strings.TrimRight(cfg.BaseURL, "/")+"/cdn/textures/"+hash+"?"+q.Encode(), http.StatusTemporaryRedirect)
	return true
}
func (a *Server) cdnTexture(w http.ResponseWriter, r *http.Request) {
	var cfg cdnSettings
	_, err := a.readSetting(r.Context(), "cdn_config", &cfg)
	hash := r.PathValue("hash")
	expires := r.URL.Query().Get("expires")
	at, e := strconv.ParseInt(expires, 10, 64)
	remaining := at - time.Now().Unix()
	sig := a.cdnSignature(hash, expires)
	if err != nil || !cfg.Enabled || e != nil || remaining < 1 || remaining > int64(cfg.MaxAge) || !validHash.MatchString(hash) || !hmac.Equal([]byte(sig), []byte(r.URL.Query().Get("sig"))) || !a.publicTextureHash(r, hash) {
		http.NotFound(w, r)
		return
	}
	f, err := a.openTextureFile(r.Context(), hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age="+strconv.FormatInt(remaining, 10)+", must-revalidate")
	w.Header().Set("ETag", `"`+hash+`"`)
	http.ServeContent(w, r, hash, time.Time{}, f)
}
