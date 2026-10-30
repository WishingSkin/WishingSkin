package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	goldhtml "github.com/yuin/goldmark/renderer/html"
)

type appearance struct {
	LogoImage     string             `json:"logoImage"`
	Accent        string             `json:"accent"`
	DefaultTheme  string             `json:"defaultTheme"`
	SurfaceEffect string             `json:"surfaceEffect"`
	Advanced      advancedAppearance `json:"advanced"`
}

func (a *Server) readSetting(ctx context.Context, key string, target any) (bool, error) {
	var value string
	err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT value FROM settings WHERE setting_key=?"), key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(value), target)
}

func (a *Server) putSetting(ctx context.Context, tx *sql.Tx, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	query := "INSERT INTO settings(setting_key,value) VALUES(?,?) ON CONFLICT(setting_key) DO UPDATE SET value=EXCLUDED.value"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO settings(setting_key,value) VALUES(?,?) ON DUPLICATE KEY UPDATE value=VALUES(value)"
	}
	_, err = tx.ExecContext(ctx, a.s.Bind(query), key, string(data))
	return err
}

func (a *Server) appearance(ctx context.Context) (appearance, error) {
	value := appearance{Accent: "blue", DefaultTheme: "system", SurfaceEffect: "solid"}
	_, err := a.readSetting(ctx, "site_appearance", &value)
	value.Advanced.localizeLegacyCopy()
	return value, err
}

// Images are decoded, bounded and re-encoded; arbitrary SVG, URLs and metadata never reach the page.
func normalizedImage(value string) (string, error) {
	return normalizedImageSize(value, 64)
}

