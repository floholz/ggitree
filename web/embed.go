// Package web holds the page. The TypeScript in src/ is bundled into dist/ by
// "go generate ./web", which runs esbuild through its Go API (no Node needed).
package web

//go:generate go run ../tools/build

import (
	"embed"
	"io/fs"
)

//go:embed index.html dist
var files embed.FS

// FS is the page's file tree: index.html and dist/.
func FS() fs.FS { return files }
