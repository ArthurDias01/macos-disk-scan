package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"disk-report/internal/aggregate"
	"disk-report/internal/config"
	"disk-report/internal/fixture"
	"disk-report/internal/schema"
)

func walkFixture(t *testing.T, root string, minFileSize, minFolderSize int64) *WalkResult {
	t.Helper()

	cfg := config.Defaults()
	cfg.Roots = []string{root}
	cfg.MinFileSize = minFileSize
	cfg.MinFolderSize = minFolderSize
	cfg.WorkerCount = 2

	result, err := Walk(Options{Config: cfg, HomeDir: "/Users/nobody"})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return result
}

func TestCountsEveryFileOnceWithTheHardlinkPairCountedOnce(t *testing.T) {
	result := walkFixture(t, fixture.Build(t), 0, 0)
	totals := result.Collector.Totals()

	// 6 loose files + 1 bundle + 1 hardlink survivor + 2 nested = 10.
	if totals.Files != 10 {
		t.Errorf("files = %d, want 10", totals.Files)
	}
	if totals.DedupedFiles != 1 {
		t.Errorf("deduped files = %d, want 1", totals.DedupedFiles)
	}
	if totals.DedupedBytes < 64*fixture.KB {
		t.Errorf("deduped bytes = %d, want at least %d", totals.DedupedBytes, 64*fixture.KB)
	}
	if totals.Dirs != 3 {
		t.Errorf("dirs = %d, want 3", totals.Dirs)
	}
}

func TestReportsTheHardlinkTwinAsFreeingNothing(t *testing.T) {
	result := walkFixture(t, fixture.Build(t), 0, 0)

	var bins, duplicates int
	for _, entry := range result.Collector.Entries() {
		if strings.HasSuffix(entry.Path, ".bin") {
			bins++
		}
		if entry.IsDupInode {
			duplicates++
			if !strings.HasSuffix(entry.Path, ".bin") {
				t.Errorf("unexpected dup inode: %s", entry.Path)
			}
		}
	}

	// Both paths are reported. Only one of them owns the bytes.
	if bins != 2 {
		t.Errorf("reported %d .bin files, want 2", bins)
	}
	if duplicates != 1 {
		t.Errorf("marked %d entries as dup inodes, want 1", duplicates)
	}
}

func TestNeverFollowsSymlinks(t *testing.T) {
	result := walkFixture(t, fixture.Build(t), 0, 0)
	for _, entry := range result.Collector.Entries() {
		if strings.HasSuffix(entry.Path, "shortcut.mp4") {
			t.Fatal("symlink was followed")
		}
	}
}

func TestExtensionStatsReconcileWithTheTotals(t *testing.T) {
	result := walkFixture(t, fixture.Build(t), 0, 0)
	collector := result.Collector

	extensions := aggregate.BuildExtensionStats(collector.Extensions(), result.Classifier, nil)

	var summed int64
	byExt := map[string]schema.ExtensionStat{}
	for _, stat := range extensions {
		summed += stat.TotalSize
		byExt[stat.Ext] = stat
	}

	// A rollup that double-counts still looks convincing in a chart; this is
	// the check that catches it.
	if summed != collector.Totals().Bytes {
		t.Errorf("extensions sum to %d, totals say %d", summed, collector.Totals().Bytes)
	}

	for _, ext := range []string{"mp4", "tar.gz", "app", schema.NoExtension} {
		if _, ok := byExt[ext]; !ok {
			t.Errorf("missing extension %q", ext)
		}
	}
	// Nothing inside the bundle reaches the aggregates, and no invented types.
	for _, ext := range []string{"png", "gz", "zshrc"} {
		if _, ok := byExt[ext]; ok {
			t.Errorf("unexpected extension %q", ext)
		}
	}

	mp4 := byExt["mp4"]
	if mp4.FileCount != 2 {
		t.Errorf("mp4 count = %d, want 2", mp4.FileCount)
	}
	if mp4.LargestPath != filepath.Join(result.ValidRoots[0], "big.mp4") {
		t.Errorf("mp4 largest = %s", mp4.LargestPath)
	}
}