func normalizedLogoImage(value string) (string, error) {
	prefix, raw, ok := strings.Cut(value, ",")
	if !ok || (prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64") || len(raw) > 700000 {
		return "", fmt.Errorf("invalid image")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) > 512<<10 {
		return "", fmt.Errorf("invalid image")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 {
		return "", fmt.Errorf("invalid dimensions")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	target := cfg.Width
	if cfg.Height > target {
		target = cfg.Height
	}
	if target < 64 {
		target = 64
	}
	if target > 512 {
		target = 512
	}
	result := image.NewNRGBA(image.Rect(0, 0, target, target))
	width, height := cfg.Width, cfg.Height
	if width > target {
		height = height * target / width
		width = target
	}
	if height > target {
		width = width * target / height
		height = target
	}
	offsetX, offsetY := (target-width)/2, (target-height)/2
	bounds := source.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/width
			sy := bounds.Min.Y + y*bounds.Dy()/height
			result.Set(offsetX+x, offsetY+y, source.At(sx, sy))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, result); err != nil {
		return "", err
	}
	if out.Len() > 512<<10 {
		return "", fmt.Errorf("encoded image too large")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

func normalizedImageSize(value string, size int) (string, error) {
	prefix, raw, ok := strings.Cut(value, ",")
	if !ok || (prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64") || len(raw) > 700000 {
		return "", fmt.Errorf("invalid image")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) > 512<<10 {
		return "", fmt.Errorf("invalid image")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 {
		return "", fmt.Errorf("invalid dimensions")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	result := image.NewNRGBA(image.Rect(0, 0, size, size))
	bounds := source.Bounds()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			result.Set(x, y, source.At(bounds.Min.X+x*bounds.Dx()/size, bounds.Min.Y+y*bounds.Dy()/size))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, result); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

func renderNotice(value, format string) string {
	if format != "markdown" {
		return "<p>" + strings.ReplaceAll(html.EscapeString(value), "\n", "<br>\n") + "</p>"
	}
	var out bytes.Buffer
	_ = goldmark.New(goldmark.WithRendererOptions(goldhtml.WithUnsafe())).Convert([]byte(value), &out)
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "br", "strong", "b", "em", "i", "s", "del", "blockquote", "ul", "ol", "li", "h2", "h3", "h4", "pre", "code", "hr", "a")
	policy.AllowAttrs("href", "title").OnElements("a")
	policy.AllowStandardURLs()
	policy.RequireNoFollowOnLinks(true)
	return policy.Sanitize(out.String())
}

func (a *Server) previewNotice(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	var body struct{ Notice, Format string }
	if !decode(w, r, &body) {
		return
	}
	if utf8.RuneCountInString(body.Notice) > 1000 || (body.Format != "plain" && body.Format != "markdown") {
		failure(w, 400, "公告格式或长度无效")
		return
	}
	respond(w, 200, map[string]string{"html": renderNotice(body.Notice, body.Format)})
}

func (a *Server) updateAppearance(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body appearance
	if !decodeLimit(w, r, &body, 4<<20) {
		return
	}
	if body.SurfaceEffect == "" {
		body.SurfaceEffect = "solid"
	}
	if body.SurfaceEffect != "solid" && body.SurfaceEffect != "translucent" && body.SurfaceEffect != "glass" {
		failure(w, 400, "面板效果无效")
		return
	}
	if !validAccent(body.Accent) || (body.DefaultTheme != "light" && body.DefaultTheme != "dark" && body.DefaultTheme != "system") {
		failure(w, 400, "个性化设置无效")
		return
	}
	body.Advanced.localizeLegacyCopy()
	if err := body.Advanced.validate(); err != nil {
		failure(w, 400, err.Error())
		return
	}
	if body.LogoImage != "" {
		var err error
		body.LogoImage, err = normalizedLogoImage(body.LogoImage)
		if err != nil {
			failure(w, 400, "请选择不超过 512 KiB、2048×2048 的 PNG 或 JPEG 图片")
			return
		}
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err := a.putSetting(r.Context(), tx, "site_appearance", body)
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.appearance", "site", "appearance")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存站点设置")
		return
	}
	respond(w, 200, body)
}

func (a *Server) registerCustomization() {
	a.mux.HandleFunc("POST /api/admin/notice-preview", a.previewNotice)
	a.mux.HandleFunc("PATCH /api/admin/appearance", a.updateAppearance)
	a.mux.HandleFunc("PATCH /api/admin/homepage", a.updateHomepage)
	a.mux.HandleFunc("GET /api/admin/homepage", a.adminHomepage)
	a.mux.HandleFunc("GET /api/homepage/background", a.homepageBackgroundImage)
	a.mux.HandleFunc("GET /api/admin/login-image", a.adminLoginImage)
	a.mux.HandleFunc("PATCH /api/admin/login-image", a.updateLoginImage)
	a.mux.HandleFunc("GET /api/login/image", a.loginImageFile)
	a.mux.HandleFunc("GET /api/account/profile", a.accountProfile)
	a.mux.HandleFunc("PATCH /api/account/profile", a.updateAccountProfile)
	a.mux.HandleFunc("GET /api/avatars/{id}", a.avatar)
	a.mux.HandleFunc("GET /api/admin/services", a.adminServices)
	a.mux.HandleFunc("PATCH /api/admin/services", a.updateServices)
	a.mux.HandleFunc("POST /api/admin/services/test", a.testServices)
	a.mux.HandleFunc("POST /api/admin/services/turnstile-test", a.testTurnstile)
	a.mux.HandleFunc("GET /api/admin/plugins/invitations", a.invitationPlugin)
	a.mux.HandleFunc("PATCH /api/admin/plugins/invitations", a.updateInvitationPlugin)
	a.mux.HandleFunc("POST /api/admin/plugins/invitations/codes", a.createInvitationBatch)
	a.mux.HandleFunc("GET /api/admin/plugins/invitations/export", a.exportInvitationCodes)
}

func copyFace(skin image.Image) image.Image {
	scale := skin.Bounds().Dx() / 64
	face := image.NewNRGBA(image.Rect(0, 0, 8*scale, 8*scale))
	draw.Draw(face, face.Bounds(), skin, image.Pt(8*scale, 8*scale), draw.Src)
	draw.Draw(face, face.Bounds(), skin, image.Pt(40*scale, 8*scale), draw.Over)
	return face
}

func validAccent(value string) bool {
	switch value {
	case "blue", "green", "violet", "rose", "orange", "teal", "gray":
		return true
	}
	return false
}
