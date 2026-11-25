package config

import (
	"strings"
	"testing"
)

func TestAccountServiceConfiguration(t *testing.T) {
	for _, name := range []string{"WS_TURNSTILE_SITE_KEY", "WS_TURNSTILE_SECRET", "WS_SMTP_ADDRESS", "WS_SMTP_FROM", "WS_SMTP_USERNAME", "WS_SMTP_PASSWORD", "WS_SMTP_TLS"} {
		t.Setenv(name, "")
	}
	t.Setenv("WS_DATABASE_DSN", "synthetic")
	c, err := Load()
	if err != nil || c.SMTP != nil || c.TurnstileSecret != "" {
		t.Fatal("empty services", err)
	}
	t.Setenv("WS_TURNSTILE_SECRET", "synthetic-secret")
	if _, err = Load(); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("unpaired secret or leaked error", err)
	}
	t.Setenv("WS_TURNSTILE_SITE_KEY", "synthetic-public-key")
	t.Setenv("WS_SMTP_PASSWORD", "synthetic-password")
	if _, err = Load(); err == nil || strings.Contains(err.Error(), "synthetic-password") {
		t.Fatal("partial mail configuration", err)
	}
	t.Setenv("WS_SMTP_ADDRESS", "127.0.0.1:2525")
	t.Setenv("WS_SMTP_FROM", "site@example.test")
	t.Setenv("WS_SMTP_USERNAME", "synthetic-user")
	c, err = Load()
	if err != nil || c.SMTP.TLSMode != "starttls" {
		t.Fatal("secure default", err)
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	t.Setenv("WS_DATABASE_DSN", "synthetic")
	for _, name := range []string{"WS_TURNSTILE_SITE_KEY", "WS_TURNSTILE_SECRET", "WS_SMTP_ADDRESS", "WS_SMTP_FROM", "WS_SMTP_USERNAME", "WS_SMTP_PASSWORD"} {
		t.Setenv(name, "")
	}
	for _, invalid := range []string{"localhost", "127.0.0.1", "127.0.0.1/32,", "192.0.2.0/33", "::ffff:192.0.2.0/120"} {
		t.Setenv("WS_TRUSTED_PROXIES", invalid)
		if _, err := Load(); err == nil {
			t.Fatal("accepted invalid proxy CIDRs", invalid)
		}
	}
	t.Setenv("WS_TRUSTED_PROXIES", " 192.0.2.10/24, 2001:db8::1/64 ")
	c, err := Load()
	if err != nil || len(c.TrustedProxies) != 2 || c.TrustedProxies[0].String() != "192.0.2.0/24" || c.TrustedProxies[1].String() != "2001:db8::/64" {
		t.Fatal("CIDR parsing", c.TrustedProxies, err)
	}
	t.Setenv("WS_TRUSTED_PROXIES", "")
	c, err = Load()
	if err != nil || len(c.TrustedProxies) != 0 {
		t.Fatal("proxies trusted by default", err)
	}
}

func TestOfficialPluginPublicKeysConfiguration(t *testing.T) {
	t.Setenv("WS_DATABASE_DSN", "synthetic")
	for _, name := range []string{"WS_TURNSTILE_SITE_KEY", "WS_TURNSTILE_SECRET", "WS_SMTP_ADDRESS", "WS_SMTP_FROM", "WS_SMTP_USERNAME", "WS_SMTP_PASSWORD", "WS_TRUSTED_PROXIES"} {
		t.Setenv(name, "")
	}
	t.Setenv("WS_OFFICIAL_PLUGIN_PUBKEYS", "invalid-key")
	if _, err := Load(); err == nil {
		t.Fatal("accepted invalid WS_OFFICIAL_PLUGIN_PUBKEYS")
	}
	validHex := strings.Repeat("ab", 32)
	t.Setenv("WS_OFFICIAL_PLUGIN_PUBKEYS", validHex)
	c, err := Load()
	if err != nil || len(c.OfficialPluginPublicKeys) != 1 || len(c.OfficialPluginPublicKeys[0]) != 32 {
		t.Fatalf("expected 1 parsed public key, got %v %v", c.OfficialPluginPublicKeys, err)
	}
}
