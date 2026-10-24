package httpapi

import (
	"crypto/md5" // Stable legacy record mapping established by the BS importer.
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
)

func (a *Server) legacyRawTexture(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("tid"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	sum := md5.Sum([]byte("bs-texture-record:" + strconv.FormatInt(id, 10)))
	var hash string
	err = a.s.DB.QueryRowContext(r.Context(), a.s.Bind("SELECT file_hash FROM texture_records WHERE record_id=? AND deleted=?"), hex.EncodeToString(sum[:]), false).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		failure(w, 503, "材质暂不可用")
		return
	}
	a.serveTextureHash(w, r, hash)
}

func (a *Server) integration(w http.ResponseWriter, r *http.Request) {
	brand, err := a.siteBranding(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取接入配置")
		return
	}
	root := a.cfg.PublicURL
	respond(w, 200, map[string]any{
		"authlibInjector": root + "/api/yggdrasil/",
		"cslRoot":         root + "/csl/",
		"skinTemplate":    root + "/skin/{USERNAME}.png",
		"capeTemplate":    root + "/cape/{USERNAME}.png",
		"customSkinLoader": map[string]any{"enable": true, "loadlist": []any{
			map[string]string{"name": brand.Name, "type": "CustomSkinAPI", "root": root + "/csl/"},
			map[string]string{"name": "Mojang", "type": "MojangAPI"},
		}},
	})
}
