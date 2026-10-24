package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

type bucket struct {
	start time.Time
	count int
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]bucket
	now     func() time.Time
	max     int
	window  time.Duration
}

func (l *limiter) allow(r *http.Request) bool {
	return l.allowKey("ip:" + connectionIP(r))
}

func (l *limiter) allowKey(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	window, maximum := l.limits()
	for key, b := range l.entries {
		if now.Sub(b.start) >= window {
			delete(l.entries, key)
		}
	}
	b, exists := l.entries[key]
	if !exists && len(l.entries) >= 4096 {
		return false
	}
	if b.count >= maximum {
		return false
	}
	if !exists {
		b.start = now
	}
	b.count++
	l.entries[key] = b
	return true
}

func (l *limiter) limits() (time.Duration, int) {
	window, maximum := l.window, l.max
	if window == 0 {
		window = time.Minute
	}
	if maximum == 0 {
		maximum = 10
	}
	return window, maximum
}

func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	window, maximum := l.limits()
	for key, b := range l.entries {
		if l.now().Sub(b.start) >= window {
			delete(l.entries, key)
		}
	}
	b, exists := l.entries[key]
	if !exists && len(l.entries) >= 4096 {
		return true
	}
	return l.now().Sub(b.start) < window && b.count >= maximum
}

func (l *limiter) reset(key string) { l.mu.Lock(); delete(l.entries, key); l.mu.Unlock() }

func connectionIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().WithZone("").String()
	}
	return host
}
