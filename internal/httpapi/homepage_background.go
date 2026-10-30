package httpapi

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strings"
)

type homepageBackground struct {
	Image    string `json:"image,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
	Overlay  int    `json:"overlay"`
}

func publicHomepage(value homepageSettings) homepageSettings {
	if value.Background.Image != "" {
		value.Background.ImageURL = "/api/homepage/background?v=" + tokenHash(value.Background.Image)[:16]
	}
	value.Background.Image = ""
	return value
}

func (a *Server) adminHomepage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	value, err := a.homepage(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取首页设置")
		return
	}
	respond(w, 200, value)
}

func (a *Server) homepageBackgroundImage(w http.ResponseWriter, r *http.Request) {
	value, err := a.homepage(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取首页设置")
		return
	}
	serveSettingsJPEG(w, r, value.Background.Image)
}

func serveSettingsJPEG(w http.ResponseWriter, r *http.Request, value string) {
	serveSettingsImage(w, r, value)
}

func serveSettingsImage(w http.ResponseWriter, r *http.Request, value string) {
	if value == "" {
		http.NotFound(w, r)
		return
	}
	_, raw, _ := strings.Cut(value, ",")
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	etag := `"` + tokenHash(value) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	contentType := "image/jpeg"
	if strings.HasPrefix(value, "data:image/png;base64,") {
		contentType = "image/png"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	_, _ = w.Write(data)
}

func normalizeHomepageBackground(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	prefix, raw, ok := strings.Cut(value, ",")
	if !ok || (prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64") || len(raw) > 2796204 {
		return "", errors.New("首页背景须为不超过 2 MiB、4096×4096 的 PNG 或 JPEG")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) > 2<<20 {
		return "", errors.New("首页背景文件过大或格式无效")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return "", errors.New("首页背景尺寸或格式无效")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("首页背景图片数据无效")
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
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			r, g, b, alpha := source.At(x*cfg.Width/width, y*cfg.Height/height).RGBA()
			result.SetRGBA(x, y, color.RGBA{uint8((r + 65535 - alpha) >> 8), uint8((g + 65535 - alpha) >> 8), uint8((b + 65535 - alpha) >> 8), 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, result, &jpeg.Options{Quality: 85}); err != nil || out.Len() > 2<<20 {
		return "", errors.New("首页背景编码后超过 2 MiB")
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}
