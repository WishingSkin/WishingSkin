package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestTrustedProxyAddressBoundary(t *testing.T) {
	a := &Server{cfg: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8:ffff::/48")}}}
	for _, c := range []struct{ name, peer, forwarded, want string }{
		{"untrusted", "192.0.2.1:1234", "198.51.100.1", "192.0.2.1"},
		{"single", "10.0.0.1:1234", "192.0.2.1", "192.0.2.1"},
		{"forged_left", "10.0.0.1:1234", "198.51.100.1, 192.0.2.1", "192.0.2.1"},
		{"trusted_chain", "10.0.0.1:1234", "198.51.100.1, 192.0.2.1, 10.0.0.2", "192.0.2.1"},
		{"ipv6", "[2001:db8:ffff::1]:1234", "2001:db8::1, 10.0.0.2", "2001:db8::1"},
		{"mapped", "[::ffff:10.0.0.1]:1234", "::ffff:192.0.2.1", "192.0.2.1"},
		{"all_trusted", "10.0.0.1:1234", "10.0.0.2, 10.0.0.3", "10.0.0.2"},
		{"missing", "10.0.0.1:1234", "", "10.0.0.1"},
		{"invalid", "10.0.0.1:1234", "unknown, 192.0.2.1", "10.0.0.1"},
		{"empty_hop", "10.0.0.1:1234", ",192.0.2.1", "10.0.0.1"},
		{"port", "10.0.0.1:1234", "192.0.2.1:9000", "10.0.0.1"},
		{"zone", "10.0.0.1:1234", "fe80::1%local", "10.0.0.1"},
		{"unspecified", "10.0.0.1:1234", "0.0.0.0", "10.0.0.1"},
		{"multicast", "10.0.0.1:1234", "ff02::1", "10.0.0.1"},
		{"too_many", "10.0.0.1:1234", strings.Repeat("10.0.0.2,", 32) + "192.0.2.1", "10.0.0.1"},
		{"too_long", "10.0.0.1:1234", strings.Repeat(" ", 4096) + "192.0.2.1", "10.0.0.1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/textures", nil)
			r.RemoteAddr = c.peer
			r.Header.Set("X-Forwarded-For", c.forwarded)
			r.Header.Set("X-Real-IP", "203.0.113.1")
			r.Header.Set("Forwarded", "for=203.0.113.2")
			if got := connectionIP(a.proxyRequest(r)); got != c.want {
				t.Fatal("wrong client", got, c.want)
			}
			if r.RemoteAddr != c.peer {
				t.Fatal("mutated original request")
			}
		})
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Add("X-Forwarded-For", "192.0.2.1")
	r.Header.Add("X-Forwarded-For", "10.0.0.2")
	if connectionIP(a.proxyRequest(r)) != "192.0.2.1" || connectionIP((&Server{}).proxyRequest(r)) != "10.0.0.1" {
		t.Fatal("repeated header or disabled default")
	}
}

func TestTrustedProxyUploadQuotaBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.app.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
			f.app.uploadIPLimit.max = 1
			request := func(peer, forwarded string, want int) {
				r := uploadLimitRequest(f, 1, peer)
				r.Header.Set("X-Forwarded-For", forwarded)
				webStatus(t, serveUploadLimit(f, r), want)
			}
			request("10.0.0.1:1234", "192.0.2.1", 400)
			request("10.0.0.1:1234", "192.0.2.2", 400)
			request("10.0.0.1:1234", "203.0.113.1, 192.0.2.1", 429)
			request("192.0.2.3:1234", "198.51.100.1", 400)
			request("192.0.2.3:5678", "198.51.100.2", 429)
		})
	}
}