func TestFolderTreeReconcilesWithTheTotals(t *testing.T) {
	root := fixture.Build(t)
	result := walkFixture(t, root, 0, 0)
	collector := result.Collector

	tree := aggregate.BuildFolderTree(collector.Dirs(), result.ValidRoots, 0)

	if tree.RecursiveSize != collector.Totals().Bytes {
		t.Errorf("tree = %d bytes, totals say %d", tree.RecursiveSize, collector.Totals().Bytes)
	}

	nested := childNamed(tree, "nested")
	if nested == nil {
		t.Fatal("no nested folder")
	}
	deeper := childNamed(*nested, "deeper")
	if deeper == nil {
		t.Fatal("no deeper folder")
	}

	if nested.RecursiveSize <= nested.OwnSize {
		t.Error("recursive size must exceed own size when a folder has children")
	}
	if nested.RecursiveSize < nested.OwnSize+deeper.RecursiveSize {
		t.Errorf("nested recursive %d < own %d + child %d",
			nested.RecursiveSize, nested.OwnSize, deeper.RecursiveSize)
	}
	if deeper.OwnSize < 8*fixture.KB {
		t.Errorf("deeper own size = %d, want at least %d", deeper.OwnSize, 8*fixture.KB)
	}

	// The folder's mtime is the freshest thing anywhere beneath it.
	info, err := os.Lstat(filepath.Join(root, "nested", "deeper", "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	deepMtimeMs := float64(info.ModTime().UnixNano()) / 1e6
	if nested.MaxMtimeMs < deepMtimeMs-1 {
		t.Errorf("nested mtime %v is older than its deepest file %v", nested.MaxMtimeMs, deepMtimeMs)
	}
}

func TestMinFileSizeFiltersRowsWithoutChangingTotals(t *testing.T) {
	root := fixture.Build(t)
	full := walkFixture(t, root, 0, 0).Collector
	filtered := walkFixture(t, root, 128*fixture.KB, 0).Collector

	for _, entry := range filtered.Entries() {
		if entry.Size < 128*fixture.KB {
			t.Errorf("%s is below the floor at %d bytes", entry.Path, entry.Size)
		}
	}
	// Filtered rows still count everywhere else.
	if filtered.Totals().Files != full.Totals().Files {
		t.Errorf("files = %d, want %d", filtered.Totals().Files, full.Totals().Files)
	}
	if filtered.Totals().Bytes != full.Totals().Bytes {
		t.Errorf("bytes = %d, want %d", filtered.Totals().Bytes, full.Totals().Bytes)
	}
}

func TestMinFolderSizePrunesIntoTruncatedTotals(t *testing.T) {
	root := fixture.Build(t)
	result := walkFixture(t, root, 0, 512*fixture.KB)
	collector := result.Collector

	tree := aggregate.BuildFolderTree(collector.Dirs(), result.ValidRoots, 512*fixture.KB)

	if tree.RecursiveSize != collector.Totals().Bytes {
		t.Errorf("tree = %d bytes, totals say %d", tree.RecursiveSize, collector.Totals().Bytes)
	}

	var listed int64
	for _, child := range tree.Children {
		listed += child.RecursiveSize
	}
	// A folder accounts for its full weight even when its small children are
	// not listed.
	if listed+tree.TruncatedSize+tree.OwnSize != tree.RecursiveSize {
		t.Errorf("listed %d + truncated %d + own %d != recursive %d",
			listed, tree.TruncatedSize, tree.OwnSize, tree.RecursiveSize)
	}
}

// A configured root with a trailing slash used to produce "//" in every
// descendant path, so the folder tree keyed on different strings than the file
// entries did and the tree silently lost its subtrees.
func TestRootsAreCleanedBeforeTheWalk(t *testing.T) {
	root := fixture.Build(t)

	plain := walkFixture(t, root, 0, 0)
	slashed := walkFixture(t, root+"/", 0, 0)

	if slashed.ValidRoots[0] != root {
		t.Errorf("root = %q, want %q", slashed.ValidRoots[0], root)
	}
	if got, want := slashed.Collector.Totals(), plain.Collector.Totals(); got != want {
		t.Errorf("totals = %+v, want %+v", got, want)
	}

	tree := aggregate.BuildFolderTree(slashed.Collector.Dirs(), slashed.ValidRoots, 0)
	if tree.RecursiveSize != slashed.Collector.Totals().Bytes {
		t.Errorf("tree = %d bytes, totals say %d", tree.RecursiveSize, slashed.Collector.Totals().Bytes)
	}
	if childNamed(tree, "nested") == nil {
		t.Error("nested subtree is missing from the tree")
	}
}

func TestWalkFailsWhenNoRootIsReadable(t *testing.T) {
	cfg := config.Defaults()
	cfg.Roots = []string{filepath.Join(t.TempDir(), "does-not-exist")}

	if _, err := Walk(Options{Config: cfg}); err != ErrNoRoots {
		t.Errorf("err = %v, want %v", err, ErrNoRoots)
	}
}

func childNamed(node schema.FolderNode, name string) *schema.FolderNode {
	for i := range node.Children {
		if node.Children[i].Name == name {
			return &node.Children[i]
		}
	}
	return nil
}
