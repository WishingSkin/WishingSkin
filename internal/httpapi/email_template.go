package httpapi

import (
	"context"
	"net/http"

	"github.com/WishingSkin/WishingSkin/internal/mailer"
)

// Load appearance only when sending, not on every site/challenge configuration read.
type siteMailSender struct {
	app  *Server
	smtp *mailer.SMTP
}

func (s siteMailSender) Send(ctx context.Context, to, purpose, link string) error {
	value, err := s.app.mailTemplate(ctx)
	if err != nil {
		return err
	}
	configured := *s.smtp
	configured.Template = &value
	return configured.Send(ctx, to, purpose, link)
}

func (s siteMailSender) SendNotification(ctx context.Context, to, title, body, link string) error {
	value, err := s.app.mailTemplate(ctx)
	if err != nil {
		return err
	}
	configured := *s.smtp
	configured.Template = &value
	return configured.SendNotification(ctx, to, title, body, link)
}

func (a *Server) emailSender(sender MailSender) MailSender {
	if smtp, ok := sender.(*mailer.SMTP); ok {
		return siteMailSender{a, smtp}
	}
	return sender
}

func (a *Server) mailTemplate(ctx context.Context) (mailer.Template, error) {
	brand, err := a.siteBranding(ctx)
	if err != nil {
		return mailer.Template{}, err
	}
	value := mailer.DefaultTemplate(brand.Name)
	if _, err := a.readSetting(ctx, "mail_template", &value); err != nil {
		return value, err
	}
	return value, value.Validate()
}

func (a *Server) adminMailTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	value, err := a.mailTemplate(r.Context())
	if err != nil {
		failure(w, 503, "暂时无法读取邮件样式")
		return
	}
	respond(w, 200, value)
}

func (a *Server) updateMailTemplate(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var value mailer.Template
	if !decode(w, r, &value) {
		return
	}
	if value.Validate() != nil {
		failure(w, 400, "邮件样式无效，请检查文案长度、颜色和 Logo 地址")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err := a.putSetting(r.Context(), tx, "mail_template", value)
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.mail-template", "site", "mail-template")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存邮件样式")
		return
	}
	respond(w, 200, value)
}

func (a *Server) previewMailTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	var body struct {
		Template mailer.Template `json:"template"`
		Purpose  string          `json:"purpose"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Purpose != "verify" && body.Purpose != "reset" && body.Purpose != "test" {
		failure(w, 400, "邮件用途无效")
		return
	}
	purpose := body.Purpose
	if purpose != "test" {
		purpose = "preview-" + purpose
	}
	content, err := body.Template.Render(purpose, "")
	if err != nil {
		failure(w, 400, "邮件样式无效，请检查文案长度、颜色和 Logo 地址")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 200, content)
}
