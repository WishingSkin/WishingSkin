package runtimeplugin_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	rp "github.com/WishingSkin/WishingSkin/internal/runtimeplugin"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

func TestRuntimeLifecycle(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "echo")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", exe, "./testdata/echo")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	pack := func(version string) []byte {
		t.Helper()
		p := rp.Package{Manifest: sdk.Manifest{ID: "example.echo", Name: "Echo", SettingsNamespace: "echo", Version: version, Permissions: []string{"routes.admin"}}, Protocol: 1, Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: filepath.Base(exe)}, Routes: []rp.Route{{Method: "POST", Path: "/echo", Access: "admin"}}}
		manifest, _ := json.Marshal(p)
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		for _, entry := range []struct {
			name string
			data []byte
		}{{"plugin.json", manifest}, {filepath.Base(exe), binary}} {
			f, e := z.Create(entry.name)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.Write(entry.data); e != nil {
				t.Fatal(e)
			}
		}
		if e := z.Close(); e != nil {
			t.Fatal(e)
		}
		return b.Bytes()
	}
	root := t.TempDir()
	m, err := rp.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	first := pack("1.0.0")
	if _, err = m.Stage(first, nil); err == nil {
		t.Fatal("installation without permission approval succeeded")
	}
	item, err := m.Stage(first, []string{"routes.admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Probe(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(item); err != nil {
		t.Fatal(err)
	}
	call := func(id string) int {
		t.Helper()
		data, e := m.Call(context.Background(), "example.echo", sdk.RuntimeRequest{Kind: "route", ID: id})
		if e != nil {
			t.Fatal(e)
		}
		var result sdk.RuntimeResponse
		if e = json.Unmarshal(data, &result); e != nil {
			t.Fatal(e)
		}
		var body struct {
			PID int    `json:"pid"`
			ID  string `json:"id"`
		}
		if e = json.Unmarshal(result.Body, &body); e != nil || body.ID != id || body.PID == 0 {
			t.Fatalf("bad response: %s %v", data, e)
		}
		return body.PID
	}
	pid := call("first")
	if call("second") != pid {
		t.Fatal("runtime not reused")
	}
	if err = m.Reload("example.echo"); err != nil {
		t.Fatal(err)
	}
	if call("reloaded") == pid {
		t.Fatal("reload reused old process")
	}
	item, err = m.Stage(pack("2.0.0"), []string{"routes.admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(item); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err = m.Call(ctx, "example.echo", sdk.RuntimeRequest{Kind: "route", ID: "wait"}); err == nil {
		t.Fatal("runtime exceeded deadline")
	}
	call("recovered")
	m.Close()
	m, err = rp.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	saved, ok := m.Get("example.echo")
	if !ok || saved.Package.Manifest.Version != "2.0.0" {
		t.Fatal("upgrade not persisted")
	}
	call("restarted")
	if err = m.Uninstall("example.echo"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Call(context.Background(), "example.echo", sdk.RuntimeRequest{}); err == nil {
		t.Fatal("uninstalled runtime callable")
	}
}
