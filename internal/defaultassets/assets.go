// Package defaultassets contains the small set of built-in Minecraft assets
// used when a character has not selected a user-provided skin yet.
package defaultassets

import (
	"bytes"
	"embed"
)

// These files are user-provided default resources, kept in the application
// binary so Yggdrasil clients can load the same defaults as the web preview.
//
//go:embed steve.png alex.png
var skins embed.FS

// Skin returns a fresh reader for the requested Minecraft arm model.
func Skin(model string) (*bytes.Reader, bool) {
	name := "steve.png"
	if model == "slim" {
		name = "alex.png"
	}
	data, err := skins.ReadFile(name)
	if err != nil {
		return nil, false
	}
	return bytes.NewReader(data), true
}
