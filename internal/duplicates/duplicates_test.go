package duplicates

import (
	"os"
	"path/filepath"
	"testing"

	"disk-report/internal/config"
	"disk-report/internal/extension"
	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

const mb = 1024 * 1024

// content is deterministic filler, so identical calls produce identical bytes.
// It matches the generator in scanner/duplicates.test.ts byte for byte.
func content(size, seed int) []byte {
	buffer := make([]byte, size)
	for i := 0; i < size; i += 4096 {
		buffer[i] = byte((seed + i) % 251)
	}
	return buffer
}

type tree struct {
	root       string
	original   string
	copy1      string
	copy2      string
	sameSize   string
	unique     string
	tooSmall   string
	candidates []walk.CandidateRecord
}

func build(t *testing.T) tree {
	t.Helper()
	root := t.TempDir()

	write := func(name string, size, seed int) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, content(size, seed), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return path
	}

	fixture := tree{root: root}
	fixture.original = write("video-a.mp4", 3*mb, 7)
	fixture.copy1 = write("video-b.mp4", 3*mb, 7)
	fixture.copy2 = write("nested-copy.mp4", 3*mb, 7)
	// Same size, different bytes: the case a size-only heuristic gets wrong.
	fixture.sameSize = write("other.mp4", 3*mb, 99)
	fixture.unique = write("unique.mp4", 5*mb, 3)
	fixture.tooSmall = write("small.mp4", 16*1024, 7)

	fixture.candidates = []walk.CandidateRecord{
		{Path: fixture.original, Size: 3 * mb},
		{Path: fixture.copy1, Size: 3 * mb},
		{Path: fixture.copy2, Size: 3 * mb},
		{Path: fixture.sameSize, Size: 3 * mb},
		{Path: fixture.unique, Size: 5 * mb},
		{Path: fixture.tooSmall, Size: 16 * 1024},
	}
	return fixture
}

func options() Options {
	return Options{
		MinSize:    1 * mb,
		Classifier: extension.New(config.Defaults(), "/Users/nobody"),
	}
}

// The cache stores fingerprints, and either scanner may have written the row.
// A format change on one side would silently split groups on the other, so the
// exact string is pinned to what scanner/duplicates.ts produces.
func TestFingerprintMatchesTheTypeScriptScanner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.bin")
	if err := os.WriteFile(path, content(3*mb, 7), 0o644); err != nil {
		t.Fatal(err)
	}

	const want = "3145728-8917570357440472415-17631464822551067132"
	got, ok := Fingerprint(path, 3*mb)
	if !ok {
		t.Fatal("Fingerprint failed on a readable file")
	}
	if got != want {
		t.Errorf("fingerprint = %s, want %s", got, want)
	}
}

func TestFingerprintDistinguishesEqualSizedFiles(t *testing.T) {
	fixture := build(t)

	original, ok := Fingerprint(fixture.original, 3*mb)
	if !ok {
		t.Fatal("Fingerprint failed")
	}
	same, _ := Fingerprint(fixture.copy1, 3*mb)
	if original != same {
		t.Error("identical files produced different fingerprints")
	}

	different, _ := Fingerprint(fixture.sameSize, 3*mb)
	if original == different {
		t.Error("same size with different bytes collided")
	}
}

func TestFingerprintReportsUnreadablePathsRatherThanGuessing(t *testing.T) {
	if _, ok := Fingerprint(filepath.Join(t.TempDir(), "missing.mp4"), 1024); ok {
		t.Error("a missing path reported a fingerprint")
	}
}

func TestDetectGroupsIdenticalFilesAndCountsTheExtras(t *testing.T) {
	fixture := build(t)
	result := Detect(fixture.candidates, options())

	if len(result.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]

	if group.Count != 3 {
		t.Errorf("count = %d, want 3", group.Count)
	}
	if group.Size != 3*mb {
		t.Errorf("size = %d, want %d", group.Size, 3*mb)
	}
	if group.ReclaimableBytes != 6*mb {
		t.Errorf("reclaimable = %d, want %d", group.ReclaimableBytes, 6*mb)
	}
	if group.Ext != "mp4" || group.Category != schema.CategoryVideo {
		t.Errorf("type = %q/%q, want mp4/video", group.Ext, group.Category)
	}

	if result.DuplicateFiles != 2 || result.DuplicateBytes != 6*mb {
		t.Errorf("redundant = %d files / %d bytes, want 2 / %d",
			result.DuplicateFiles, result.DuplicateBytes, 6*mb)
	}
	if len(result.DuplicatePaths) != 2 {
		t.Errorf("duplicate paths = %d, want 2", len(result.DuplicatePaths))
	}
}

