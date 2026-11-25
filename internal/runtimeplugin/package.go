// Package runtimeplugin loads trusted executable plugins without restarting the host.
package runtimeplugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
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
	"unicode/utf8"

	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

const MaxPackage = 128 << 20
const maxUserFrontendBytes = 2 << 20

var (
	ErrOfficialSignatureRequired = errors.New("official plugin namespace requires valid signature")
	ErrUnsupportedPlatform       = errors.New("package does not support this platform")
	ErrUnsupportedProtocol       = errors.New("unsupported plugin protocol")
)

var v1Permissions = []string{
	"routes.public", "routes.user", "routes.admin",
	"events", "pages", "tasks", "frontend", "database", "webhooks",
}

var Permissions = append(slices.Clone(v1Permissions),
	"hooks", "hooks.image", "user.frontend",
	"host.public.read", "host.textures.read", "host.textures.write",
	"host.characters.read", "host.characters.write",
	"host.users.read", "host.points.read", "host.points.spend",
)

var validHooks = []string{"text.check", "texture.upload.check"}

var routePathPattern = regexp.MustCompile(`^/(?:[A-Za-z0-9._~-]+/)*[A-Za-z0-9._~-]*$`)
var userFrontendIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

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
	Manifest      sdk.Manifest       `json:"manifest"`
	Protocol      int                `json:"protocol"`
	Executables   map[string]string  `json:"executables"`
	Routes        []Route            `json:"routes"`
	Frontend      string             `json:"frontend,omitempty"`
	UserFrontends []sdk.UserFrontend `json:"userFrontends,omitempty"`
	Hooks         []string           `json:"hooks,omitempty"`
	Migrations    []Migration        `json:"migrations,omitempty"`
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
	if strings.HasPrefix(p.Manifest.ID, "wishingskin.") {
		return errors.New("invalid package identity or protocol")
	}
	return p.validateStructure()
}

func (p Package) validateStructure() error {
	if err := p.Manifest.Validate(); err != nil {
		return err
	}
	if p.Protocol != 1 && p.Protocol != 2 {
		return ErrUnsupportedProtocol
	}
	if len(p.Manifest.Version) > 64 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`).MatchString(p.Manifest.Version) {
		return errors.New("invalid package identity or protocol")
	}
	seen := map[string]bool{}
	for _, permission := range p.Manifest.Permissions {
		if !slices.Contains(Permissions, permission) || seen[permission] {
			return errors.New("unknown or duplicate permission")
		}
		if p.Protocol == 1 && !slices.Contains(v1Permissions, permission) {
			return errors.New("permission requires protocol 2")
		}
		seen[permission] = true
	}
	if p.Protocol == 1 && (len(p.Hooks) > 0 || len(p.UserFrontends) > 0) {
		return errors.New("hooks and userFrontends require protocol 2")
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
	}{
		{"events", len(p.Manifest.Events) > 0},
		{"pages", len(p.Manifest.Pages) > 0},
		{"tasks", len(p.Manifest.Tasks) > 0},
		{"frontend", p.Frontend != ""},
		{"database", len(p.Migrations) > 0},
		{"hooks", len(p.Hooks) > 0 || seen["hooks.image"]},
		{"user.frontend", len(p.UserFrontends) > 0},
	} {
		if err := need(check.name, check.used); err != nil {
			return err
		}
	}
	seenHooks := map[string]bool{}
	for _, hook := range p.Hooks {
		if !slices.Contains(validHooks, hook) || seenHooks[hook] {
			return errors.New("invalid or duplicate hook")
		}
		seenHooks[hook] = true
	}
	if len(p.UserFrontends) > 8 {
		return errors.New("too many user frontends")
	}
	seenUF := map[string]bool{}
	for _, uf := range p.UserFrontends {
		if !userFrontendIDPattern.MatchString(uf.ID) || len(uf.ID) > 64 || seenUF[uf.ID] {
			return errors.New("invalid user frontend id")
		}
		seenUF[uf.ID] = true
		label := strings.TrimSpace(uf.Label)
		if label == "" || !utf8.ValidString(uf.Label) || utf8.RuneCountInString(label) > 64 {
			return errors.New("invalid user frontend label")
		}
		if !utf8.ValidString(uf.Description) || utf8.RuneCountInString(uf.Description) > 256 {
			return errors.New("invalid user frontend description")
		}
		if uf.Slot != "user.tools" && uf.Slot != "texture.action" {
			return errors.New("invalid user frontend slot")
		}
		if !safePath(uf.Path) {
			return errors.New("invalid user frontend path")
		}
		if len(uf.Kinds) > 0 {
			if uf.Slot != "texture.action" {
				return errors.New("kinds only allowed for texture.action slot")
			}
			seenKinds := map[string]bool{}
			for _, kind := range uf.Kinds {
				if (kind != "skin" && kind != "cape") || seenKinds[kind] {
					return errors.New("invalid user frontend texture kind")
				}
				seenKinds[kind] = true
			}
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
		return ErrUnsupportedPlatform
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
	pkg, _, err := InspectWithKeys(data, BuiltInOfficialPublicKeys())
	return pkg, err
}

func InspectWithKeys(data []byte, officialKeys []ed25519.PublicKey) (Package, bool, error) {
	var p Package
	if len(data) > MaxPackage {
		return p, false, errors.New("package exceeds 128 MiB")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return p, false, err
	}
	if len(z.File) > 256 {
		return p, false, errors.New("too many package files")
	}
	total := uint64(0)
	seen := map[string]bool{}
	entrySizes := map[string]uint64{}
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !safePath(name) || seen[name] || f.Mode()&os.ModeSymlink != 0 {
			return p, false, errors.New("unsafe archive entry")
		}
		seen[name] = true
		total += f.UncompressedSize64
		if total > MaxPackage {
			return p, false, errors.New("expanded package exceeds limit")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.Mode()&os.ModeType != 0 {
			return p, false, errors.New("unsupported archive entry")
		}
		entrySizes[f.Name] = f.UncompressedSize64
		if f.Name == "plugin.json" {
			r, e := f.Open()
			if e != nil {
				return p, false, e
			}
			b, e := io.ReadAll(io.LimitReader(r, 1<<20))
			r.Close()
			if e != nil {
				return p, false, e
			}
			if e = json.Unmarshal(b, &p); e != nil {
				return p, false, e
			}
		}
	}
	signedOfficial, err := VerifyArchiveSignature(data, officialKeys)
	if err != nil {
		return p, false, err
	}
	if strings.HasPrefix(p.Manifest.ID, "wishingskin.") && !signedOfficial {
		return p, false, ErrOfficialSignatureRequired
	}
	if err = p.validateStructure(); err != nil {
		return p, false, err
	}
	if !seen[p.Executables[runtime.GOOS+"/"+runtime.GOARCH]] || (p.Frontend != "" && !seen[p.Frontend]) {
		return p, false, errors.New("package entry file missing")
	}
	for _, uf := range p.UserFrontends {
		if !seen[uf.Path] || entrySizes[uf.Path] > maxUserFrontendBytes {
			return p, false, errors.New("user frontend file missing or too large")
		}
	}
	return p, signedOfficial, nil
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
