package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

type homepageFeature struct {
	Icon  string `json:"icon,omitempty"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Href  string `json:"href"`
}

type homepageBlock struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Enabled     bool              `json:"enabled"`
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	ButtonLabel string            `json:"buttonLabel"`
	ButtonHref  string            `json:"buttonHref"`
	Features    []homepageFeature `json:"features,omitempty"`
}

type homepageSettings struct {
	Blocks     []homepageBlock    `json:"blocks"`
	Background homepageBackground `json:"background"`
}

var homepageIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func defaultHomepage() homepageSettings {
	return homepageSettings{Background: homepageBackground{Overlay: 65}, Blocks: []homepageBlock{
		{ID: "intro", Kind: "intro", Enabled: true, Title: "Minecraft 外观工作台", Body: "管理角色、皮肤和披风，在公开库中发现新的外观，并通过 Yggdrasil 接入游戏。", ButtonLabel: "登录工作台", ButtonHref: "/login"},
		{ID: "features", Kind: "features", Enabled: true, Title: "从角色到游戏", Features: []homepageFeature{{Title: "角色与外观", Body: "用模型预览管理标准和纤细模型，皮肤与披风分别绑定。", Href: "/login"}, {Title: "公开库", Body: "浏览公开材质并收藏到自己的衣柜。", Href: "/library"}, {Title: "游戏接入", Body: "使用本站提供的 Yggdrasil 或兼容地址加载外观。", Href: "/login"}}},
		{ID: "notice", Kind: "notice", Enabled: true, Title: "最新公告"},
	}}
}

func (a *Server) homepage(ctx context.Context) (homepageSettings, error) {
	value := defaultHomepage()
	var raw string
	err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT value FROM settings WHERE setting_key=?"), "homepage_blocks").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || strings.TrimSpace(raw) == "" {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	var blocks []homepageBlock
	if err := unmarshalSetting(raw, &blocks); err != nil {
		return value, err
	}
	value.Blocks = blocks
	_, err = a.readSetting(ctx, "homepage_background", &value.Background)
	return value, err
}

func (a *Server) updateHomepage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body homepageSettings
	if !decodeLimit(w, r, &body, 4<<20) {
		return
	}
	if err := validateHomepage(body); err != nil {
		failure(w, 400, err.Error())
		return
	}
	if body.Background.Overlay < 0 || body.Background.Overlay > 100 {
		failure(w, 400, "背景遮罩须为 0–100%")
		return
	}
	select {
	case a.uploads <- struct{}{}:
		defer func() { <-a.uploads }()
	default:
		failure(w, 429, "图片处理繁忙，请稍后重试")
		return
	}
	current, err := a.homepage(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取首页设置")
		return
	}
	// Preserve the saved JPEG when only its overlay or the blocks change.
	if body.Background.Image != current.Background.Image {
		body.Background.Image, err = normalizeHomepageBackground(body.Background.Image)
	}
	if err != nil {
		failure(w, 400, err.Error())
		return
	}
	body.Background.ImageURL = ""
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	if err := a.putSetting(r.Context(), tx, "homepage_blocks", body.Blocks); err != nil {
		failure(w, 503, "暂时无法保存首页设置")
		return
	}
	if err := a.putSetting(r.Context(), tx, "homepage_background", body.Background); err != nil {
		failure(w, 503, "暂时无法保存首页设置")
		return
	}
	if err := a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.homepage", "site", "homepage"); err != nil || tx.Commit() != nil {
		_ = tx.Rollback()
		failure(w, 503, "暂时无法保存首页设置")
		return
	}
	respond(w, 200, body)
}

func validateHomepage(value homepageSettings) error {
	if len(value.Blocks) > 8 {
		return errors.New("首页区块最多 8 个")
	}
	seen := map[string]bool{}
	allowed := map[string]bool{"intro": true, "features": true, "notice": true, "cta": true}
	for _, block := range value.Blocks {
		if !homepageIDPattern.MatchString(block.ID) || seen[block.ID] || !allowed[block.Kind] {
			return errors.New("首页区块标识或类型无效")
		}
		seen[block.ID] = true
		if utf8.RuneCountInString(block.Title) > 80 || utf8.RuneCountInString(block.Body) > 1000 || utf8.RuneCountInString(block.ButtonLabel) > 40 {
			return errors.New("首页区块文案过长")
		}
		for _, text := range []string{block.Title, block.Body, block.ButtonLabel} {
			if strings.ContainsAny(text, "\x00\r") {
				return errors.New("首页区块文案包含无效字符")
			}
		}
		if err := validHomepageHref(block.ButtonHref); err != nil {
			return err
		}
		if len(block.Features) > 6 {
			return errors.New("首页功能卡最多 6 个")
		}
		for _, feature := range block.Features {
			if utf8.RuneCountInString(feature.Icon) > 4 || strings.ContainsAny(feature.Icon, "\r\n") {
				return errors.New("功能卡图标最多 4 个字符且不能换行")
			}
			if utf8.RuneCountInString(feature.Title) > 80 || utf8.RuneCountInString(feature.Body) > 240 {
				return errors.New("首页功能卡文案过长")
			}
			if err := validHomepageHref(feature.Href); err != nil {
				return err
			}
		}
	}
	return nil
}

func validHomepageHref(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || strings.ContainsAny(value, "\r\n") {
		return errors.New("首页按钮链接无效")
	}
	if u.IsAbs() {
		if u.Scheme != "https" || u.Host == "" {
			return errors.New("首页外部链接必须使用 HTTPS")
		}
		return nil
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || u.Host != "" {
		return errors.New("首页链接必须是站内路径或 HTTPS 地址")
	}
	return nil
}

func unmarshalSetting(raw string, target any) error {
	return json.Unmarshal([]byte(raw), target)
}
