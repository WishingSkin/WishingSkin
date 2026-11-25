package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	rp "github.com/WishingSkin/WishingSkin/internal/runtimeplugin"
	"github.com/WishingSkin/WishingSkin/internal/testutil"
	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

func v2TestPNG(t *testing.T, width, height int, shade uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: shade, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func compileV2HarnessBinary(t *testing.T) []byte {
	t.Helper()
	srcDir := t.TempDir()
	code := `package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	sdk "github.com/WishingSkin/WishingSkin/pkg/plugin"
)

func makePNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 64, B: 32, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func callHost(method, path string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, os.Getenv("WS_HOST_API_URL")+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("WS_HOST_API_TOKEN"))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

func main() {
	_ = sdk.ServeRuntimeV2(func(_ context.Context, req sdk.RuntimeRequest) (any, error) {
		switch req.Kind {
		case "hook":
			if req.Hook == nil {
				return sdk.RuntimeResponse{Status: 400, Body: json.RawMessage("{}")}, nil
			}
			rejectWord, _ := req.Settings["reject_word"].(string)
			if rejectWord == "" {
				rejectWord = "BadWord"
			}
			if req.Hook.Name == "text.check" {
				for _, v := range req.Hook.Fields {
					if strings.Contains(v, rejectWord) {
						b, _ := json.Marshal(sdk.HookDecision{Decision: "reject", Reason: "包含违规词"})
						return sdk.RuntimeResponse{Status: 200, Body: b}, nil
					}
					if strings.Contains(v, "reviewword") {
						b, _ := json.Marshal(sdk.HookDecision{Decision: "review", Reason: "触发敏感词复核"})
						return sdk.RuntimeResponse{Status: 200, Body: b}, nil
					}
				}
				return sdk.RuntimeResponse{Status: 200, Body: json.RawMessage("{\"decision\":\"allow\"}")}, nil
			}
			if req.Hook.Name == "texture.upload.check" {
				if req.Hook.PNGBase64 == "" {
					b, _ := json.Marshal(sdk.HookDecision{Decision: "reject", Reason: "missing image bytes"})
					return sdk.RuntimeResponse{Status: 200, Body: b}, nil
				}
				if req.Hook.Fields["name"] == "BadUpload" {
					b, _ := json.Marshal(sdk.HookDecision{Decision: "reject", Reason: "禁止上传此图片"})
					return sdk.RuntimeResponse{Status: 200, Body: b}, nil
				}
				if req.Hook.Fields["name"] == "NeedsReview" {
					b, _ := json.Marshal(sdk.HookDecision{Decision: "review", Reason: "图像需人工复核"})
					return sdk.RuntimeResponse{Status: 200, Body: b}, nil
				}
				return sdk.RuntimeResponse{Status: 200, Body: json.RawMessage("{\"decision\":\"allow\"}")}, nil
			}
			return sdk.RuntimeResponse{Status: 200, Body: json.RawMessage("{\"decision\":\"allow\"}")}, nil
		case "route":
			if req.Path == "/card" {
				if strings.Contains(req.Query, "svg=1") {
					svg := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"16\" height=\"16\"></svg>")
					return sdk.RuntimeResponse{
						Status:       200,
						ContentType:  "image/svg+xml",
						BodyBase64:  base64.StdEncoding.EncodeToString(svg),
						CacheControl: "public, max-age=120",
						ETag:         "\"v2-svg\"",
					}, nil
				}
				return sdk.RuntimeResponse{
					Status:       200,
					ContentType:  "image/png",
					BodyBase64:  base64.StdEncoding.EncodeToString(makePNG()),
					CacheControl: "public, max-age=120",
					ETag:         "\"v2-png\"",
				}, nil
			}
			if req.Path == "/host-roundtrip" {
				var input struct {
					CharID      int64  ` + "`json:\"charId\"`" + `
					CharName    string ` + "`json:\"charName\"`" + `
					TextureHash string ` + "`json:\"textureHash\"`" + `
					SpendAmount int64  ` + "`json:\"spendAmount\"`" + `
					RefID       string ` + "`json:\"refId\"`" + `
				}
				_ = json.Unmarshal(req.Body, &input)
				uid := strconv.FormatInt(req.Principal.ID, 10)
				stSite, siteRaw, _ := callHost("GET", "/v1/site", nil)
				stChar, _, _ := callHost("GET", "/v1/characters/by-name?name="+input.CharName, nil)
				stUser, userRaw, _ := callHost("GET", "/v1/users/"+uid+"/summary", nil)
				stList, _, _ := callHost("GET", "/v1/users/"+uid+"/textures", nil)
				stBytes, rawPNG, _ := callHost("GET", "/v1/textures/"+input.TextureHash+"/bytes?userId="+uid, nil)
				pngOK := stBytes == 200 && len(rawPNG) > 8 && rawPNG[0] == 0x89 && rawPNG[1] == 0x50

				createdPNG := makePNG()
				stUpload, uploadRaw, _ := callHost("POST", "/v1/users/"+uid+"/textures", map[string]any{
					"name":      "CreatedByHostAPI",
					"kind":      "skin",
					"model":     "slim",
					"isPublic":  false,
					"pngBase64": base64.StdEncoding.EncodeToString(createdPNG),
				})
				var created struct{ ID string ` + "`json:\"id\"`" + ` }
				_ = json.Unmarshal(uploadRaw, &created)

				stBind, _, _ := callHost("PUT", "/v1/users/"+uid+"/characters/"+strconv.FormatInt(input.CharID, 10)+"/textures/skin", map[string]any{
					"recordId": created.ID,
				})
				stSpend1, spend1Raw, _ := callHost("POST", "/v1/users/"+uid+"/points/spend", map[string]any{
					"amount":      input.SpendAmount,
					"referenceId": input.RefID,
				})
				stSpend2, spend2Raw, _ := callHost("POST", "/v1/users/"+uid+"/points/spend", map[string]any{
					"amount":      input.SpendAmount,
					"referenceId": input.RefID,
				})
				stSpendOver, _, _ := callHost("POST", "/v1/users/"+uid+"/points/spend", map[string]any{
					"amount":      100000,
					"referenceId": input.RefID + "-over",
				})
				out, _ := json.Marshal(map[string]any{
					"stSite":      stSite,
					"site":        json.RawMessage(siteRaw),
					"stChar":      stChar,
					"stUser":      stUser,
					"user":        json.RawMessage(userRaw),
					"stList":      stList,
					"pngOK":       pngOK,
					"stUpload":    stUpload,
					"uploadedId":  created.ID,
					"stBind":      stBind,
					"stSpend1":    stSpend1,
					"spend1":      json.RawMessage(spend1Raw),
					"stSpend2":    stSpend2,
					"spend2":      json.RawMessage(spend2Raw),
					"stSpendOver": stSpendOver,
				})
				return sdk.RuntimeResponse{Status: 200, Body: out}, nil
			}
		}
		return sdk.RuntimeResponse{Status: 200, Body: json.RawMessage("{\"ok\":true}")}, nil
	})
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	goMod := "module example.test/v2harness\ngo 1.26.11\nrequire github.com/WishingSkin/WishingSkin v0.0.0\nreplace github.com/WishingSkin/WishingSkin => " + filepath.ToSlash(repoRoot) + "\n"
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "v2harness")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", exe, ".")
	cmd.Dir = srcDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build v2 harness: %v\n%s", err, out)
	}
	bin, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return bin
}

func buildSignedV2Archive(t *testing.T, pkg rp.Package, files map[string][]byte, priv ed25519.PrivateKey) []byte {
	t.Helper()
	rawManifest, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	all := map[string][]byte{"plugin.json": rawManifest}
	for k, v := range files {
		all[k] = v
	}
	for name, content := range all {
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
	if priv == nil {
		return b.Bytes()
	}
	signed, err := rp.SignArchive(b.Bytes(), priv)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestPluginRuntimeV2SignedOfficialSyncHooksAndHostAPIBothDatabases(t *testing.T) {
	binary := compileV2HarnessBinary(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	binEntry := "bin/harness"
	if runtime.GOOS == "windows" {
		binEntry += ".exe"
	}

	pkg := rp.Package{
		Manifest: sdk.Manifest{
			ID:                "wishingskin.v2harness",
			Name:              "V2 Official Harness",
			Version:           "0.1.0",
			SettingsNamespace: "wishingskin.v2harness",
			Permissions: []string{
				"routes.public", "routes.user",
				"hooks", "hooks.image",
				"user.frontend", "events", "webhooks",
				"host.public.read", "host.users.read",
				"host.textures.read", "host.textures.write",
				"host.characters.read", "host.characters.write",
				"host.points.spend",
			},
			Events: []string{"character.create", "character.rename", "character.delete"},
			Settings: []sdk.SettingField{
				{Key: "reject_word", Label: "Reject Word", Type: "text"},
				{Key: "secret_token", Label: "Secret Token", Type: "secret"},
			},
		},
		Protocol:    2,
		Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: binEntry},
		Hooks:       []string{"text.check", "texture.upload.check"},
		UserFrontends: []sdk.UserFrontend{
			{ID: "workbench", Label: "Tool Bench", Slot: "user.tools", Path: "tool.html"},
			{ID: "skin-op", Label: "Skin Op", Slot: "texture.action", Path: "skin.html", Kinds: []string{"skin"}},
		},
		Routes: []rp.Route{
			{Method: "GET", Path: "/card", Access: "public"},
			{Method: "POST", Path: "/host-roundtrip", Access: "user"},
		},
	}

	unsigned := buildSignedV2Archive(t, pkg, map[string][]byte{
		binEntry:    binary,
		"tool.html": []byte("<h1>Workbench</h1>"),
		"skin.html": []byte("<h1>SkinOp</h1>"),
	}, nil)
	signed := buildSignedV2Archive(t, pkg, map[string][]byte{
		binEntry:    binary,
		"tool.html": []byte("<h1>Workbench</h1>"),
		"skin.html": []byte("<h1>SkinOp</h1>"),
	}, priv)

	// Limited plugin without host.points.spend to assert 403 enforcement.
	limitedBinEntry := binEntry
	limitedPkg := rp.Package{
		Manifest: sdk.Manifest{
			ID:                "community.limited",
			Name:              "Limited Plugin",
			Version:           "1.0.0",
			SettingsNamespace: "community.limited",
			Permissions:       []string{"routes.user"},
		},
		Protocol:    2,
		Executables: map[string]string{runtime.GOOS + "/" + runtime.GOARCH: limitedBinEntry},
		Routes:      []rp.Route{{Method: "POST", Path: "/host-roundtrip", Access: "user"}},
	}
	limitedArchive := buildSignedV2Archive(t, limitedPkg, map[string][]byte{limitedBinEntry: binary}, nil)

	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := testutil.Database(t, dialect)
			f := &managementApp{t: t, db: db}
			f.exec("UPDATE settings SET value='closed' WHERE setting_key='registration_mode'")
			for id, permission := range map[int]int{1: 0, 2: 1, 3: 2} {
				f.exec("INSERT INTO users(id,email,email_key,nickname,password_hash,password_method,password_salt,score,permission,banned) VALUES(?,?,?,?,?,?,?,?,?,?)", id, fmt.Sprintf("%d@example.test", id), fmt.Sprintf("%d@example.test", id), "fixture", "unused", "ARGON2ID", "", 20, permission, false)
				f.exec("INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", tokenHash(fmt.Sprintf("%064d", id)), id, time.Now().Add(time.Hour).Unix())
			}
			var err error
			assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><html><body>SPA</body></html>")}}
			f.app, err = New(db, Config{
				PublicURL:                "http://example.test",
				TextureDir:               t.TempDir(),
				SettingsKey:              bytes.Repeat([]byte{9}, 32),
				OfficialPluginPublicKeys: []ed25519.PublicKey{pub},
				Assets:                   assets,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer f.app.WaitBackground()

			// 1. Official market preset present.
			market := f.call(3, "GET", "/api/admin/plugin-market", nil)
			webStatus(t, market, 200)
			if !strings.Contains(market.Body.String(), "https://wishingskin.github.io/plugins/catalog.json") {
				t.Fatalf("missing officialSource: %s", market.Body.String())
			}

			// 2. Unsigned official package rejected; signed official package inspected & installed.
			webStatus(t, postPluginTestPackage(f, "/api/admin/plugin-packages/inspect", bytes.NewReader(unsigned), rp.Digest(unsigned), nil), 400)
			inspect := postPluginTestPackage(f, "/api/admin/plugin-packages/inspect", bytes.NewReader(signed), rp.Digest(signed), nil)
			webStatus(t, inspect, 200)
			var insp struct {
				Digest         string `json:"digest"`
				SignedOfficial bool   `json:"signedOfficial"`
			}
			if err := json.Unmarshal(inspect.Body.Bytes(), &insp); err != nil || !insp.SignedOfficial || insp.Digest != rp.Digest(signed) {
				t.Fatalf("unexpected inspect response: %s %v", inspect.Body.String(), err)
			}
			webStatus(t, postPluginTestPackage(f, "/api/admin/plugin-packages/install", bytes.NewReader(signed), rp.Digest(signed), pkg.Manifest.Permissions), 200)

			// 3. Official external settings work and hide secrets; admin plugin list exposes external/signedOfficial.
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugin-settings/wishingskin.v2harness", map[string]any{
				"reject_word":  "BlockMe",
				"secret_token": "top-secret-123",
			}), 200)
			getSet := f.call(3, "GET", "/api/admin/plugin-settings/wishingskin.v2harness", nil)
			webStatus(t, getSet, 200)
			if strings.Contains(getSet.Body.String(), "top-secret-123") || !strings.Contains(getSet.Body.String(), `"secret_token":true`) || !strings.Contains(getSet.Body.String(), `"BlockMe"`) {
				t.Fatalf("unexpected settings response: %s", getSet.Body.String())
			}
			adminList := f.call(3, "GET", "/api/admin/plugins", nil)
			webStatus(t, adminList, 200)
			if !strings.Contains(adminList.Body.String(), `"external":true`) || !strings.Contains(adminList.Body.String(), `"signedOfficial":true`) {
				t.Fatalf("admin plugin metadata missing external/signedOfficial: %s", adminList.Body.String())
			}

			// Enable official plugin and points module.
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.v2harness", map[string]bool{"enabled": true}), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.points", map[string]bool{"enabled": true}), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/points", map[string]any{"enabled": true, "dailyReward": 10, "uploadCost": 0, "characterCost": 0}), 204)

			// 4. User frontends and SPA tool routes.
			up := f.call(1, "GET", "/api/user/plugins", nil)
			webStatus(t, up, 200)
			if !strings.Contains(up.Body.String(), "wishingskin.v2harness") || !strings.Contains(up.Body.String(), "workbench") || !strings.Contains(up.Body.String(), "skin-op") {
				t.Fatalf("unexpected user plugins list: %s", up.Body.String())
			}
			uf := f.call(1, "GET", "/api/user/plugin-packages/wishingskin.v2harness/frontend/workbench", nil)
			webStatus(t, uf, 200)
			var ufPayload struct {
				HTML   string     `json:"html"`
				Routes []rp.Route `json:"routes"`
			}
			if err := json.Unmarshal(uf.Body.Bytes(), &ufPayload); err != nil || !strings.Contains(ufPayload.HTML, "img-src data: blob:") {
				t.Fatalf("unexpected user frontend payload: %s %v", uf.Body.String(), err)
			}
			webStatus(t, f.raw(1, "GET", "/user/tools/wishingskin.v2harness/workbench", "", nil), 200)
			webStatus(t, f.raw(1, "GET", "/user/tools/wishingskin.v2harness/missing", "", nil), 404)

			// Register expanded webhook events before rename/delete so both deliveries are captured.
			var mu sync.Mutex
			var seenEvents []string
			hookSink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var ev struct {
					Event sdk.Event `json:"event"`
				}
				_ = json.NewDecoder(r.Body).Decode(&ev)
				mu.Lock()
				seenEvents = append(seenEvents, ev.Event.Name)
				mu.Unlock()
				w.WriteHeader(204)
			}))
			defer hookSink.Close()
			webStatus(t, f.call(3, "POST", "/api/admin/webhooks", webhookSubscription{
				URL:     hookSink.URL,
				Events:  []string{"character.rename", "character.delete", "minecraft.link", "minecraft.unlink"},
				Enabled: true,
			}), 200)

			// 5. Sync text hooks on character create, rename, and profile nickname.
			webStatus(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "BlockMe12"}), 400)
			chResp := f.call(1, "POST", "/api/characters", map[string]string{"name": "ValidHero"})
			c := webCharacter(t, chResp)
			webStatus(t, f.call(1, "PATCH", fmt.Sprintf("/api/characters/%d", c.ID), map[string]string{"name": "BlockMe99"}), 400)
			webStatus(t, f.call(1, "PATCH", fmt.Sprintf("/api/characters/%d", c.ID), map[string]string{"name": "RenamedHero"}), 200)
			webStatus(t, f.call(1, "PATCH", "/api/account/profile", map[string]any{"nickname": "BlockMeUser", "avatarMode": "default"}), 400)

			// 6. Sync texture upload & metadata update hooks (reject and force pending review).
			uploadPNG := func(name string, shade uint8) *httptest.ResponseRecorder {
				var body bytes.Buffer
				mw := multipart.NewWriter(&body)
				_ = mw.WriteField("kind", "skin")
				_ = mw.WriteField("model", "default")
				_ = mw.WriteField("name", name)
				_ = mw.WriteField("isPublic", "false")
				fw, _ := mw.CreateFormFile("file", "skin.png")
				_, _ = fw.Write(v2TestPNG(t, 64, 64, shade))
				_ = mw.Close()
				return f.raw(1, "POST", "/api/textures", mw.FormDataContentType(), body.Bytes())
			}
			webStatus(t, uploadPNG("BadUpload", 10), 400)
			revResp := uploadPNG("NeedsReview", 20)
			webStatus(t, revResp, 201)
			var revRec textureRecord
			_ = json.Unmarshal(revResp.Body.Bytes(), &revRec)
			if revRec.ReviewState != "pending" || revRec.ReviewReason != "图像需人工复核" {
				t.Fatalf("expected hook to force pending review, got %+v", revRec)
			}

			okResp := uploadPNG("CleanSkin", 30)
			webStatus(t, okResp, 201)
			var okRec textureRecord
			_ = json.Unmarshal(okResp.Body.Bytes(), &okRec)
			if okRec.ReviewState != "approved" {
				t.Fatalf("expected approved skin, got %+v", okRec)
			}

			// Patching metadata with rejectWord / reviewword triggers sync text hook.
			webStatus(t, f.call(1, "PATCH", "/api/textures/"+okRec.ID, map[string]any{"name": "BlockMeTexture", "description": "", "isPublic": false}), 400)
			webStatus(t, f.call(1, "PATCH", "/api/textures/"+okRec.ID, map[string]any{"name": "Clean reviewword", "description": "", "isPublic": false}), 204)
			var updatedState, updatedReason string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT review_state,review_reason FROM texture_metadata WHERE record_id=?"), okRec.ID).Scan(&updatedState, &updatedReason); err != nil || updatedState != "pending" || updatedReason != "触发敏感词复核" {
				t.Fatalf("expected patch hook to downgrade to pending, got state=%q reason=%q err=%v", updatedState, updatedReason, err)
			}
			// Re-approve for host bind test.
			f.exec("UPDATE texture_metadata SET review_state='approved',review_reason='' WHERE record_id=?", okRec.ID)

			// 7. Binary PNG and sandboxed SVG responses with Cache-Control and ETag.
			cardPNG := f.raw(0, "GET", "/api/plugins/wishingskin.v2harness/card", "", nil)
			webStatus(t, cardPNG, 200)
			if cardPNG.Header().Get("Content-Type") != "image/png" || cardPNG.Header().Get("X-Content-Type-Options") != "nosniff" || cardPNG.Header().Get("Cache-Control") != "public, max-age=120" || cardPNG.Header().Get("ETag") != `"v2-png"` {
				t.Fatalf("unexpected PNG headers: %v", cardPNG.Header())
			}
			if !bytes.HasPrefix(cardPNG.Body.Bytes(), []byte{0x89, 0x50, 0x4e, 0x47}) {
				t.Fatal("invalid PNG magic bytes from plugin route")
			}
			cardSVG := f.raw(0, "GET", "/api/plugins/wishingskin.v2harness/card?svg=1", "", nil)
			webStatus(t, cardSVG, 200)
			if cardSVG.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(cardSVG.Header().Get("Content-Security-Policy"), "sandbox") {
				t.Fatalf("unexpected SVG headers: %v", cardSVG.Header())
			}

			// 8. Host API roundtrip (site, character lookup, user summary, textures list/bytes/write, character bind, idempotent point spend, 409 insufficient).
			rtResp := f.call(1, "POST", "/api/plugins/wishingskin.v2harness/host-roundtrip", map[string]any{
				"charId":      c.ID,
				"charName":    "RenamedHero",
				"textureHash": okRec.FileHash,
				"spendAmount": 5,
				"refId":       "skin-overlay-order-1",
			})
			webStatus(t, rtResp, 200)
			var rt struct {
				StSite      int  `json:"stSite"`
				StChar      int  `json:"stChar"`
				StUser      int  `json:"stUser"`
				StList      int  `json:"stList"`
				PNGOK       bool `json:"pngOK"`
				StUpload    int  `json:"stUpload"`
				UploadedID  string `json:"uploadedId"`
				StBind      int  `json:"stBind"`
				StSpend1    int  `json:"stSpend1"`
				Spend1      struct{ Balance string; Deducted bool } `json:"spend1"`
				StSpend2    int  `json:"stSpend2"`
				Spend2      struct{ Balance string; Deducted bool } `json:"spend2"`
				StSpendOver int  `json:"stSpendOver"`
			}
			if err := json.Unmarshal(rtResp.Body.Bytes(), &rt); err != nil {
				t.Fatal(err)
			}
			if rt.StSite != 200 || rt.StChar != 200 || rt.StUser != 200 || rt.StList != 200 || !rt.PNGOK ||
				rt.StUpload != 201 || rt.UploadedID == "" || rt.StBind != 204 ||
				rt.StSpend1 != 200 || rt.Spend1.Balance != "15" || !rt.Spend1.Deducted ||
				rt.StSpend2 != 200 || rt.Spend2.Balance != "15" || rt.Spend2.Deducted ||
				rt.StSpendOver != 409 {
				t.Fatalf("unexpected Host API roundtrip outcome: %+v raw=%s", rt, rtResp.Body.String())
			}

			// Unauthorized direct call to loopback Host API must return 401.
			unauthReq, _ := http.NewRequest("GET", f.app.hostAPIURL+"/v1/users/1/summary", nil)
			unauthReq.Header.Set("Authorization", "Bearer "+strings.Repeat("0", 64))
			unauthResp, err := http.DefaultClient.Do(unauthReq)
			if err != nil {
				t.Fatal(err)
			}
			unauthResp.Body.Close()
			if unauthResp.StatusCode != 401 {
				t.Fatalf("expected 401 for bad Host API bearer, got %d", unauthResp.StatusCode)
			}

			// Limited plugin lacking Host API grants must receive 403 on Host API endpoints.
			webStatus(t, postPluginTestPackage(f, "/api/admin/plugin-packages/install", bytes.NewReader(limitedArchive), rp.Digest(limitedArchive), limitedPkg.Manifest.Permissions), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/community.limited", map[string]bool{"enabled": true}), 200)
			limResp := f.call(1, "POST", "/api/plugins/community.limited/host-roundtrip", map[string]any{
				"charId":      c.ID,
				"charName":    "RenamedHero",
				"textureHash": okRec.FileHash,
				"spendAmount": 1,
				"refId":       "denied-op",
			})
			webStatus(t, limResp, 200)
			var lim struct {
				StSite   int `json:"stSite"`
				StSpend1 int `json:"stSpend1"`
			}
			_ = json.Unmarshal(limResp.Body.Bytes(), &lim)
			if lim.StSite != 403 || lim.StSpend1 != 403 {
				t.Fatalf("expected 403 when plugin lacks Host API grants, got %+v", lim)
			}

			// 9. Verify expanded Webhook deliveries for character.rename & character.delete.
			webStatus(t, f.call(1, "DELETE", fmt.Sprintf("/api/characters/%d", c.ID), nil), 204)
			f.app.runExtensionDeliveries(context.Background(), time.Now())
			mu.Lock()
			gotDelete := strings.Join(seenEvents, ",")
			mu.Unlock()
			if !strings.Contains(gotDelete, "character.rename") || !strings.Contains(gotDelete, "character.delete") {
				t.Fatalf("expected character.rename & character.delete webhook events, got %q", gotDelete)
			}
		})
	}
}
