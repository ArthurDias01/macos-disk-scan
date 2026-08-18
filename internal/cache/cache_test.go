package cache

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"disk-report/internal/config"
	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

// The exact string scanner/cache.ts writes for the default config, read back
// out of a cache that scanner built.
//
// This is the whole of cross-scanner interop: if the two fingerprints differ,
// each scanner sees the other's cache as stale and wipes it, so alternating
// runs would both be cold and neither would ever say why.
const typescriptFingerprint = `{"minFileSize":104857600,"duplicateMinSize":1048576,` +
	`"detectDuplicates":true,"bundleExtensions":["aplibrary","app","appex","band",` +
	`"bundle","component","download","fcpbundle","framework","imovielibrary","kext",` +
	`"key","logicx","lrcat","numbers","pages","photolibrary","photoslibrary",` +
	`"playground","plugin","rtfd","scptd","sparsebundle","theater","xcodeproj",` +
	`"xcworkspace","xpc"],"compoundExtensions":["tar.br","tar.bz2","tar.gz",` +
	`"tar.lz4","tar.xz","tar.zst"],"excludePaths":[],"followSymlinks":false,` +
	`"crossFilesystems":false}`

func TestConfigFingerprintMatchesTheTypeScriptScanner(t *testing.T) {
	got, err := configFingerprint(config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if got != typescriptFingerprint {
		t.Errorf("fingerprint mismatch\n got: %s\nwant: %s", got, typescriptFingerprint)
	}
}

// JSON.stringify leaves `<`, `>` and `&` alone; Go's encoder escapes them by
// default. A single ampersand in an excluded path would otherwise reset the
// cache on every alternate run.
func TestConfigFingerprintDoesNotEscapeHTML(t *testing.T) {
	cfg := config.Defaults()
	cfg.ExcludePaths = []string{"/Users/x/R&D", "/Users/x/<tmp>"}

	got, err := configFingerprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"/Users/x/R&D"`, `"/Users/x/<tmp>"`} {
		if !strings.Contains(got, want) {
			t.Errorf("fingerprint escaped %s:\n%s", want, got)
		}
	}
}

func delta(path string) *walk.DirDelta {
	fingerprint := "123-456-789"
	return &walk.DirDelta{
		Path:         path,
		DirMtimeMs:   1700000000123.5,
		OwnSize:      4096,
		OwnFileCount: 2,
		MaxMtimeMs:   1700000000999.25,
		IsCloud:      true,
		Volatile:     true,
		SubdirNames:  []string{"child"},
		ExtDelta: walk.DirExtMap{
			"mp4": {Bytes: 4096, Count: 2, Buckets: [][2]int64{{12, 2}}, MaxSize: 2048, MaxName: "a.mp4"},
		},
		Entries: []schema.FileEntry{{
			Path: filepath.Join(path, "a.mp4"), Ext: "mp4", Category: schema.CategoryVideo,
			Size: 2048, MtimeMs: 1700000000999.25, Nlink: 1,
		}},
		Candidates: []walk.CandidateRecord{{
			Path: filepath.Join(path, "a.mp4"), Size: 2048,
			MtimeMs: 1700000000999.25, Fingerprint: &fingerprint,
		}},
		Hardlinks: []walk.HardlinkRecord{{
			Entry: schema.FileEntry{Path: filepath.Join(path, "b.bin"), Size: 1024, Nlink: 2},
			Dev:   16777232, Ino: 987654, Dir: path,
		}},
	}
}

func openTemp(t *testing.T, cfg schema.ScanConfig) (*Cache, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache", "scan.sqlite")

	cache, _, err := Open(path, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { cache.Close() })
	return cache, path
}

// A cache hit replays the stored row as if the walk had produced it, so every
// field has to survive the round trip intact.
func TestRowsRoundTrip(t *testing.T) {
	cache, _ := openTemp(t, config.Defaults())
	original := delta("/root/media")

	if err := cache.Save([]*walk.DirDelta{original}, map[string]bool{"/root/media": true}, []string{"/root"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := cache.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	row := loaded["/root/media"]
	if row == nil {
		t.Fatal("row is missing")
	}
	if row.DirMtimeMs != original.DirMtimeMs || row.MaxMtimeMs != original.MaxMtimeMs {
		t.Errorf("mtimes = %v/%v, want %v/%v",
			row.DirMtimeMs, row.MaxMtimeMs, original.DirMtimeMs, original.MaxMtimeMs)
	}
	if row.OwnSize != 4096 || row.OwnFileCount != 2 || !row.IsCloud || !row.Volatile {
		t.Errorf("scalars did not survive: %+v", row)
	}
	if len(row.SubdirNames) != 1 || row.SubdirNames[0] != "child" {
		t.Errorf("subdirs = %v", row.SubdirNames)
	}
	if row.ExtDelta["mp4"].MaxName != "a.mp4" || row.ExtDelta["mp4"].Buckets[0] != [2]int64{12, 2} {
		t.Errorf("ext delta = %+v", row.ExtDelta["mp4"])
	}
	if len(row.Entries) != 1 || row.Entries[0].Category != schema.CategoryVideo {
		t.Errorf("entries = %+v", row.Entries)
	}
	if len(row.Candidates) != 1 || row.Candidates[0].Fingerprint == nil ||
		*row.Candidates[0].Fingerprint != "123-456-789" {
		t.Errorf("candidates = %+v", row.Candidates)
	}
	if len(row.Hardlinks) != 1 || row.Hardlinks[0].Ino != 987654 {
		t.Errorf("hardlinks = %+v", row.Hardlinks)
	}
	// Errors are transient: a cached directory was readable when it was stored.
	if len(row.Errors) != 0 {
		t.Errorf("errors = %+v, want none", row.Errors)
	}
}

// A nil slice encodes as `null`, and the TypeScript scanner iterates these
// lists straight after JSON.parse.
func TestEmptyListsAreStoredAsArraysNotNull(t *testing.T) {
	cache, path := openTemp(t, config.Defaults())

	bare := &walk.DirDelta{Path: "/root/empty", ExtDelta: walk.DirExtMap{}}
	if err := cache.Save([]*walk.DirDelta{bare}, map[string]bool{"/root/empty": true}, []string{"/root"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var subdirs, entries, candidates, hardlinks, extMap string
	err = db.QueryRow(
		"SELECT subdirs, entries, candidates, hardlinks, ext_map FROM dirs WHERE path = ?",
		"/root/empty",
	).Scan(&subdirs, &entries, &candidates, &hardlinks, &extMap)
	if err != nil {
		t.Fatal(err)
	}

	for name, stored := range map[string]string{
		"subdirs": subdirs, "entries": entries,
		"candidates": candidates, "hardlinks": hardlinks,
	} {
		if stored != "[]" {
			t.Errorf("%s stored as %s, want []", name, stored)
		}
	}
	if extMap != "{}" {
		t.Errorf("ext_map stored as %s, want {}", extMap)
	}
}

func TestConfigChangeDiscardsTheCache(t *testing.T) {
	cache, path := openTemp(t, config.Defaults())
	if err := cache.Save([]*walk.DirDelta{delta("/root/media")}, map[string]bool{"/root/media": true}, []string{"/root"}); err != nil {
		t.Fatal(err)
	}
	cache.Close()

	// A changed threshold makes every stored aggregate a lie.
	changed := config.Defaults()
	changed.MinFileSize = 999

	reopened, reset, err := Open(path, changed)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	if !reset {
		t.Error("reset = false, want true after a threshold change")
	}
	loaded, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Errorf("kept %d rows after a config change", len(loaded))
	}
}

// Rows are keyed by absolute path, so they stay valid whichever root reached
// them. Including roots in the fingerprint once meant that scanning any subtree
// discarded the cache for the whole home directory.
func TestChangingRootsAloneKeepsTheCache(t *testing.T) {
	cache, path := openTemp(t, config.Defaults())
	cache.Close()

	elsewhere := config.Defaults()
	elsewhere.Roots = []string{"/somewhere/else"}

	reopened, reset, err := Open(path, elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	if reset {
		t.Error("reset = true, want false when only the roots moved")
	}
}

// A fresh cache has no stored config to have moved away from, so opening one is
// not a reset — the user has nothing to be told about.
func TestAFreshCacheIsNotReportedAsReset(t *testing.T) {
	_, reset, err := Open(filepath.Join(t.TempDir(), "new.sqlite"), config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if reset {
		t.Error("reset = true on a cache that did not exist")
	}
}

func TestPruningIsScopedToTheScannedRoots(t *testing.T) {
	cache, _ := openTemp(t, config.Defaults())

	rows := []*walk.DirDelta{delta("/root/media"), delta("/root/code"), delta("/other/place")}
	visited := map[string]bool{"/root/media": true, "/root/code": true, "/other/place": true}
	if err := cache.Save(rows, visited, []string{"/root", "/other"}); err != nil {
		t.Fatal(err)
	}

	// Now scan only /root/code. /root/media is gone from disk; /other/place was
	// never looked at, so nothing was learned about it.
	if err := cache.Save(
		[]*walk.DirDelta{delta("/root/code")},
		map[string]bool{"/root/code": true},
		[]string{"/root/code"},
	); err != nil {
		t.Fatal(err)
	}

	loaded, err := cache.Load()
	if err != nil {
		t.Fatal(err)
	}

	if loaded["/root/code"] == nil {
		t.Error("the scanned directory was dropped")
	}
	if loaded["/other/place"] == nil {
		t.Error("a row outside the scanned root was pruned")
	}
	if loaded["/root/media"] == nil {
		t.Error("a row outside the scanned root was pruned")
	}
}

func TestPruningDropsVanishedDirectories(t *testing.T) {
	cache, _ := openTemp(t, config.Defaults())

	if err := cache.Save(
		[]*walk.DirDelta{delta("/root/media"), delta("/root/code")},
		map[string]bool{"/root/media": true, "/root/code": true},
		[]string{"/root"},
	); err != nil {
		t.Fatal(err)
	}

	// A full rescan of /root that never saw /root/media: it is gone.
	if err := cache.Save(
		[]*walk.DirDelta{delta("/root/code")},
		map[string]bool{"/root/code": true},
		[]string{"/root"},
	); err != nil {
		t.Fatal(err)
	}

	loaded, err := cache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded["/root/media"]; ok {
		t.Error("a vanished directory kept its row")
	}
	if len(loaded) != 1 {
		t.Errorf("kept %d rows, want 1", len(loaded))
	}
}
