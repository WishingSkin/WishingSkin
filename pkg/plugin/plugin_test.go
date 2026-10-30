package plugin

import (
	"testing"
	"time"
)

func TestTaskScheduleBounds(t *testing.T) {
	for _, value := range []string{"@every 0s", "@every 59s", "@every 169h", "* * * * *", "@every nope"} {
		if _, err := TaskInterval(value); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	for _, value := range []string{"", "@every 1m", "@every 168h"} {
		if _, err := TaskInterval(value); err != nil {
			t.Fatal(err)
		}
	}
	d, _ := TaskInterval("@every 1m")
	if d != time.Minute {
		t.Fatal(d)
	}
}
func TestManifestContributionValidation(t *testing.T) {
	base := Manifest{ID: "community.example", Version: "1", SettingsNamespace: "community.example"}
	for _, page := range []PageContribution{{ID: "../bad", Area: "admin.plugins", Label: "X"}, {ID: "page", Area: "public", Label: "X"}, {ID: "page", Area: "admin.plugins"}} {
		m := base
		m.Pages = []PageContribution{page}
		if m.Validate() == nil {
			t.Fatal("invalid page accepted")
		}
	}
	m := base
	m.Tasks = []TaskDeclaration{{ID: "task"}, {ID: "task"}}
	if m.Validate() == nil {
		t.Fatal("duplicate task accepted")
	}
}
