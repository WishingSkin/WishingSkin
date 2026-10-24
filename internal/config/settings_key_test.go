package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsKeyRetainsOriginalAndRejectsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "settings.key")
	first, err := SettingsKey(path)
	if err != nil || len(first) != 32 {
		t.Fatal("key creation", err)
	}
	second, err := SettingsKey(path)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("restart replaced original key", err)
	}
	damaged := []byte("damaged")
	if err := os.WriteFile(path, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SettingsKey(path); err == nil {
		t.Fatal("damaged key accepted")
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, damaged) {
		t.Fatal("damaged key was overwritten", err)
	}
	if _, err := SettingsKey(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted as key")
	}
}
