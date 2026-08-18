package aggregate

import (
	"testing"

	"disk-report/internal/config"
	"disk-report/internal/extension"
	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

func classifier() *extension.Classifier {
	return extension.New(config.Defaults(), "/Users/test")
}

// accum builds a global accumulator with every file in one bucket, which is
// enough for the rollups under test.
func accum(bytes, count, maxSize int64, maxPath string) *ExtAccum {
	histogram := schema.EmptyHistogram()
	histogram[schema.HistogramBucket(maxSize)] = count
	return &ExtAccum{
		Bytes:     bytes,
		Count:     count,
		MaxSize:   maxSize,
		MaxPath:   maxPath,
		Histogram: histogram,
	}
}

func file(path string, size int64, ext string) schema.FileEntry {
	return schema.FileEntry{Path: path, Ext: ext, Category: schema.CategoryOther, Size: size, Nlink: 1}
}

func TestMergeDirSumsAndKeepsTheLargest(t *testing.T) {
	extMap := ExtMap{}

	extMap.MergeDir("/media", walk.DirExtMap{
		"mp4": {Bytes: 100, Count: 2, Buckets: [][2]int64{{10, 2}}, MaxSize: 100, MaxName: "small.mp4"},
	})
	extMap.MergeDir("/archive", walk.DirExtMap{
		"mp4": {Bytes: 900, Count: 1, Buckets: [][2]int64{{20, 1}}, MaxSize: 900, MaxName: "big.mp4"},
		"zip": {Bytes: 10, Count: 1, Buckets: [][2]int64{{3, 1}}, MaxSize: 10, MaxName: "a.zip"},
	})

	mp4 := extMap["mp4"]
	if mp4.Bytes != 1000 || mp4.Count != 3 {
		t.Errorf("mp4 = %d bytes / %d files, want 1000 / 3", mp4.Bytes, mp4.Count)
	}
	// The basename is joined back onto the directory it came from.
	if mp4.MaxPath != "/archive/big.mp4" {
		t.Errorf("mp4 largest = %q, want /archive/big.mp4", mp4.MaxPath)
	}

	var counted int64
	for _, count := range mp4.Histogram {
		counted += count
	}
	if counted != 3 {
		t.Errorf("histogram holds %d files, want 3", counted)
	}
	if extMap["zip"].Bytes != 10 {
		t.Errorf("zip = %d bytes, want 10", extMap["zip"].Bytes)
	}
}

// Directories are merged in whatever order the pool finished them, so without a
// tiebreak "the largest .go file" would change between two scans of an
// unchanged tree.
func TestLargestPathTiesResolveOnPathNotArrival(t *testing.T) {
	first := ExtMap{}
	first.MergeDir("/z", walk.DirExtMap{"go": {Bytes: 100, Count: 1, MaxSize: 100, MaxName: "late.go"}})
	first.MergeDir("/a", walk.DirExtMap{"go": {Bytes: 100, Count: 1, MaxSize: 100, MaxName: "early.go"}})

	reversed := ExtMap{}
	reversed.MergeDir("/a", walk.DirExtMap{"go": {Bytes: 100, Count: 1, MaxSize: 100, MaxName: "early.go"}})
	reversed.MergeDir("/z", walk.DirExtMap{"go": {Bytes: 100, Count: 1, MaxSize: 100, MaxName: "late.go"}})

	if first["go"].MaxPath != "/a/early.go" || reversed["go"].MaxPath != "/a/early.go" {
		t.Errorf("merge order changed the answer: %q vs %q",
			first["go"].MaxPath, reversed["go"].MaxPath)
	}

	// The same tie, one file at a time.
	byFile := ExtMap{}
	byFile.AddFile(file("/z/late.go", 100, "go"))
	byFile.AddFile(file("/a/early.go", 100, "go"))
	if byFile["go"].MaxPath != "/a/early.go" {
		t.Errorf("AddFile tie = %q, want /a/early.go", byFile["go"].MaxPath)
	}
}

func TestAddFileAccountsLikeADirectoryDelta(t *testing.T) {
	extMap := ExtMap{}
	extMap.AddFile(file("/small.mp4", 100, "mp4"))
	extMap.AddFile(file("/big.mp4", 900, "mp4"))

	mp4 := extMap["mp4"]
	if mp4.Bytes != 1000 || mp4.Count != 2 {
		t.Errorf("mp4 = %d bytes / %d files, want 1000 / 2", mp4.Bytes, mp4.Count)
	}
	if mp4.MaxPath != "/big.mp4" {
		t.Errorf("mp4 largest = %q, want /big.mp4", mp4.MaxPath)
	}
}

func TestBuildExtensionStatsSortsAndAveragesRoundToBytes(t *testing.T) {
	stats := BuildExtensionStats(ExtMap{
		"zip": accum(100, 4, 100, "/x"),
		"mp4": accum(900, 3, 900, "/y"),
	}, classifier(), nil)

	if len(stats) != 2 || stats[0].Ext != "mp4" || stats[1].Ext != "zip" {
		t.Fatalf("order = %v, want [mp4 zip]", extsOf(stats))
	}
	if stats[0].MeanSize != 300 {
		t.Errorf("mean = %d, want 300", stats[0].MeanSize)
	}
	if stats[0].Category != schema.CategoryVideo {
		t.Errorf("category = %q, want video", stats[0].Category)
	}
}

func TestBuildCategoryStatsRollsUpAndDropsEmpties(t *testing.T) {
	extensions := BuildExtensionStats(ExtMap{
		"mp4": accum(900, 3, 900, "/a"),
		"mov": accum(100, 1, 100, "/b"),
		"zip": accum(50, 1, 50, "/c"),
	}, classifier(), nil)

	categories := BuildCategoryStats(extensions)

	video := findCategory(categories, schema.CategoryVideo)
	if video == nil {
		t.Fatal("no video category")
	}
	if video.TotalSize != 1000 || video.FileCount != 4 {
		t.Errorf("video = %d bytes / %d files, want 1000 / 4", video.TotalSize, video.FileCount)
	}
	// The extension list inherits the size ordering it arrived in.
	if len(video.Extensions) != 2 || video.Extensions[0] != "mp4" || video.Extensions[1] != "mov" {
		t.Errorf("video extensions = %v, want [mp4 mov]", video.Extensions)
	}
	if findCategory(categories, schema.CategoryAudio) != nil {
		t.Error("audio category has no files and should have been dropped")
	}
}

func TestSelectTopFilesCapsPerExtensionThenGlobally(t *testing.T) {
	entries := []schema.FileEntry{
		file("/a.mp4", 500, "mp4"),
		file("/b.mp4", 400, "mp4"),
		file("/c.mp4", 300, "mp4"),
		file("/d.zip", 450, "zip"),
		file("/e.zip", 350, "zip"),
	}

	kept := SelectTopFiles(entries, 2, 10)
	assertPaths(t, kept, "/a.mp4", "/d.zip", "/b.mp4", "/e.zip")

	assertPaths(t, SelectTopFiles(entries, 2, 2), "/a.mp4", "/d.zip")
}

// fixtureDirs builds:
//
//	/root            own 10,   1 file,  mtime 100
//	  big            own 1000, 2 files, mtime 500
//	    deep         own 2000, 3 files, mtime 900
//	  small          own 5,    1 file,  mtime 50
func fixtureDirs() Dirs {
	dirs := Dirs{}
	dirs.Ensure("/root", false)
	for _, path := range []string{"/root/big", "/root/small", "/root/big/deep"} {
		dirs.LinkChild(path, false)
	}

	set := func(path string, ownSize, files int64, mtime float64) {
		dir := dirs[path]
		dir.OwnSize = ownSize
		dir.OwnFileCount = files
		dir.MaxMtimeMs = mtime
	}
	set("/root", 10, 1, 100)
	set("/root/big", 1000, 2, 500)
	set("/root/big/deep", 2000, 3, 900)
	set("/root/small", 5, 1, 50)
	return dirs
}

func TestFolderTreeSeparatesRecursiveFromOwn(t *testing.T) {
	node := BuildFolderTree(fixtureDirs(), []string{"/root"}, 0)

	if node.RecursiveSize != 3015 {
		t.Errorf("recursive = %d, want 3015", node.RecursiveSize)
	}
	if node.OwnSize != 10 {
		t.Errorf("own = %d, want 10", node.OwnSize)
	}
	if node.FileCount != 7 {
		t.Errorf("files = %d, want 7", node.FileCount)
	}
	if node.OwnFileCount != 1 {
		t.Errorf("own files = %d, want 1", node.OwnFileCount)
	}
	// The folder's activity signal is the freshest thing anywhere beneath it.
	if node.MaxMtimeMs != 900 {
		t.Errorf("mtime = %v, want 900", node.MaxMtimeMs)
	}
}

func TestFolderTreePrunesSmallChildrenWithoutLosingTheirWeight(t *testing.T) {
	node := BuildFolderTree(fixtureDirs(), []string{"/root"}, 100)

	if len(node.Children) != 1 || node.Children[0].Path != "/root/big" {
		t.Fatalf("children = %v, want [/root/big]", pathsOf(node.Children))
	}
	if node.TruncatedChildCount != 1 || node.TruncatedSize != 5 {
		t.Errorf("truncated = %d children / %d bytes, want 1 / 5", node.TruncatedChildCount, node.TruncatedSize)
	}
	// Pruning never changes the weight the folder accounts for.
	if node.RecursiveSize != 3015 {
		t.Errorf("recursive = %d, want 3015", node.RecursiveSize)
	}
}

func TestFolderTreeSortsChildrenBiggestFirst(t *testing.T) {
	dirs := fixtureDirs()
	dirs.LinkChild("/root/medium", false)
	dirs["/root/medium"].OwnSize = 2500

	node := BuildFolderTree(dirs, []string{"/root"}, 0)
	assertNodePaths(t, node.Children, "/root/big", "/root/medium", "/root/small")
}

func TestFolderTreeWrapsMultipleRoots(t *testing.T) {
	dirs := fixtureDirs()
	other := dirs.Ensure("/other", false)
	other.OwnSize = 42
	other.OwnFileCount = 1

	node := BuildFolderTree(dirs, []string{"/root", "/other"}, 0)

	if node.Path != "" || node.Name != "roots" {
		t.Errorf("synthetic root = %q/%q, want \"\"/roots", node.Path, node.Name)
	}
	if node.RecursiveSize != 3057 {
		t.Errorf("recursive = %d, want 3057", node.RecursiveSize)
	}
	if len(node.Children) != 2 {
		t.Errorf("children = %d, want 2", len(node.Children))
	}
}

// A directory discovered but never scanned still needs a parent, or the tree
// has a hole where its weight should be.
func TestLinkOrphansAttachesUnscannedDirectories(t *testing.T) {
	dirs := fixtureDirs()
	orphan := dirs.Ensure("/root/big/excluded", false)
	orphan.OwnSize = 7

	dirs.LinkOrphans([]string{"/root"})
	node := BuildFolderTree(dirs, []string{"/root"}, 0)

	if node.RecursiveSize != 3022 {
		t.Errorf("recursive = %d, want 3022 — the orphan's 7 bytes are missing", node.RecursiveSize)
	}
}

func TestDuplicateBytesRollUpThroughExtensionsAndCategories(t *testing.T) {
	extensions := BuildExtensionStats(ExtMap{
		"mp4": accum(900, 3, 900, "/a"),
		"zip": accum(100, 2, 100, "/b"),
	}, classifier(), map[string]int64{"mp4": 600})

	if extensions[0].DuplicateBytes != 600 || extensions[1].DuplicateBytes != 0 {
		t.Errorf("duplicate bytes = %d / %d, want 600 / 0",
			extensions[0].DuplicateBytes, extensions[1].DuplicateBytes)
	}

	video := findCategory(BuildCategoryStats(extensions), schema.CategoryVideo)
	if video.DuplicateBytes != 600 {
		t.Errorf("video duplicate bytes = %d, want 600", video.DuplicateBytes)
	}
	// The unique reading is what the charts subtract to.
	if video.TotalSize-video.DuplicateBytes != 300 {
		t.Errorf("video unique = %d, want 300", video.TotalSize-video.DuplicateBytes)
	}
}

func TestDuplicateBytesRollUpThroughTheFolderTree(t *testing.T) {
	dirs := Dirs{}
	dirs.Ensure("/root", false)
	dirs.LinkChild("/root/media", false)
	dirs.LinkChild("/root/media/clips", false)

	set := func(path string, ownSize, duplicateOwnSize int64) {
		dir := dirs[path]
		dir.OwnSize = ownSize
		dir.OwnFileCount = 1
		dir.DuplicateOwnSize = duplicateOwnSize
	}
	set("/root", 100, 0)
	set("/root/media", 500, 200)
	set("/root/media/clips", 900, 600)

	node := BuildFolderTree(dirs, []string{"/root"}, 0)

	if node.RecursiveSize != 1500 || node.DuplicateRecursiveSize != 800 {
		t.Errorf("root = %d bytes / %d redundant, want 1500 / 800",
			node.RecursiveSize, node.DuplicateRecursiveSize)
	}
	if node.DuplicateOwnSize != 0 {
		t.Errorf("root own redundant = %d, want 0", node.DuplicateOwnSize)
	}

	media := node.Children[0]
	if media.DuplicateRecursiveSize != 800 || media.DuplicateOwnSize != 200 {
		t.Errorf("media = %d recursive / %d own redundant, want 800 / 200",
			media.DuplicateRecursiveSize, media.DuplicateOwnSize)
	}
	if media.RecursiveSize-media.DuplicateRecursiveSize != 600 {
		t.Errorf("media unique = %d, want 600", media.RecursiveSize-media.DuplicateRecursiveSize)
	}
}

func findCategory(stats []schema.CategoryStat, want schema.Category) *schema.CategoryStat {
	for i := range stats {
		if stats[i].Category == want {
			return &stats[i]
		}
	}
	return nil
}

func extsOf(stats []schema.ExtensionStat) []string {
	out := make([]string, len(stats))
	for i, stat := range stats {
		out[i] = stat.Ext
	}
	return out
}

func pathsOf(nodes []schema.FolderNode) []string {
	out := make([]string, len(nodes))
	for i, node := range nodes {
		out[i] = node.Path
	}
	return out
}

func assertPaths(t *testing.T, entries []schema.FileEntry, want ...string) {
	t.Helper()
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, path := range want {
		if entries[i].Path != path {
			t.Errorf("entry %d = %s, want %s", i, entries[i].Path, path)
		}
	}
}

func assertNodePaths(t *testing.T, nodes []schema.FolderNode, want ...string) {
	t.Helper()
	got := pathsOf(nodes)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
