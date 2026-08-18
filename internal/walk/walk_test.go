package walk

import (
	"os"
	"path/filepath"
	"testing"

	"disk-report/internal/config"
	"disk-report/internal/extension"
	"disk-report/internal/fixture"
	"disk-report/internal/schema"
)

// collect runs the pool over a root and returns every directory's delta, keyed
// by path. It stands in for the aggregation the collector will do.
func collect(t *testing.T, root string, cfg schema.ScanConfig) map[string]*DirDelta {
	t.Helper()

	info, err := os.Lstat(root)
	if err != nil {
		t.Fatalf("lstat root: %v", err)
	}
	stat, ok := statOf(info)
	if !ok {
		t.Fatal("no stat_t for root")
	}

	scanner := NewScanner(
		cfg,
		extension.New(cfg, "/Users/nobody"),
		config.VolatileExtensions(),
		map[int64]bool{stat.Dev: true},
	)

	queue := []string{root}
	deltas := map[string]*DirDelta{}

	Run(scanner, int(cfg.WorkerCount), Handler{
		Next: func() (string, bool) {
			if len(queue) == 0 {
				return "", false
			}
			next := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			return next, true
		},
		OnScanned: func(delta *DirDelta) {
			deltas[delta.Path] = delta
			for _, name := range delta.SubdirNames {
				queue = append(queue, filepath.Join(delta.Path, name))
			}
		},
		OnUnchanged: func(string) {},
	})

	return deltas
}

func fixtureConfig(root string, minFileSize int64) schema.ScanConfig {
	cfg := config.Defaults()
	cfg.Roots = []string{root}
	cfg.MinFileSize = minFileSize
	cfg.MinFolderSize = 0
	cfg.WorkerCount = 2
	return cfg
}

func TestWalkVisitsEveryDirectoryButNotBundles(t *testing.T) {
	root := fixture.Build(t)
	deltas := collect(t, root, fixtureConfig(root, 0))

	want := []string{root, filepath.Join(root, "nested"), filepath.Join(root, "nested", "deeper")}
	for _, path := range want {
		if deltas[path] == nil {
			t.Errorf("no delta for %s", path)
		}
	}
	// The bundle is summed in place, never queued as a directory of its own.
	if len(deltas) != len(want) {
		t.Errorf("visited %d directories, want %d", len(deltas), len(want))
	}
}

func TestWalkHoldsBackHardlinksAndSkipsSymlinks(t *testing.T) {
	root := fixture.Build(t)
	rootDelta := collect(t, root, fixtureConfig(root, 0))[root]

	// 6 ordinary files plus the bundle. The hardlink pair is excluded here:
	// only the collector knows which path already claimed the inode.
	if rootDelta.OwnFileCount != 7 {
		t.Errorf("OwnFileCount = %d, want 7", rootDelta.OwnFileCount)
	}
	if len(rootDelta.Hardlinks) != 2 {
		t.Fatalf("Hardlinks = %d, want 2", len(rootDelta.Hardlinks))
	}
	if rootDelta.Hardlinks[0].Ino != rootDelta.Hardlinks[1].Ino {
		t.Error("hardlink pair does not share an inode")
	}

	for _, entry := range rootDelta.Entries {
		if filepath.Base(entry.Path) == "shortcut.mp4" {
			t.Error("symlink was followed")
		}
	}
}

func TestWalkTreatsBundlesAsAtomic(t *testing.T) {
	root := fixture.Build(t)
	rootDelta := collect(t, root, fixtureConfig(root, 0))[root]

	var bundle *schema.FileEntry
	for i, entry := range rootDelta.Entries {
		if entry.IsBundle {
			bundle = &rootDelta.Entries[i]
		}
		if filepath.Dir(entry.Path) != root {
			t.Errorf("entry from inside a bundle: %s", entry.Path)
		}
	}

	if bundle == nil {
		t.Fatal("no bundle entry")
	}
	if bundle.Path != filepath.Join(root, "Sample.app") {
		t.Errorf("bundle path = %s", bundle.Path)
	}
	if bundle.Ext != "app" {
		t.Errorf("bundle ext = %q, want app", bundle.Ext)
	}
	if bundle.Size < 96*fixture.KB {
		t.Errorf("bundle size = %d, want at least %d", bundle.Size, 96*fixture.KB)
	}
	// Nothing inside the bundle reaches the extension aggregates either.
	if _, ok := rootDelta.ExtDelta["png"]; ok {
		t.Error("png from inside the bundle reached the aggregates")
	}
}

