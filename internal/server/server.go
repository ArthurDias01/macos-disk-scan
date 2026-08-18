// Package server hosts the report UI and the small API behind it.
//
// The app was built as a pure static SPA, and it still is: no route renders on
// the server, and every page reads the same snapshot JSON it always did. What
// this adds is the two things a static file cannot do — start a scan, and move a
// file to the Trash — behind the guards in security.go.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"disk-report/internal/config"
	"disk-report/internal/scan"
	"disk-report/internal/schema"
)

// Options configures the server.
type Options struct {
	Port int
	// Config is the resolved scan configuration, used as the default for
	// UI-triggered scans and as the allow-list for file actions.
	Config schema.ScanConfig
	// ConfigPath is re-read on each scan so edits to scan.config.json are picked
	// up without a restart.
	ConfigPath string
	OutDir     string
	CachePath  string
	LedgerPath string
}

// Server is the assembled application.
type Server struct {
	options Options
	token   *Token
	guard   *guard
	runner  *Runner
	actions *Actions
}

// New builds the server and mints its token.
func New(options Options) (*Server, error) {
	token, err := NewToken()
	if err != nil {
		return nil, err
	}

	return &Server{
		options: options,
		token:   token,
		guard:   newGuard(token, options.Port),
		runner:  NewRunner(options.OutDir),
		actions: NewActions(NewPathGuard(options.Config.Roots), options.LedgerPath),
	}, nil
}

// Token exposes the per-run token, for writing to disk and for tests.
func (s *Server) Token() *Token { return s.token }

// Handler builds the route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Read-only endpoints. Nothing here changes state, so nothing here is worth
	// embedding in someone else's page.
	mux.HandleFunc("GET /api/token", s.handleToken)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/scan/events", s.handleScanEvents)

	// Mutating endpoints, all behind checkMutating.
	mux.HandleFunc("POST /api/scan", s.mutating(s.handleScanStart))
	mux.HandleFunc("POST /api/actions/trash", s.mutating(s.handleTrash))
	mux.HandleFunc("POST /api/actions/reveal", s.mutating(s.handleReveal))

	// Snapshots, and then the SPA itself as the catch-all.
	mux.HandleFunc("GET /data/index.json", s.handleIndex)
	mux.Handle("GET /data/", http.StripPrefix("/data/",
		http.FileServer(http.Dir(s.options.OutDir))))
	mux.Handle("/", s.staticHandler())

	return s.withHostCheck(mux)
}

// withHostCheck runs on every request, including GETs for the page itself.
// DNS rebinding targets exactly the requests an Origin check does not see.
func (s *Server) withHostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.guard.checkHost(r); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) mutating(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.guard.checkMutating(r); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// StateResponse tells the page what it is allowed to offer.
type StateResponse struct {
	Job JobState `json:"job"`
	// Defaults the scan form starts from.
	Roots         []string `json:"roots"`
	MinFileSize   int64    `json:"minFileSize"`
	MinFolderSize int64    `json:"minFolderSize"`
	// UIBuilt is false when the binary has no embedded UI, which only happens in
	// development.
	Version string `json:"version"`
}

// handleIndex serves the snapshot index, inventing an empty one when there is
// no file yet.
//
// A fresh clone has no snapshots, and a 404 here would surface as "could not
// load the snapshot" — an error, for the least surprising state the app has. An
// empty index is what "you have not scanned yet" actually looks like, and the
// app already has a page for it.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	body, err := os.ReadFile(filepath.Join(s.options.OutDir, "index.json"))
	if err != nil {
		writeJSON(w, http.StatusOK, schema.SnapshotIndex{
			Snapshots: []schema.SnapshotIndexEntry{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

// handleToken hands the page its token.
//
// Safe to serve unauthenticated because a cross-origin reader cannot see the
// response: without CORS headers the browser refuses to expose it, and a
// no-cors fetch gets an opaque body. The one way around that is DNS rebinding,
// which arrives with someone else's Host header and is refused before it gets
// here.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	// No-store: the whole point is that it is never reused past a restart.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"token": s.token.Value()})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, StateResponse{
		Job:           s.runner.State(),
		Roots:         s.options.Config.Roots,
		MinFileSize:   s.options.Config.MinFileSize,
		MinFolderSize: s.options.Config.MinFolderSize,
		Version:       "1",
	})
}

// ScanRequest is what the scan form sends.
type ScanRequest struct {
	Roots         []string `json:"roots"`
	MinFileSize   *int64   `json:"minFileSize"`
	MinFolderSize *int64   `json:"minFolderSize"`
	// Full ignores the cache for reads but still writes it.
	Full bool `json:"full"`
}

func (s *Server) handleScanStart(w http.ResponseWriter, r *http.Request) {
	var request ScanRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "malformed request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg, err := s.configFor(request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = s.runner.Start(scan.RunOptions{
		Config:      cfg,
		CachePath:   s.options.CachePath,
		IgnoreCache: request.Full,
		LedgerPath:  s.options.LedgerPath,
	})
	if errors.Is(err, ErrScanRunning) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, s.runner.State())
}

