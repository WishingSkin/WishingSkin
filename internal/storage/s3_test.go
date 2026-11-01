package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestS3SignedPutOpenDelete(t *testing.T) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("request not signed")
			w.WriteHeader(403)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "PUT":
			var body io.Reader = r.Body
			// The SDK signs HTTP payloads using AWS chunk framing. A real S3
			// server removes this framing before persisting the object.
			if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
				body = httputil.NewChunkedReader(body)
			}
			data, err := io.ReadAll(body)
			if err != nil {
				t.Error(err)
			}
			objects[r.URL.Path] = data
			w.Header().Set("ETag", `"fixture"`)
			w.WriteHeader(200)
		case "HEAD", "GET":
			data, ok := objects[r.URL.Path]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("ETag", `"fixture"`)
			w.Header().Set("Last-Modified", "Sun, 25 Oct 2026 00:00:00 GMT")
			if r.Method == "GET" {
				_, _ = w.Write(data)
			}
		case "DELETE":
			delete(objects, r.URL.Path)
			w.WriteHeader(204)
		default:
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	provider, err := NewS3(S3Options{Endpoint: server.URL, Bucket: "test-bucket", Region: "us-east-1", Prefix: "textures", AccessKey: "synthetic-test-access", SecretKey: "synthetic-test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := provider.Put(ctx, "hash", "image/png", bytes.NewReader([]byte("png-fixture"))); err != nil {
		t.Fatal("put failed")
	}
	f, err := provider.Open(ctx, "hash")
	if err != nil {
		t.Fatal("open failed")
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(got) != "png-fixture" {
		t.Fatal("object bytes differ")
	}
	if err := provider.Delete(ctx, "hash"); err != nil {
		t.Fatal("delete failed")
	}
	if _, err := provider.Open(ctx, "hash"); err == nil {
		t.Fatal("deleted object still opens")
	}
}

// Exercise the S3 pagination protocol, prefix confinement and object metadata.
func TestS3InventoryPaginationAndStat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("unsigned inventory")
			w.WriteHeader(403)
			return
		}
		if r.Method == "HEAD" {
			if r.URL.Path != "/test-bucket/textures/b" {
				t.Error("stat escaped prefix")
			}
			w.Header().Set("Content-Length", "12")
			w.Header().Set("Last-Modified", "Sun, 25 Oct 2026 00:00:00 GMT")
			return
		}
		q := r.URL.Query()
		if q.Get("prefix") != "textures/" || q.Get("list-type") != "2" {
			t.Error("incorrect list scope")
		}
		w.Header().Set("Content-Type", "application/xml")
		if q.Get("continuation-token") == "two" || q.Get("start-after") == "textures/b" {
			fmt.Fprint(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>test-bucket</Name><IsTruncated>false</IsTruncated><Contents><Key>textures/c</Key><LastModified>2026-10-25T00:00:00.000Z</LastModified><Size>12</Size></Contents></ListBucketResult>`)
		} else {
			fmt.Fprint(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>test-bucket</Name><IsTruncated>true</IsTruncated><NextContinuationToken>two</NextContinuationToken><Contents><Key>textures/a</Key><LastModified>2026-10-25T00:00:00.000Z</LastModified><Size>12</Size></Contents><Contents><Key>textures/b</Key><LastModified>2026-10-25T00:00:00.000Z</LastModified><Size>12</Size></Contents></ListBucketResult>`)
		}
	}))
	defer server.Close()
	provider, err := NewS3(S3Options{Endpoint: server.URL, Bucket: "test-bucket", Region: "us-east-1", Prefix: "textures", AccessKey: "synthetic", SecretKey: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := provider.List(context.Background(), "", 2)
	if err != nil || len(page.Objects) != 2 || page.Next != "b" || page.Objects[0].Key != "a" {
		t.Fatal("first page", page, err)
	}
	next, err := provider.List(context.Background(), page.Next, 2)
	if err != nil || len(next.Objects) != 1 || next.Next != "" || next.Objects[0].Key != "c" {
		t.Fatal("next page", next, err)
	}
	info, err := provider.Stat(context.Background(), "b")
	if err != nil || info.Bytes != 12 || !info.ModifiedAt.Equal(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("stat", info, err)
	}
}
