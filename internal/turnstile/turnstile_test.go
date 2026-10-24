package turnstile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVerificationContract(t *testing.T) {
	for _, test := range []struct {
		name, body           string
		status               int
		invalid, unavailable bool
	}{
		{"accepted", `{"success":true,"hostname":"skin.example.test","action":"register"}`, 200, false, false},
		{"replayed", `{"success":false,"error-codes":["timeout-or-duplicate"]}`, 200, true, false},
		{"wrong host", `{"success":true,"hostname":"evil.test","action":"register"}`, 200, true, false},
		{"wrong action", `{"success":true,"hostname":"skin.example.test","action":"login"}`, 200, true, false},
		{"bad JSON", `{`, 200, false, true},
		{"redirect", `{}`, 302, false, true},
		{"provider error", `{}`, 503, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := Client{Secret: "synthetic-secret", Hostname: "skin.example.test", HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" || r.Method != "POST" {
					t.Fatal("wrong verification endpoint")
				}
				if err := r.ParseForm(); err != nil || r.Form.Get("secret") != "synthetic-secret" || r.Form.Get("response") != "once" || r.Form.Get("remoteip") != "127.0.0.1" {
					t.Fatal("verification form", err)
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("unbounded provider request")
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Header: http.Header{}}, nil
			})}}
			err := c.Verify(context.Background(), "once", "127.0.0.1", "register")
			if errors.Is(err, ErrInvalid) != test.invalid || (err != nil && !errors.Is(err, ErrInvalid)) != test.unavailable {
				t.Fatal("wrong result", err)
			}
		})
	}
}

func TestVerificationFailsClosed(t *testing.T) {
	called := false
	c := Client{HTTP: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, errors.New("network") })}}
	if c.Verify(context.Background(), "token", "", "login") == nil || called {
		t.Fatal("missing configuration made request")
	}
	c.Secret, c.Hostname = "synthetic", "example.test"
	for _, token := range []string{"", strings.Repeat("x", 2049)} {
		if !errors.Is(c.Verify(context.Background(), token, "", "login"), ErrInvalid) || called {
			t.Fatal("invalid token made request")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Verify(ctx, "token", "", "login") == nil {
		t.Fatal("cancelled verification accepted")
	}
}
