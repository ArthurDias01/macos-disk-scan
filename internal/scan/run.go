package scan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"disk-report/internal/aggregate"
	"disk-report/internal/config"
	"disk-report/internal/extension"
	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

// Options configures a walk.
type Options struct {
	Config schema.ScanConfig
	// Rows from the previous scan, keyed by directory. Nil for a cold scan.
	Cached map[string]*walk.DirDelta
	// HomeDir decides which paths count as cloud-backed. Defaults to the
	// current user's home.
	HomeDir string
	// OnProgress is polled on a timer while the walk runs, on its own
	// goroutine. Nil disables progress entirely.
	OnProgress func(Progress)
	// ProgressInterval defaults to 250ms — fast enough to look live, slow
	// enough that a terminal is not the bottleneck.
	ProgressInterval time.Duration
}

// WalkResult is the state a completed walk leaves behind, before duplicate
// detection and snapshot assembly.
type WalkResult struct {
	Collector  *Collector
	Classifier *extension.Classifier
	// Roots that were readable directories. The others are in Warnings or
	// Unscanned.
	ValidRoots []string
	Warnings   []string
}

// ErrNoRoots means every configured root was missing, unreadable, or not a
// directory — there is nothing to report on.
var ErrNoRoots = errors.New("no readable roots to scan")

// Walk visits every root and returns the assembled per-directory state.
func Walk(options Options) (*WalkResult, error) {
	cfg := options.Config

	// Everything downstream assumes clean paths. The walk builds child paths by
	// appending "/" + name, and exclusion is a prefix test — a configured root
	// of "~/Downloads/" would otherwise produce "//" in every descendant path,
	// so the folder tree and the file entries would key on different strings.
	cfg.Roots = cleanPaths(cfg.Roots)
	cfg.ExcludePaths = cleanPaths(cfg.ExcludePaths)

	homeDir := options.HomeDir
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}

	classifier := extension.New(cfg, homeDir)
	collector := NewCollector(cfg, classifier, options.Cached)

	rootDevices := map[int64]bool{}
	validRoots := make([]string, 0, len(cfg.Roots))
	var warnings []string

	for _, root := range cfg.Roots {
		// Stat, not Lstat: a root given as a symlink is a pointer the user
		// typed, not a link discovered mid-walk.
		info, err := os.Stat(root)
		if err != nil {
			collector.unscanned = append(collector.unscanned, walk.ToScanError(root, err))
			continue
		}
		if !info.IsDir() {
			warnings = append(warnings, fmt.Sprintf("Root is not a directory, skipped: %s", root))
			continue
		}
		device, ok := walk.DeviceOf(info)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("Root has no device information, skipped: %s", root))
			continue
		}

		rootDevices[device] = true
		validRoots = append(validRoots, root)
		collector.AddRoot(root)
	}

	if len(validRoots) == 0 {
		return nil, ErrNoRoots
	}

	stopProgress := startProgress(collector, options)

	scanner := walk.NewScanner(cfg, classifier, config.VolatileExtensions(), rootDevices)
	walk.Run(scanner, int(cfg.WorkerCount), collector.Handler())

	stopProgress()

	// Directories discovered but never scanned — excluded, or a failed read —
	// still need a parent link so the tree has no holes.
	collector.dirs.LinkOrphans(validRoots)

	return &WalkResult{
		Collector:  collector,
		Classifier: classifier,
		ValidRoots: validRoots,
		Warnings:   warnings,
	}, nil
}

// startProgress polls the collector's counters on a timer and returns a stop
// function.
//
// Polling rather than reporting from inside the walk: an update per directory
// would be 737k terminal writes, and the counters are atomic precisely so
// another goroutine can read them without slowing the walk down.
func startProgress(collector *Collector, options Options) func() {
	if options.OnProgress == nil {
		return func() {}
	}

	interval := options.ProgressInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}

	done := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				options.OnProgress(collector.Progress())
			case <-done:
				return
			}
		}
	}()

	return func() {
		close(done)
		<-finished
	}
}

// cleanPaths normalizes a configured path list without touching the caller's
// slice — the config is embedded in the snapshot and must not be edited from
// underneath it.
func cleanPaths(paths []string) []string {
	cleaned := make([]string, len(paths))
	for i, path := range paths {
		cleaned[i] = filepath.Clean(path)
	}
	return cleaned
}

// Dirs exposes the per-directory accumulators.
func (c *Collector) Dirs() aggregate.Dirs { return c.dirs }

// Extensions exposes the global per-extension accumulators.
func (c *Collector) Extensions() aggregate.ExtMap { return c.extMap }

// Entries returns every file at or above MinFileSize, uncapped and unsorted.
func (c *Collector) Entries() []schema.FileEntry { return c.entries }

// Candidates returns the files eligible for duplicate fingerprinting.
func (c *Collector) Candidates() []walk.CandidateRecord { return c.candidates }

// Unscanned returns the paths that could not be read.
func (c *Collector) Unscanned() []schema.ScanError { return c.unscanned }

// FreshDeltas returns the rows that were actually read this scan, for the cache
// to persist.
func (c *Collector) FreshDeltas() []*walk.DirDelta { return c.freshDeltas }

// Visited returns every directory reached, so the cache can prune the rest.
func (c *Collector) Visited() map[string]bool { return c.visited }

// Totals assembles the snapshot's headline numbers.
//
// The duplicate figures are zero until ApplyDuplicates runs, at which point
// UniqueBytes drops to the floor of what is really stored.
func (c *Collector) Totals() schema.ScanTotals {
	bytes := c.totalBytes.Load()

	return schema.ScanTotals{
		Bytes:           bytes,
		Files:           c.totalFiles.Load(),
		Dirs:            int64(len(c.dirs)),
		DedupedBytes:    c.dedupedBytes,
		DedupedFiles:    c.dedupedFiles,
		UnreadablePaths: int64(len(c.unscanned)),
		UniqueBytes:     bytes - c.duplicateBytes,
		DuplicateBytes:  c.duplicateBytes,
		DuplicateFiles:  c.duplicateFiles,
	}
}

// Progress is a live view of the walk, safe to read from another goroutine.
type Progress struct {
	DirsDone    int64
	DirsQueued  int64
	Files       int64
	Bytes       int64
	CacheHits   int64
	CacheMisses int64
	CurrentPath string
}

// Progress reads the live counters.
func (c *Collector) Progress() Progress {
	current := ""
	if path := c.currentPath.Load(); path != nil {
		current = *path
	}

	return Progress{
		DirsDone:    c.dirsDone.Load(),
		DirsQueued:  c.queued.Load(),
		Files:       c.totalFiles.Load(),
		Bytes:       c.totalBytes.Load(),
		CacheHits:   c.cacheHits.Load(),
		CacheMisses: c.cacheMisses.Load(),
		CurrentPath: current,
	}
}
