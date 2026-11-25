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
	Paths        []string          `json:"paths"`
	Aliases      map[string]string `json:"aliases"`
	LibraryPath  string            `json:"libraryPath"`
	PluginPath   string            `json:"pluginPath"`
	UserToolPath string            `json:"userToolPath"`
}

// The same page contract is used by the browser and the HTTP fallback.
var pageRoutes = func() pageRouteTable {
	var routes pageRouteTable
	if err := json.Unmarshal(pageRoutesJSON, &routes); err != nil {
		panic(err)
	}
	return routes
}()

var (
	libraryPagePattern  = regexp.MustCompile(pageRoutes.LibraryPath)
	pluginPagePattern   = regexp.MustCompile(pageRoutes.PluginPath)
	userToolPagePattern = regexp.MustCompile(pageRoutes.UserToolPath)
)

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
	if libraryPagePattern.MatchString(path) {
		return true
	}
	if pluginPagePattern.MatchString(path) && len(strings.TrimPrefix(path, "/admin/plugins/")) <= 64 {
		return true
	}
	if userToolPagePattern.MatchString(path) {
		rest := strings.TrimPrefix(path, "/user/tools/")
		pid, fid, ok := strings.Cut(rest, "/")
		return ok && len(pid) <= 64 && len(fid) <= 64
	}
	return false
}
