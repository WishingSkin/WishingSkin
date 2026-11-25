package runtimeplugin_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	rp "github.com/WishingSkin/WishingSkin/internal/runtimeplugin"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

func buildTestArchive(t *testing.T, pkg rp.Package, files map[string][]byte) []byte {
	t.Helper()
	rawManifest, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	allFiles := map[string][]byte{"plugin.json": rawManifest}
	for k, v := range files {
		allFiles[k] = v
	}
	for name, content := range allFiles {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestParsePublicKeysAndArchiveSigning(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)

	raw := hex.EncodeToString(pub) + ", " + base64.StdEncoding.EncodeToString(pub2)
	keys, err := rp.ParsePublicKeys(raw)
	if err != nil || len(keys) != 2 || !bytes.Equal(keys[0], pub) || !bytes.Equal(keys[1], pub2) {
		t.Fatalf("ParsePublicKeys failed: %v %#v", err, keys)
	}
	if _, err := rp.ParsePublicKeys("not-a-key"); err == nil {
		t.Fatal("accepted invalid public key")
	}

	pkg := rp.Package{
		Manifest: sdk.Manifest{
			ID:                "wishingskin.official-demo",
			Name:              "Official Demo",
			Version:           "0.1.0",
			SettingsNamespace: "wishingskin.official-demo",
			Permissions:       []string{"routes.public", "user.frontend", "hooks"},
		},
		Protocol:    2,
		Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: "bin/demo"},
		Routes:      []rp.Route{{Method: "GET", Path: "/ping", Access: "public"}},
		Hooks:       []string{"text.check"},
		UserFrontends: []sdk.UserFrontend{{
			ID:    "tool",
			Label: "Tool",
			Slot:  "user.tools",
			Path:  "user.html",
		}},
	}
	unsigned := buildTestArchive(t, pkg, map[string][]byte{
		"bin/demo":  []byte("fake-binary"),
		"user.html": []byte("<h1>Hi</h1>"),
	})

	// Unsigned wishingskin.* package must be rejected.
	if _, _, err := rp.InspectWithKeys(unsigned, keys); err == nil {
		t.Fatal("expected unsigned wishingskin.* package to fail")
	}

	signed, err := rp.SignArchive(unsigned, priv)
	if err != nil {
		t.Fatalf("SignArchive failed: %v", err)
	}
	parsed, isOfficial, err := rp.InspectWithKeys(signed, keys)
	if err != nil || !isOfficial || parsed.Manifest.ID != "wishingskin.official-demo" {
		t.Fatalf("expected signed official package to succeed, got official=%v err=%v", isOfficial, err)
	}

	// Wrong public keys must reject.
	if _, _, err := rp.InspectWithKeys(signed, []ed25519.PublicKey{pub2}); err == nil {
		t.Fatal("expected wrong public key to fail signature verification")
	}

	// Tampered content must invalidate signature.
	var tamperedBuf bytes.Buffer
	zr, _ := zip.NewReader(bytes.NewReader(signed), int64(len(signed)))
	zw := zip.NewWriter(&tamperedBuf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		var data bytes.Buffer
		_, _ = data.ReadFrom(rc)
		rc.Close()
		w, _ := zw.Create(f.Name)
		if f.Name == "user.html" {
			_, _ = w.Write([]byte("<h1>Tampered</h1>"))
		} else {
			_, _ = w.Write(data.Bytes())
		}
	}
	_ = zw.Close()
	if _, _, err := rp.InspectWithKeys(tamperedBuf.Bytes(), keys); err == nil {
		t.Fatal("expected tampered signed archive to fail")
	}
}

