package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestAllowedOutboundAddr(t *testing.T) {
	blocked := []string{
		"0.0.0.0",
		"::",
		"10.0.0.1",
		"172.16.0.5",
		"192.168.1.10",
		"169.254.169.254",
		"100.64.0.1",
		"198.18.0.1",
		"224.0.0.1",
		"fe80::1",
		"fc00::1",
		"fd12:3456::1",
		"::ffff:169.254.169.254",
		"::ffff:10.0.0.1",
	}
	for _, raw := range blocked {
		addr := netip.MustParseAddr(raw)
		if allowedOutboundAddr(addr, false) || allowedOutboundAddr(addr, true) {
			t.Fatalf("expected non-public address %s to be blocked", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1"} {
		addr := netip.MustParseAddr(raw)
		if allowedOutboundAddr(addr, false) {
			t.Fatalf("expected loopback %s to be blocked when host is not loopback", raw)
		}
		if !allowedOutboundAddr(addr, true) {
			t.Fatalf("expected loopback %s to be allowed for explicit loopback hosts", raw)
		}
	}
	for _, raw := range []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"} {
		addr := netip.MustParseAddr(raw)
		if !allowedOutboundAddr(addr, false) {
			t.Fatalf("expected public address %s to be allowed", raw)
		}
	}
}

func TestSafeExtensionClientBlocksPrivateAndAllowsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	data, err := fetchExtension(context.Background(), srv.URL, 1024)
	if err != nil || string(data) != "ok" {
		t.Fatalf("loopback extension fetch failed: data=%q err=%v", string(data), err)
	}

	client := newSafeExtensionClient(2 * time.Second)
	for _, target := range []string{"https://169.254.169.254/latest/meta-data/", "https://10.0.0.1/hook", "https://192.168.1.1/catalog.json"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		if !errors.Is(err, errBlockedOutboundIP) {
			t.Fatalf("expected %s to fail with errBlockedOutboundIP, got %v", target, err)
		}
	}
}
