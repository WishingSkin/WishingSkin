package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyReportsReadOnlyBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			path := "/api/admin/legacy-reports"
			webStatus(t, f.call(0, "GET", path, nil), 401)
			webStatus(t, f.call(1, "GET", path, nil), 403)
			w := f.call(3, "GET", path, nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"reports":[]`) {
				t.Fatal("empty history", w.Body)
			}
			f.exec("INSERT INTO legacy_reports(id,tid,uploader,reporter,reason,status,report_at) VALUES(9007199254740993,9007199254740995,77777,88888,?,0,'0000-00-00 00:00:00')", "Historical <script>plain text</script>\n"+strings.Repeat("旧", 24000))
			f.exec("INSERT INTO legacy_reports(id,tid,uploader,reporter,reason,status,report_at) VALUES(2,0,-1,0,'resolved',1,NULL),(3,0,0,0,'rejected',2,'2020-01-02 03:04:05')")
			w = f.call(3, "GET", path+"?status=0&pageSize=1", nil)
			webStatus(t, w, 200)
			var data struct {
				Reports []struct {
					ID, TextureID, UploaderID, ReporterID, Reason string
					Status                                        int
					ReportAt                                      *string
				}
				Total int
			}
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if data.Total != 1 || len(data.Reports) != 1 || data.Reports[0].ID != "9007199254740993" || data.Reports[0].TextureID != "9007199254740995" || data.Reports[0].UploaderID != "77777" || data.Reports[0].ReporterID != "88888" || data.Reports[0].ReportAt == nil || *data.Reports[0].ReportAt != "0000-00-00 00:00:00" || !strings.Contains(data.Reports[0].Reason, strings.Repeat("旧", 24000)) {
				t.Fatal("history projection changed source values")
			}
			for _, forbidden := range []string{"@example.test", "reviewer", "resolvedAt", "resolution", "ownerEmail"} {
				if strings.Contains(w.Body.String(), forbidden) {
					t.Fatal("invented history metadata", forbidden)
				}
			}
			w = f.call(3, "GET", path+"?status=1", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"reportAt":null`) {
				t.Fatal("unknown source time guessed", w.Body)
			}
			w = f.call(3, "GET", path+"?pageSize=1&page=2", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"id":"3"`) || !strings.Contains(w.Body.String(), `"total":3`) {
				t.Fatal("history pagination", w.Body)
			}
			for _, query := range []string{"?status=pending", "?status=3", "?page=0", "?pageSize=61"} {
				webStatus(t, f.call(3, "GET", path+query, nil), 400)
			}
			for _, method := range []string{"POST", "PATCH", "PUT", "DELETE"} {
				webStatus(t, f.call(3, method, path, map[string]string{"status": "2"}), 404)
			}
			webStatus(t, f.call(3, "PATCH", path+"/9007199254740993", map[string]string{"status": "2"}), 404)
			w = f.call(3, "GET", "/api/admin/reports", nil)
			webStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"total":0`) {
				t.Fatal("history mixed into new queue")
			}
			f.exec("ALTER TABLE legacy_reports RENAME COLUMN reason TO unavailable_reason")
			webStatus(t, f.call(3, "GET", path, nil), 503)
			f.exec("ALTER TABLE legacy_reports RENAME COLUMN unavailable_reason TO reason")
		})
	}
}
