package server

import (
	"io/fs"
	"mime"
	"net/http"
	"strings"

	"disk-report/internal/webui"
)

func init() {
	// Go's table has no entry for .webmanifest, so it would be sniffed as plain
	// text and Chrome would decline to install the app.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// tokenMeta is where the page finds its token. Injected into the HTML rather
// than fetched from an endpoint, because an endpoint that hands out the token
// would have to be reachable without one — which is the hole the token exists
// to close.
const tokenMeta = "disk-report-token"

// staticHandler serves the built SPA.
//
// Every unknown path falls through to index.html: the router is client-side, so
// /folders is a route the server has never heard of and must not 404.
func (s *Server) staticHandler() http.Handler {
	assets, err := webui.Assets()
	if err != nil || !webui.Built() {
		return http.HandlerFunc(s.noUIBuilt)
	}

	files := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			s.serveIndex(w, assets)
			return
		}

		// A real file, or the shell for a client-side route.
		if info, err := fs.Stat(assets, path); err != nil || info.IsDir() {
			s.serveIndex(w, assets)
			return
		}

		// Hashed asset names are immutable; index.html never is.
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

// serveIndex hands back the shell.
//
// The token deliberately is not baked in here. The service worker caches this
// document so the app opens with the server down, and a cached token would be a
// dead one after the next restart — every action would fail with a 403 that
// looked like a permissions problem. The page asks for a fresh token instead.
func (s *Server) serveIndex(w http.ResponseWriter, assets fs.FS) {
	body, err := fs.ReadFile(assets, webui.ShellName)
	if err != nil {
		s.noUIBuilt(w, nil)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

// noUIBuilt explains the one confusing state: a binary built before the UI was.
func (s *Server) noUIBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(
		"No UI has been built into this binary.\n\n" +
			"Run `bun run build` and rebuild, or use `bun run dev` and open Vite's\n" +
			"port instead — it proxies /api and /data here.\n"))
}
