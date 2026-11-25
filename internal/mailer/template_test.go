package mailer

import (
	"strings"
	"testing"
)

func TestNotificationRendersSafeHTML(t *testing.T) {
	template := DefaultTemplate("Test site")
	message, err := template.RenderNotification("材质已下架", "<script>literal</script>", "https://example.test/user/resources")
	if err != nil || message.Subject != "材质已下架" || !strings.Contains(message.HTML, "&lt;script&gt;literal&lt;/script&gt;") || !strings.Contains(message.Text, "https://example.test/user/resources") {
		t.Fatalf("notification: %v %+v", err, message)
	}
	if _, err := template.RenderNotification("title", "body", "javascript:alert(1)"); err == nil {
		t.Fatal("unsafe notification link was accepted")
	}
}

func TestCustomTemplateAndMandatoryCopy(t *testing.T) {
	template := DefaultTemplate("我的皮肤站")
	template.AccentColor = "#8b5cf6"
	template.BackgroundColor = "#faf5ff"
	template.LogoURL = "https://skin.example.test/logo.png"
	template.Footer = "站点邮件页脚"
	template.Verify = Copy{Subject: "验证你的邮箱", Title: "欢迎 <script>alert(1)</script>", Body: "第一段\n第二段", Button: "验证邮箱"}
	message, err := template.Render("verify", "https://skin.example.test/#verify=synthetic&a=b")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"我的皮肤站", "#8b5cf6", "#faf5ff", "https://skin.example.test/logo.png", "站点邮件页脚", "24 小时", "只能使用一次", "&lt;script&gt;", "第一段", "第二段"} {
		if !strings.Contains(message.HTML, value) {
			t.Fatalf("HTML missing %q", value)
		}
	}
	if strings.Contains(message.HTML, "<script>") || message.Subject != "验证你的邮箱" || !strings.Contains(message.Text, "24 小时") || !strings.Contains(message.Text, "&a=b") {
		t.Fatal("customization changed safety copy or escaped markup unsafely")
	}
}

func TestEmailSamplesNeverHaveLiveActions(t *testing.T) {
	for _, purpose := range []string{"test", "preview-verify", "preview-reset"} {
		message, err := DefaultTemplate("WishingSkin").Render(purpose, "https://example.test/#verify=should-not-appear")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(message.HTML, "href=") || strings.Contains(message.HTML+message.Text, "should-not-appear") {
			t.Fatal("sample contained a live action")
		}
		if purpose != "test" && (!strings.Contains(message.HTML, "样例邮件") || !strings.Contains(message.Text, "样例邮件")) {
			t.Fatal("account sample was not labeled")
		}
	}
}

func TestTemplateRejectsUnsafeFields(t *testing.T) {
	for _, change := range []func(*Template){
		func(v *Template) { v.AccentColor = "red; background:url(https://evil.test)" },
		func(v *Template) { v.BackgroundColor = "#000" },
		func(v *Template) { v.LogoURL = "javascript:alert(1)" },
		func(v *Template) { v.LogoURL = "http://remote.example.test/logo.png" },
		func(v *Template) { v.LogoURL = "https://user:password@example.test/logo.png" },
		func(v *Template) { v.Verify.Subject = "hello\r\nBcc: hidden@example.test" },
		func(v *Template) { v.Reset.Body = strings.Repeat("字", 2001) },
		func(v *Template) { v.Footer = "control\x00" },
	} {
		value := DefaultTemplate("WishingSkin")
		change(&value)
		if value.Validate() == nil {
			t.Fatal("unsafe template accepted")
		}
	}
	for _, link := range []string{"javascript:alert(1)", "//example.test/path", "https://user:pass@example.test/path", "https://example.test/\r\nInjected"} {
		if _, err := DefaultTemplate("WishingSkin").Render("verify", link); err == nil {
			t.Fatal("unsafe account link accepted")
		}
	}
}
