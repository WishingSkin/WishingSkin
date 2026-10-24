package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrInvalid = errors.New("challenge rejected")

type Client struct {
	Secret, Hostname string
	HTTP             *http.Client
}

func (c *Client) Verify(ctx context.Context, token, ip, action string) error {
	if c.Secret == "" || c.Hostname == "" {
		return errors.New("challenge unavailable")
	}
	if token == "" || len(token) > 2048 {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	form := url.Values{"secret": {c.Secret}, "response": {token}, "remoteip": {ip}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("challenge unavailable")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := c.HTTP
	if client == nil {
		client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("challenge unavailable")
	}
	defer response.Body.Close()
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil {
		return errors.New("challenge unavailable")
	}
	if !result.Success || !strings.EqualFold(result.Hostname, c.Hostname) || result.Action != action {
		return ErrInvalid
	}
	return nil
}
