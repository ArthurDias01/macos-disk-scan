// Package scan drives a walk and assembles the result into a snapshot.
package scan

import (
	"sync/atomic"

	"disk-report/internal/aggregate"
	"disk-report/internal/extension"
	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

type inodeKey struct {
	dev int64
	ino uint64
}

// Collector owns everything that cannot be decided from one directory alone:
// the queue, the folder tree, and the global inode set used for hardlink dedup.
//
// Every method except CachedMtime runs on the pool's driver goroutine, one call
// at a time, so the fields need no locking. The counters are atomic only
// because a progress ticker reads them from outside.
type Collector struct {
	config     schema.ScanConfig
	classifier *extension.Classifier
	// Rows from the previous scan. Immutable for the whole walk, which is what
	// makes CachedMtime safe to call from workers.
	cached map[string]*walk.DirDelta

	queue      []string
	dirs       aggregate.Dirs
	extMap     aggregate.ExtMap
	entries    []schema.FileEntry
	candidates []walk.CandidateRecord
	unscanned  []schema.ScanError
	seenInodes map[inodeKey]bool

	// Rows to persist, and every directory visited — the rest are stale.
	freshDeltas []*walk.DirDelta
	visited     map[string]bool

	totalBytes atomic.Int64
	totalFiles atomic.Int64
	dirsDone   atomic.Int64
	// Directories discovered but not yet visited. Tracked as a counter rather
	// than read off the queue, which belongs to the driver goroutine.
	queued       atomic.Int64
	cacheHits    atomic.Int64
	cacheMisses  atomic.Int64
	dedupedBytes int64
	dedupedFiles int64
	// Filled by the second pass, which runs after the walk is complete.
	duplicateBytes int64
	duplicateFiles int64
	currentPath    atomic.Pointer[string]
}

// NewCollector prepares a collector. cached may be nil for a cold scan.
func NewCollector(
	config schema.ScanConfig,
	classifier *extension.Classifier,
	cached map[string]*walk.DirDelta,
) *Collector {
	if cached == nil {
		cached = map[string]*walk.DirDelta{}
	}

	return &Collector{
		config:     config,
		classifier: classifier,
		cached:     cached,
		dirs:       aggregate.Dirs{},
		extMap:     aggregate.ExtMap{},
		seenInodes: map[inodeKey]bool{},
		visited:    map[string]bool{},
	}
}

// AddRoot seeds the queue with a root directory.
func (c *Collector) AddRoot(path string) {
	c.dirs.Ensure(path, c.classifier.IsCloudPath(path))
	c.queue = append(c.queue, path)
	c.queued.Add(1)
}

// Handler exposes the collector to the worker pool.
func (c *Collector) Handler() walk.Handler {
	return walk.Handler{
		Next:        c.next,
		CachedMtime: c.cachedMtime,
		OnScanned:   c.onScanned,
		OnUnchanged: c.onUnchanged,
	}
}

// next pops depth-first. The TypeScript original took from the front; taking
// from the back keeps the queue shallow, which matters at 737k directories.
// Nothing downstream depends on visit order.
func (c *Collector) next() (string, bool) {
	if len(c.queue) == 0 {
		return "", false
	}
	path := c.queue[len(c.queue)-1]
	c.queue = c.queue[:len(c.queue)-1]
	c.queued.Add(-1)
	return path, true
}

// cachedMtime is the only method workers call. A volatile directory is always
// rescanned: it holds a large file or a type that grows in place, neither of
// which moves the directory's own mtime.
func (c *Collector) cachedMtime(path string) (float64, bool) {
	row, ok := c.cached[path]
	if !ok || row.Volatile {
		return 0, false
	}
	return row.DirMtimeMs, true
}

func (c *Collector) onScanned(delta *walk.DirDelta) {
	c.cacheMisses.Add(1)
	c.freshDeltas = append(c.freshDeltas, delta)
	c.applyDelta(delta)
}

func (c *Collector) onUnchanged(path string) {
	row, ok := c.cached[path]
	if !ok {
		// No row to replay: the directory vanished between queue and stat.
		c.visited[path] = true
		return
	}
	c.cacheHits.Add(1)
	c.applyDelta(row)
}

// applyDelta folds one directory's contribution into the totals, cached or
// fresh. A cache hit replays exactly the object a worker would have produced,
// so there is one accounting path rather than two that must agree.
func (c *Collector) applyDelta(delta *walk.DirDelta) {
	c.dirsDone.Add(1)
	path := delta.Path
	c.currentPath.Store(&path)
	c.visited[delta.Path] = true

	accum := c.dirs.Ensure(delta.Path, delta.IsCloud)
	accum.OwnSize += delta.OwnSize
	accum.OwnFileCount += delta.OwnFileCount
	if delta.MaxMtimeMs > accum.MaxMtimeMs {
		accum.MaxMtimeMs = delta.MaxMtimeMs
	}
	c.totalBytes.Add(delta.OwnSize)
	c.totalFiles.Add(delta.OwnFileCount)

	c.extMap.MergeDir(delta.Path, delta.ExtDelta)

	for _, name := range delta.SubdirNames {
		subdir := joinPath(delta.Path, name)
		c.dirs.LinkChild(subdir, c.classifier.IsCloudPath(subdir))
		c.queue = append(c.queue, subdir)
		c.queued.Add(1)
	}

	c.entries = append(c.entries, delta.Entries...)
	c.candidates = append(c.candidates, delta.Candidates...)
	c.unscanned = append(c.unscanned, delta.Errors...)

	c.applyHardlinks(delta)
}

// applyHardlinks resolves the files the walk held back.
//
// This has to happen here because only the collector sees every inode. The
// first path to claim an inode owns its bytes; the rest are reported as freeing
// nothing, which is what stops a Time Machine or Homebrew hardlink farm from
// inflating a folder several-fold.
func (c *Collector) applyHardlinks(delta *walk.DirDelta) {
	for _, record := range delta.Hardlinks {
		key := inodeKey{record.Dev, record.Ino}
		if c.seenInodes[key] {
			c.dedupedBytes += record.Entry.Size
			c.dedupedFiles++
			if record.Entry.Size >= c.config.MinFileSize {
				duplicate := record.Entry
				duplicate.IsDupInode = true
				c.entries = append(c.entries, duplicate)
			}
			continue
		}
		c.seenInodes[key] = true
		c.creditFile(record.Dir, record.Entry)
	}
}

// creditFile accounts for one file in its directory and in the global totals.
//
// The walk does this for ordinary files as it goes; hardlink survivors reach it
// here, after the inode question is settled. Both paths must agree exactly, so
// there is one implementation.
func (c *Collector) creditFile(dirPath string, entry schema.FileEntry) {
	owner := c.dirs.Ensure(dirPath, c.classifier.IsCloudPath(dirPath))
	owner.OwnSize += entry.Size
	owner.OwnFileCount++
	if entry.MtimeMs > owner.MaxMtimeMs {
		owner.MaxMtimeMs = entry.MtimeMs
	}

	c.totalBytes.Add(entry.Size)
	c.totalFiles.Add(1)
	c.extMap.AddFile(entry)

	if entry.Size >= c.config.MinFileSize {
		c.entries = append(c.entries, entry)
	}
	if c.config.DetectDuplicates && entry.Size >= c.config.DuplicateMinSize {
		c.candidates = append(c.candidates, walk.CandidateRecord{
			Path:    entry.Path,
			Size:    entry.Size,
			MtimeMs: entry.MtimeMs,
		})
	}
}

// joinPath appends a basename to a directory. filepath.Join would also clean
// the result, which costs real time when it is called once per directory.
//
// Safe only because Walk cleans every root before the walk starts, so every
// path derived from one is already clean.
func joinPath(dir, name string) string {
	if dir == "/" {
		return dir + name
	}
	return dir + "/" + name
}
