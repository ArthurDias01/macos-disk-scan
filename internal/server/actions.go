package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Result is what happened to one path.
type Result struct {
	Path  string `json:"path"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Method names how it was trashed, because the two differ in whether Finder
	// can put the file back.
	Method string `json:"method,omitempty"`
}

// Actions performs the narrow set of things the app is allowed to do.
//
// Trash and reveal only. Permanent deletion stays a command you copy and run
// yourself: the Trash is recoverable, and `rm -rf` against a ~/Library path can
// break an app with nothing to undo it. Review before execution is the safety
// mechanism for the irreversible half, and it is not worth trading for a click.
type Actions struct {
	paths  *PathGuard
	ledger string
	// trash is a field so tests can exercise the ledger and the result
	// reporting without actually moving anything to the Trash.
	trash func(path string) (string, error)

	// One mutation at a time. Two concurrent trash calls could interleave their
	// ledger writes, and the ledger is the only record that the move happened.
	mu sync.Mutex
}

func NewActions(paths *PathGuard, ledgerPath string) *Actions {
	return &Actions{paths: paths, ledger: ledgerPath, trash: trashOne}
}

// Trash moves paths to the Trash and records each success in the ledger.
func (a *Actions) Trash(requested []string) ([]Result, error) {
	resolved, err := a.paths.CheckAll(requested)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	results := make([]Result, 0, len(resolved))
	moved := make([]string, 0, len(resolved))

	for _, path := range resolved {
		method, err := a.trash(path)
		if err != nil {
			results = append(results, Result{Path: path, Error: err.Error()})
			continue
		}
		results = append(results, Result{Path: path, OK: true, Method: method})
		moved = append(moved, path)
	}

	// Recorded only for what actually moved, exactly as the exported cleanup
	// script does: a line in the ledger is a claim that the move succeeded, and
	// the next scan checks it.
	if err := a.appendLedger(moved); err != nil {
		return results, fmt.Errorf("moved %d item(s) but could not write the ledger: %w", len(moved), err)
	}

	return results, nil
}

// Reveal opens each enclosing folder in Finder with the item selected.
func (a *Actions) Reveal(requested []string) ([]Result, error) {
	resolved, err := a.paths.CheckAll(requested)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(resolved))
	args := append([]string{"-R"}, resolved...)
	if output, err := exec.Command("open", args...).CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		for _, path := range resolved {
			results = append(results, Result{Path: path, Error: message})
		}
		return results, nil
	}

	for _, path := range resolved {
		results = append(results, Result{Path: path, OK: true, Method: "open -R"})
	}
	return results, nil
}

// trashOne moves a single path to the Trash.
//
// Finder is asked to do it rather than moving the file ourselves, because only
// Finder records where the item came from — that is what makes "Put Back" work,
// and what makes this recoverable rather than merely reversible by hand. It also
// handles files on other volumes, which have their own .Trashes directory that a
// move to ~/.Trash would fail on.
//
// The fallback exists because Finder automation needs a permission the user may
// decline. It still gets the file out of the way; it just cannot be put back.
func trashOne(path string) (string, error) {
	if _, err := os.Lstat(path); err != nil {
		return "", fmt.Errorf("no longer there: %w", err)
	}

	script := fmt.Sprintf(
		"tell application \"Finder\" to delete POSIX file %s", appleScriptString(path))
	if output, err := exec.Command("osascript", "-e", script).CombinedOutput(); err == nil {
		return "Finder", nil
	} else if fallbackErr := moveToTrashDir(path); fallbackErr != nil {
		return "", fmt.Errorf("Finder refused (%s) and the fallback failed: %w",
			strings.TrimSpace(string(output)), fallbackErr)
	}

	return "mv (no Put Back)", nil
}

// moveToTrashDir is the fallback: a plain move into ~/.Trash, uniquifying the
// name rather than overwriting anything already in there.
func moveToTrashDir(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	trash := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return err
	}

	target := filepath.Join(trash, filepath.Base(path))
	for suffix := 1; ; suffix++ {
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			break
		}
		target = filepath.Join(trash, fmt.Sprintf("%s %d", filepath.Base(path), suffix))
	}

	return os.Rename(path, target)
}

// appendLedger writes one ISO_TIMESTAMP<TAB>PATH line per moved item.
//
// The format is the one the exported cleanup script has always written and the
// one `ledger.Read` parses: appendable from a shell, fixable by hand.
func (a *Actions) appendLedger(paths []string) error {
	if len(paths) == 0 || a.ledger == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(a.ledger), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(a.ledger, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	stamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	for _, path := range paths {
		if _, err := fmt.Fprintf(file, "%s\t%s\n", stamp, path); err != nil {
			return err
		}
	}
	return nil
}

// appleScriptString quotes a path for AppleScript, where the only escapes that
// exist inside a double-quoted string are \" and \\. macOS filenames routinely
// contain quotes and backslashes.
func appleScriptString(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
