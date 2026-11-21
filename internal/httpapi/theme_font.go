package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/WishingSkin/WishingSkin/internal/store"
)

const maxThemeFontBytes = 2 << 20

type publicThemeFont struct {
	store.ThemeFontMetadata
	URL string `json:"url,omitempty"`
}

func (a *Server) themeFont(ctx context.Context) (publicThemeFont, error) {
	var value store.ThemeFontMetadata
	_, err := a.readSetting(ctx, store.ThemeFontSetting, &value)
	return summarizeThemeFont(value), err
}

func summarizeThemeFont(stored store.ThemeFontMetadata) publicThemeFont {
	if stored.Version == "" {
		return publicThemeFont{}
	}
	return publicThemeFont{
		ThemeFontMetadata: stored,
		URL:               "/api/theme-font/" + stored.Version,
	}
}

func (a *Server) adminThemeFont(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	stored, err := a.themeFont(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取自定义字体")
		return
	}
	respond(w, 200, stored)
}

func (a *Server) uploadThemeFont(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	select {
	case a.uploads <- struct{}{}:
		defer func() { <-a.uploads }()
	default:
		failure(w, 429, "字体上传繁忙，请稍后重试")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxThemeFontBytes+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		failure(w, 400, "请上传 WOFF2 字体文件")
		return
	}
	var data []byte
	var fileName string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			failure(w, 400, "字体文件不能超过 2 MiB")
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		fileName = sanitizeAssetFilename(part.FileName())
		data, err = io.ReadAll(io.LimitReader(part, maxThemeFontBytes+1))
		_ = part.Close()
		if err != nil {
			failure(w, 400, "读取字体文件失败")
			return
		}
		break
	}
	if len(data) < 4 || len(data) > maxThemeFontBytes || string(data[:4]) != "wOF2" {
		failure(w, 400, "请上传不超过 2 MiB 的有效 WOFF2 字体文件")
		return
	}
	sum := sha256.Sum256(data)
	stored := store.ThemeFontMetadata{
		Version:    hex.EncodeToString(sum[:])[:16],
		FileName:   fileName,
		Size:       len(data),
		UploadedAt: time.Now().UTC().Format(time.RFC3339),
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err = a.s.PutThemeAsset(r.Context(), tx, store.ThemeFontDataSetting, stored.Version, base64.StdEncoding.EncodeToString(data))
	if err == nil {
		err = a.putSetting(r.Context(), tx, store.ThemeFontSetting, stored)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.theme_font", "site", "theme_font")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存自定义字体")
		return
	}
	respond(w, 200, summarizeThemeFont(stored))
}

func (a *Server) deleteThemeFont(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	_, err := tx.ExecContext(r.Context(), a.s.Bind("DELETE FROM settings WHERE setting_key=?"), store.ThemeFontDataSetting)
	if err == nil {
		err = a.putSetting(r.Context(), tx, store.ThemeFontSetting, store.ThemeFontMetadata{})
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.theme_font", "site", "theme_font")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法清除自定义字体")
		return
	}
	respond(w, 200, publicThemeFont{})
}

func (a *Server) serveThemeFont(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	storedVersion, encoded, err := a.s.ReadThemeAsset(r.Context(), store.ThemeFontDataSetting)
	if err != nil {
		failure(w, 503, "暂时无法读取自定义字体")
		return
	}
	if storedVersion == "" || encoded == "" {
		http.NotFound(w, r)
		return
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	if version != "" && version == storedVersion {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=300")
	}
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
