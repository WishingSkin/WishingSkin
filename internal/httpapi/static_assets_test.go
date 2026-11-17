package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestServeAssetOrSPAUsesIndexWithoutRedirect(t *testing.T) {
	a := &Server{cfg: Config{Assets: fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>WishingSkin</title>")},
	}}}
	for _, path := range []string{"/", "/login", "/user/account", "/user/resources", "/user/resources/upload", "/admin/users", "/admin/resources", "/admin/events", "/admin/settings/appearance", "/admin/plugins/wishingskin.oidc"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)
			a.serveAssetOrSPA(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d want 200, location %q", w.Code, w.Header().Get("Location"))
			}
			if w.Body.String() == "" {
				t.Fatal("empty SPA response")
			}
		})
	}
}

func TestUnknownPagesReturnHTML404(t *testing.T) {
	const index = "<!doctype html><title>WishingSkin</title>"
	a := &Server{cfg: Config{Assets: fstest.MapFS{
		"index.html": {Data: []byte(index)},
	}}}
	for _, path := range []string{"/unknown", "/admin/settings/appearances", "/admin/settings/site/extra", "/admin/users/extra", "/user/account/missing", "/user/resources/upload/extra"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			a.serveAssetOrSPA(w, httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil))
			if w.Code != http.StatusNotFound || w.Body.String() != index || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || w.Header().Get("Location") != "" {
				t.Fatalf("unknown page should serve the app with HTTP 404, got status=%d content-type=%q location=%q body=%q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Location"), w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	a.serveAssetOrSPA(w, httptest.NewRequest(http.MethodHead, "http://example.test/admin/settings/appearances", nil))
	if w.Code != http.StatusNotFound || w.Body.Len() != 0 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("HEAD on a missing page: status=%d body=%q", w.Code, w.Body.String())
	}
	for _, path := range []string{"/assets/missing.js", "/missing.png"} {
		w := httptest.NewRecorder()
		a.serveAssetOrSPA(w, httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil))
		if w.Code != http.StatusNotFound || strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("missing resource %s should not return the app, status=%d", path, w.Code)
		}
	}
}
