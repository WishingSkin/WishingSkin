package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
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
	// Login artwork keeps PNG alpha. Homepage backgrounds intentionally flatten
	// transparency to JPEG, so they use a separate normalizer.
	if body.Image != current {
		body.Image, err = normalizeLoginImage(body.Image)
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
	serveSettingsImage(w, r, value)
}

func normalizeLoginImage(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	prefix, raw, ok := strings.Cut(value, ",")
	if !ok || (prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64") || len(raw) > 2796204 {
		return "", errors.New("登录页图片须为不超过 2 MiB、4096×4096 的 PNG 或 JPEG")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) > 2<<20 {
		return "", errors.New("登录页图片文件过大或格式无效")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return "", errors.New("登录页图片尺寸或格式无效")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("登录页图片数据无效")
	}
	width, height := cfg.Width, cfg.Height
	if width > 1920 {
		height = max(1, height*1920/width)
		width = 1920
	}
	if height > 1920 {
		width = max(1, width*1920/height)
		height = 1920
	}
	var out bytes.Buffer
	if format == "png" {
		result := image.NewNRGBA(image.Rect(0, 0, width, height))
		bounds := source.Bounds()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				result.Set(x, y, source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height))
			}
		}
		if err := png.Encode(&out, result); err != nil {
			return "", err
		}
		if out.Len() > 2<<20 { return "", errors.New("登录页图片编码后超过 2 MiB") }
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
	}
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := source.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, alpha := source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height).RGBA()
			result.SetRGBA(x, y, color.RGBA{uint8((r + 65535 - alpha) >> 8), uint8((g + 65535 - alpha) >> 8), uint8((b + 65535 - alpha) >> 8), 255})
		}
	}
	if err := jpeg.Encode(&out, result, &jpeg.Options{Quality: 85}); err != nil {
		return "", err
	}
	if out.Len() > 2<<20 { return "", errors.New("登录页图片编码后超过 2 MiB") }
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}
