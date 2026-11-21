package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"wishingskin/internal/runtimeplugin"
)

type marketEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}
type marketCatalog struct {
	Packages []marketEntry `json:"packages"`
}

func validExtensionURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || u.Scheme == "http" && loopbackOriginHost(u.Hostname()))
}
func fetchExtension(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if !validExtensionURL(raw) {
		return nil, errors.New("invalid extension URL")
	}
	client := newSafeExtensionClient(30 * time.Second)
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("download failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("download too large")
	}
	return data, err
}
func (a *Server) registerPluginMarket() {
	a.mux.HandleFunc("GET /api/admin/plugin-market", a.pluginMarket)
	a.mux.HandleFunc("PUT /api/admin/plugin-market", a.savePluginMarket)
	a.mux.HandleFunc("POST /api/admin/plugin-market/inspect", a.inspectMarketEntry)
	a.mux.HandleFunc("POST /api/admin/plugin-market/install", a.installMarketEntry)
}
func (a *Server) marketCatalog(ctx context.Context) (string, marketCatalog, error) {
	var source string
	catalog := marketCatalog{Packages: []marketEntry{}}
	if _, err := a.readSetting(ctx, "plugin_market_source", &source); err != nil {
		return source, catalog, err
	}
	if source == "" {
		return source, catalog, nil
	}
	data, err := fetchExtension(ctx, source, 512<<10)
	if err != nil {
		return source, catalog, err
	}
	if err = json.Unmarshal(data, &catalog); err != nil || len(catalog.Packages) > 100 {
		return source, marketCatalog{}, errors.New("invalid catalog")
	}
	seen := map[string]bool{}
	for _, entry := range catalog.Packages {
		if seen[entry.ID] || !validExtensionURL(entry.URL) || len(entry.SHA256) != 64 || entry.ID == "" {
			return source, marketCatalog{}, errors.New("invalid catalog entry")
		}
		seen[entry.ID] = true
	}
	return source, catalog, nil
}
func (a *Server) pluginMarket(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	source, catalog, err := a.marketCatalog(r.Context())
	if err != nil {
		respond(w, 200, map[string]any{"source": source, "packages": []marketEntry{}, "unavailable": true})
		return
	}
	respond(w, 200, map[string]any{"source": source, "packages": catalog.Packages})
}
func (a *Server) savePluginMarket(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	var body struct {
		Source string `json:"source"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Source = strings.TrimSpace(body.Source)
	if body.Source != "" && !validExtensionURL(body.Source) {
		failure(w, 400, "市场地址须使用 HTTPS 或本机 HTTP")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	err := a.putSetting(r.Context(), tx, "plugin_market_source", body.Source)
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "plugin.market.configure", "site", "plugins")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存市场地址")
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
func (a *Server) marketPackage(r *http.Request) ([]byte, error) {
	var body struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body) != nil {
		return nil, errors.New("invalid entry")
	}
	_, catalog, err := a.marketCatalog(r.Context())
	if err != nil {
		return nil, err
	}
	for _, entry := range catalog.Packages {
		if entry.ID == body.ID {
			data, err := fetchExtension(r.Context(), entry.URL, runtimeplugin.MaxPackage)
			if err != nil {
				return nil, err
			}
			if runtimeplugin.Digest(data) != strings.ToLower(entry.SHA256) {
				return nil, errors.New("checksum mismatch")
			}
			pkg, err := runtimeplugin.Inspect(data)
			if err != nil || pkg.ValidateRoutePaths() != nil || pkg.Manifest.ID != entry.ID || pkg.Manifest.Version != entry.Version {
				return nil, errors.New("catalog manifest mismatch")
			}
			return data, nil
		}
	}
	return nil, errors.New("unknown package")
}
func (a *Server) inspectMarketEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRequest(w, r); !ok {
		return
	}
	release, ok := a.acquirePluginUploadSlot(w)
	if !ok {
		return
	}
	defer release()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(pluginPackageTimeout))
	data, err := a.marketPackage(r)
	if err != nil {
		failure(w, 400, "无法下载或校验插件包")
		return
	}
	pkg, _ := runtimeplugin.Inspect(data)
	respond(w, 200, map[string]any{"package": pkg, "digest": runtimeplugin.Digest(data)})
}
func (a *Server) installMarketEntry(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	release, ok := a.acquirePluginUploadSlot(w)
	if !ok {
		return
	}
	defer release()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(pluginPackageTimeout))
	data, err := a.marketPackage(r)
	if err != nil {
		failure(w, 400, "无法下载或校验插件包")
		return
	}
	var grants []string
	if json.Unmarshal([]byte(r.Header.Get("X-Plugin-Permissions")), &grants) != nil || r.Header.Get("X-Plugin-Digest") != runtimeplugin.Digest(data) {
		failure(w, 409, "插件包已变化，请重新检查并确认权限")
		return
	}
	if err = a.activatePackage(r.Context(), data, grants); err != nil {
		pluginActivationError(w, err)
		return
	}
	pkg, _ := runtimeplugin.Inspect(data)
	_ = a.recordDynamicEvent(r.Context(), u.ID, "plugin.install", pkg.Manifest.ID)
	respond(w, 200, map[string]bool{"installed": true})
}
