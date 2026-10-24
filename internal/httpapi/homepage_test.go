package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHomepageSettingsBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			var initial struct {
				Homepage homepageSettings `json:"homepage"`
			}
			webStatus(t, f.call(0, "GET", "/api/site", nil), 200)
			w := f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil {
				t.Fatal(err)
			}
			if len(initial.Homepage.Blocks) != 3 || initial.Homepage.Blocks[0].ID != "intro" {
				t.Fatalf("default homepage: %+v", initial.Homepage)
			}

			body := homepageSettings{Blocks: []homepageBlock{
				{ID: "intro", Kind: "intro", Enabled: true, Title: "欢迎", Body: "安全内容", ButtonLabel: "进入", ButtonHref: "/login"},
				{ID: "cards", Kind: "features", Enabled: true, Title: "功能", Features: []homepageFeature{{Title: "公开库", Body: "浏览材质", Href: "/library"}}},
			}}
			webStatus(t, f.call(0, "PATCH", "/api/admin/homepage", body), 401)
			webStatus(t, f.call(1, "PATCH", "/api/admin/homepage", body), 403)
			for _, invalid := range []homepageSettings{
				{Blocks: []homepageBlock{{ID: "same", Kind: "cta"}, {ID: "same", Kind: "notice"}}},
				{Blocks: []homepageBlock{{ID: "bad", Kind: "cta", ButtonHref: "javascript:alert(1)"}}},
				{Blocks: []homepageBlock{{ID: "bad", Kind: "cta", ButtonHref: "http://example.test"}}},
				{Blocks: []homepageBlock{{ID: "bad", Kind: "cta", Title: strings.Repeat("字", 81)}}},
			} {
				webStatus(t, f.call(3, "PATCH", "/api/admin/homepage", invalid), 400)
			}

			w = f.call(3, "PATCH", "/api/admin/homepage", body)
			webStatus(t, w, 200)
			var saved homepageSettings
			if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || len(saved.Blocks) != 2 || saved.Blocks[1].Features[0].Href != "/library" {
				t.Fatalf("saved homepage: %v %+v", err, saved)
			}
			w = f.call(0, "GET", "/api/site", nil)
			webStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil || initial.Homepage.Blocks[0].Title != "欢迎" {
				t.Fatalf("roundtrip homepage: %v %+v", err, initial.Homepage)
			}
			var action, target string
			if err := f.db.DB.QueryRow("SELECT action,target_id FROM admin_events WHERE action='settings.homepage'").Scan(&action, &target); err != nil || target != "homepage" {
				t.Fatalf("homepage audit: %v %s %s", err, action, target)
			}
		})
	}
}