func TestV2ManifestValidationRules(t *testing.T) {
	base := rp.Package{
		Manifest: sdk.Manifest{
			ID:                "community.v2demo",
			Version:           "1.0.0",
			SettingsNamespace: "community.v2demo",
			Permissions:       []string{"user.frontend", "hooks"},
		},
		Protocol:    2,
		Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: "bin/p"},
	}

	// V1 package must reject v2 permissions.
	v1bad := base
	v1bad.Protocol = 1
	archive := buildTestArchive(t, v1bad, map[string][]byte{"bin/p": []byte("x")})
	if _, _, err := rp.InspectWithKeys(archive, nil); err == nil {
		t.Fatal("v1 protocol accepted v2 permissions")
	}

	// Invalid user frontend slot must fail.
	badSlot := base
	badSlot.UserFrontends = []sdk.UserFrontend{{ID: "t", Label: "T", Slot: "bad.slot", Path: "t.html"}}
	archive = buildTestArchive(t, badSlot, map[string][]byte{"bin/p": []byte("x"), "t.html": []byte("hi")})
	if _, _, err := rp.InspectWithKeys(archive, nil); err == nil {
		t.Fatal("accepted invalid user frontend slot")
	}

	// Kinds only valid on texture.action.
	badKinds := base
	badKinds.UserFrontends = []sdk.UserFrontend{{ID: "t", Label: "T", Slot: "user.tools", Path: "t.html", Kinds: []string{"skin"}}}
	archive = buildTestArchive(t, badKinds, map[string][]byte{"bin/p": []byte("x"), "t.html": []byte("hi")})
	if _, _, err := rp.InspectWithKeys(archive, nil); err == nil {
		t.Fatal("accepted kinds on user.tools slot")
	}

	// Invalid hook name must fail.
	badHook := base
	badHook.Hooks = []string{"unknown.hook"}
	archive = buildTestArchive(t, badHook, map[string][]byte{"bin/p": []byte("x")})
	if _, _, err := rp.InspectWithKeys(archive, nil); err == nil {
		t.Fatal("accepted unknown hook name")
	}
}

func TestV2RuntimeProbeAndHostEnv(t *testing.T) {
	srcDir := t.TempDir()
	prog := `package main
import (
	"context"
	"encoding/json"
	"os"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)
func main() {
	_ = sdk.ServeRuntimeV2(func(_ context.Context, req sdk.RuntimeRequest) (any, error) {
		body, _ := json.Marshal(map[string]string{
			"proto": os.Getenv("WS_PLUGIN_PROTOCOL"),
			"url":   os.Getenv("WS_HOST_API_URL"),
			"token": os.Getenv("WS_HOST_API_TOKEN"),
		})
		return sdk.RuntimeResponse{Status: 200, Body: body}, nil
	})
}`
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	goMod := "module example.test/v2fixture\ngo 1.26.11\nrequire github.com/WishingSkin/WishingSkin v0.0.0\nreplace github.com/WishingSkin/WishingSkin => " + filepath.ToSlash(repoRoot) + "\n"
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(t.TempDir(), "v2plugin")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", exe, ".")
	cmd.Dir = srcDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build v2 fixture: %v\n%s", err, out)
	}
	binBytes, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	binEntry := "bin/plugin"
	if runtime.GOOS == "windows" {
		binEntry += ".exe"
	}
	pkg := rp.Package{
		Manifest: sdk.Manifest{
			ID:                "wishingskin.v2probe",
			Name:              "V2 Probe",
			Version:           "0.1.0",
			SettingsNamespace: "wishingskin.v2probe",
			Permissions:       []string{"routes.public"},
		},
		Protocol:    2,
		Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: binEntry},
		Routes:      []rp.Route{{Method: "GET", Path: "/env", Access: "public"}},
	}
	unsigned := buildTestArchive(t, pkg, map[string][]byte{binEntry: binBytes})
	signed, err := rp.SignArchive(unsigned, priv)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	m, err := rp.OpenWithOptions(root, rp.Options{
		OfficialPublicKeys: []ed25519.PublicKey{pub},
		HostAPIURL:         "http://127.0.0.1:45678",
		HostTokenFunc:      func(id string) string { return "tok-" + id },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	item, err := m.Stage(signed, pkg.Manifest.Permissions)
	if err != nil || !item.SignedOfficial {
		t.Fatalf("stage failed: signed=%v err=%v", item.SignedOfficial, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Probe(ctx, item); err != nil {
		t.Fatalf("v2 probe failed: %v", err)
	}
	if err := m.Activate(item); err != nil {
		t.Fatal(err)
	}
	rawResp, err := m.Call(ctx, pkg.Manifest.ID, sdk.RuntimeRequest{Kind: "route", Method: "GET", Path: "/env"})
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	var resp sdk.RuntimeResponse
	if err := json.Unmarshal(rawResp, &resp); err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["proto"] != "2" || payload["url"] != "http://127.0.0.1:45678" || payload["token"] != "tok-wishingskin.v2probe" || strings.TrimSpace(payload["token"]) == "" {
		t.Fatalf("unexpected v2 env: %#v", payload)
	}
}
