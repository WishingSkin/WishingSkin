package httpapi

import (
	"context"
	"database/sql"
	localstorage "github.com/WishingSkin/WishingSkin/internal/storage"
	"net/http"
)

func (a *Server) registerPlugins() {
	a.mux.HandleFunc("GET /api/admin/plugin-runtime/{id}", a.adminPluginRuntime)
	a.mux.HandleFunc("GET /api/admin/plugin-runtime/{id}/pages/{page}", a.adminPluginPage)
	a.mux.HandleFunc("POST /api/admin/plugin-runtime/{id}/tasks/{task}", a.adminStartPluginTask)
	a.mux.HandleFunc("GET /api/admin/plugins", a.adminPlugins)
	a.mux.HandleFunc("GET /api/admin/object-storage", a.adminObjectStorage)
	a.mux.HandleFunc("PATCH /api/admin/object-storage", a.updateObjectStorage)
	a.mux.HandleFunc("GET /api/admin/plugin-settings/{id}", a.communitySettings)
	a.mux.HandleFunc("PATCH /api/admin/plugin-settings/{id}", a.updateCommunitySettings)
	a.mux.HandleFunc("PATCH /api/admin/plugins/{id}", a.updatePlugin)
}

func (a *Server) storedPluginEnabled(ctx context.Context, id string) (bool, error) {
	return a.readPluginEnabled(ctx, a.s.DB, id)
}

func (a *Server) readPluginEnabled(ctx context.Context, reader settingReader, id string) (bool, error) {
	var enabled bool
	err := reader.QueryRowContext(ctx, a.s.Bind("SELECT enabled FROM plugins WHERE plugin_id=?"), id).Scan(&enabled)
	if err == sql.ErrNoRows {
		if id == "wishingskin.oidc" {
			value, err := a.readOIDCConfiguration(ctx, reader)
			return value.Enabled, err
		}
		return false, nil
	}
	return enabled, err
}

func (a *Server) putPluginEnabled(ctx context.Context, tx *sql.Tx, id, version string, enabled bool) error {
	query := "INSERT INTO plugins(plugin_id,version,enabled,settings_json) VALUES(?,?,?,'{}') ON CONFLICT(plugin_id) DO UPDATE SET enabled=EXCLUDED.enabled,version=EXCLUDED.version"
	if a.s.Dialect == "mysql" {
		query = "INSERT INTO plugins(plugin_id,version,enabled,settings_json) VALUES(?,?,?,'{}') ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),version=VALUES(version)"
	}
	_, err := tx.ExecContext(ctx, a.s.Bind(query), id, version, enabled)
	return err
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
		item := map[string]any{
			"name": state.Manifest.Name, "description": state.Manifest.Description, "settings": state.Manifest.Settings, "origin": pluginOrigin(state.Manifest.ID), "id": state.Manifest.ID, "version": state.Manifest.Version, "dependencies": state.Manifest.Dependencies,
			"settingsNamespace": state.Manifest.SettingsNamespace, "migrationVersion": state.Manifest.MigrationVersion,
			"routes": state.Manifest.Routes, "capabilities": state.Manifest.Capabilities, "events": state.Manifest.Events,
			"pages": state.Manifest.Pages, "tasks": state.Manifest.Tasks, "permissions": state.Manifest.Permissions,
			"enabled": enabled && state.Enabled, "error": state.Error,
		}
		if installed, exists := a.dynamic.Get(state.Manifest.ID); exists {
			checkpoint, err := a.dynamicCheckpoint(r.Context(), a.s.DB, state.Manifest.ID)
			if err != nil {
				failure(w, 503, "暂时无法读取插件状态")
				return
			}
			if checkpoint.Pending {
				item["enabled"] = false
				item["recoveryRequired"] = true
			}
			if !checkpoint.Pending && checkpoint.Digest != "" && checkpoint.Digest != installed.Digest {
				item["enabled"] = false
				item["staleRuntime"] = true
			}
		}
		items = append(items, item)
	}
	respond(w, 200, map[string]any{"plugins": items})
}

