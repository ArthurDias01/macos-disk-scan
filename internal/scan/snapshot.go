package scan

import (
	"fmt"
	"os"
	"time"

	"disk-report/internal/aggregate"
	"disk-report/internal/duplicates"
	"disk-report/internal/schema"
)

// isoMillis is JavaScript's Date.toISOString: UTC, exactly three decimal
// places, trailing Z. Snapshots are read by the SPA and diffed against ones the
// TypeScript scanner wrote, so the timestamps have to be the same shape.
const isoMillis = "2006-01-02T15:04:05.000Z07:00"

// SnapshotInput is everything gathered outside the walk itself.
type SnapshotInput struct {
	Config     schema.ScanConfig
	StartedAt  time.Time
	FinishedAt time.Time
	Duplicates duplicates.Result
	Volume     *schema.VolumeInfo
	// Paths from the deletion ledger, checked before the walk started.
	Rechecked []schema.RecheckedPath
}

// Snapshot assembles the finished report.
func (r *WalkResult) Snapshot(input SnapshotInput) schema.ScanSnapshot {
	collector := r.Collector
	totals := collector.Totals()

	extensions := aggregate.BuildExtensionStats(
		collector.Extensions(), r.Classifier, input.Duplicates.ByExtension,
	)

	hostname, _ := os.Hostname()

	snapshot := schema.ScanSnapshot{
		SchemaVersion: schema.SchemaVersion,
		ID:            "scan-" + input.StartedAt.UTC().Format("2006-01-02T15-04-05"),
		StartedAt:     input.StartedAt.UTC().Format(isoMillis),
		FinishedAt:    input.FinishedAt.UTC().Format(isoMillis),
		DurationMs:    input.FinishedAt.Sub(input.StartedAt).Milliseconds(),
		Hostname:      hostname,
		// Embedded so a six-month-old snapshot still explains its own numbers,
		// and so a diff can warn when two scans used different thresholds.
		Config:          input.Config,
		Totals:          totals,
		Extensions:      extensions,
		Categories:      aggregate.BuildCategoryStats(extensions),
		FolderTree:      aggregate.BuildFolderTree(collector.Dirs(), r.ValidRoots, input.Config.MinFolderSize),
		Files:           aggregate.SelectTopFiles(collector.Entries(), input.Config.TopNPerExtension, input.Config.GlobalTopN),
		DuplicateGroups: input.Duplicates.Groups,
		Volume:          input.Volume,
		Rechecked:       input.Rechecked,
		Unscanned:       collector.Unscanned(),
		Warnings:        r.warnings(input, totals),
	}

	normalize(&snapshot)
	return snapshot
}

// warnings are the things the numbers alone would not tell you.
func (r *WalkResult) warnings(input SnapshotInput, totals schema.ScanTotals) []string {
	warnings := append([]string{}, r.Warnings...)

	// The reconciliation check. When the scan total exceeds what the volume
	// says is used, block sharing is proven rather than suspected — and the
	// SPA switches its charts to the unique reading on the strength of it.
	if input.Volume != nil && totals.Bytes > input.Volume.UsedBytes {
		warnings = append(warnings, fmt.Sprintf(
			"Scanned total (%d bytes) exceeds the volume's used bytes (%d). "+
				"Blocks are shared between files — APFS clones are counted once per copy "+
				"by the filesystem itself.",
			totals.Bytes, input.Volume.UsedBytes,
		))
	}

	var survivors int
	var survivingBytes int64
	for _, entry := range input.Rechecked {
		if entry.Present {
			survivors++
			survivingBytes += entry.Size
		}
	}
	if survivors > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d path(s) you moved to the Trash are back on disk (%d bytes). "+
				"Restored from the Trash, or recreated.",
			survivors, survivingBytes,
		))
	}

	// Without Full Disk Access macOS hides ~/Library/Mail, ~/Library/Messages,
	// ~/Library/Safari and the Photos library — frequently the largest single
	// item on a Mac.
	var denied int
	for _, failure := range r.Collector.Unscanned() {
		if failure.Code == "EPERM" || failure.Code == "EACCES" {
			denied++
		}
	}
	if denied > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d path(s) could not be read. Grant Full Disk Access to see Mail, "+
				"Messages, Safari and the Photos library.",
			denied,
		))
	}

	return warnings
}

// normalize replaces nil slices with empty ones.
//
// A nil slice marshals to `null`, and the SPA maps over every one of these
// fields without a guard. An empty scan would otherwise produce a snapshot that
// crashes the page it was written for.
func normalize(snapshot *schema.ScanSnapshot) {
	if snapshot.Extensions == nil {
		snapshot.Extensions = []schema.ExtensionStat{}
	}
	if snapshot.Categories == nil {
		snapshot.Categories = []schema.CategoryStat{}
	}
	if snapshot.Files == nil {
		snapshot.Files = []schema.FileEntry{}
	}
	if snapshot.DuplicateGroups == nil {
		snapshot.DuplicateGroups = []schema.DuplicateGroup{}
	}
	if snapshot.Rechecked == nil {
		snapshot.Rechecked = []schema.RecheckedPath{}
	}
	if snapshot.Unscanned == nil {
		snapshot.Unscanned = []schema.ScanError{}
	}
	if snapshot.Warnings == nil {
		snapshot.Warnings = []string{}
	}

	for i := range snapshot.Extensions {
		if snapshot.Extensions[i].Histogram == nil {
			snapshot.Extensions[i].Histogram = schema.EmptyHistogram()
		}
	}
	for i := range snapshot.Categories {
		if snapshot.Categories[i].Extensions == nil {
			snapshot.Categories[i].Extensions = []string{}
		}
	}
	for i := range snapshot.DuplicateGroups {
		if snapshot.DuplicateGroups[i].Paths == nil {
			snapshot.DuplicateGroups[i].Paths = []string{}
		}
	}
	normalizeFolder(&snapshot.FolderTree)

	// The config is embedded verbatim; its lists reach the SPA too.
	if snapshot.Config.Roots == nil {
		snapshot.Config.Roots = []string{}
	}
	if snapshot.Config.ExcludePaths == nil {
		snapshot.Config.ExcludePaths = []string{}
	}
	if snapshot.Config.BundleExtensions == nil {
		snapshot.Config.BundleExtensions = []string{}
	}
	if snapshot.Config.CompoundExtensions == nil {
		snapshot.Config.CompoundExtensions = []string{}
	}
	if snapshot.Config.CategoryMap == nil {
		snapshot.Config.CategoryMap = map[string]schema.Category{}
	}
}

func normalizeFolder(node *schema.FolderNode) {
	if node.Children == nil {
		node.Children = []schema.FolderNode{}
	}
	for i := range node.Children {
		normalizeFolder(&node.Children[i])
	}
}
