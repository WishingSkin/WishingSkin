package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

var providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func (a *Server) oidcConfigurations(ctx context.Context) ([]OIDCSettings, error) {
	first, err := a.oidcConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	values := []OIDCSettings{first}
	var sealed string
	ok, err := a.readSetting(ctx, "oidc_providers", &sealed)
	if err != nil || !ok {
		return values, err
	}
	var extra []OIDCSettings
	if err = a.openOIDC(sealed, &extra, "providers"); err != nil {
		return nil, err
	}
	return append(values, extra...), nil
}

func (a *Server) oidcSettingsFor(ctx context.Context, id string) (OIDCSettings, error) {
	values, err := a.oidcConfigurations(ctx)
	if err != nil {
		return OIDCSettings{}, err
	}
	enabled, err := a.storedPluginEnabled(ctx, "wishingskin.oidc")
	if err != nil {
		return OIDCSettings{}, err
	}
	for _, v := range values {
		if v.ID == id {
			v.Enabled = v.Enabled && enabled
			return v, nil
		}
	}
	return OIDCSettings{}, errors.New("unknown OIDC provider")
}

func (a *Server) publicOIDCProviders(ctx context.Context) ([]map[string]any, error) {
	values, err := a.oidcConfigurations(ctx)
	if err != nil {
		return nil, err
	}
	enabled, err := a.storedPluginEnabled(ctx, "wishingskin.oidc")
	result := []map[string]any{}
	for _, v := range values {
		if enabled && v.Enabled {
			result = append(result, map[string]any{"id": v.ID, "name": v.Name})
		}
	}
	return result, err
}

func (a *Server) adminOIDCProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	values, err := a.oidcConfigurations(r.Context())
	if err != nil {
		failure(w, 503, "无法读取 OIDC 设置，请检查设置密钥")
		return
	}
	items := []map[string]any{}
	for _, v := range values {
		items = append(items, a.oidcView(v))
	}
	respond(w, 200, map[string]any{"providers": items})
}

func (a *Server) updateOIDCProviders(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Providers []struct {
			ID           string `json:"id"`
			Enabled      bool   `json:"enabled"`
			Name         string `json:"name"`
			Issuer       string `json:"issuer"`
			ClientID     string `json:"clientId"`
			ClientSecret string `json:"clientSecret"`
			ClearSecret  bool   `json:"clearSecret"`
		} `json:"providers"`
	}
	if !decodeLimit(w, r, &body, 96<<10) {
		return
	}
	if len(body.Providers) < 1 || len(body.Providers) > 12 || body.Providers[0].ID != "" {
		failure(w, 400, "须保留默认提供商，最多添加 12 个提供商")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	old, err := a.oidcConfigurations(r.Context())
	if err != nil {
		failure(w, 503, "无法读取 OIDC 设置，请检查设置密钥")
		return
	}
	values := []OIDCSettings{}
	seen := map[string]bool{}
	issuers := map[string]bool{}
	anyEnabled := false
	for _, item := range body.Providers {
		if seen[item.ID] || item.ID != "" && !providerIDPattern.MatchString(item.ID) {
			failure(w, 400, "提供商标识无效或重复")
			return
		}
		seen[item.ID] = true
		v := OIDCSettings{ID: item.ID, Enabled: item.Enabled, Name: strings.TrimSpace(item.Name), Issuer: strings.TrimRight(strings.TrimSpace(item.Issuer), "/"), ClientID: strings.TrimSpace(item.ClientID), ClientSecret: item.ClientSecret}
		if v.ClientSecret == "" && !item.ClearSecret {
			for _, saved := range old {
				if saved.ID == v.ID {
					v.ClientSecret = saved.ClientSecret
				}
			}
		}
		if v.Name == "" {
			v.Name = "OIDC"
		}
		if utf8.RuneCountInString(v.Name) > 64 || strings.ContainsAny(v.Name, "\r\n") || len(v.ClientID) > 256 || len(v.ClientSecret) > 4096 || v.Issuer != "" && (!validOIDCIssuer(v.Issuer) || issuers[v.Issuer]) || v.Enabled && (v.Issuer == "" || v.ClientID == "" || v.ClientSecret == "") {
			failure(w, 400, "OIDC 配置无效或 Issuer 重复")
			return
		}
		if v.Issuer != "" {
			issuers[v.Issuer] = true
		}
		anyEnabled = anyEnabled || v.Enabled
		values = append(values, v)
	}
	first, err := a.sealOIDC(values[0], "config")
	if err == nil {
		err = a.putSetting(r.Context(), tx, "oidc_config", first)
	}
	var extra string
	if err == nil {
		extra, err = a.sealOIDC(values[1:], "providers")
	}
	if err == nil {
		err = a.putSetting(r.Context(), tx, "oidc_providers", extra)
	}
	if err == nil {
		err = a.putPluginEnabled(r.Context(), tx, "wishingskin.oidc", "1.0.0", anyEnabled)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM oidc_flows")
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "settings.oidc", "site", "oidc")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存 OIDC 设置")
		return
	}
	items := []map[string]any{}
	for _, v := range values {
		items = append(items, a.oidcView(v))
	}
	respond(w, 200, map[string]any{"providers": items})
}
