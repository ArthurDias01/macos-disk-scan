package scan

import (
	"os"
	"path/filepath"
	"testing"

	"disk-report/internal/aggregate"
	"disk-report/internal/config"
	"disk-report/internal/duplicates"
	"disk-report/internal/fixture"
)

const mb = 1024 * 1024

// clones builds a tree holding one 2 MB file three times, in two directories:
//
//	root/
//	  a.mp4          2 MB
//	  b.mp4          2 MB  (identical)
//	  media/
//	    c.mp4        2 MB  (identical)
//	    only.mp4     3 MB  (unique)
func clones(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	content := func(size, seed int) []byte {
		buffer := make([]byte, size)
		for i := 0; i < size; i += 4096 {
			buffer[i] = byte((seed + i) % 251)
		}
		return buffer
	}
	write := func(path string, size, seed int) {
		t.Helper()
		if err := os.WriteFile(path, content(size, seed), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(root, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(root, "a.mp4"), 2*mb, 5)
	write(filepath.Join(root, "b.mp4"), 2*mb, 5)
	write(filepath.Join(root, "media", "c.mp4"), 2*mb, 5)
	write(filepath.Join(root, "media", "only.mp4"), 3*mb, 9)

	return root
}

func walkClones(t *testing.T) (*WalkResult, duplicates.Result) {
	t.Helper()

	cfg := config.Defaults()
	cfg.Roots = []string{clones(t)}
	cfg.MinFileSize = 0
	cfg.MinFolderSize = 0
	cfg.WorkerCount = 2
	cfg.DuplicateMinSize = 1 * mb

	result, err := Walk(Options{Config: cfg, HomeDir: "/Users/nobody"})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	found := duplicates.Detect(result.Collector.Candidates(), duplicates.Options{
		MinSize:    cfg.DuplicateMinSize,
		Classifier: result.Classifier,
	})
	result.Collector.ApplyDuplicates(found)

	return result, found
}

func TestSecondPassNeverRewritesSizes(t *testing.T) {
	result, found := walkClones(t)
	totals := result.Collector.Totals()

	if len(found.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(found.Groups))
	}
	copySize := found.Groups[0].Size

	// Allocated is what the filesystem bills, and it must keep matching du even
	// when two thirds of it is redundant. Nine megabytes went to disk.
	if totals.Bytes < 9*mb {
		t.Errorf("allocated = %d, want at least %d", totals.Bytes, 9*mb)
	}
	if totals.DuplicateFiles != 2 {
		t.Errorf("duplicate files = %d, want 2", totals.DuplicateFiles)
	}
	if totals.DuplicateBytes != 2*copySize {
		t.Errorf("duplicate bytes = %d, want %d", totals.DuplicateBytes, 2*copySize)
	}
	// Unique is the floor of what is really stored, not a corrected allocation.
	if totals.UniqueBytes != totals.Bytes-totals.DuplicateBytes {
		t.Errorf("unique %d != allocated %d - redundant %d",
			totals.UniqueBytes, totals.Bytes, totals.DuplicateBytes)
	}
}

func TestSecondPassAnnotatesEveryMember(t *testing.T) {
	result, _ := walkClones(t)

	members, copies := 0, 0
	for _, entry := range result.Collector.Entries() {
		if entry.DuplicateGroup == nil {
			continue
		}
		members++
		if *entry.DuplicateCopies != 3 {
			t.Errorf("%s reports %d copies, want 3", entry.Path, *entry.DuplicateCopies)
		}
		if *entry.IsDuplicateCopy {
			copies++
		}
	}

	if members != 3 {
		t.Errorf("annotated %d entries, want 3", members)
	}
	// Exactly one member of the group is the original.
	if copies != 2 {
		t.Errorf("%d entries marked as redundant copies, want 2", copies)
	}
}

func TestRedundantBytesAreAttributedPerDirectoryAndRollUp(t *testing.T) {
	result, found := walkClones(t)
	collector := result.Collector
	want := found.DuplicateBytes

	root := result.ValidRoots[0]
	media := filepath.Join(root, "media")

	// The two redundant copies are split across the two directories, whichever
	// member the path sort chose as the original.
	attributed := collector.Dirs()[root].DuplicateOwnSize + collector.Dirs()[media].DuplicateOwnSize
	if attributed != want {
		t.Errorf("attributed %d redundant bytes, want %d", attributed, want)
	}
	if collector.Dirs()[media].DuplicateOwnSize == 0 {
		t.Error("media holds a redundant copy but was attributed none")
	}

	// The rollup is what lets a parent folder show a unique reading.
	tree := aggregate.BuildFolderTree(collector.Dirs(), result.ValidRoots, 0)
	if tree.DuplicateRecursiveSize != want {
		t.Errorf("tree rolls up %d redundant bytes, want %d", tree.DuplicateRecursiveSize, want)
	}
	if tree.DuplicateOwnSize >= tree.DuplicateRecursiveSize {
		t.Error("the root's own redundant bytes should be less than its subtree's")
	}
}

func TestFingerprintsAreStoredOnCandidatesForTheCache(t *testing.T) {
	result, found := walkClones(t)

	stored := 0
	for _, delta := range result.Collector.FreshDeltas() {
		for _, candidate := range delta.Candidates {
			if candidate.Fingerprint != nil {
				stored++
				if *candidate.Fingerprint != found.FingerprintOf[candidate.Path] {
					t.Errorf("%s stored the wrong fingerprint", candidate.Path)
				}
			}
		}
	}

	// The three identical files were fingerprinted; the unique 3 MB file never
	// collided on size, so it was never read and has nothing to store.
	if stored != 3 {
		t.Errorf("stored %d fingerprints, want 3", stored)
	}
}

// Fingerprinting opens the candidate. A .photoslibrary is a directory, so
// offering it up only ever produced a failed open.
func TestBundlesAreNeverDuplicateCandidates(t *testing.T) {
	root := fixture.Build(t)

	cfg := config.Defaults()
	cfg.Roots = []string{root}
	cfg.MinFileSize = 0
	cfg.MinFolderSize = 0
	cfg.WorkerCount = 2
	// Low enough that the 96 KB Sample.app would qualify on size alone.
	cfg.DuplicateMinSize = 64 * fixture.KB

	result, err := Walk(Options{Config: cfg, HomeDir: "/Users/nobody"})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	for _, candidate := range result.Collector.Candidates() {
		if filepath.Base(candidate.Path) == "Sample.app" {
			t.Fatal("a bundle was offered for fingerprinting")
		}
	}
}

func TestDetectionOffLeavesTotalsUntouched(t *testing.T) {
	cfg := config.Defaults()
	cfg.Roots = []string{clones(t)}
	cfg.MinFileSize = 0
	cfg.MinFolderSize = 0
	cfg.WorkerCount = 2
	cfg.DetectDuplicates = false

	result, err := Walk(Options{Config: cfg, HomeDir: "/Users/nobody"})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	result.Collector.ApplyDuplicates(duplicates.Empty())

	totals := result.Collector.Totals()
	if len(result.Collector.Candidates()) != 0 {
		t.Errorf("collected %d candidates with detection off", len(result.Collector.Candidates()))
	}
	if totals.DuplicateBytes != 0 || totals.UniqueBytes != totals.Bytes {
		t.Errorf("unique %d / redundant %d, want %d / 0",
			totals.UniqueBytes, totals.DuplicateBytes, totals.Bytes)
	}
}
