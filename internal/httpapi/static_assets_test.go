package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestServeAssetOrSPAUsesIndexWithoutRedirect(t *testing.T) {
	a := &Server{cfg: Config{Assets: fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>WishingSkin</title>")},
	}}}
	for _, path := range []string{"/", "/login", "/user/account"} {
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