func TestWalkNormalizesExtensions(t *testing.T) {
	root := fixture.Build(t)
	rootDelta := collect(t, root, fixtureConfig(root, 0))[root]

	for _, ext := range []string{"mp4", "tar.gz", "app", schema.NoExtension} {
		if _, ok := rootDelta.ExtDelta[ext]; !ok {
			t.Errorf("missing extension %q", ext)
		}
	}
	// Makefile, .zshrc and the two .bin files must not invent types.
	for _, ext := range []string{"gz", "zshrc"} {
		if _, ok := rootDelta.ExtDelta[ext]; ok {
			t.Errorf("unexpected extension %q", ext)
		}
	}

	mp4 := rootDelta.ExtDelta["mp4"]
	if mp4.Count != 2 {
		t.Errorf("mp4 count = %d, want 2", mp4.Count)
	}
	if mp4.MaxName != "big.mp4" {
		t.Errorf("mp4 largest = %q, want big.mp4", mp4.MaxName)
	}
}

func TestMinFileSizeKeepsSmallFilesOutOfEntries(t *testing.T) {
	root := fixture.Build(t)
	deltas := collect(t, root, fixtureConfig(root, 128*fixture.KB))

	for _, delta := range deltas {
		for _, entry := range delta.Entries {
			if entry.Size < 128*fixture.KB {
				t.Errorf("%s is below the floor at %d bytes", entry.Path, entry.Size)
			}
		}
	}

	// Filtered rows still count in the directory's own totals.
	if got := deltas[root].OwnFileCount; got != 7 {
		t.Errorf("OwnFileCount = %d, want 7", got)
	}
}

// A directory is volatile when something in it can change size without moving
// the directory's own mtime — the one case a cache hit would get wrong.
func TestVolatileMarksDirectoriesTheCacheCannotTrust(t *testing.T) {
	root := fixture.Build(t)
	deltas := collect(t, root, fixtureConfig(root, 128*fixture.KB))

	if !deltas[root].Volatile {
		t.Error("root holds a bundle and a 512 KB file, want volatile")
	}
	deep := deltas[filepath.Join(root, "nested", "deeper")]
	if deep.Volatile {
		t.Error("a directory of one 8 KB json is not volatile")
	}
}

func TestExcludePathsSkipsSubtrees(t *testing.T) {
	root := fixture.Build(t)
	cfg := fixtureConfig(root, 0)
	cfg.ExcludePaths = []string{filepath.Join(root, "nested")}

	deltas := collect(t, root, cfg)

	if len(deltas) != 1 {
		t.Errorf("visited %d directories, want 1", len(deltas))
	}
	if len(deltas[root].SubdirNames) != 0 {
		t.Errorf("SubdirNames = %v, want none", deltas[root].SubdirNames)
	}
}

func TestDuplicateCandidatesRespectTheFloor(t *testing.T) {
	root := fixture.Build(t)
	cfg := fixtureConfig(root, 0)
	cfg.DuplicateMinSize = 128 * fixture.KB

	rootDelta := collect(t, root, cfg)[root]

	// Only big.mp4 clears 128 KB. The 96 KB bundle, the small files and the
	// hardlink pair are all below it or held back.
	if len(rootDelta.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1 (big.mp4)", len(rootDelta.Candidates))
	}
	for _, candidate := range rootDelta.Candidates {
		if candidate.Size < 128*fixture.KB {
			t.Errorf("%s is below the fingerprint floor", candidate.Path)
		}
	}
}
