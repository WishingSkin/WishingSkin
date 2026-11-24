package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountQuotaAndAdminOverviewBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			f.app.cfg.TextureAccountLimitBytes = 64 << 20
			f.app.cfg.TextureSiteLimitBytes = 512 << 20

			webStatus(t, f.call(0, "GET", "/api/account/quota", nil), 401)
			webStatus(t, f.call(1, "GET", "/api/admin/overview", nil), 403)

			w := f.call(1, "GET", "/api/account/quota", nil)
			webStatus(t, w, 200)
			var initial struct {
				AccountUsedBytes  int64 `json:"accountUsedBytes"`
				AccountLimitBytes int64 `json:"accountLimitBytes"`
				SiteUsedBytes     int64 `json:"siteUsedBytes"`
				SiteLimitBytes    int64 `json:"siteLimitBytes"`
				MaxFileBytes      int64 `json:"maxFileBytes"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil {
				t.Fatal(err)
			}
			if initial.AccountUsedBytes != 0 || initial.AccountLimitBytes != 64<<20 || initial.SiteUsedBytes != 0 || initial.SiteLimitBytes != 512<<20 || initial.MaxFileBytes != maxTextureBytes {
				t.Fatalf("unexpected initial quota: %+v", initial)
			}

			pngBytes := testPNG(t, 64, 64)
			uploadedRecord(t, f.upload(1, pngBytes, map[string]string{"name": "QuotaSkin", "isPublic": "true"}))

			w = f.call(1, "GET", "/api/account/quota", nil)
			webStatus(t, w, 200)
			var afterUpload struct {
				AccountUsedBytes int64 `json:"accountUsedBytes"`
				SiteUsedBytes    int64 `json:"siteUsedBytes"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &afterUpload); err != nil {
				t.Fatal(err)
			}
			if afterUpload.AccountUsedBytes != int64(len(pngBytes)) || afterUpload.SiteUsedBytes != int64(len(pngBytes)) {
				t.Fatalf("quota usage mismatch: got %+v, want %d", afterUpload, len(pngBytes))
			}

			// Other user should have 0 account usage while seeing site usage.
			w = f.call(2, "GET", "/api/account/quota", nil)
			webStatus(t, w, 200)
			var otherUser struct {
				AccountUsedBytes int64 `json:"accountUsedBytes"`
				SiteUsedBytes    int64 `json:"siteUsedBytes"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &otherUser); err != nil {
				t.Fatal(err)
			}
			if otherUser.AccountUsedBytes != 0 || otherUser.SiteUsedBytes != int64(len(pngBytes)) {
				t.Fatalf("other user quota mismatch: got %+v", otherUser)
			}

			// Admin overview reflects users, textures, storage, plugins, and unconfigured backup cleanly.
			ovResp := f.call(3, "GET", "/api/admin/overview", nil)
			webStatus(t, ovResp, 200)
			var overview struct {
				Users          int64 `json:"users"`
				Characters     int64 `json:"characters"`
				Textures       int64 `json:"textures"`
				PendingReviews int64 `json:"pendingReviews"`
				PendingReports int64 `json:"pendingReports"`
				Storage        struct {
					SiteUsedBytes     int64 `json:"siteUsedBytes"`
					SiteLimitBytes    int64 `json:"siteLimitBytes"`
					AccountLimitBytes int64 `json:"accountLimitBytes"`
				} `json:"storage"`
				Plugins struct {
					Enabled int `json:"enabled"`
					Total   int `json:"total"`
				} `json:"plugins"`
				Backup struct {
					Configured bool `json:"configured"`
					Count      int  `json:"count"`
				} `json:"backup"`
			}
			if err := json.Unmarshal(ovResp.Body.Bytes(), &overview); err != nil {
				t.Fatal(err)
			}
			if overview.Users < 3 || overview.Textures != 1 || overview.PendingReviews != 0 || overview.PendingReports != 0 {
				t.Fatalf("unexpected overview counts: %+v", overview)
			}
			if overview.Storage.SiteUsedBytes != int64(len(pngBytes)) || overview.Storage.SiteLimitBytes != 512<<20 || overview.Storage.AccountLimitBytes != 64<<20 {
				t.Fatalf("unexpected overview storage: %+v", overview.Storage)
			}
			if overview.Plugins.Total == 0 || overview.Backup.Configured {
				t.Fatalf("unexpected overview plugins/backup: %+v %+v", overview.Plugins, overview.Backup)
			}

			// Admin operations exposes current quota and supports runtime overrides.
			opsResp := f.call(3, "GET", "/api/admin/operations", nil)
			webStatus(t, opsResp, 200)
			var ops struct {
				Backup backupSchedule   `json:"backup"`
				CDN    cdnSettings      `json:"cdn"`
				Quota  storageQuotaView `json:"quota"`
			}
			if err := json.Unmarshal(opsResp.Body.Bytes(), &ops); err != nil {
				t.Fatal(err)
			}
			if ops.Quota.AccountLimitBytes != 64<<20 || ops.Quota.SiteLimitBytes != 512<<20 {
				t.Fatalf("unexpected operations quota: %+v", ops.Quota)
			}

			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", map[string]any{
				"backup": ops.Backup,
				"cdn":    ops.CDN,
				"quota":  storageQuotaView{AccountLimitBytes: -1, SiteLimitBytes: 512 << 20},
			}), 400)

			tightLimit := int64(len(pngBytes)) + 10
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", map[string]any{
				"backup": ops.Backup,
				"cdn":    ops.CDN,
				"quota":  storageQuotaView{AccountLimitBytes: tightLimit, SiteLimitBytes: 512 << 20},
			}), 204)

			qResp := f.call(1, "GET", "/api/account/quota", nil)
			webStatus(t, qResp, 200)
			if !strings.Contains(qResp.Body.String(), `"accountLimitBytes":`) {
				t.Fatalf("missing accountLimitBytes in %s", qResp.Body.String())
			}
			var updatedQuota struct {
				AccountLimitBytes int64 `json:"accountLimitBytes"`
				SiteLimitBytes    int64 `json:"siteLimitBytes"`
			}
			if err := json.Unmarshal(qResp.Body.Bytes(), &updatedQuota); err != nil || updatedQuota.AccountLimitBytes != tightLimit || updatedQuota.SiteLimitBytes != 512<<20 {
				t.Fatalf("updated quota not reflected: %+v err=%v", updatedQuota, err)
			}

			secondPNG := testPNG(t, 128, 64)
			blockedUpload := f.upload(1, secondPNG, map[string]string{"name": "OverQuotaSkin"})
			webStatus(t, blockedUpload, 409)
			var blockedErr struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(blockedUpload.Body.Bytes(), &blockedErr); err != nil || blockedErr.Code != "texture_account_capacity" {
				t.Fatalf("expected texture_account_capacity, got %s err=%v", blockedUpload.Body.String(), err)
			}

			// Setting 0 disables both runtime limits immediately.
			webStatus(t, f.call(3, "PATCH", "/api/admin/operations", map[string]any{
				"backup": ops.Backup,
				"cdn":    ops.CDN,
				"quota":  storageQuotaView{AccountLimitBytes: 0, SiteLimitBytes: 0},
			}), 204)
			ovAfterUnlimited := f.call(3, "GET", "/api/admin/overview", nil)
			webStatus(t, ovAfterUnlimited, 200)
			if err := json.Unmarshal(ovAfterUnlimited.Body.Bytes(), &overview); err != nil || overview.Storage.AccountLimitBytes != 0 || overview.Storage.SiteLimitBytes != 0 {
				t.Fatalf("expected 0 unlimited quotas in overview: %+v err=%v", overview.Storage, err)
			}
			uploadedRecord(t, f.upload(1, secondPNG, map[string]string{"name": "UnlimitedNowSkin"}))
		})
	}
}
