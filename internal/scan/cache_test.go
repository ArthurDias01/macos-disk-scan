package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"disk-report/internal/aggregate"
	"disk-report/internal/cache"
	"disk-report/internal/config"
	"disk-report/internal/schema"
)

const kbLocal = 1024

// incrementalTree builds:
//
//	root/
//	  media/clip.mov       256 KB
//	  media/photo.jpg       64 KB
//	  code/main.ts           8 KB
//	  code/deep/data.json   16 KB
func incrementalTree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tree")

	for _, dir := range []string{"media", filepath.Join("code", "deep")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSized(t, filepath.Join(root, "media", "clip.mov"), 256*kbLocal)
	writeSized(t, filepath.Join(root, "media", "photo.jpg"), 64*kbLocal)
	writeSized(t, filepath.Join(root, "code", "main.ts"), 8*kbLocal)
	writeSized(t, filepath.Join(root, "code", "deep", "data.json"), 16*kbLocal)

	return root
}

func writeSized(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func incrementalConfig(root string) schema.ScanConfig {
	cfg := config.Defaults()
	cfg.Roots = []string{root}
	cfg.MinFileSize = 0
	cfg.MinFolderSize = 0
	cfg.WorkerCount = 2
	// Keep the duplicate pass out of the way; it has its own tests.
	cfg.DetectDuplicates = false
	return cfg
}

// scanWith runs one full scan, reading and writing the cache at cachePath.
// Passing an empty cachePath scans cold, which is the control every incremental
// assertion is measured against.
func scanWith(t *testing.T, cfg schema.ScanConfig, cachePath string) *WalkResult {
	t.Helper()

	options := Options{Config: cfg, HomeDir: "/Users/nobody"}
	var store *cache.Cache

	if cachePath != "" {
		opened, _, err := cache.Open(cachePath, cfg)
		if err != nil {
			t.Fatalf("open cache: %v", err)
		}
		store = opened

		cached, err := store.Load()
		if err != nil {
			t.Fatalf("load cache: %v", err)
		}
		options.Cached = cached
	}

	result, err := Walk(options)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if store != nil {
		if err := store.Save(
			result.Collector.FreshDeltas(),
			result.Collector.Visited(),
			result.ValidRoots,
		); err != nil {
			t.Fatalf("save cache: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("close cache: %v", err)
		}
	}

	return result
}

// shape is the set of numbers a cache bug would corrupt. Comparing shapes is
// the only assertion that matters: a warm scan must be indistinguishable from a
// cold one except in how long it took.
func shape(t *testing.T, result *WalkResult) string {
	t.Helper()
	collector := result.Collector

	totals := collector.Totals()
	tree := aggregate.BuildFolderTree(collector.Dirs(), result.ValidRoots, 0)
	extensions := aggregate.BuildExtensionStats(collector.Extensions(), result.Classifier, nil)

	var out strings.Builder
	fmt.Fprintf(&out, "bytes=%d files=%d dirs=%d tree=%d\n",
		totals.Bytes, totals.Files, totals.Dirs, tree.RecursiveSize)

	for _, stat := range extensions {
		fmt.Fprintf(&out, "ext %q %d %d %s\n",
			stat.Ext, stat.TotalSize, stat.FileCount, stat.LargestPath)
	}

	reported := make([]string, 0, len(collector.Entries()))
	for _, entry := range collector.Entries() {
		reported = append(reported, fmt.Sprintf("file %s %d", entry.Path, entry.Size))
	}
	sort.Strings(reported)
	out.WriteString(strings.Join(reported, "\n"))

	return out.String()
}

func TestColdRunPopulatesTheCacheAndReportsNoHits(t *testing.T) {
	root := incrementalTree(t)
	cachePath := filepath.Join(t.TempDir(), "cache", "tree.sqlite")

	result := scanWith(t, incrementalConfig(root), cachePath)
	progress := result.Collector.Progress()

	if progress.CacheHits != 0 {
		t.Errorf("cache hits = %d on a cold run, want 0", progress.CacheHits)
	}
	if progress.CacheMisses == 0 {
		t.Error("cache misses = 0 on a cold run")
	}
	if result.Collector.Totals().Files != 4 {
		t.Errorf("files = %d, want 4", result.Collector.Totals().Files)
	}
}

func TestWarmRunHitsTheCacheAndProducesAnIdenticalResult(t *testing.T) {
	root := incrementalTree(t)
	cachePath := filepath.Join(t.TempDir(), "cache", "tree.sqlite")
	cfg := incrementalConfig(root)

	cold := scanWith(t, cfg, "")
	scanWith(t, cfg, cachePath) // populate
	warm := scanWith(t, cfg, cachePath)

	if warm.Collector.Progress().CacheHits == 0 {
		t.Error("a second scan of an untouched tree hit the cache zero times")
	}
	// The whole promise of the cache: same numbers, less work.
	if got, want := shape(t, warm), shape(t, cold); got != want {
		t.Errorf("warm scan differs from cold:\n%s\n---\n%s", got, want)
	}
}

func TestAChangedDirectoryIsRescanned(t *testing.T) {
	root := incrementalTree(t)
	cachePath := filepath.Join(t.TempDir(), "cache", "tree.sqlite")
	cfg := incrementalConfig(root)

	scanWith(t, cfg, cachePath)

	writeSized(t, filepath.Join(root, "media", "new.mov"), 128*kbLocal)

	warm := scanWith(t, cfg, cachePath)
	control := scanWith(t, cfg, "")

	if got, want := shape(t, warm), shape(t, control); got != want {
		t.Errorf("warm scan missed the change:\n%s\n---\n%s", got, want)
	}
	if warm.Collector.Totals().Files != 5 {
		t.Errorf("files = %d, want 5", warm.Collector.Totals().Files)
	}
	// Only the touched directory should have missed.
	if warm.Collector.Progress().CacheHits == 0 {
		t.Error("one changed directory invalidated every row")
	}
}

func TestADeletedFileIsNoticed(t *testing.T) {
	root := incrementalTree(t)
	cachePath := filepath.Join(t.TempDir(), "cache", "tree.sqlite")
	cfg := incrementalConfig(root)

	scanWith(t, cfg, cachePath)
	if err := os.Remove(filepath.Join(root, "media", "photo.jpg")); err != nil {
		t.Fatal(err)
	}

	warm := scanWith(t, cfg, cachePath)
	control := scanWith(t, cfg, "")

	if got, want := shape(t, warm), shape(t, control); got != want {
		t.Errorf("warm scan kept a deleted file:\n%s\n---\n%s", got, want)
	}
	if warm.Collector.Totals().Files != 3 {
		t.Errorf("files = %d, want 3", warm.Collector.Totals().Files)
	}
}

func TestADeletedDirectoryDropsOutOfTheTreeAndTheCache(t *testing.T) {
	root := incrementalTree(t)
	cachePath := filepath.Join(t.TempDir(), "cache", "tree.sqlite")
	cfg := incrementalConfig(root)

	scanWith(t, cfg, cachePath)
	if err := os.RemoveAll(filepath.Join(root, "code", "deep")); err != nil {
		t.Fatal(err)
	}

	warm := scanWith(t, cfg, cachePath)
	control := scanWith(t, cfg, "")

	if got, want := shape(t, warm), shape(t, control); got != want {
		t.Errorf("warm scan kept a deleted directory:\n%s\n---\n%s", got, want)
	}

	store, _, err := cache.Open(cachePath, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	remaining, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for path := range remaining {
		if strings.HasSuffix(path, "/deep") {
			t.Errorf("the cache kept a row for the deleted %s", path)
		}
	}
}

// Files can grow in place without touching their directory's mtime, so a
// directory holding anything above the reporting floor is always rescanned.
// This is the mitigation for the cache's one documented blind spot.
func TestADirectoryHoldingALargeFileIsNeverTrusted(t *testing.T) {
	root := incrementalTree(t)
	cachePath := filepath.Join(t.TempDir(), "cache", "tree.sqlite")

	cfg := incrementalConfig(root)
	cfg.MinFileSize = 100 * kbLocal

	scanWith(t, cfg, cachePath)

	store, _, err := cache.Open(cachePath, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	media := rows[filepath.Join(root, "media")]
	if media == nil || !media.Volatile {
		t.Fatal("the directory holding a 256 KB file was not marked volatile")
	}

	// Grow the large file in place: its size changes, the directory's does not.
	writeSized(t, filepath.Join(root, "media", "clip.mov"), 512*kbLocal)

	warm := scanWith(t, cfg, cachePath)
	control := scanWith(t, cfg, "")

	if warm.Collector.Totals().Bytes != control.Collector.Totals().Bytes {
		t.Errorf("warm total = %d, cold total = %d — the growth was missed",
			warm.Collector.Totals().Bytes, control.Collector.Totals().Bytes)
	}
}

// A subtree scan never looked at the rest of the tree, so it cannot conclude
// those directories are gone.
func TestScanningASubtreeLeavesOtherRowsAlone(t *testing.T) {
	root := incrementalTree(t)
	cachePath := filepath.Join(t.TempDir(), "cache", "tree.sqlite")

	scanWith(t, incrementalConfig(root), cachePath)

	subtree := incrementalConfig(root)
	subtree.Roots = []string{filepath.Join(root, "code")}
	scanWith(t, subtree, cachePath)

	store, _, err := cache.Open(cachePath, subtree)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	rows, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if rows[filepath.Join(root, "media")] == nil {
		t.Error("scanning code/ discarded the row for media/")
	}
}
