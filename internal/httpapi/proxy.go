package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func (a *Server) trustedProxy(ip netip.Addr) bool {
	for _, prefix := range a.cfg.TrustedProxies {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// A trusted proxy must append the actual peer to X-Forwarded-For, or replace
// the header with the actual client. Stop at the first untrusted hop from right.
func (a *Server) proxyRequest(r *http.Request) *http.Request {
	peer, err := netip.ParseAddr(connectionIP(r))
	if err != nil || !a.trustedProxy(peer) {
		return r
	}
	raw := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if raw == "" || len(raw) > 4096 {
		return r
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 32 {
		return r
	}
	ips := make([]netip.Addr, len(parts))
	for i, part := range parts {
		ip, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			return r
		}
		ips[i] = ip.Unmap()
	}
	client := peer
	for i := len(ips) - 1; i >= 0 && a.trustedProxy(client); i-- {
		client = ips[i]
	}
	copy := r.Clone(r.Context())
	copy.RemoteAddr = net.JoinHostPort(client.String(), "0")
	return copy
}