func TestDetectKeepsExactlyOneOriginalPerGroup(t *testing.T) {
	fixture := build(t)
	result := Detect(fixture.candidates, options())

	kept := 0
	for _, path := range []string{fixture.original, fixture.copy1, fixture.copy2} {
		if !result.DuplicatePaths[path] {
			kept++
		}
	}
	if kept != 1 {
		t.Errorf("%d members kept as originals, want 1", kept)
	}
}

func TestDetectAnnotatesEveryMemberIncludingTheOriginal(t *testing.T) {
	fixture := build(t)
	result := Detect(fixture.candidates, options())

	for _, path := range []string{fixture.original, fixture.copy2} {
		if result.Membership[path].Copies != 3 {
			t.Errorf("%s reports %d copies, want 3", path, result.Membership[path].Copies)
		}
	}
	if _, ok := result.Membership[fixture.unique]; ok {
		t.Error("a unique file was annotated with a group")
	}
}

func TestDetectLeavesUniqueAndSameSizeDifferentFilesAlone(t *testing.T) {
	fixture := build(t)
	result := Detect(fixture.candidates, options())

	if result.DuplicatePaths[fixture.unique] {
		t.Error("a unique file was marked redundant")
	}
	if result.DuplicatePaths[fixture.sameSize] {
		t.Error("a same-size file with different bytes was marked redundant")
	}
}

func TestDetectIgnoresFilesBelowTheFloor(t *testing.T) {
	fixture := build(t)

	small := filepath.Join(fixture.root, "small-copy.mp4")
	if err := os.WriteFile(small, content(16*1024, 7), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Detect([]walk.CandidateRecord{
		{Path: fixture.tooSmall, Size: 16 * 1024},
		{Path: small, Size: 16 * 1024},
	}, options())

	if len(result.Groups) != 0 {
		t.Errorf("groups = %d, want 0 — both files are below the floor", len(result.Groups))
	}
}

// The size-collision filter is what makes this pass affordable: it is the
// difference between a few thousand reads and a few million.
func TestDetectOnlyReadsFilesWhoseSizeCollides(t *testing.T) {
	fixture := build(t)

	probed := 0
	opts := options()
	opts.OnProgress = func(int, int) { probed++ }
	Detect(fixture.candidates, opts)

	// The four 3 MB files are suspects; the 5 MB and 16 KB files never open.
	if probed != 4 {
		t.Errorf("probed %d files, want 4", probed)
	}
}

func TestDetectOrdersGroupsByReclaimableBytes(t *testing.T) {
	fixture := build(t)

	bigA := filepath.Join(fixture.root, "big-a.mov")
	bigB := filepath.Join(fixture.root, "big-b.mov")
	for _, path := range []string{bigA, bigB} {
		if err := os.WriteFile(path, content(8*mb, 11), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result := Detect(append(fixture.candidates,
		walk.CandidateRecord{Path: bigA, Size: 8 * mb},
		walk.CandidateRecord{Path: bigB, Size: 8 * mb},
	), options())

	if len(result.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(result.Groups))
	}
	if result.Groups[0].ReclaimableBytes != 8*mb || result.Groups[1].ReclaimableBytes != 6*mb {
		t.Errorf("order = %d, %d, want %d, %d",
			result.Groups[0].ReclaimableBytes, result.Groups[1].ReclaimableBytes, 8*mb, 6*mb)
	}
}

// Re-reading an unchanged file is the most expensive thing a warm scan can do,
// so a cached fingerprint is used as-is.
func TestDetectReusesCachedFingerprintsWithoutReading(t *testing.T) {
	fixture := build(t)

	cached, ok := Fingerprint(fixture.original, 3*mb)
	if !ok {
		t.Fatal("Fingerprint failed")
	}

	candidates := []walk.CandidateRecord{
		{Path: fixture.original, Size: 3 * mb, Fingerprint: &cached},
		{Path: fixture.copy1, Size: 3 * mb, Fingerprint: &cached},
	}

	result := Detect(candidates, options())

	if result.Reused != 2 || result.Computed != 0 {
		t.Errorf("reused %d / computed %d, want 2 / 0", result.Reused, result.Computed)
	}
	if len(result.Groups) != 1 || result.Groups[0].Count != 2 {
		t.Errorf("groups = %v, want one group of 2", result.Groups)
	}
}

func TestEmptyResultIsUsableWithoutDetection(t *testing.T) {
	result := Empty()
	if result.Groups == nil || result.ByExtension == nil || result.Membership == nil {
		t.Error("Empty() must return usable maps and slices, not nils")
	}
}
