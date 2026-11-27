package httpapi

import (
	"context"
	"errors"
	"fmt"
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

func TestFetchExtensionFollowsSafeRedirectsAndBlocksUnsafe(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect-ok":
			http.Redirect(w, r, srv.URL+"/payload", http.StatusFound)
		case "/payload":
			_, _ = w.Write([]byte("zip-bytes"))
		case "/loop":
			next := 1
			if r.URL.Query().Get("n") != "" {
				_, _ = fmt.Sscanf(r.URL.Query().Get("n"), "%d", &next)
				next++
			}
			http.Redirect(w, r, fmt.Sprintf("%s/loop?n=%d", srv.URL, next), http.StatusFound)
		case "/to-metadata":
			http.Redirect(w, r, "https://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/to-private":
			http.Redirect(w, r, "https://10.0.0.1/pkg.zip", http.StatusFound)
		case "/to-file":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	data, err := fetchExtension(context.Background(), srv.URL+"/redirect-ok", 1024)
	if err != nil || string(data) != "zip-bytes" {
		t.Fatalf("expected safe redirect fetch to succeed, got data=%q err=%v", string(data), err)
	}
	if _, err := fetchExtension(context.Background(), srv.URL+"/loop", 1024); err == nil {
		t.Fatal("expected excessive redirect hops to fail")
	}
	for _, path := range []string{"/to-metadata", "/to-private", "/to-file"} {
		if _, err := fetchExtension(context.Background(), srv.URL+path, 1024); err == nil {
			t.Fatalf("expected unsafe redirect %s to fail", path)
		}
	}

	dlClient := newSafeExtensionDownloadClient(2 * time.Second)
	for _, target := range []string{
		"https://example.com/start",
	} {
		origReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		for _, unsafeNext := range []string{"http://example.com/downgrade", "https://127.0.0.1/internal"} {
			nextReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, unsafeNext, nil)
			if err := dlClient.CheckRedirect(nextReq, []*http.Request{origReq}); err == nil {
				t.Fatalf("expected redirect %s -> %s to be blocked", target, unsafeNext)
			}
		}
	}
}
