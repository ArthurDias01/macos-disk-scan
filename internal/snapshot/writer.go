// Package snapshot writes finished scans to disk.
//
// Each scan is its own file plus an index entry, rather than one file that gets
// overwritten. The stated problem is recurring bloat, which is a trend
// question: "what grew since last month" is more actionable than "what is big",
// and at a few hundred kilobytes each, keeping fifty snapshots is free.
package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"

	"disk-report/internal/schema"
)

// Write stores the snapshot and updates the index, returning the file written.
func Write(dir string, snap schema.ScanSnapshot) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	name := snap.ID + ".json"
	path := filepath.Join(dir, name)

	// Compact: the file is fetched by the SPA, not read by a person, and the
	// histograms alone make indentation cost megabytes.
	body, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}

	if err := updateIndex(dir, snap, name); err != nil {
		return "", err
	}
	return path, nil
}

// ErrIndexRebuilt reports that an unreadable index.json was discarded. It is
// worth telling the user: the snapshots are all still on disk, but anything not
// listed has just become invisible to the app.
type ErrIndexRebuilt struct{ Cause error }

func (e ErrIndexRebuilt) Error() string {
	return "index.json was unreadable and has been rebuilt: " + e.Cause.Error()
}

// ReadIndex loads the snapshot index. A missing or corrupt index yields an
// empty one rather than an error, so a scan is never lost to it.
func ReadIndex(dir string) (schema.SnapshotIndex, error) {
	index := schema.SnapshotIndex{Snapshots: []schema.SnapshotIndexEntry{}}

	body, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if os.IsNotExist(err) {
		return index, nil
	}
	if err != nil {
		return index, ErrIndexRebuilt{Cause: err}
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return schema.SnapshotIndex{Snapshots: []schema.SnapshotIndexEntry{}}, ErrIndexRebuilt{Cause: err}
	}
	if index.Snapshots == nil {
		index.Snapshots = []schema.SnapshotIndexEntry{}
	}
	return index, nil
}

func updateIndex(dir string, snap schema.ScanSnapshot, file string) error {
	index, _ := ReadIndex(dir)

	entry := schema.SnapshotIndexEntry{
		ID:            snap.ID,
		File:          file,
		StartedAt:     snap.StartedAt,
		DurationMs:    snap.DurationMs,
		TotalBytes:    snap.Totals.Bytes,
		TotalFiles:    snap.Totals.Files,
		SchemaVersion: snap.SchemaVersion,
	}

	// Newest first, and a rerun within the same second replaces its own entry
	// rather than appearing twice.
	updated := []schema.SnapshotIndexEntry{entry}
	for _, existing := range index.Snapshots {
		if existing.ID != snap.ID {
			updated = append(updated, existing)
		}
	}
	index.Snapshots = updated

	// Indented: this one is small, and it is the file a person opens when the
	// app will not load.
	body, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "index.json"), body, 0o644)
}
