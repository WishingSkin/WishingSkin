package httpapi

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type advancedAppearance struct {
	Light         map[string]string            `json:"light,omitempty"`
	Dark          map[string]string            `json:"dark,omitempty"`
	Copy          map[string]string            `json:"copy,omitempty"`
	LocalizedCopy map[string]map[string]string `json:"localizedCopy,omitempty"`
	Icons         map[string]string            `json:"icons,omitempty"`
}

// Preserve legacy overrides in both languages until each language is edited.
func (v *advancedAppearance) localizeLegacyCopy() {
	if len(v.Copy) == 0 {
		return
	}
	if v.LocalizedCopy == nil {
		v.LocalizedCopy = map[string]map[string]string{}
	}
	for _, locale := range []string{"zh-CN", "en"} {
		if v.LocalizedCopy[locale] == nil {
			v.LocalizedCopy[locale] = map[string]string{}
		}
		for key, text := range v.Copy {
			if _, exists := v.LocalizedCopy[locale][key]; !exists {
				v.LocalizedCopy[locale][key] = text
			}
		}
	}
	v.Copy = nil
}

var appearanceColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var appearanceTokens = map[string]bool{"bg": true, "panel": true, "subtle": true, "ink": true, "muted": true, "border": true, "accent": true, "accent-hover": true, "accent-soft": true, "heading": true, "button-text": true, "error": true, "error-bg": true, "nav": true, "login-title": true, "login-body": true, "footer": true}
var appearanceIcons = map[string]bool{"login": true, "characters": true, "materials": true, "wardrobe": true, "account": true, "administration": true, "empty-character": true, "library": true, "arrow": true}

func (v advancedAppearance) validate() error {
	for _, palette := range []map[string]string{v.Light, v.Dark} {
		for key, color := range palette {
			if !appearanceTokens[key] || color != "" && !appearanceColor.MatchString(color) {
				return fmt.Errorf("界面颜色须为 #RRGGBB 格式，且使用支持的颜色项")
			}
		}
	}
	copies := []map[string]string{v.Copy}
	count := len(v.Copy)
	for locale, texts := range v.LocalizedCopy {
		if locale != "zh-CN" && locale != "en" {
			return fmt.Errorf("自定义文案语言无效")
		}
		copies = append(copies, texts)
		count += len(texts)
	}
	if count > 2400 {
		return fmt.Errorf("自定义文案最多 1200 项")
	}
	for _, texts := range copies {
		if len(texts) > 1200 {
			return fmt.Errorf("每种语言的自定义文案最多 1200 项")
		}
		for key, text := range texts {
			if !utf8.ValidString(key) || !utf8.ValidString(text) || strings.TrimSpace(key) == "" || utf8.RuneCountInString(key) > 1000 || utf8.RuneCountInString(text) > 1000 || strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
				return fmt.Errorf("自定义文案最多 1000 字，不能包含控制字符")
			}
		}
	}
	for key, icon := range v.Icons {
		if !appearanceIcons[key] || !utf8.ValidString(icon) || utf8.RuneCountInString(icon) > 8 || strings.IndexFunc(icon, unicode.IsControl) >= 0 || strings.ContainsAny(icon, "<>") {
			return fmt.Errorf("图标须为不超过 8 字的纯文本")
		}
	}
	return nil
}
