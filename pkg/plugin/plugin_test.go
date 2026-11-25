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

func TestManifestSettingDefaultsAndOptionsValidation(t *testing.T) {
	base := Manifest{ID: "community.example", Version: "1", SettingsNamespace: "community.example"}

	valid := base
	valid.Settings = []SettingField{
		{Key: "daily_limit", Label: "Daily Limit", Type: "number", Default: 5, Description: "Max per day"},
		{Key: "enabled", Label: "Enabled", Type: "boolean", Default: true},
		{
			Key:     "provider",
			Label:   "Provider",
			Type:    "text",
			Default: "openai",
			Options: []SettingOption{{Value: "openai", Label: "OpenAI"}, {Value: "local", Label: "Local"}},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid settings, got %v", err)
	}

	invalidCases := []SettingField{
		{Key: "bad_num", Label: "Bad", Type: "number", Default: "five"},
		{Key: "bad_bool", Label: "Bad", Type: "boolean", Default: "yes"},
		{Key: "bad_text", Label: "Bad", Type: "text", Default: 123},
		{Key: "secret_default", Label: "Bad", Type: "secret", Default: "leaky"},
		{Key: "secret_opts", Label: "Bad", Type: "secret", Options: []SettingOption{{Value: "a", Label: "A"}}},
		{Key: "dup_opt", Label: "Bad", Type: "text", Options: []SettingOption{{Value: "a", Label: "A"}, {Value: "a", Label: "A2"}}},
		{Key: "bad_opt_default", Label: "Bad", Type: "text", Default: "c", Options: []SettingOption{{Value: "a", Label: "A"}}},
	}
	for _, field := range invalidCases {
		m := base
		m.Settings = []SettingField{field}
		if err := m.Validate(); err == nil {
			t.Fatalf("expected invalid setting %#v to fail validation", field)
		}
	}
}
