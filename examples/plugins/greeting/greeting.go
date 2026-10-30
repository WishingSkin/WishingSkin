// Package greeting demonstrates a source-built plugin with an authenticated
// route and an editable, encrypted setting. It is not enabled by default.
package greeting

import (
	"context"
	"encoding/json"
	"net/http"
	sdk "wishingskin/pkg/plugin"
)

type Plugin struct{}

func (Plugin) Manifest() sdk.Manifest {
	return sdk.Manifest{ID: "community.greeting", Name: "问候示例", Description: "社区插件 SDK 的最小示例。", Version: "1.1.0", SettingsNamespace: "community.greeting", Settings: []sdk.SettingField{{Key: "message", Label: "问候文案", Type: "text"}}, Routes: []string{"/api/plugins/community.greeting/hello"}, Permissions: []string{"user"}, Pages: []sdk.PageContribution{{ID: "overview", Area: "admin.plugins", Label: "插件概览"}}, Tasks: []sdk.TaskDeclaration{{ID: "check-settings"}}}
}
func (Plugin) Register(host sdk.Host) error {
	if err := host.Page(sdk.Page{ID: "overview", Handler: func(ctx context.Context, _ sdk.Principal) (sdk.PageDocument, error) {
		var settings struct {
			Message string `json:"message"`
		}
		err := host.Settings(ctx, &settings)
		return sdk.PageDocument{Title: "问候示例", Summary: "社区插件页面", Sections: []sdk.PageSection{{Title: "当前配置", Items: []sdk.PageItem{{Label: "问候文案", Value: settings.Message}}}}}, err
	}}); err != nil {
		return err
	}
	if err := host.Task(sdk.Task{ID: "check-settings", Handler: func(ctx context.Context) error { var settings map[string]any; return host.Settings(ctx, &settings) }}); err != nil {
		return err
	}
	return host.Handle(sdk.Route{Method: "GET", Path: "/hello", Access: "user", Handler: func(w http.ResponseWriter, r *http.Request, u sdk.Principal) {
		var settings struct {
			Message string `json:"message"`
		}
		if err := host.Settings(r.Context(), &settings); err != nil {
			http.Error(w, "Plugin configuration unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"message": settings.Message, "userId": u.ID})
	}})
}
