package httpapi

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewUploadDimensions(t *testing.T) {
	for _, test := range []struct {
		kind          string
		width, height int
		valid         bool
	}{
		{"skin", 64, 64, true}, {"skin", 64, 32, true},
		{"skin", 128, 128, true}, {"skin", 128, 64, true},
		{"skin", 32, 32, false}, {"skin", 63, 64, false},
		{"skin", 96, 96, false}, {"skin", 128, 32, false},
		{"skin", 192, 192, false}, {"skin", 256, 256, false},
		{"skin", 1024, 512, false}, {"skin", 1024, 1024, false},
		{"cape", 64, 32, true}, {"cape", 128, 64, true},
		{"cape", 192, 96, true}, {"cape", 1024, 512, true},
		{"cape", 64, 64, false}, {"cape", 96, 48, false},
		{"cape", 1088, 544, false},
	} {
		t.Run(fmt.Sprintf("%s_%dx%d", test.kind, test.width, test.height), func(t *testing.T) {
			if got := validTextureDimensions(test.kind, test.width, test.height); got != test.valid {
				t.Fatalf("accepted=%t, want %t", got, test.valid)
			}
		})
	}
}

func TestSkinUploadSizesBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			for _, size := range [][2]int{{64, 64}, {64, 32}, {128, 128}, {128, 64}} {
				uploadedRecord(t, f.upload(1, testPNG(t, size[0], size[1]), nil))
			}
			for _, size := range [][2]int{{192, 192}, {256, 256}, {1024, 512}} {
				w := f.upload(1, testPNG(t, size[0], size[1]), nil)
				webStatus(t, w, 400)
				if !strings.Contains(w.Body.String(), "64×64、64×32、128×128 或 128×64") {
					t.Fatal("rejection did not list supported skin sizes")
				}
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM texture_records").Scan(&count); err != nil || count != 4 {
				t.Fatal("rejected skin created a record", count, err)
			}
			uploadedRecord(t, f.upload(1, testPNG(t, 1024, 512), map[string]string{"kind": "cape"}))
		})
	}
}
