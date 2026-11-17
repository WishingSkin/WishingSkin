package httpapi

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
)

//go:embed page_routes.json
var pageRoutesJSON []byte

type pageRouteTable struct {
	Paths      []string          `json:"paths"`
	Aliases    map[string]string `json:"aliases"`
	PluginPath string            `json:"pluginPath"`
}

// The same page contract is used by the browser and the HTTP fallback.
var pageRoutes = func() pageRouteTable {
	var routes pageRouteTable
	if err := json.Unmarshal(pageRoutesJSON, &routes); err != nil {
		panic(err)
	}
	return routes
}()

var pluginPagePattern = regexp.MustCompile(pageRoutes.PluginPath)

func isSPAPath(path string) bool {
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	if alias, ok := pageRoutes.Aliases[path]; ok {
		path = alias
	}
	for _, known := range pageRoutes.Paths {
		if path == known {
			return true
		}
	}
	return pluginPagePattern.MatchString(path) && len(strings.TrimPrefix(path, "/admin/plugins/")) <= 64
}