// configFor layers the request onto the config the server is running with, so a
// scan started from the UI resolves the way one started from the CLI does.
//
// The config file is re-read first, so editing scan.config.json takes effect
// without a restart. When there is no file, the server's own configuration is
// the base — falling through to the built-in defaults would silently scan $HOME
// instead of whatever this server was pointed at.
func (s *Server) configFor(request ScanRequest) (schema.ScanConfig, error) {
	base := s.options.Config

	if _, err := os.Stat(s.options.ConfigPath); err == nil {
		overrides, err := config.LoadFile(s.options.ConfigPath)
		if err != nil {
			return schema.ScanConfig{}, err
		}
		base = config.Resolve(overrides)
	}

	if len(request.Roots) > 0 {
		roots := make([]string, 0, len(request.Roots))
		for _, root := range request.Roots {
			if !filepath.IsAbs(root) && root != "~" && !isTilde(root) {
				return schema.ScanConfig{}, fmt.Errorf(
					"roots must be absolute or start with ~: %s", root)
			}
			roots = append(roots, root)
		}
		// Resolve alone handles ~ expansion and cleaning.
		base.Roots = config.Resolve(config.Overrides{Roots: &roots}).Roots
	}
	if request.MinFileSize != nil {
		base.MinFileSize = *request.MinFileSize
	}
	if request.MinFolderSize != nil {
		base.MinFolderSize = *request.MinFolderSize
	}

	return base, nil
}

func isTilde(path string) bool {
	return len(path) > 1 && path[0] == '~' && path[1] == '/'
}

// handleScanEvents streams progress as Server-Sent Events.
//
// SSE rather than a WebSocket: this is one-way, it is text, and EventSource
// reconnects on its own. A WebSocket would be a second protocol to secure for no
// gain.
func (s *Server) handleScanEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	updates, unsubscribe := s.runner.Subscribe()
	defer unsubscribe()

	// The current state first, so a page that connects mid-scan renders
	// immediately instead of waiting for the next tick.
	writeEvent(w, flusher, s.runner.State())

	// A heartbeat keeps the connection from being reaped while nothing is
	// happening, which is most of the time.
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case state, open := <-updates:
			if !open {
				return
			}
			writeEvent(w, flusher, state)
		case <-heartbeat.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeEvent(w http.ResponseWriter, flusher http.Flusher, state JobState) {
	body, err := json.Marshal(state)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", body)
	flusher.Flush()
}

// PathsRequest is the body of both action endpoints.
type PathsRequest struct {
	Paths []string `json:"paths"`
}

func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	s.performAction(w, r, s.actions.Trash)
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	s.performAction(w, r, s.actions.Reveal)
}

func (s *Server) performAction(
	w http.ResponseWriter,
	r *http.Request,
	action func([]string) ([]Result, error),
) {
	var request PathsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "malformed request: "+err.Error(), http.StatusBadRequest)
		return
	}

	results, err := action(request.Paths)
	if err != nil && results == nil {
		// Validation failed, so nothing was attempted.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := map[string]any{"results": results}
	if err != nil {
		response["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, response)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
