// Package runtimeplugin loads trusted executable plugins without restarting the host.
package runtimeplugin

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"runtime"
	"slices"
	"strings"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

const MaxPackage = 128 << 20

var Permissions = []string{"routes.public", "routes.user", "routes.admin", "events", "pages", "tasks", "frontend", "database", "webhooks"}
var routePathPattern = regexp.MustCompile(`^/(?:[A-Za-z0-9._~-]+/)*[A-Za-z0-9._~-]*$`)

func ValidRoutePath(value string) bool {
	if !routePathPattern.MatchString(value) {
		return false
	}
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Access string `json:"access"`
}
type Migration struct {
	Version  int      `json:"version"`
	MySQL    []string `json:"mysql"`
	Postgres []string `json:"postgres"`
}
type Package struct {
	Manifest    sdk.Manifest      `json:"manifest"`
	Protocol    int               `json:"protocol"`
	Executables map[string]string `json:"executables"`
	Routes      []Route           `json:"routes"`
	Frontend    string            `json:"frontend,omitempty"`
	Migrations  []Migration       `json:"migrations,omitempty"`
}

func (p Package) ValidateRoutePaths() error {
	for _, route := range p.Routes {
		if !ValidRoutePath(route.Path) {
			return errors.New("invalid route path")
		}
	}
	return nil
}

func (p Package) Validate() error {
	if err := p.Manifest.Validate(); err != nil {
		return err
	}
	if strings.HasPrefix(p.Manifest.ID, "wishingskin.") || p.Protocol != 1 || len(p.Manifest.Version) > 64 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`).MatchString(p.Manifest.Version) {
		return errors.New("invalid package identity or protocol")
	}
	seen := map[string]bool{}
	for _, permission := range p.Manifest.Permissions {
		if !slices.Contains(Permissions, permission) || seen[permission] {
			return errors.New("unknown or duplicate permission")
		}
		seen[permission] = true
	}
	need := func(name string, used bool) error {
		if used && !seen[name] {
			return fmt.Errorf("permission %s required", name)
		}
		return nil
	}
	for _, r := range p.Routes {
		if !slices.Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}, r.Method) || !slices.Contains([]string{"public", "user", "admin"}, r.Access) || !strings.HasPrefix(r.Path, "/") || strings.ContainsAny(r.Path, "{}? ") || strings.IndexByte(r.Path, 92) >= 0 || strings.IndexFunc(r.Path, func(c rune) bool { return c < 32 }) >= 0 || strings.Contains(r.Path, "..") || strings.Contains(r.Path, "//") {
			return errors.New("invalid route")
		}
		key := r.Method + " " + r.Path
		if seen[key] {
			return errors.New("duplicate route")
		}
		seen[key] = true
		if err := need("routes."+r.Access, true); err != nil {
			return err
		}
	}
	for _, check := range []struct {
		name string
		used bool
	}{{"events", len(p.Manifest.Events) > 0}, {"pages", len(p.Manifest.Pages) > 0}, {"tasks", len(p.Manifest.Tasks) > 0}, {"frontend", p.Frontend != ""}, {"database", len(p.Migrations) > 0}} {
		if err := need(check.name, check.used); err != nil {
			return err
		}
	}
	if p.Frontend != "" && !safePath(p.Frontend) {
		return errors.New("invalid frontend path")
	}
	for platform, file := range p.Executables {
		if !strings.Contains(platform, "/") || !safePath(file) {
			return errors.New("invalid executable")
		}
	}
	if p.Executables[runtime.GOOS+"/"+runtime.GOARCH] == "" {
		return errors.New("package does not support this platform")
	}
	for i, m := range p.Migrations {
		if m.Version != i+1 || len(m.MySQL) == 0 || len(m.Postgres) == 0 {
			return errors.New("migrations must be consecutive and support both databases")
		}
	}
	if p.Manifest.MigrationVersion != len(p.Migrations) {
		return errors.New("migration version mismatch")
	}
	return nil
}
func safePath(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../") && !strings.Contains(name, ":") && strings.IndexByte(name, 92) < 0 && strings.IndexFunc(name, func(c rune) bool { return c < 32 }) < 0
}
func Inspect(data []byte) (Package, error) {
	var p Package
	if len(data) > MaxPackage {
		return p, errors.New("package exceeds 128 MiB")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return p, err
	}
	if len(z.File) > 256 {
		return p, errors.New("too many package files")
	}
	total := uint64(0)
	seen := map[string]bool{}
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !safePath(name) || seen[name] || f.Mode()&os.ModeSymlink != 0 {
			return p, errors.New("unsafe archive entry")
		}
		seen[name] = true
		total += f.UncompressedSize64
		if total > MaxPackage {
			return p, errors.New("expanded package exceeds limit")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.Mode()&os.ModeType != 0 {
			return p, errors.New("unsupported archive entry")
		}
		if f.Name == "plugin.json" {
			r, e := f.Open()
			if e != nil {
				return p, e
			}
			b, e := io.ReadAll(io.LimitReader(r, 1<<20))
			r.Close()
			if e != nil {
				return p, e
			}
			if e = json.Unmarshal(b, &p); e != nil {
				return p, e
			}
		}
	}
	if err = p.Validate(); err != nil {
		return p, err
	}
	if !seen[p.Executables[runtime.GOOS+"/"+runtime.GOARCH]] || (p.Frontend != "" && !seen[p.Frontend]) {
		return p, errors.New("package entry file missing")
	}
	return p, nil
}
func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
