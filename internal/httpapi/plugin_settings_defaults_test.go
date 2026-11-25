package httpapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"testing"

	rp "github.com/WishingSkin/WishingSkin/internal/runtimeplugin"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

type settingsDefaultsFixture struct{}

func (settingsDefaultsFixture) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:                "community.defaults",
		Name:              "Defaults Fixture",
		Version:           "1.0.0",
		SettingsNamespace: "community.defaults",
		Permissions:       []string{"user"},
		Settings: []sdk.SettingField{
			{Key: "daily_limit", Label: "Daily Limit", Type: "number", Default: 5, Description: "Max per day"},
			{Key: "allow_demo", Label: "Allow Demo", Type: "boolean", Default: true},
			{
				Key:     "provider",
				Label:   "Provider",
				Type:    "text",
				Default: "auto",
				Options: []sdk.SettingOption{{Value: "auto", Label: "Auto"}, {Value: "local", Label: "Local"}},
			},
			{Key: "api_key", Label: "API Key", Type: "secret"},
		},
	}
}

func (settingsDefaultsFixture) Register(h sdk.Host) error {
	return h.Handle(sdk.Route{
		Method: "GET",
		Path:   "/effective",
		Access: "user",
		Handler: func(w http.ResponseWriter, r *http.Request, _ sdk.Principal) {
			var settings map[string]any
			if err := h.Settings(r.Context(), &settings); err != nil {
				http.Error(w, "settings unavailable", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(settings)
		},
	})
}

func TestPluginInspectErrorMessageSplit(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantSub   string
		notSub    string
	}{
		{"official signature required", rp.ErrOfficialSignatureRequired, "签名", "平台"},
		{"invalid signature", rp.ErrInvalidSignature, "签名", "平台"},
		{"unsupported platform", rp.ErrUnsupportedPlatform, "平台", "签名"},
		{"unsupported protocol", rp.ErrUnsupportedProtocol, "协议", "签名"},
		{"checksum mismatch", errMarketChecksumMismatch, "校验", "签名"},
		{"generic manifest", errors.New("manifest bad"), "清单", "签名"},
	}
	for _, tc := range cases {
		msg := pluginInspectErrorMessage(tc.err)
		if strings.Contains(msg, "·") {
			t.Fatalf("%s message must not contain middle dot: %q", tc.name, msg)
		}
		if !strings.Contains(msg, tc.wantSub) {
			t.Fatalf("%s message %q missing expected %q", tc.name, msg, tc.wantSub)
		}
		if tc.notSub != "" && strings.Contains(msg, tc.notSub) {
			t.Fatalf("%s message %q must not conflate %q", tc.name, msg, tc.notSub)
		}
	}
}

func TestMergePluginSettingDefaultsPreservesExplicitZero(t *testing.T) {
	manifest := (settingsDefaultsFixture{}).Manifest()

	// Empty stored settings should receive all non-secret defaults.
	eff := mergePluginSettingDefaults(manifest, nil)
	if eff["daily_limit"] != 5 || eff["allow_demo"] != true || eff["provider"] != "auto" {
		t.Fatalf("expected defaults for unset keys, got %#v", eff)
	}
	if _, hasSecret := eff["api_key"]; hasSecret {
		t.Fatalf("secret key must not receive a default, got %#v", eff)
	}

	// Explicit zero and false values stored in DB must be preserved verbatim.
	explicit := map[string]any{
		"daily_limit": float64(0),
		"allow_demo":  false,
	}
	eff2 := mergePluginSettingDefaults(manifest, explicit)
	if eff2["daily_limit"] != float64(0) || eff2["allow_demo"] != false || eff2["provider"] != "auto" {
		t.Fatalf("explicit 0/false overridden by default: %#v", eff2)
	}
}

func TestPluginSettingsDefaultsAndInspectErrorsBothDatabases(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	binEntry := "bin/demo"
	if runtime.GOOS == "windows" {
		binEntry += ".exe"
	}

	unsignedOfficial := buildSignedV2Archive(t, rp.Package{
		Manifest: sdk.Manifest{
			ID:                "wishingskin.unsigned-test",
			Name:              "Unsigned",
			Version:           "0.1.0",
			SettingsNamespace: "wishingskin.unsigned-test",
		},
		Protocol:    2,
		Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: binEntry},
	}, map[string][]byte{binEntry: []byte("stub")}, nil)

	wrongPlatform := buildSignedV2Archive(t, rp.Package{
		Manifest: sdk.Manifest{
			ID:                "community.wrongplat",
			Name:              "Wrong Plat",
			Version:           "0.1.0",
			SettingsNamespace: "community.wrongplat",
		},
		Protocol:    2,
		Executables: map[string]string{"plan9/mips64": binEntry},
	}, map[string][]byte{binEntry: []byte("stub")}, nil)

	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			cfg := f.app.cfg
			cfg.CommunityPlugins = []sdk.Plugin{settingsDefaultsFixture{}}
			cfg.OfficialPluginPublicKeys = []ed25519.PublicKey{pub}
			app, err := New(f.db, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer app.dynamic.Close()
			f.app = app

			// Unsigned official package reports signature error, not platform error.
			respSig := postPluginTestPackage(f, "/api/admin/plugin-packages/inspect", bytes.NewReader(unsignedOfficial), rp.Digest(unsignedOfficial), nil)
			webStatus(t, respSig, 400)
			if !strings.Contains(respSig.Body.String(), "签名") || strings.Contains(respSig.Body.String(), "平台") {
				t.Fatalf("unexpected unsigned official inspect error: %s", respSig.Body.String())
			}

			// Unsupported platform reports platform error, not signature error.
			respPlat := postPluginTestPackage(f, "/api/admin/plugin-packages/inspect", bytes.NewReader(wrongPlatform), rp.Digest(wrongPlatform), nil)
			webStatus(t, respPlat, 400)
			if !strings.Contains(respPlat.Body.String(), "平台") || strings.Contains(respPlat.Body.String(), "签名") {
				t.Fatalf("unexpected wrong platform inspect error: %s", respPlat.Body.String())
			}

			// Before saving settings, GET settings and plugin runtime receive manifest defaults.
			getDefault := f.call(3, "GET", "/api/admin/plugin-settings/community.defaults", nil)
			webStatus(t, getDefault, 200)
			var adminSettings struct {
				Values     map[string]any  `json:"values"`
				Configured map[string]bool `json:"configured"`
			}
			if err := json.Unmarshal(getDefault.Body.Bytes(), &adminSettings); err != nil {
				t.Fatal(err)
			}
			if adminSettings.Values["daily_limit"] != float64(5) || adminSettings.Values["allow_demo"] != true || adminSettings.Values["provider"] != "auto" || adminSettings.Configured["api_key"] {
				t.Fatalf("expected GET settings defaults, got %#v", adminSettings)
			}

			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.defaults", map[string]bool{"enabled": true}), 200)
			effResp := f.call(1, "GET", "/api/plugins/community.defaults/effective", nil)
			webStatus(t, effResp, 200)
			var runtimeSettings map[string]any
			if err := json.Unmarshal(effResp.Body.Bytes(), &runtimeSettings); err != nil {
				t.Fatal(err)
			}
			if runtimeSettings["daily_limit"] != float64(5) || runtimeSettings["allow_demo"] != true || runtimeSettings["provider"] != "auto" {
				t.Fatalf("expected runtime settings defaults, got %#v", runtimeSettings)
			}

			// Reject invalid option values, then persist explicit 0 / false overrides.
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugin-settings/community.defaults", map[string]any{"provider": "invalid"}), 400)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugin-settings/community.defaults", map[string]any{
				"daily_limit": 0,
				"allow_demo":  false,
				"provider":    "local",
			}), 200)

			afterResp := f.call(1, "GET", "/api/plugins/community.defaults/effective", nil)
			webStatus(t, afterResp, 200)
			var afterSettings map[string]any
			if err := json.Unmarshal(afterResp.Body.Bytes(), &afterSettings); err != nil {
				t.Fatal(err)
			}
			if afterSettings["daily_limit"] != float64(0) || afterSettings["allow_demo"] != false || afterSettings["provider"] != "local" {
				t.Fatalf("explicit 0/false/option override lost: %#v", afterSettings)
			}
		})
	}
}
