package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
	"github.com/WishingSkin/WishingSkin/internal/securebox"
	"github.com/WishingSkin/WishingSkin/internal/store"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

func pluginOrigin(id string) string {
	if strings.HasPrefix(id, "wishingskin.") {
		return "official"
	}
	return "community"
}

type communityHost struct {
	a        *Server
	manifest sdk.Manifest
	routes   map[string]bool
}

func (h *communityHost) Handle(route sdk.Route) error {
	if route.Handler == nil || !strings.HasPrefix(route.Path, "/") || strings.ContainsAny(route.Path, "{}?\\ \r\n") || strings.Contains(route.Path, "..") || strings.Contains(route.Path, "//") {
		return fmt.Errorf("invalid community route")
	}
	if route.Access != "public" && route.Access != "user" && route.Access != "admin" {
		return fmt.Errorf("invalid community route permission")
	}
	if route.Method != "GET" && route.Method != "POST" && route.Method != "PATCH" && route.Method != "PUT" && route.Method != "DELETE" {
		return fmt.Errorf("invalid community route method")
	}
	pattern := route.Method + " /api/plugins/" + h.manifest.ID + route.Path
	if h.routes[pattern] {
		return fmt.Errorf("duplicate community route")
	}
	h.routes[pattern] = true
	h.a.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		enabled, err := h.a.communityEnabled(r.Context(), h.manifest.ID)
		if err != nil {
			failure(w, 503, "暂时无法读取插件状态")
			return
		}
		if !enabled {
			http.NotFound(w, r)
			return
		}
		principal := sdk.Principal{}
		if route.Access != "public" {
			u, token, err := h.a.current(r)
			if err != nil {
				sessionError(w, err)
				return
			}
			if route.Access == "admin" && !u.IsAdmin {
				failure(w, 403, "需要管理员权限")
				return
			}
			if route.Method != "GET" && !h.a.validWriteRequest(w, r, token) {
				return
			}
			principal = sdk.Principal{ID: u.ID, Admin: u.IsAdmin}
		} else if route.Method != "GET" && !h.a.publicWrite(w, r) {
			return
		}
		route.Handler(w, r, principal)
	})
	return nil
}
func (a *Server) communityEnabled(ctx context.Context, id string) (bool, error) {
	return a.readCommunityEnabled(ctx, a.s.DB, id)
}
func (a *Server) readCommunityEnabled(ctx context.Context, reader settingReader, id string) (bool, error) {
	return a.communityDependencyEnabled(ctx, reader, id, map[string]bool{})
}
func (a *Server) communityDependencyEnabled(ctx context.Context, reader settingReader, id string, visiting map[string]bool) (bool, error) {
	state, ok := a.plugins.State(id)
	if !ok || !state.Enabled || state.Error != "" || visiting[id] {
		return false, nil
	}
	enabled, err := a.readPluginEnabled(ctx, reader, id)
	if err != nil || !enabled {
		return enabled, err
	}
	if a.dynamic != nil {
		if item, exists := a.dynamic.Get(id); exists {
			state, err := a.dynamicCheckpoint(ctx, reader, id)
			if err != nil || state.Pending || state.Digest != "" && state.Digest != item.Digest {
				return false, err
			}
		}
	}
	visiting[id] = true
	defer delete(visiting, id)
	for _, dep := range state.Manifest.Dependencies {
		on, err := a.communityDependencyEnabled(ctx, reader, dep, visiting)
		if err != nil || !on {
			return false, err
		}
	}
	return true, nil
}
func (h *communityHost) Settings(ctx context.Context, target any) error {
	raw, err := h.a.readCommunitySettings(ctx, h.manifest.ID)
	if err != nil {
		return err
	}
	effective := h.a.effectiveCommunitySettings(h.manifest.ID, raw)
	data, err := json.Marshal(effective)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
func (h *communityHost) Subscribe(name string, handler func(context.Context, sdk.Event) error) error {
	if handler == nil {
		return errors.New("nil plugin handler")
	}
	allowed := false
	for _, v := range h.manifest.Events {
		if v == name {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("event not declared")
	}
	return h.a.pluginEvents.Subscribe(name, func(ctx context.Context, value any) error {
		on, err := h.a.communityEnabled(ctx, h.manifest.ID)
		if err != nil || !on {
			return err
		}
		event, ok := value.(sdk.Event)
		if !ok {
			return errors.New("invalid event")
		}
		return handler(ctx, event)
	})
}
func (a *Server) publishPluginEvent(ctx context.Context, name string, userID int64, resource string) {
	event := sdk.Event{Name: name, UserID: userID, ResourceID: resource}
	_ = a.pluginEvents.Publish(ctx, name, event)
	_ = a.queueExtensionEvent(ctx, event)
}
func (a *Server) readCommunitySettings(ctx context.Context, id string) (map[string]any, error) {
	return a.readCommunitySettingsFrom(ctx, a.s.DB, id)
}
func (a *Server) readCommunitySettingsFrom(ctx context.Context, reader settingReader, id string) (map[string]any, error) {
	var raw string
	err := reader.QueryRowContext(ctx, a.s.Bind("SELECT settings_json FROM plugins WHERE plugin_id=?"), id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || raw == "{}" {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := securebox.Open(a.cfg.SettingsKey, raw, "wishingskin:community:"+id)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	err = json.Unmarshal([]byte(plain), &data)
	return data, err
}
func (a *Server) isExternalPlugin(id string) bool {
	if a.dynamic != nil {
		if _, ok := a.dynamic.Get(id); ok {
			return true
		}
	}
	return pluginOrigin(id) == "community"
}

func mergePluginSettingDefaults(manifest sdk.Manifest, stored map[string]any) map[string]any {
	merged := make(map[string]any, len(stored)+len(manifest.Settings))
	for k, v := range stored {
		merged[k] = v
	}
	for _, f := range manifest.Settings {
		if f.Type == "secret" || f.Default == nil {
			continue
		}
		if v, exists := merged[f.Key]; !exists || v == nil {
			merged[f.Key] = f.Default
		}
	}
	return merged
}

func (a *Server) effectiveCommunitySettings(id string, stored map[string]any) map[string]any {
	state, ok := a.plugins.State(id)
	if !ok {
		return stored
	}
	return mergePluginSettingDefaults(state.Manifest, stored)
}

func (a *Server) communitySettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	state, ok := a.plugins.State(r.PathValue("id"))
	if !ok || !a.isExternalPlugin(state.Manifest.ID) {
		http.NotFound(w, r)
		return
	}
	raw, err := a.readCommunitySettings(r.Context(), state.Manifest.ID)
	if err != nil {
		failure(w, 503, "暂时无法读取插件配置")
		return
	}
	configured := map[string]bool{}
	for _, f := range state.Manifest.Settings {
		if f.Type == "secret" {
			v, _ := raw[f.Key].(string)
			configured[f.Key] = v != ""
			delete(raw, f.Key)
		}
	}
	data := mergePluginSettingDefaults(state.Manifest, raw)
	respond(w, 200, map[string]any{"values": data, "configured": configured})
}
func (a *Server) updateCommunitySettings(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	state, ok := a.plugins.State(r.PathValue("id"))
	if !ok || !a.isExternalPlugin(state.Manifest.ID) {
		http.NotFound(w, r)
		return
	}
	var body map[string]any
	if !decodeLimit(w, r, &body, 65536) {
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	previous, err := a.readCommunitySettings(r.Context(), state.Manifest.ID)
	if err != nil {
		failure(w, 503, "暂时无法读取插件配置")
		return
	}
	next := map[string]any{}
	for _, f := range state.Manifest.Settings {
		value, exists := body[f.Key]
		if !exists {
			next[f.Key] = previous[f.Key]
			continue
		}
		valid := false
		switch f.Type {
		case "boolean":
			_, valid = value.(bool)
		case "number":
			_, valid = value.(float64)
		default:
			text, ok := value.(string)
			valid = ok && utf8.ValidString(text) && len(text) <= 4096
			if valid && len(f.Options) > 0 && text != "" {
				valid = slices.ContainsFunc(f.Options, func(opt sdk.SettingOption) bool {
					return opt.Value == text
				})
			}
			if valid && f.Type == "secret" && text == "" {
				value = previous[f.Key]
			}
		}
		if !valid {
			failure(w, 400, "插件配置格式无效")
			return
		}
		next[f.Key] = value
		delete(body, f.Key)
	}
	if len(body) != 0 {
		failure(w, 400, "插件配置包含未声明的字段")
		return
	}
	for _, field := range state.Manifest.Settings {
		if fmt.Sprint(previous[field.Key]) != fmt.Sprint(next[field.Key]) {
			if field.Type == "secret" || field.Type == "string" || field.Type == "text" {
				store.CaptureAdminChange(r.Context(), "plugin."+field.Key, "", "配置已更新")
			} else {
				a.auditChange(r.Context(), "plugin."+field.Key, previous[field.Key], next[field.Key])
			}
		}
	}
	data, err := json.Marshal(next)
	var sealed string
	if err == nil {
		sealed, err = securebox.Seal(a.cfg.SettingsKey, string(data), "wishingskin:community:"+state.Manifest.ID)
	}
	if err == nil {
		var enabled bool
		enabled, err = a.storedPluginEnabled(r.Context(), state.Manifest.ID)
		if err == nil {
			err = a.putPluginEnabled(r.Context(), tx, state.Manifest.ID, state.Manifest.Version, enabled)
		}
	}
	// Preserve the existing switch when configuring a plugin.
	if err == nil {
		_, err = tx.ExecContext(r.Context(), a.s.Bind("UPDATE plugins SET settings_json=? WHERE plugin_id=?"), sealed, state.Manifest.ID)
	}
	if err == nil {
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, "plugin.configure", "site", state.Manifest.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(w, 503, "暂时无法保存插件配置")
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
