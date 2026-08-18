package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"disk-report/internal/config"
)

func testServer(t *testing.T) (*Server, http.Handler, string) {
	t.Helper()

	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Roots = []string{root}
	cfg.MinFileSize = 0
	cfg.MinFolderSize = 0
	cfg.WorkerCount = 2
	cfg.DetectDuplicates = false

	instance, err := New(Options{
		Port:       7777,
		Config:     cfg,
		ConfigPath: filepath.Join(t.TempDir(), "absent.json"),
		OutDir:     out,
		LedgerPath: filepath.Join(t.TempDir(), "deleted.log"),
	})
	if err != nil {
		t.Fatal(err)
	}

	return instance, instance.Handler(), root
}

func post(t *testing.T, handler http.Handler, instance *Server, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	r.Host = "localhost:7777"
	r.Header.Set("Origin", "http://localhost:7777")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(tokenHeader, instance.Token().Value())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestStateIsReadableWithoutAToken(t *testing.T) {
	instance, handler, root := testServer(t)
	_ = instance

	r := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	r.Host = "localhost:7777"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var state StateResponse
	if err := json.NewDecoder(w.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if len(state.Roots) != 1 || state.Roots[0] != root {
		t.Errorf("roots = %v, want [%s]", state.Roots, root)
	}
	if state.Job.Running {
		t.Error("reported a scan running on a fresh server")
	}
}

// The endpoints that change something are the ones a hostile page would want.
func TestMutatingEndpointsRefuseUnauthorisedRequests(t *testing.T) {
	instance, handler, _ := testServer(t)

	for _, path := range []string{"/api/scan", "/api/actions/trash", "/api/actions/reveal"} {
		// No token, right origin.
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		r.Host = "localhost:7777"
		r.Header.Set("Origin", "http://localhost:7777")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s without a token = %d, want 403", path, w.Code)
		}

		// Right token, hostile origin.
		r = httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		r.Host = "localhost:7777"
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set(tokenHeader, instance.Token().Value())
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s from a hostile origin = %d, want 403", path, w.Code)
		}
	}
}

func TestScanRunsAndIsSingleFlight(t *testing.T) {
	instance, handler, _ := testServer(t)

	if w := post(t, handler, instance, "/api/scan", ScanRequest{}); w.Code != http.StatusAccepted {
		t.Fatalf("first scan = %d: %s", w.Code, w.Body)
	}

	// Two walks would fight over the same cache file and output directory.
	second := post(t, handler, instance, "/api/scan", ScanRequest{})
	if second.Code != http.StatusConflict && second.Code != http.StatusAccepted {
		t.Errorf("second scan = %d, want 409 (or 202 if the first already finished)", second.Code)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		state := instance.runner.State()
		if state.Done && !state.Running {
			if state.Error != "" {
				t.Fatalf("scan failed: %s", state.Error)
			}
			if state.SnapshotFile == "" {
				t.Fatal("finished without naming a snapshot")
			}
			// The snapshot the UI is about to fetch has to be on disk.
			if _, err := os.Stat(filepath.Join(instance.options.OutDir, state.SnapshotFile)); err != nil {
				t.Fatalf("snapshot not written: %v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the scan never finished")
}

func TestScanRejectsRelativeRoots(t *testing.T) {
	instance, handler, _ := testServer(t)

	w := post(t, handler, instance, "/api/scan", ScanRequest{Roots: []string{"relative/path"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTrashRefusesPathsOutsideTheConfiguredRoots(t *testing.T) {
	instance, handler, _ := testServer(t)

	w := post(t, handler, instance, "/api/actions/trash", PathsRequest{Paths: []string{"/etc/passwd"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 — /etc/passwd is outside every root", w.Code)
	}
}

// The client-side router owns /folders; the server has never heard of it and
// must not answer 404.
func TestUnknownRoutesFallThroughToTheApp(t *testing.T) {
	_, handler, _ := testServer(t)

	r := httptest.NewRequest(http.MethodGet, "/folders", nil)
	r.Host = "localhost:7777"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	// Without an embedded UI this is the "no UI built" notice rather than the
	// shell, but either way it must not be a 404.
	if w.Code == http.StatusNotFound {
		t.Error("a client-side route 404ed")
	}
}

func TestSnapshotsAreServedFromTheOutputDirectory(t *testing.T) {
	instance, handler, _ := testServer(t)

	path := filepath.Join(instance.options.OutDir, "index.json")
	if err := os.WriteFile(path, []byte(`{"snapshots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodGet, "/data/index.json", nil)
	r.Host = "localhost:7777"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "snapshots") {
		t.Errorf("status = %d, body = %s", w.Code, w.Body)
	}
}
