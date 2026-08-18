package scan

import (
	"fmt"
	"time"

	"disk-report/internal/cache"
	"disk-report/internal/duplicates"
	"disk-report/internal/ledger"
	"disk-report/internal/schema"
	"disk-report/internal/volume"
)

// Phase names which half of the scan is running. The duplicate pass reads file
// contents and has nothing useful to say about directories, so it reports its
// own numbers.
type Phase string

const (
	PhaseWalk       Phase = "walk"
	PhaseDuplicates Phase = "duplicates"
)

// Report is one progress update.
type Report struct {
	Phase Phase
	Progress
	FingerprintsDone  int
	FingerprintsTotal int
}

// RunOptions configures a complete scan.
type RunOptions struct {
	Config schema.ScanConfig
	// CachePath enables incremental scanning. Empty disables the cache
	// entirely: nothing is read and nothing is written.
	CachePath string
	// IgnoreCache rescans everything but still writes the results, so the next
	// run is warm again.
	IgnoreCache bool
	// LedgerPath is the deletion log written by exported cleanup scripts.
	LedgerPath string
	HomeDir    string

	OnProgress       func(Report)
	ProgressInterval time.Duration
}

// Stats are the numbers about the scan rather than about the disk.
type Stats struct {
	CacheHits            int64
	CacheMisses          int64
	FingerprintsReused   int
	FingerprintsComputed int
	CacheDiscardedOnOpen bool
}

// RunResult is a finished scan.
type RunResult struct {
	Snapshot schema.ScanSnapshot
	Stats    Stats
}

// Run walks the configured roots, finds byte-identical files, and assembles a
// snapshot.
func Run(options RunOptions) (*RunResult, error) {
	startedAt := time.Now()

	// Before anything else: a path that was deleted never appears in a walk, so
	// its absence has to be established deliberately rather than inferred.
	var rechecked []schema.RecheckedPath
	if options.LedgerPath != "" {
		rechecked = ledger.Recheck(ledger.Read(options.LedgerPath))
	}

	var stats Stats
	var store *cache.Cache

	if options.CachePath != "" {
		opened, reset, err := cache.Open(options.CachePath, options.Config)
		if err != nil {
			return nil, err
		}
		defer opened.Close()

		store = opened
		stats.CacheDiscardedOnOpen = reset
	}

	walkOptions := Options{
		Config:           options.Config,
		HomeDir:          options.HomeDir,
		ProgressInterval: options.ProgressInterval,
	}
	if options.OnProgress != nil {
		walkOptions.OnProgress = func(progress Progress) {
			options.OnProgress(Report{Phase: PhaseWalk, Progress: progress})
		}
	}

	if store != nil && !options.IgnoreCache {
		cached, err := store.Load()
		if err != nil {
			return nil, err
		}
		walkOptions.Cached = cached
	}

	result, err := Walk(walkOptions)
	if err != nil {
		return nil, err
	}

	collector := result.Collector
	stats.CacheHits = collector.Progress().CacheHits
	stats.CacheMisses = collector.Progress().CacheMisses

	found := duplicates.Empty()
	if options.Config.DetectDuplicates {
		found = duplicates.Detect(collector.Candidates(), duplicates.Options{
			MinSize:    options.Config.DuplicateMinSize,
			Classifier: result.Classifier,
			OnProgress: duplicateProgress(options, collector),
		})
	}
	collector.ApplyDuplicates(found)

	stats.FingerprintsReused = found.Reused
	stats.FingerprintsComputed = found.Computed

	// Saved after the duplicate pass so the fingerprints go in with the rows.
	// Without them a warm scan would re-read every suspect, which is the single
	// most expensive thing it could do.
	if store != nil {
		if err := store.Save(collector.FreshDeltas(), collector.Visited(), result.ValidRoots); err != nil {
			return nil, err
		}
	}

	snapshot := result.Snapshot(SnapshotInput{
		Config:     options.Config,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
		Duplicates: found,
		Volume:     volume.Read(result.ValidRoots[0]),
		Rechecked:  rechecked,
	})

	return &RunResult{Snapshot: snapshot, Stats: stats}, nil
}

func duplicateProgress(options RunOptions, collector *Collector) func(done, total int) {
	if options.OnProgress == nil {
		return nil
	}
	return func(done, total int) {
		options.OnProgress(Report{
			Phase:             PhaseDuplicates,
			Progress:          collector.Progress(),
			FingerprintsDone:  done,
			FingerprintsTotal: total,
		})
	}
}

// Diff compares an incremental snapshot against a full one.
//
// The cache trades correctness for speed on a documented edge — a file modified
// in place inside an otherwise untouched directory — so there has to be a way
// to prove it on real data rather than on a fixture.
func Diff(cached, full schema.ScanSnapshot) []string {
	var problems []string

	compare := func(label string, a, b int64) {
		if a != b {
			problems = append(problems, sprintDelta(label, a, b))
		}
	}

	compare("totals.bytes", cached.Totals.Bytes, full.Totals.Bytes)
	compare("totals.files", cached.Totals.Files, full.Totals.Files)
	compare("totals.dirs", cached.Totals.Dirs, full.Totals.Dirs)
	compare("folderTree.recursiveSize", cached.FolderTree.RecursiveSize, full.FolderTree.RecursiveSize)
	compare("extensions", int64(len(cached.Extensions)), int64(len(full.Extensions)))
	compare("files reported", int64(len(cached.Files)), int64(len(full.Files)))

	fullByExt := make(map[string]schema.ExtensionStat, len(full.Extensions))
	for _, stat := range full.Extensions {
		fullByExt[stat.Ext] = stat
	}

	for _, stat := range cached.Extensions {
		other, ok := fullByExt[stat.Ext]
		if !ok {
			problems = append(problems, "extension "+displayExt(stat.Ext)+": present in cached, absent in full")
			continue
		}
		if stat.TotalSize != other.TotalSize {
			problems = append(problems, sprintDelta("extension "+displayExt(stat.Ext), stat.TotalSize, other.TotalSize))
		}
	}

	return problems
}

// The cache and the ledger live together, because the ledger is scan state in
// the same sense the cache is.
const (
	CacheDirName  = ".scan-cache"
	CacheFileName = "tree.sqlite"
	LedgerName    = "deleted.log"
)

func sprintDelta(label string, cached, full int64) string {
	return fmt.Sprintf("%s: cached %d vs full %d (delta %d)", label, cached, full, cached-full)
}

// displayExt names the no-extension bucket, which is otherwise an empty string
// in the middle of a sentence.
func displayExt(ext string) string {
	if ext == "" {
		return "(none)"
	}
	return ext
}
