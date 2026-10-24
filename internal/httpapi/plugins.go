package httpapi

import (
	"context"
	"database/sql"
	"net/http"
)

func (a *Server) registerPlugins() {
	a.mux.HandleFunc("GET /api/admin/plugins", a.adminPlugins)
	a.mux.HandleFunc("PATCH /api/admin/plugins/{id}", a.updatePlugin)
}

func (a *Server) storedPluginEnabled(ctx context.Context, id string) (bool, error) {
	var enabled bool
	err := a.s.DB.QueryRowContext(ctx, a.s.Bind("SELECT enabled FROM plugins WHERE plugin_id=?"), id).Scan(&enabled)
	if err == sql.ErrNoRows {
		return true, nil
	}
	return enabled, err
}

func (a *Server) adminPlugins(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminRead(w, r); !ok {
		return
	}
	states := a.plugins.States()
	items := make([]map[string]any, 0, len(states))
	for _, state := range states {
		enabled, err := a.storedPluginEnabled(r.Context(), state.Manifest.ID)
		if err != nil {
			failure(w, 503, "暂时无法读取插件状态")
			return
		}
		items = append(items, map[string]any{
			"id": state.Manifest.ID, "version": state.Manifest.Version, "dependencies": state.Manifest.Dependencies,
			"settingsNamespace": state.Manifest.SettingsNamespace, "migrationVersion": state.Manifest.MigrationVersion,
			"routes": state.Manifest.Routes, "capabilities": state.Manifest.Capabilities, "events": state.Manifest.Events,
			"pages": state.Manifest.Pages, "tasks": state.Manifest.Tasks, "permissions": state.Manifest.Permissions,
			"enabled": enabled && state.Enabled, "error": state.Error,
		})
	}
	respond(w, 200, map[string]any{"plugins": items})
}

func (a *Server) updatePlugin(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	state, exists := a.plugins.State(r.PathValue("id"))
	if !exists {
		failure(w, 404, "插件不存在")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if !decode(w, r, &body) || body.Enabled == nil {
		failure(w, 400, "插件状态无效")
		return
	}
	if err := a.plugins.SetEnabled(state.Manifest.ID, *body.Enabled); err != nil {
		failure(w, 409, "插件依赖尚未满足")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	_, err := tx.ExecContext(r.Context(), a.s.Bind("UPDATE plugins SET enabled=? WHERE plugin_id=?"), *body.Enabled, state.Manifest.ID)
	if err == nil {
		action := "plugin.disable"
		if *body.Enabled {
			action = "plugin.enable"
		}
		err = a.s.RecordAdminEvent(r.Context(), tx, u.ID, action, "site", state.Manifest.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		_ = a.plugins.SetEnabled(state.Manifest.ID, !*body.Enabled)
		failure(w, 503, "插件状态保存失败")
		return
	}
	respond(w, 200, map[string]any{"id": state.Manifest.ID, "enabled": *body.Enabled})
}
