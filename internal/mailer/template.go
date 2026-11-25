package mailer

import (
	"bytes"
	"errors"
	"html/template"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Copy struct {
	Subject string `json:"subject"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Button  string `json:"button"`
}

type Template struct {
	BrandName       string `json:"brandName"`
	LogoURL         string `json:"logoUrl"`
	AccentColor     string `json:"accentColor"`
	BackgroundColor string `json:"backgroundColor"`
	Footer          string `json:"footer"`
	Verify          Copy   `json:"verify"`
	Reset           Copy   `json:"reset"`
	Test            Copy   `json:"test"`
}

type Content struct {
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

func DefaultTemplate(name string) Template {
	return Template{
		BrandName: name, AccentColor: "#2563eb", BackgroundColor: "#f4f6fb", Footer: name,
		Verify: Copy{Subject: name + " - Verify your email / 邮箱验证", Title: "邮箱验证 / Verify your email", Body: "请确认此邮箱属于你。\nConfirm your email using the button below.", Button: "确认邮箱 / Confirm email"},
		Reset:  Copy{Subject: name + " - Reset your password / 找回密码", Title: "找回密码 / Reset your password", Body: "通过下面的按钮设置新密码。\nSet a new password using the button below.", Button: "设置新密码 / Set a new password"},
		Test:   Copy{Subject: name + " - SMTP test / SMTP 测试", Title: "SMTP 测试 / SMTP test", Body: "这是一封 SMTP 测试邮件，说明当前配置可以提交邮件。\nThis is an SMTP test message. The current configuration accepted the message for delivery."},
	}
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func safeURL(raw string, logo bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || strings.ContainsAny(raw, "\r\n\x00") {
		return false
	}
	if logo && u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		return u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	}
	return true
}

func validText(s string, max int, required, multiline bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max || required && strings.TrimSpace(s) == "" {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) && !(multiline && (c == '\n' || c == '\r' || c == '\t')) {
			return false
		}
	}
	return true
}

func (t Template) Validate() error {
	if !validText(t.BrandName, 64, true, false) || !validText(t.Footer, 500, false, true) || !hexColor.MatchString(t.AccentColor) || !hexColor.MatchString(t.BackgroundColor) || len(t.LogoURL) > 2048 || t.LogoURL != "" && !safeURL(t.LogoURL, true) {
		return errors.New("invalid email appearance")
	}
	for i, c := range []Copy{t.Verify, t.Reset, t.Test} {
		if !validText(c.Subject, 160, true, false) || !validText(c.Title, 160, true, false) || !validText(c.Body, 2000, true, true) || !validText(c.Button, 80, i < 2, false) {
			return errors.New("invalid email copy")
		}
	}
	return nil
}

const ignoreRequest = "若非本人操作，请忽略此邮件。\nIf you did not request this, ignore this email."
const sampleNotice = "样例邮件，不会验证邮箱或修改密码。\nSample only. This email cannot verify an account or reset a password."

func (t Template) Render(purpose, link string) (Content, error) {
	if err := t.Validate(); err != nil {
		return Content{}, err
	}
	sample := strings.HasPrefix(purpose, "preview-")
	kind := strings.TrimPrefix(purpose, "preview-")
	var c Copy
	var details string
	switch kind {
	case "verify":
		c = t.Verify
		details = "链接有效期为 24 小时，只能使用一次。\nThis link expires in 24 hours and can be used once."
	case "reset":
		c = t.Reset
		details = "链接有效期为 30 分钟，只能使用一次。\nThis link expires in 30 minutes and can be used once."
	case "test":
		c = t.Test
	default:
		return Content{}, errors.New("invalid email purpose")
	}
	if sample || kind == "test" {
		link = ""
	} else if !safeURL(link, false) {
		return Content{}, errors.New("invalid account email link")
	}
	return t.render(c, link, details, ignoreRequest, sample)
}

func (t Template) RenderNotification(title, body, link string) (Content, error) {
	if err := t.Validate(); err != nil {
		return Content{}, err
	}
	if !validText(title, 160, true, false) || !validText(body, 600, true, true) || link != "" && !safeURL(link, false) {
		return Content{}, errors.New("invalid notification email")
	}
	return t.render(Copy{Subject: title, Title: title, Body: body, Button: "查看详情"}, link, "", "", false)
}

func (t Template) render(c Copy, link, details, notice string, sample bool) (Content, error) {
	text := t.BrandName + "\r\n\r\n" + c.Title + "\r\n\r\n" + c.Body
	for _, line := range []string{details, link, notice, t.Footer} {
		if line != "" {
			text += "\r\n\r\n" + line
		}
	}
	if sample {
		text = sampleNotice + "\r\n\r\n" + text
	}
	data := struct {
		Template
		Copy
		Link, Details, Notice string
		Sample                bool
		Paragraphs            []string
	}{t, c, link, details, notice, sample, strings.Split(strings.ReplaceAll(c.Body, "\r\n", "\n"), "\n")}
	var html bytes.Buffer
	if err := emailHTML.Execute(&html, data); err != nil {
		return Content{}, err
	}
	subject := c.Subject
	if sample {
		subject = "[样例 / Sample] " + subject
	}
	return Content{subject, strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n"), html.String()}, nil
}

var emailHTML = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;background:{{.BackgroundColor}};color:#172033;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;line-height:1.6">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:{{.BackgroundColor}}"><tr><td align="center" style="padding:32px 12px">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:560px;background:#fff;border:1px solid #e5e7eb;border-radius:14px">
<tr><td style="padding:28px 24px">
{{if .LogoURL}}<img src="{{.LogoURL}}" width="56" height="56" alt="" style="display:block;margin:0 0 16px;border:0">{{end}}
<p style="margin:0;font-size:14px;font-weight:700;color:{{.AccentColor}}">{{.BrandName}}</p>
{{if .Sample}}<p style="padding:12px;background:#f3f4f6;font-size:13px;color:#4b5563">样例邮件，不会验证邮箱或修改密码。<br>Sample only. No account action is available.</p>{{end}}
<h1 style="margin:18px 0;font-size:24px;line-height:1.3;color:#111827">{{.Title}}</h1>
{{range .Paragraphs}}<p style="margin:8px 0;overflow-wrap:anywhere">{{.}}</p>{{end}}
{{if .Details}}<p style="margin:20px 0;color:#4b5563;white-space:pre-line">{{.Details}}</p>{{end}}
{{if .Link}}<p style="margin:28px 0"><a href="{{.Link}}" style="display:inline-block;background:{{.AccentColor}};color:#fff;text-decoration:none;border-radius:8px;padding:12px 20px;font-weight:600">{{.Button}}</a></p>
<p style="font-size:12px;color:#6b7280">若按钮无法打开，请复制链接。 / If the button does not open, copy this link:<br><a href="{{.Link}}" style="color:{{.AccentColor}};overflow-wrap:anywhere;word-break:break-all">{{.Link}}</a></p>
{{else if .Sample}}<p style="margin:28px 0"><span style="display:inline-block;background:#e5e7eb;color:#4b5563;border-radius:8px;padding:12px 20px;font-weight:600">{{.Button}}</span></p>{{end}}
{{if .Notice}}<p style="margin:24px 0 0;color:#6b7280;font-size:13px;white-space:pre-line">{{.Notice}}</p>{{end}}
</td></tr></table>
{{if .Footer}}<p style="max-width:560px;margin:16px 0 0;color:#6b7280;font-size:12px;white-space:pre-line;overflow-wrap:anywhere">{{.Footer}}</p>{{end}}
</td></tr></table></body></html>`))
