package server

import (
	"errors"
	"path/filepath"
	"sync"
	"time"

	"disk-report/internal/scan"
	"disk-report/internal/schema"
	"disk-report/internal/snapshot"
)

// ErrScanRunning means a scan is already in flight.
//
// Scanning is single-flight rather than queued: two concurrent walks would
// fight over the same cache file and the same output directory, and a second
// scan started 30 seconds after the first has nothing new to report anyway.
var ErrScanRunning = errors.New("a scan is already running")

// JobState is what the UI needs to render, whether or not a scan is running.
type JobState struct {
	Running bool `json:"running"`
	// Phase is "walk" or "duplicates".
	Phase       string `json:"phase,omitempty"`
	DirsDone    int64  `json:"dirsDone"`
	DirsQueued  int64  `json:"dirsQueued"`
	Files       int64  `json:"files"`
	Bytes       int64  `json:"bytes"`
	CurrentPath string `json:"currentPath,omitempty"`
	CacheHits   int64  `json:"cacheHits"`

	FingerprintsDone  int `json:"fingerprintsDone,omitempty"`
	FingerprintsTotal int `json:"fingerprintsTotal,omitempty"`

	// Set once the scan finishes.
	Done         bool   `json:"done"`
	SnapshotID   string `json:"snapshotId,omitempty"`
	SnapshotFile string `json:"snapshotFile,omitempty"`
	DurationMs   int64  `json:"durationMs,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Runner owns the one scan that may be in flight.
type Runner struct {
	outDir string

	mu        sync.Mutex
	running   bool
	state     JobState
	listeners map[chan JobState]struct{}
}

func NewRunner(outDir string) *Runner {
	return &Runner{outDir: outDir, listeners: map[chan JobState]struct{}{}}
}

// State reads the current state.
func (r *Runner) State() JobState {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.state
	state.Running = r.running
	return state
}

// Subscribe returns a channel of state updates and a function to stop.
//
// The channel is buffered and dropped-on-full: a browser that stops reading must
// not be able to block the scan that is feeding it.
func (r *Runner) Subscribe() (<-chan JobState, func()) {
	updates := make(chan JobState, 8)

	r.mu.Lock()
	r.listeners[updates] = struct{}{}
	r.mu.Unlock()

	return updates, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.listeners[updates]; ok {
			delete(r.listeners, updates)
			close(updates)
		}
	}
}

func (r *Runner) publish(state JobState) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.state = state
	for listener := range r.listeners {
		select {
		case listener <- state:
		default:
		}
	}
}

// Start begins a scan in the background, returning immediately.
func (r *Runner) Start(options scan.RunOptions) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return ErrScanRunning
	}
	r.running = true
	r.state = JobState{Running: true, Phase: string(scan.PhaseWalk)}
	r.mu.Unlock()

	// Throttled: the walk reports every 250ms, and an SSE frame per report is
	// plenty for a progress line.
	options.ProgressInterval = 250 * time.Millisecond
	options.OnProgress = func(report scan.Report) {
		r.publish(JobState{
			Running:           true,
			Phase:             string(report.Phase),
			DirsDone:          report.DirsDone,
			DirsQueued:        report.DirsQueued,
			Files:             report.Files,
			Bytes:             report.Bytes,
			CurrentPath:       report.CurrentPath,
			CacheHits:         report.CacheHits,
			FingerprintsDone:  report.FingerprintsDone,
			FingerprintsTotal: report.FingerprintsTotal,
		})
	}

	go r.run(options)
	return nil
}

func (r *Runner) run(options scan.RunOptions) {
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	result, err := scan.Run(options)
	if err != nil {
		r.publish(JobState{Done: true, Error: err.Error()})
		return
	}

	file, err := snapshot.Write(r.outDir, result.Snapshot)
	if err != nil {
		r.publish(JobState{Done: true, Error: err.Error()})
		return
	}

	r.publish(finished(result.Snapshot, file))
}

func finished(snap schema.ScanSnapshot, path string) JobState {
	return JobState{
		Done:       true,
		SnapshotID: snap.ID,
		// The name the SPA fetches from /data, taken from what was written
		// rather than reconstructed.
		SnapshotFile: filepath.Base(path),
		DurationMs:   snap.DurationMs,
		Files:        snap.Totals.Files,
		Bytes:        snap.Totals.Bytes,
		DirsDone:     snap.Totals.Dirs,
	}
}