func (a *Server) updatePlugin(w http.ResponseWriter, r *http.Request) {
	u, ok := a.adminRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	state, exists := a.plugins.State(id)
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
	if _, dynamic := a.dynamic.Get(id); dynamic {
		lock := a.dynamicLock(id)
		lock.Lock()
		defer lock.Unlock()
		var stillInstalled bool
		state, stillInstalled = a.plugins.State(id)
		if !stillInstalled {
			failure(w, 404, "插件不存在")
			return
		}
		checkpoint, err := a.dynamicCheckpoint(r.Context(), a.s.DB, id)
		if err != nil {
			failure(w, 503, "暂时无法读取插件状态")
			return
		}
		if checkpoint.Pending {
			pluginRecoveryResponse(w, "插件状态保存失败")
			return
		}
		item, installed := a.dynamic.Get(id)
		if !installed || checkpoint.Digest != "" && checkpoint.Digest != item.Digest {
			respond(w, 409, map[string]any{"error": "插件状态保存失败", "staleRuntime": true})
			return
		}
	}
	if state.Error != "" && *body.Enabled {
		failure(w, 409, "插件依赖尚未满足")
		return
	}
	tx, ok := a.beginAdminWrite(w, r, u)
	if !ok {
		return
	}
	defer tx.Rollback()
	if *body.Enabled {
		for _, id := range state.Manifest.Dependencies {
			enabled, err := a.storedPluginEnabled(r.Context(), id)
			if err != nil {
				failure(w, 503, "暂时无法读取插件状态")
				return
			}
			if !enabled {
				failure(w, 409, "插件依赖尚未满足")
				return
			}
		}
	}
	if state.Manifest.ID == "wishingskin.oidc" && *body.Enabled {
		values, err := a.oidcConfigurations(r.Context())
		valid := false
		for _, value := range values {
			valid = valid || value.Enabled && validOIDCIssuer(value.Issuer) && value.ClientID != "" && value.ClientSecret != ""
		}
		if err != nil || !valid {
			failure(w, 409, "请先保存有效的 OIDC 配置")
			return
		}
	}
	if state.Manifest.ID == "wishingskin.object-storage" && *body.Enabled {
		v, _, err := a.objectStorageSettings(r.Context())
		if err != nil || localstorage.ValidateS3(v) != nil {
			failure(w, 409, "请先保存有效的对象存储配置")
			return
		}
	}
	var err error
	if id == minecraftPlugin {
		cfg, configErr := a.minecraftSettings(r.Context(), tx)
		if *body.Enabled && (configErr != nil || !cfg.valid()) {
			failure(w, 409, "请先保存有效的微软应用配置")
			return
		}
		if configErr == nil && cfg.valid() && cfg.Enabled != *body.Enabled {
			cfg.Enabled = *body.Enabled
			cfg.Revision, err = secret()
			if err == nil {
				err = a.putMinecraftSettings(r.Context(), tx, cfg)
			}
		}
		if err == nil && !*body.Enabled {
			_, err = tx.ExecContext(r.Context(), "DELETE FROM minecraft_flows")
		}
	}
	if err == nil {
		err = a.putPluginEnabled(r.Context(), tx, state.Manifest.ID, state.Manifest.Version, *body.Enabled)
	}
	if err == nil && state.Manifest.ID == "wishingskin.oidc" && !*body.Enabled {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM oidc_flows")
	}
	if err == nil && state.Manifest.ID == "wishingskin.oauth2" && !*body.Enabled {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM oauth_codes")
		if err == nil {
			_, err = tx.ExecContext(r.Context(), "DELETE FROM oauth_tokens")
		}
		for _, table := range []string{"oauth_refresh_tokens", "oauth_access_families", "oauth_refresh_metadata", "oauth_code_metadata"} {
			if err == nil {
				_, err = tx.ExecContext(r.Context(), "DELETE FROM "+table)
			}
		}
	}
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
		failure(w, 503, "插件状态保存失败")
		return
	}
	if !*body.Enabled {
		a.cancelCommunityTasks(state.Manifest.ID)
		if a.dynamic != nil {
			_ = a.dynamic.Reload(state.Manifest.ID)
		}
	}
	respond(w, 200, map[string]any{"name": state.Manifest.Name, "description": state.Manifest.Description, "settings": state.Manifest.Settings, "origin": pluginOrigin(state.Manifest.ID), "id": state.Manifest.ID, "enabled": *body.Enabled})
}
