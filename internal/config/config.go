package config

import (
	"errors"
	"net/netip"
	"os"
	"strings"
	"wishingskin/internal/mailer"
)

type Config struct {
	Listen, Driver, DSN, PublicURL, TextureDir string
	PluginDir                                  string
	SMTP                                       *mailer.SMTP
	TurnstileSiteKey, TurnstileSecret          string
	TrustedProxies                             []netip.Prefix
}

func Load() (Config, error) {
	c := Config{Listen: value("WS_LISTEN", "127.0.0.1:8090"), Driver: value("WS_DATABASE_DRIVER", "postgres"), DSN: os.Getenv("WS_DATABASE_DSN"), PublicURL: value("WS_PUBLIC_URL", "http://localhost:8090"), TextureDir: value("WS_TEXTURE_DIR", "data/textures")}
	c.PluginDir = value("WS_PLUGIN_DIR", c.TextureDir+"/.plugins")
	if c.DSN == "" {
		return c, errors.New("WS_DATABASE_DSN is required")
	}
	if c.Driver != "mysql" && c.Driver != "postgres" {
		return c, errors.New("WS_DATABASE_DRIVER must be mysql or postgres")
	}
	if raw := strings.TrimSpace(os.Getenv("WS_TRUSTED_PROXIES")); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(entry))
			if err != nil || prefix.Addr().Is4In6() {
				return c, errors.New("WS_TRUSTED_PROXIES must contain comma-separated IPv4 or IPv6 CIDRs")
			}
			c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
		}
	}
	c.TurnstileSiteKey, c.TurnstileSecret = os.Getenv("WS_TURNSTILE_SITE_KEY"), os.Getenv("WS_TURNSTILE_SECRET")
	if (c.TurnstileSiteKey == "") != (c.TurnstileSecret == "") {
		return c, errors.New("Turnstile site key and secret must be configured together")
	}
	if os.Getenv("WS_SMTP_ADDRESS") != "" {
		c.SMTP = &mailer.SMTP{Address: os.Getenv("WS_SMTP_ADDRESS"), From: os.Getenv("WS_SMTP_FROM"), Username: os.Getenv("WS_SMTP_USERNAME"), Password: os.Getenv("WS_SMTP_PASSWORD"), TLSMode: value("WS_SMTP_TLS", "starttls")}
		if err := c.SMTP.Validate(); err != nil {
			return c, err
		}
	} else if os.Getenv("WS_SMTP_FROM") != "" || os.Getenv("WS_SMTP_USERNAME") != "" || os.Getenv("WS_SMTP_PASSWORD") != "" {
		return c, errors.New("SMTP address is required when configuring mail")
	}
	return c, nil
}
func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
