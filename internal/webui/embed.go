// Package webui carries the built SPA so the binary can serve it on its own.
//
// go:embed cannot reach outside its own package directory, so `bun run build`
// copies dist/client here. The .gitkeep placeholder keeps this package
// compiling before the first UI build — a Go test run should not require a
// front-end build to have happened.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:assets
var embedded embed.FS

// ShellName is the document every client-side route falls back to.
//
// TanStack Start's SPA build calls it _shell.html rather than index.html: it is
// the prerendered shell, not a page.
const ShellName = "_shell.html"

// Assets is the built SPA, rooted so that the shell is at the top.
func Assets() (fs.FS, error) {
	return fs.Sub(embedded, "assets")
}

// Built reports whether a UI build has been copied in. Without one the server
// still runs and serves the API; it just has no page to hand back, and says so
// rather than returning a confusing 404.
func Built() bool {
	assets, err := Assets()
	if err != nil {
		return false
	}
	_, err = fs.Stat(assets, ShellName)
	return err == nil
}
