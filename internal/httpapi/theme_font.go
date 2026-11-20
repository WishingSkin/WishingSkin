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
)

const maxThemeFontBytes = 2 << 20

type storedThemeFont struct {
	Version    string `json:"version"`
	FileName   string `json:"fileName"`
	Size       int    `json:"size"`
	UploadedAt string `json:"uploadedAt"`
	Data       string `json:"data"`
}

type publicThemeFont struct {
	Version    string `json:"version"`
	FileName   string `json:"fileName,omitempty"`
	Size       int    `json:"size,omitempty"`
	UploadedAt string `json:"uploadedAt,omitempty"`
	URL        string `json:"url,omitempty"`
}

func (a *Server) themeFont(ctx context.Context) (storedThemeFont, error) {
	var value storedThemeFont
	found, err := a.readSetting(ctx, "theme_custom_font", &value)
	if err != nil || !found {
		return storedThemeFont{}, err
	}
	return value, nil
}

func summarizeThemeFont(stored storedThemeFont) publicThemeFont {
	if stored.Version == "" || stored.Data == "" {
		return publicThemeFont{}
	}
	return publicThemeFont{
		Version:    stored.Version,
		FileName:   stored.FileName,
		Size:       stored.Size,
		UploadedAt: stored.UploadedAt,
		URL:        "/api/theme-font/" + stored.Version,
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
	respond(w, 200, summarizeThemeFont(stored))
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
	stored := storedThemeFont{
		Version:    hex.EncodeToString(sum[:])[:16],
		FileName:   fileName,
		Size:       len(data),
		UploadedAt: time.Now().UTC().Format(time.RFC3339),
		Data:       base64.StdEncoding.EncodeToString(data),
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err = a.putSetting(r.Context(), tx, "theme_custom_font", stored)
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
	err := a.putSetting(r.Context(), tx, "theme_custom_font", storedThemeFont{})
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
	stored, err := a.themeFont(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取自定义字体")
		return
	}
	if stored.Version == "" || stored.Data == "" {
		http.NotFound(w, r)
		return
	}
	data, err := base64.StdEncoding.DecodeString(stored.Data)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	if version != "" && version == stored.Version {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=300")
	}
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
