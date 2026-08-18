// Package ledger closes the loop on deletions.
//
// A cleanup that quietly failed looks exactly like one that worked, because a
// deleted path simply stops appearing in the report. The browser cannot write
// files and the scanner cannot read a browser, so the bridge is the cleanup
// script the basket exports: each line records itself only if the move
// succeeded, and the next scan checks those paths before walking anything.
package ledger

import (
	"os"
	"strings"

	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

// Entry is one line of the ledger.
type Entry struct {
	Path       string
	IntendedAt string
}

// Read parses the deletion ledger.
//
// The format is one `ISO_TIMESTAMP<TAB>PATH` per line: appendable from a shell
// script with printf, and readable without a JSON parser if it ever needs
// fixing by hand. Lines that do not parse are skipped rather than failing the
// scan — a corrupt ledger must not cost you the scan.
func Read(path string) []Entry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var entries []Entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		entry := Entry{Path: line}
		if tab := strings.Index(line, "\t"); tab != -1 {
			entry.IntendedAt = line[:tab]
			entry.Path = line[tab+1:]
		}

		// Anything that is not an absolute path is not something that was moved.
		if !strings.HasPrefix(entry.Path, "/") {
			continue
		}
		entries = append(entries, entry)
	}

	return entries
}

// Recheck stats each path and reports the verdict.
//
// This is the question the walk cannot answer: an absent path never appears in
// a scan at all, so its absence has to be established deliberately rather than
// inferred from the results.
func Recheck(entries []Entry) []schema.RecheckedPath {
	seen := map[string]bool{}
	results := make([]schema.RecheckedPath, 0, len(entries))

	for _, entry := range entries {
		// The same path can be trashed more than once over time; the newest
		// intention is the one worth reporting.
		if seen[entry.Path] {
			continue
		}
		seen[entry.Path] = true

		result := schema.RecheckedPath{Path: entry.Path, IntendedAt: entry.IntendedAt}
		if info, err := os.Lstat(entry.Path); err == nil {
			result.Present = true
			if stat, ok := walk.SizeOf(info); ok {
				result.Size = stat
			}
		}
		results = append(results, result)
	}

	return results
}
