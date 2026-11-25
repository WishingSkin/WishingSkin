package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"github.com/WishingSkin/WishingSkin/internal/runtimeplugin"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

var (
	pluginCacheControlPattern = regexp.MustCompile(`^public,\s*max-age=(\d+)$`)
)

func allowedPluginBinaryType(contentType string) bool {
	switch contentType {
	case "image/png", "image/webp", "image/svg+xml", "text/plain; charset=utf-8", "application/octet-stream":
		return true
	default:
		return false
	}
}

func validPluginCacheControl(value string) bool {
	matches := pluginCacheControlPattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return false
	}
	seconds, err := strconv.Atoi(matches[1])
	return err == nil && seconds >= 0 && seconds <= 86400
}

func validPluginETag(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 0x21 || c > 0x7e || c == '\\' {
			return false
		}
	}
	return true
}

func (a *Server) dynamicLock(id string) *sync.Mutex {
	lock, _ := a.dynamicCalls.LoadOrStore(id, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (a *Server) unregisterDynamicContributions(id string) {
	rt := &a.communityRuntime
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for key := range rt.pages {
		if strings.HasPrefix(key, id+"/") {
			delete(rt.pages, key)
		}
	}
	for key := range rt.tasks {
		if strings.HasPrefix(key, id+"/") {
			delete(rt.tasks, key)
		}
	}
}
func (a *Server) registerDynamicContributions(item runtimeplugin.Installed) {
	id := item.Package.Manifest.ID
	rt := &a.communityRuntime
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, p := range item.Package.Manifest.Pages {
		pageID := p.ID
		rt.pages[runtimeKey(id, pageID)] = sdk.Page{ID: pageID, Handler: func(ctx context.Context, p sdk.Principal) (sdk.PageDocument, error) {
			var doc sdk.PageDocument
			data, err := a.callDynamic(ctx, id, sdk.RuntimeRequest{Kind: "page", ID: pageID, Principal: p})
			if err == nil {
				var result sdk.RuntimeResponse
				err = json.Unmarshal(data, &result)
				if err == nil {
					err = json.Unmarshal(result.Body, &doc)
				}
			}
			return doc, err
		}}
	}
	for _, t := range item.Package.Manifest.Tasks {
		taskID := t.ID
		interval, _ := sdk.TaskInterval(t.Schedule)
		rt.tasks[runtimeKey(id, taskID)] = registeredTask{interval: interval, task: sdk.Task{ID: taskID, Timeout: time.Minute, Handler: func(ctx context.Context) error {
			_, err := a.callDynamic(ctx, id, sdk.RuntimeRequest{Kind: "task", ID: taskID})
			return err
		}}}
	}
}
func (a *Server) callDynamic(ctx context.Context, id string, request sdk.RuntimeRequest) (json.RawMessage, error) {
	mu := a.dynamicLock(id)
	mu.Lock()
	defer mu.Unlock()
	item, ok := a.dynamic.Get(id)
	if !ok {
		return nil, errPluginUnavailable
	}
	return a.callDynamicLocked(ctx, item, request)
}

func (a *Server) callDynamicLocked(ctx context.Context, item runtimeplugin.Installed, request sdk.RuntimeRequest) (json.RawMessage, error) {
	id := item.Package.Manifest.ID
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var database interface {
		settingReader
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	} = a.s.DB
	hasData := slices.Contains(item.Grants, "database")
	if hasData {
		conn, release, err := a.s.LeasePluginData(ctx, id)
		if err != nil {
			return nil, err
		}
		defer release()
		database = conn
	}
	enabled, err := a.readCommunityEnabled(ctx, database, id)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, errPluginUnavailable
	}
	request.Settings, err = a.readCommunitySettingsFrom(ctx, database, id)
	if err != nil {
		return nil, err
	}
	if hasData {
		request.Data = map[string]json.RawMessage{}
		rows, err := database.QueryContext(ctx, a.s.Bind("SELECT data_key,value_json FROM plugin_data WHERE plugin_id=?"), id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key, value string
			if err = rows.Scan(&key, &value); err != nil {
				rows.Close()
				return nil, err
			}
			request.Data[key] = json.RawMessage(value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	data, err := a.dynamic.Call(ctx, id, request)
	if err != nil {
		return nil, err
	}
	var response sdk.RuntimeResponse
	if err = json.Unmarshal(data, &response); err != nil {
		return nil, errors.New("invalid runtime response")
	}
	if response.Status < 200 || response.Status >= 600 || response.Status >= 300 && response.Status < 400 {
		return nil, errors.New("invalid runtime response")
	}
	if response.BodyBase64 != "" {
		if request.Kind != "route" || !allowedPluginBinaryType(response.ContentType) {
			return nil, errors.New("invalid runtime binary response")
		}
		decoded, decErr := base64.StdEncoding.DecodeString(response.BodyBase64)
		if decErr != nil || len(decoded) > maxTextureBytes {
			return nil, errors.New("invalid runtime binary payload")
		}
		if len(response.Body) == 0 {
			response.Body = json.RawMessage("null")
		}
	} else if !json.Valid(response.Body) {
		return nil, errors.New("invalid runtime response")
	}
	if request.Kind != "route" && response.Status >= 400 {
		return nil, errors.New("plugin operation failed")
	}
	if len(response.Writes) > 0 {
		if response.Status >= 400 {
			return nil, errors.New("failed operation cannot write data")
		}
		if !hasData || len(response.Writes) > 128 {
			return nil, errors.New("database permission required")
		}
		for key, value := range response.Writes {
			if key == "" || len(key) > 128 || len(value) > 65536 || !json.Valid(value) {
				return nil, errors.New("invalid plugin data")
			}
			if string(value) == "null" {
				delete(request.Data, key)
			} else {
				request.Data[key] = value
			}
		}
		total := 0
		for key, value := range request.Data {
			total += len(key) + len(value)
		}
		if total > 1<<20 || len(request.Data) > 1024 {
			return nil, errors.New("plugin data limit")
		}
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		for key, value := range response.Writes {
			if string(value) == "null" {
				_, err = tx.ExecContext(ctx, a.s.Bind("DELETE FROM plugin_data WHERE plugin_id=? AND data_key=?"), id, key)
			} else {
				query := "INSERT INTO plugin_data(plugin_id,data_key,value_json) VALUES(?,?,?) ON CONFLICT(plugin_id,data_key) DO UPDATE SET value_json=EXCLUDED.value_json"
				if a.s.Dialect == "mysql" {
					query = "INSERT INTO plugin_data(plugin_id,data_key,value_json) VALUES(?,?,?) ON DUPLICATE KEY UPDATE value_json=VALUES(value_json)"
				}
				_, err = tx.ExecContext(ctx, a.s.Bind(query), id, key, string(value))
			}
			if err != nil {
				return nil, err
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	}
	// Writes are host instructions and must never be returned to a public route.
	response.Writes = nil
	return json.Marshal(response)
}
func (a *Server) dynamicRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path := "/" + r.PathValue("path")
	if !runtimeplugin.ValidRoutePath(path) || r.URL.EscapedPath() != "/api/plugins/"+id+path {
		http.NotFound(w, r)
		return
	}
	item, ok := a.dynamic.Get(id)
	if !ok || !a.availableCommunity(r.Context(), id) {
		http.NotFound(w, r)
		return
	}
	if digest := r.Header.Get("X-Plugin-Digest"); digest != "" && digest != item.Digest {
		respond(w, 409, map[string]any{"error": "插件暂时不可用", "staleRuntime": true})
		return
	}
	var route *runtimeplugin.Route
	for _, v := range item.Package.Routes {
		if v.Method == r.Method && v.Path == path {
			copy := v
			route = &copy
			break
		}
	}
	if route == nil {
		http.NotFound(w, r)
		return
	}
	principal := sdk.Principal{}
	if route.Access != "public" {
		u, token, err := a.current(r)
		if err != nil {
			sessionError(w, err)
			return
		}
		if route.Access == "admin" && !u.IsAdmin {
			failure(w, 403, "需要管理员权限")
			return
		}
		if r.Method != "GET" && !a.validWriteRequest(w, r, token) {
			return
		}
		principal = sdk.Principal{ID: u.ID, Admin: u.IsAdmin}
	} else if r.Method != "GET" && !a.publicWrite(w, r) {
		return
	}
	bodyLimit := int64(65536)
	if slices.Contains(item.Grants, "user.frontend") || slices.Contains(item.Grants, "host.textures.write") {
		bodyLimit = 12 << 20
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bodyLimit))
	if err != nil || (len(body) > 0 && !json.Valid(body)) {
		failure(w, 400, "请求内容无效")
		return
	}
	mu := a.dynamicLock(id)
	mu.Lock()
	defer mu.Unlock()
	current, ok := a.dynamic.Get(id)
	if !ok || current.Digest != item.Digest {
		respond(w, 409, map[string]any{"error": "插件暂时不可用", "staleRuntime": true})
		return
	}
	data, err := a.callDynamicLocked(r.Context(), item, sdk.RuntimeRequest{Kind: "route", Method: r.Method, Path: route.Path, Query: r.URL.RawQuery, Body: body, Principal: principal})
	if errors.Is(err, errPluginUnavailable) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		failure(w, 503, "插件暂时不可用")
		return
	}
	var result sdk.RuntimeResponse
	if json.Unmarshal(data, &result) != nil || result.Status < 200 || result.Status > 599 || result.Status >= 300 && result.Status < 400 {
		failure(w, 503, "插件响应无效")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if route.Access == "public" && r.Method == http.MethodGet {
		if validPluginCacheControl(result.CacheControl) {
			w.Header().Set("Cache-Control", result.CacheControl)
		}
		if validPluginETag(result.ETag) {
			w.Header().Set("ETag", result.ETag)
		}
	}
	if result.BodyBase64 != "" {
		if !allowedPluginBinaryType(result.ContentType) {
			failure(w, 503, "插件响应无效")
			return
		}
		rawBytes, decErr := base64.StdEncoding.DecodeString(result.BodyBase64)
		if decErr != nil || len(rawBytes) > maxTextureBytes {
			failure(w, 503, "插件响应无效")
			return
		}
		w.Header().Set("Content-Type", result.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if result.ContentType == "image/svg+xml" {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		}
		w.WriteHeader(result.Status)
		_, _ = w.Write(rawBytes)
		return
	}
	if len(result.Body) > 1<<20 || !json.Valid(result.Body) {
		failure(w, 503, "插件响应无效")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.Body)
}
func (a *Server) dynamicFrontend(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	mu := a.dynamicLock(id)
	mu.Lock()
	defer mu.Unlock()
	item, ok := a.dynamic.Get(id)
	if !ok || !a.availableCommunity(r.Context(), id) || item.Package.Frontend == "" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(a.dynamic.Root(item), filepath.FromSlash(item.Package.Frontend)))
	if err != nil || len(data) > 1<<20 {
		failure(w, 503, "暂时无法读取插件界面")
		return
	}
	// srcdoc runs in an opaque-origin sandbox. Only the explicit host bridge can make requests.
	html := "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; form-action 'none'; base-uri 'none'\">" + string(data)
	routes := []runtimeplugin.Route{}
	for _, route := range item.Package.Routes {
		if runtimeplugin.ValidRoutePath(route.Path) {
			routes = append(routes, route)
		}
	}
	respond(w, 200, map[string]any{"html": html, "routes": routes, "digest": item.Digest})
}

func (a *Server) userDynamicFrontend(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.current(r); err != nil {
		sessionError(w, err)
		return
	}
	id := r.PathValue("id")
	fid := r.PathValue("fid")
	mu := a.dynamicLock(id)
	mu.Lock()
	defer mu.Unlock()
	item, ok := a.dynamic.Get(id)
	if !ok || !a.availableCommunity(r.Context(), id) || !slices.Contains(item.Grants, "user.frontend") {
		http.NotFound(w, r)
		return
	}
	var frontend *sdk.UserFrontend
	for _, uf := range item.Package.UserFrontends {
		if uf.ID == fid {
			copyUF := uf
			frontend = &copyUF
			break
		}
	}
	if frontend == nil {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(a.dynamic.Root(item), filepath.FromSlash(frontend.Path)))
	if err != nil || len(data) > 2<<20 {
		failure(w, 503, "暂时无法读取插件界面")
		return
	}
	html := "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; connect-src 'none'; form-action 'none'; base-uri 'none'\">" + string(data)
	routes := []runtimeplugin.Route{}
	for _, route := range item.Package.Routes {
		if route.Access != "admin" && runtimeplugin.ValidRoutePath(route.Path) {
			routes = append(routes, route)
		}
	}
	respond(w, 200, map[string]any{
		"html":        html,
		"routes":      routes,
		"digest":      item.Digest,
		"permissions": slices.Clone(item.Grants),
		"frontend":    frontend,
	})
}
