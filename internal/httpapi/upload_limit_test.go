package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type unreadUploadBody struct{ t *testing.T }

func (b unreadUploadBody) Read([]byte) (int, error) {
	b.t.Error("rate-limited request read the upload body")
	return 0, io.EOF
}
func (unreadUploadBody) Close() error { return nil }

func uploadLimitRequest(f *managementApp, user int, address string) *http.Request {
	r := httptest.NewRequest("POST", "http://example.test/api/textures", strings.NewReader("invalid multipart"))
	r.RemoteAddr = address
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")
	if user > 0 {
		token := fmt.Sprintf("%064d", user)
		r.AddCookie(&http.Cookie{Name: "ws_session", Value: token})
		r.Header.Set("X-CSRF-Token", csrf(token))
	}
	return r
}

func serveUploadLimit(f *managementApp, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.app.ServeHTTP(w, r)
	return w
}

func TestUploadRateLimitsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			t.Run("account_across_ips_and_expiry", func(t *testing.T) {
				f := newManagementApp(t, dialect)
				now := time.Now()
				f.app.uploadAccountLimit.now = func() time.Time { return now }
				f.app.uploadIPLimit.now = func() time.Time { return now }
				configurePoints(t, f, true, 0, 5, 0)
				f.exec("UPDATE users SET score=50")
				for i := 0; i < 10; i++ {
					webStatus(t, serveUploadLimit(f, uploadLimitRequest(f, 1, fmt.Sprintf("198.51.100.%d:1234", i+1))), 400)
				}
				blocked := uploadLimitRequest(f, 1, "203.0.113.1:4321")
				blocked.Body = unreadUploadBody{t}
				w := serveUploadLimit(f, blocked)
				webStatus(t, w, 429)
				if w.Header().Get("Retry-After") != "60" {
					t.Fatal("missing retry interval")
				}
				if pointsBalance(t, f, 1) != 50 {
					t.Fatal("failed uploads charged points")
				}
				for _, table := range []string{"textures", "texture_records", "point_entries"} {
					var count int
					if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
						t.Fatal("failed admission wrote data", table, count, err)
					}
				}
				uploadedRecord(t, f.upload(2, testPNG(t, 64, 64), nil))
				now = now.Add(59 * time.Second)
				webStatus(t, f.upload(1, testPNG(t, 64, 64), nil), 429)
				now = now.Add(time.Second)
				uploadedRecord(t, f.upload(1, testPNG(t, 64, 64), nil))
				if pointsBalance(t, f, 1) != 45 || pointsBalance(t, f, 2) != 45 {
					t.Fatal("recovered upload did not charge exactly once")
				}
			})
			t.Run("ip_across_accounts_and_headers", func(t *testing.T) {
				f := newManagementApp(t, dialect)
				now := time.Now()
				f.app.uploadIPLimit.now = func() time.Time { return now }
				f.app.uploadAccountLimit.now = func() time.Time { return now }
				// Unauthenticated failures consume the same connection-source quota.
				for i := 0; i < 30; i++ {
					user := i % 4
					want := 400
					if user == 0 {
						want = 401
					}
					webStatus(t, serveUploadLimit(f, uploadLimitRequest(f, user, fmt.Sprintf("192.0.2.8:%d", i+1000))), want)
				}
				for _, address := range []string{"192.0.2.8:9000", "[::ffff:192.0.2.8]:9001"} {
					r := uploadLimitRequest(f, 1, address)
					r.Header.Set("X-Forwarded-For", "198.51.100.25")
					r.Header.Set("X-Real-IP", "198.51.100.26")
					r.Header.Set("Forwarded", "for=198.51.100.27")
					r.Body = unreadUploadBody{t}
					webStatus(t, serveUploadLimit(f, r), 429)
				}
				webStatus(t, serveUploadLimit(f, uploadLimitRequest(f, 0, "192.0.2.9:1234")), 401)
				now = now.Add(time.Minute)
				webStatus(t, serveUploadLimit(f, uploadLimitRequest(f, 1, "192.0.2.8:1234")), 400)
			})
			t.Run("concurrent_account_quota", func(t *testing.T) {
				f := newManagementApp(t, dialect)
				var wg sync.WaitGroup
				codes := make(chan int, 64)
				for i := 0; i < 64; i++ {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						codes <- serveUploadLimit(f, uploadLimitRequest(f, 1, fmt.Sprintf("198.51.100.%d:1234", i+1))).Code
					}(i)
				}
				wg.Wait()
				close(codes)
				got := map[int]int{}
				for code := range codes {
					got[code]++
				}
				if got[400]+got[429] != 64 || f.app.uploadAccountLimit.entries["1"].count != 10 {
					t.Fatal("concurrent quota bypass", got)
				}
			})
			t.Run("busy_and_csrf_failures_count", func(t *testing.T) {
				f := newManagementApp(t, dialect)
				f.app.uploads <- struct{}{}
				f.app.uploads <- struct{}{}
				w := serveUploadLimit(f, uploadLimitRequest(f, 1, "192.0.2.1:1234"))
				webStatus(t, w, 429)
				if !strings.Contains(w.Body.String(), "正在处理其他上传") {
					t.Fatal("lost concurrency protection")
				}
				<-f.app.uploads
				<-f.app.uploads
				for i := 0; i < 9; i++ {
					r := uploadLimitRequest(f, 1, "192.0.2.1:1234")
					r.Header.Del("X-CSRF-Token")
					webStatus(t, serveUploadLimit(f, r), 403)
				}
				webStatus(t, f.upload(1, testPNG(t, 64, 64), nil), 429)
			})
		})
	}
}

func TestUploadSourceAddressNormalization(t *testing.T) {
	for _, address := range []string{"192.0.2.1:1234", "[::ffff:192.0.2.1]:4321", "192.0.2.1"} {
		r := httptest.NewRequest("POST", "/api/textures", nil)
		r.RemoteAddr = address
		if connectionIP(r) != "192.0.2.1" {
			t.Fatal("source address not normalized", address, connectionIP(r))
		}
	}
	for _, address := range []string{"[2001:db8::1]:1234", "[2001:0db8:0:0:0:0:0:1]:4321"} {
		r := httptest.NewRequest("POST", "/api/textures", nil)
		r.RemoteAddr = address
		if connectionIP(r) != "2001:db8::1" {
			t.Fatal("IPv6 address not normalized", address, connectionIP(r))
		}
	}
}
