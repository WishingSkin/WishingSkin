package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

var (
	errBlockedOutboundIP = errors.New("outbound destination IP is not allowed")
	cgnatPrefix          = netip.MustParsePrefix("100.64.0.0/10")
	benchmarkPrefix      = netip.MustParsePrefix("198.18.0.0/15")
)

func allowedOutboundAddr(addr netip.Addr, allowLoopback bool) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return allowLoopback
	}
	if addr.IsUnspecified() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() ||
		cgnatPrefix.Contains(addr) ||
		benchmarkPrefix.Contains(addr) {
		return false
	}
	return true
}

func safeExtensionDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	allowLoopback := loopbackOriginHost(host)
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, item := range resolved {
			if ip, ok := netip.AddrFromSlice(item.IP); ok {
				addrs = append(addrs, ip)
			}
		}
	}
	if len(addrs) == 0 {
		return nil, errBlockedOutboundIP
	}
	valid := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		if !allowedOutboundAddr(addr, allowLoopback) {
			return nil, errBlockedOutboundIP
		}
		valid = append(valid, addr.Unmap())
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, addr := range valid {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errBlockedOutboundIP
}

func newSafeExtensionClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = safeExtensionDialContext
	return &http.Client{
		Timeout:       timeout,
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func newSafeExtensionDownloadClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = safeExtensionDialContext
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.Method != http.MethodGet && req.Method != http.MethodHead {
				return http.ErrUseLastResponse
			}
			if !validExtensionURL(req.URL.String()) {
				return errors.New("invalid redirect URL")
			}
			if len(via) > 0 {
				if strings.EqualFold(via[0].URL.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
					return errors.New("https downgrade redirect is not allowed")
				}
				if !loopbackOriginHost(via[0].URL.Hostname()) && loopbackOriginHost(req.URL.Hostname()) {
					return errors.New("redirect to loopback host is not allowed")
				}
			}
			return nil
		},
	}
}
