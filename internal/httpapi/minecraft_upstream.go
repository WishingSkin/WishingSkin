package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/oauth2"
)

type minecraftEndpoints struct {
	authorize, token, xbox, xsts, login, entitlements, profile string
}

func (a *Server) minecraftOAuth(cfg minecraftConfig) *oauth2.Config {
	return &oauth2.Config{ClientID: cfg.ClientID, Endpoint: oauth2.Endpoint{AuthURL: a.minecraftEndpoints.authorize}, RedirectURL: a.cfg.PublicURL + minecraftPath + "/callback", Scopes: []string{"XboxLive.signin"}}
}

type xboxResponse struct {
	Token         string
	XErr          int64
	DisplayClaims struct{ XUI []struct{ UHS string } }
}

func (a *Server) exchangeMinecraft(ctx context.Context, cfg minecraftConfig, code, verifier string) (uuid, name, subject, reason string) {
	select {
	case a.connectorSlots <- struct{}{}:
		defer func() { <-a.connectorSlots }()
	default:
		return "", "", "", "denied"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	endpoints := a.minecraftEndpoints
	var ms, mc struct {
		AccessToken string `json:"access_token"`
	}
	tokenRequest := url.Values{
		"client_id": {cfg.ClientID}, "client_secret": {cfg.ClientSecret}, "grant_type": {"authorization_code"}, "code": {code},
		"code_verifier": {verifier}, "redirect_uri": {a.minecraftOAuth(cfg).RedirectURL}, "scope": {"XboxLive.signin"},
	}
	if _, err := a.minecraftRequest(ctx, endpoints.token, "", tokenRequest, &ms); err != nil || ms.AccessToken == "" {
		return "", "", "", "denied"
	}
	xboxRequest := map[string]any{
		"Properties":   map[string]string{"AuthMethod": "RPS", "SiteName": "user.auth.xboxlive.com", "RpsTicket": "d=" + ms.AccessToken},
		"RelyingParty": "http://auth.xboxlive.com", "TokenType": "JWT",
	}
	var xbox, xsts xboxResponse
	if _, err := a.minecraftRequest(ctx, endpoints.xbox, "", xboxRequest, &xbox); err != nil || xbox.Token == "" {
		return "", "", "", "denied"
	}
	xstsRequest := map[string]any{
		"Properties":   map[string]any{"SandboxId": "RETAIL", "UserTokens": []string{xbox.Token}},
		"RelyingParty": "rp://api.minecraftservices.com/", "TokenType": "JWT",
	}
	_, err := a.minecraftRequest(ctx, endpoints.xsts, "", xstsRequest, &xsts)
	switch xsts.XErr {
	case 2148916233:
		return "", "", "", "no_xbox"
	case 2148916238:
		return "", "", "", "child"
	}
	if err != nil || xsts.Token == "" || len(xsts.DisplayClaims.XUI) == 0 || xsts.DisplayClaims.XUI[0].UHS == "" {
		return "", "", "", "denied"
	}
	uhs := xsts.DisplayClaims.XUI[0].UHS
	if _, err := a.minecraftRequest(ctx, endpoints.login, "", map[string]string{"identityToken": "XBL3.0 x=" + uhs + ";" + xsts.Token}, &mc); err != nil || mc.AccessToken == "" {
		return "", "", "", "denied"
	}
	var entitlement struct{ Items []struct{ Name string } }
	status, err := a.minecraftRequest(ctx, endpoints.entitlements, mc.AccessToken, nil, &entitlement)
	if status == http.StatusNotFound {
		return "", "", "", "not_owned"
	}
	if err != nil {
		return "", "", "", "denied"
	}
	owned := false
	for _, item := range entitlement.Items {
		if item.Name == "game_minecraft" || item.Name == "product_minecraft" {
			owned = true
			break
		}
	}
	if !owned {
		return "", "", "", "not_owned"
	}
	var profile struct{ ID, Name string }
	status, err = a.minecraftRequest(ctx, endpoints.profile, mc.AccessToken, nil, &profile)
	name = strings.TrimSpace(profile.Name)
	if status == http.StatusNotFound || status == http.StatusOK && (!isHexID(profile.ID, 32) || name == "" || utf8.RuneCountInString(name) > 64) {
		return "", "", "", "not_owned"
	}
	if err != nil {
		return "", "", "", "denied"
	}
	return strings.ToLower(profile.ID), name, tokenHash("xbl:" + uhs), ""
}

func (a *Server) minecraftRequest(ctx context.Context, endpoint, bearer string, payload, target any) (int, error) {
	method, contentType := http.MethodGet, ""
	var body io.Reader
	switch value := payload.(type) {
	case nil:
	case url.Values:
		method, contentType = http.MethodPost, "application/x-www-form-urlencoded"
		body = strings.NewReader(value.Encode())
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return 0, err
		}
		method, contentType = http.MethodPost, "application/json"
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := a.minecraftClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	const maxResponse = 256 << 10
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return res.StatusCode, errors.New("invalid Minecraft upstream response")
	}
	if res.StatusCode != http.StatusOK {
		if value, ok := target.(*xboxResponse); ok {
			_ = json.Unmarshal(data, value)
		}
		return res.StatusCode, errors.New("Minecraft upstream rejected request")
	}
	return res.StatusCode, json.Unmarshal(data, target)
}
