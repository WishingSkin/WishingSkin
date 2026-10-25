package httpapi

import (
	"context"
	"net/http"
	"strings"
)

type loginImageSettings struct {
	Image string `json:"image"`
}

func (a *Server) loginImage(ctx context.Context) (string, error) {
	var value string
	_, err := a.readSetting(ctx, "login_image", &value)
	return value, err
}

func (a *Server) adminLoginImage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	value, err := a.loginImage(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取登录页图片")
		return
	}
	respond(w, 200, loginImageSettings{Image: value})
}

func (a *Server) updateLoginImage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body loginImageSettings
	if !decodeLimit(w, r, &body, 3<<20) {
		return
	}
	select {
	case a.uploads <- struct{}{}:
		defer func() { <-a.uploads }()
	default:
		failure(w, 429, "图片处理繁忙，请稍后重试")
		return
	}
	current, err := a.loginImage(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取登录页图片")
		return
	}
	// Use the same bounded decoding as homepage images; preserve unchanged JPEGs.
	if body.Image != current {
		body.Image, err = normalizeHomepageBackground(body.Image)
	}
	if err != nil {
		failure(w, 400, strings.NewReplacer("首页背景图片", "登录页图片", "首页背景", "登录页图片").Replace(err.Error()))
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err = a.putSetting(r.Context(), tx, "login_image", body.Image)
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.login_image", "site", "login_image")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存登录页图片")
		return
	}
	respond(w, 200, body)
}

func (a *Server) loginImageFile(w http.ResponseWriter, r *http.Request) {
	value, err := a.loginImage(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取登录页图片")
		return
	}
	serveSettingsJPEG(w, r, value)
}
