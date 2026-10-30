package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestObjectStorageTextureFlowBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			var mu sync.Mutex
			objects := map[string][]byte{}
			fail, corrupt := false, false
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if fail {
					w.WriteHeader(403)
					return
				}
				if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
					t.Error("unsigned S3 request")
					w.WriteHeader(403)
					return
				}
				switch r.Method {
				case "PUT":
					var reader io.Reader = r.Body
					if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
						reader = httputil.NewChunkedReader(reader)
					}
					data, err := io.ReadAll(reader)
					if err != nil {
						t.Error("invalid S3 body")
						w.WriteHeader(400)
						return
					}
					objects[r.URL.Path] = data
					w.Header().Set("ETag", `"fixture"`)
				case "HEAD", "GET":
					data, ok := objects[r.URL.Path]
					if !ok {
						w.WriteHeader(404)
						return
					}
					if corrupt {
						data = bytes.Repeat([]byte{'x'}, len(data))
					}
					w.Header().Set("Content-Length", strconv.Itoa(len(data)))
					w.Header().Set("ETag", `"fixture"`)
					w.Header().Set("Last-Modified", "Sun, 25 Oct 2026 00:00:00 GMT")
					if r.Method == "GET" {
						_, _ = w.Write(data)
					}
				default:
					w.WriteHeader(400)
				}
			}))
			defer remote.Close()
			webStatus(t, f.call(3, "PATCH", "/api/admin/object-storage", map[string]string{"endpoint": remote.URL, "bucket": "test-bucket", "region": "us-east-1", "prefix": "textures", "accessKey": "synthetic-access", "secretKey": "synthetic-secret"}), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.object-storage", map[string]bool{"enabled": true}), 200)
			png := testPNG(t, 64, 64)
			record := uploadedRecord(t, f.upload(1, png, nil))
			hash := record.FileHash
			mu.Lock()
			stored := append([]byte(nil), objects["/test-bucket/textures/"+hash]...)
			mu.Unlock()
			if !bytes.Equal(stored, png) {
				t.Fatal("upload was not synced to S3")
			}
			local := filepath.Join(f.app.cfg.TextureDir, hash)
			if err := os.Remove(local); err != nil {
				t.Fatal(err)
			}
			w := f.call(0, "GET", "/textures/"+hash, nil)
			webStatus(t, w, 200)
			if !bytes.Equal(w.Body.Bytes(), png) {
				t.Fatal("remote read differs")
			}
			if err := saveTexture(f.app.cfg.TextureDir, hash, png); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			corrupt = true
			mu.Unlock()
			w = f.call(0, "GET", "/textures/"+hash, nil)
			webStatus(t, w, 200)
			if !bytes.Equal(w.Body.Bytes(), png) {
				t.Fatal("corrupt remote data did not fall back")
			}
			mu.Lock()
			fail = true
			corrupt = false
			mu.Unlock()
			w = f.call(0, "GET", "/textures/"+hash, nil)
			webStatus(t, w, 200)
			if !bytes.Equal(w.Body.Bytes(), png) {
				t.Fatal("remote outage did not fall back")
			}
			var before, after int
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM texture_records").Scan(&before)
			webStatus(t, f.upload(1, png, nil), 503)
			_ = f.db.DB.QueryRow("SELECT COUNT(*) FROM texture_records").Scan(&after)
			if before != after {
				t.Fatal("failed remote upload committed a record")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.object-storage", map[string]bool{"enabled": false}), 200)
			webStatus(t, f.call(0, "GET", "/textures/"+hash, nil), 200)
		})
	}
}

func TestObjectStorageConfigBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			webStatus(t, f.call(0, "GET", "/api/admin/object-storage", nil), 401)
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.object-storage", map[string]bool{"enabled": true}), 409)
			body := map[string]string{"endpoint": "http://127.0.0.1:9000", "bucket": "test-bucket", "region": "us-east-1", "prefix": "textures", "accessKey": "synthetic-access", "secretKey": "synthetic-storage-secret"}
			webStatus(t, f.call(1, "PATCH", "/api/admin/object-storage", body), 403)
			webStatus(t, f.call(3, "PATCH", "/api/admin/object-storage", body), 200)
			body["accessKey"] = ""
			body["secretKey"] = ""
			webStatus(t, f.call(3, "PATCH", "/api/admin/object-storage", body), 200)
			var raw string
			if err := f.db.DB.QueryRow("SELECT value FROM settings WHERE setting_key='object_storage_config'").Scan(&raw); err != nil || strings.Contains(raw, "synthetic-storage-secret") {
				t.Fatal("plaintext object storage credentials")
			}
			w := f.call(3, "GET", "/api/admin/object-storage", nil)
			webStatus(t, w, 200)
			if strings.Contains(w.Body.String(), "synthetic-") {
				t.Fatal("storage secret leaked")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.object-storage", map[string]bool{"enabled": true}), 200)
			w = f.call(3, "GET", "/api/admin/plugins", nil)
			var catalog struct {
				Plugins []struct {
					ID      string
					Enabled bool
				}
			}
			if json.Unmarshal(w.Body.Bytes(), &catalog) != nil {
				t.Fatal("invalid plugin catalog")
			}
			found := false
			for _, p := range catalog.Plugins {
				if p.ID == "wishingskin.object-storage" && p.Enabled {
					found = true
				}
			}
			if !found {
				t.Fatal("storage plugin switch not saved")
			}
			webStatus(t, f.call(3, "PATCH", "/api/admin/plugins/wishingskin.object-storage", map[string]bool{"enabled": false}), 200)
		})
	}
}
