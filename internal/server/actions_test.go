package server

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTrash records what it was asked to move and fails on anything named
// "locked", so partial failure can be exercised without touching the Trash.
func newTestActions(t *testing.T, root string) (*Actions, string, *[]string) {
	t.Helper()

	ledger := filepath.Join(t.TempDir(), "state", "deleted.log")
	var moved []string

	actions := NewActions(NewPathGuard([]string{root}), ledger)
	actions.trash = func(path string) (string, error) {
		if strings.Contains(filepath.Base(path), "locked") {
			return "", errors.New("permission denied")
		}
		moved = append(moved, path)
		return "Finder", nil
	}
	return actions, ledger, &moved
}

func touch(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The ledger is the whole point: a deleted path never appears in a later scan,
// so the only way the app can confirm the deletion happened is to have written
// down that it tried.
func TestTrashRecordsEverySuccessInTheLedger(t *testing.T) {
	root := t.TempDir()
	actions, ledger, moved := newTestActions(t, root)

	first := touch(t, filepath.Join(root, "big.mov"))
	second := touch(t, filepath.Join(root, "nested", "clip.mp4"))

	results, err := actions.Trash([]string{first, second})
	if err != nil {
		t.Fatalf("trash: %v", err)
	}
	if len(results) != 2 || !results[0].OK || !results[1].OK {
		t.Fatalf("results = %+v", results)
	}
	if len(*moved) != 2 {
		t.Errorf("moved %d items, want 2", len(*moved))
	}

	body, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("no ledger written: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("ledger has %d lines, want 2:\n%s", len(lines), body)
	}
	for _, line := range lines {
		stamp, path, found := strings.Cut(line, "\t")
		if !found || !strings.HasSuffix(stamp, "Z") || !strings.HasPrefix(path, "/") {
			t.Errorf("malformed ledger line: %q", line)
		}
	}
}

// A failure has to be visible per path. A cleanup that half worked and reported
// success is the exact failure mode the ledger exists to catch.
func TestAFailedMoveIsReportedAndNotRecorded(t *testing.T) {
	root := t.TempDir()
	actions, ledger, _ := newTestActions(t, root)

	ok := touch(t, filepath.Join(root, "fine.mov"))
	bad := touch(t, filepath.Join(root, "locked.mov"))

	results, err := actions.Trash([]string{ok, bad})
	if err != nil {
		t.Fatalf("trash: %v", err)
	}

	byPath := map[string]Result{}
	for _, result := range results {
		byPath[filepath.Base(result.Path)] = result
	}
	if !byPath["fine.mov"].OK {
		t.Error("the successful move was not reported as such")
	}
	if byPath["locked.mov"].OK || byPath["locked.mov"].Error == "" {
		t.Errorf("the failure was not reported: %+v", byPath["locked.mov"])
	}

	body, _ := os.ReadFile(ledger)
	if strings.Contains(string(body), "locked.mov") {
		t.Error("a failed move was written to the ledger")
	}
	if !strings.Contains(string(body), "fine.mov") {
		t.Error("the successful move was not written to the ledger")
	}
}

func TestTrashRefusesPathsOutsideTheRoots(t *testing.T) {
	root := t.TempDir()
	actions, ledger, moved := newTestActions(t, root)

	inside := touch(t, filepath.Join(root, "keep.mov"))

	// One disallowed path fails the batch before anything is attempted.
	if _, err := actions.Trash([]string{inside, "/etc/passwd"}); err == nil {
		t.Fatal("a batch reaching outside the roots was accepted")
	}
	if len(*moved) != 0 {
		t.Errorf("moved %v despite the batch being invalid", *moved)
	}
	if _, err := os.Stat(ledger); err == nil {
		t.Error("a ledger was written for a batch that never ran")
	}
}

func TestAppleScriptStringEscapesQuotesAndBackslashes(t *testing.T) {
	// macOS filenames genuinely contain both.
	got := appleScriptString(`/Users/x/He said "hi"\back.mov`)
	want := `"/Users/x/He said \"hi\"\\back.mov"`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
