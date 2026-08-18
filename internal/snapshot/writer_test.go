package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"disk-report/internal/schema"
)

func snap(id string, bytes int64) schema.ScanSnapshot {
	return schema.ScanSnapshot{
		SchemaVersion: schema.SchemaVersion,
		ID:            id,
		StartedAt:     "2026-08-17T10:00:00.000Z",
		DurationMs:    1234,
		Totals:        schema.ScanTotals{Bytes: bytes, Files: 7},
		Extensions:    []schema.ExtensionStat{},
		Files:         []schema.FileEntry{},
	}
}

func TestWriteStoresTheSnapshotAndIndexesIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")

	path, err := Write(dir, snap("scan-2026-08-17T10-00-00", 4096))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if filepath.Base(path) != "scan-2026-08-17T10-00-00.json" {
		t.Errorf("wrote %s", path)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored schema.ScanSnapshot
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatalf("the snapshot is not readable JSON: %v", err)
	}
	if stored.Totals.Bytes != 4096 {
		t.Errorf("bytes = %d, want 4096", stored.Totals.Bytes)
	}

	index, err := ReadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Snapshots) != 1 {
		t.Fatalf("index holds %d entries, want 1", len(index.Snapshots))
	}
	entry := index.Snapshots[0]
	if entry.File != "scan-2026-08-17T10-00-00.json" || entry.TotalBytes != 4096 || entry.TotalFiles != 7 {
		t.Errorf("index entry = %+v", entry)
	}
}

// "What grew since last month" is the question the app exists to answer, so the
// index has to keep every scan, newest first.
func TestIndexKeepsEveryScanNewestFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")

	for _, id := range []string{"scan-a", "scan-b", "scan-c"} {
		if _, err := Write(dir, snap(id, 1024)); err != nil {
			t.Fatal(err)
		}
	}

	index, err := ReadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scan-c", "scan-b", "scan-a"}
	if len(index.Snapshots) != len(want) {
		t.Fatalf("index holds %d entries, want %d", len(index.Snapshots), len(want))
	}
	for i, id := range want {
		if index.Snapshots[i].ID != id {
			t.Errorf("entry %d = %s, want %s", i, index.Snapshots[i].ID, id)
		}
	}
}

// The id carries only seconds, so two scans in the same second share one. The
// rerun should replace its own entry rather than appear twice.
func TestRewritingTheSameIDReplacesItsEntry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")

	if _, err := Write(dir, snap("scan-same", 1024)); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, snap("scan-same", 8192)); err != nil {
		t.Fatal(err)
	}

	index, err := ReadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Snapshots) != 1 {
		t.Fatalf("index holds %d entries, want 1", len(index.Snapshots))
	}
	if index.Snapshots[0].TotalBytes != 8192 {
		t.Errorf("kept the stale entry: %+v", index.Snapshots[0])
	}
}

// A corrupt index must not cost you the scan that is about to be written.
func TestACorruptIndexIsRebuiltRatherThanFatal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadIndex(dir); err == nil {
		t.Error("a corrupt index should report that it was rebuilt")
	}

	if _, err := Write(dir, snap("scan-after-corruption", 2048)); err != nil {
		t.Fatalf("write: %v", err)
	}

	index, err := ReadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Snapshots) != 1 || index.Snapshots[0].ID != "scan-after-corruption" {
		t.Errorf("index = %+v", index.Snapshots)
	}
}

func TestAMissingIndexIsEmptyNotAnError(t *testing.T) {
	index, err := ReadIndex(t.TempDir())
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if index.Snapshots == nil {
		t.Error("Snapshots must be an empty slice, not nil — the app maps over it")
	}
}
