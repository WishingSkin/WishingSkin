package web

import "embed"

// Assets are built by pnpm before compiling the Go application.
//
//go:embed dist
var Assets embed.FS
