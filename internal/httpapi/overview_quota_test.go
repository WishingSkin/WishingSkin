package httpapi

import (
	"encoding/json"
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
		})
	}
}
