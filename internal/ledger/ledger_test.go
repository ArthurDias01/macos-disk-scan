package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLedger(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deleted.log")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadParsesTimestampAndPath(t *testing.T) {
	path := writeLedger(t, "2026-08-01T10:00:00Z\t/Users/x/.npm\n")

	entries := Read(path)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].IntendedAt != "2026-08-01T10:00:00Z" || entries[0].Path != "/Users/x/.npm" {
		t.Errorf("entry = %+v", entries[0])
	}
}

// A path with no timestamp is still a path someone meant to delete. The format
// is meant to be fixable by hand, which means tolerating a hand-written line.
func TestReadAcceptsABarePath(t *testing.T) {
	entries := Read(writeLedger(t, "/Users/x/.cache\n"))
	if len(entries) != 1 || entries[0].Path != "/Users/x/.cache" || entries[0].IntendedAt != "" {
		t.Errorf("entries = %+v", entries)
	}
}

// A corrupt ledger must not cost you the scan.
func TestReadSkipsCommentsBlanksAndRelativePaths(t *testing.T) {
	entries := Read(writeLedger(t, `
# a comment

2026-08-01T10:00:00Z	/Users/x/keep
relative/path
	trailing-tab-no-path
2026-08-02T10:00:00Z	/Users/x/also-keep
`))

	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2: %+v", len(entries), entries)
	}
	if entries[0].Path != "/Users/x/keep" || entries[1].Path != "/Users/x/also-keep" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestReadOfAMissingFileIsEmptyNotAnError(t *testing.T) {
	if entries := Read(filepath.Join(t.TempDir(), "nothing.log")); len(entries) != 0 {
		t.Errorf("entries = %+v, want none", entries)
	}
}

// The whole point: a deleted path never appears in a walk, so its absence has
// to be established deliberately.
func TestRecheckReportsPresenceAndSize(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "still-here")
	if err := os.WriteFile(present, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "really-deleted")

	results := Recheck([]Entry{
		{Path: present, IntendedAt: "2026-08-01T10:00:00Z"},
		{Path: gone, IntendedAt: "2026-08-01T10:00:00Z"},
	})

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if !results[0].Present || results[0].Size < 4096 {
		t.Errorf("surviving path reported as %+v", results[0])
	}
	if results[1].Present || results[1].Size != 0 {
		t.Errorf("deleted path reported as %+v", results[1])
	}
}

// The same path can be trashed more than once over time; the newest intention
// is the one worth reporting.
func TestRecheckReportsEachPathOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gone")

	results := Recheck([]Entry{
		{Path: path, IntendedAt: "2026-08-02T10:00:00Z"},
		{Path: path, IntendedAt: "2026-08-01T10:00:00Z"},
	})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].IntendedAt != "2026-08-02T10:00:00Z" {
		t.Errorf("kept %q, want the first (newest) entry", results[0].IntendedAt)
	}
}
