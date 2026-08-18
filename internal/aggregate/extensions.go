// Package aggregate turns per-directory deltas into the rollups a snapshot
// reports: extension and category stats, and the folder tree.
//
// A port of scanner/aggregate.ts, with one deliberate difference: every sort
// carries an explicit tiebreak. The TypeScript version leaned on V8's stable
// sort over insertion-ordered objects, and Go map iteration is randomized — the
// same scan would otherwise rank equal-sized extensions differently each run.
package aggregate

import (
	"math"
	"path/filepath"
	"sort"

	"disk-report/internal/extension"
	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

// ExtAccum is the global accumulator for one extension. Merging two of these is
// associative, which is what lets the walk aggregate per directory and the
// collector merge without coordination.
type ExtAccum struct {
	Bytes   int64
	Count   int64
	MaxSize int64
	MaxPath string
	// Dense here, unlike the sparse per-directory form: there is one of these
	// per extension for the whole scan, not one per directory.
	Histogram []int64
}

// ExtMap holds every extension seen in the scan.
type ExtMap map[string]*ExtAccum

func (m ExtMap) ensure(ext string) *ExtAccum {
	accum, ok := m[ext]
	if !ok {
		accum = &ExtAccum{Histogram: schema.EmptyHistogram()}
		m[ext] = accum
	}
	return accum
}

// MergeDir folds one directory's compact extension delta into the global map.
//
// MaxName is a basename, joined back onto the directory path here: storing full
// paths per extension per directory would dominate the cache file.
func (m ExtMap) MergeDir(dirPath string, delta walk.DirExtMap) {
	for ext, incoming := range delta {
		accum := m.ensure(ext)
		accum.Bytes += incoming.Bytes
		accum.Count += incoming.Count

		for _, pair := range incoming.Buckets {
			accum.Histogram[pair[0]] += pair[1]
		}
		// Ties resolve on path, so the largest file of a type is the same one on
		// every run. Directories are merged in whatever order the pool finished
		// them, which is not stable between scans.
		if incoming.MaxSize > accum.MaxSize {
			accum.MaxSize = incoming.MaxSize
			accum.MaxPath = filepath.Join(dirPath, incoming.MaxName)
		} else if incoming.MaxSize == accum.MaxSize {
			if candidate := filepath.Join(dirPath, incoming.MaxName); candidate < accum.MaxPath {
				accum.MaxPath = candidate
			}
		}
	}
}

// AddFile folds a single file in. Hardlink survivors reach the aggregates this
// way, after the collector has decided which path owns the inode.
func (m ExtMap) AddFile(entry schema.FileEntry) {
	accum := m.ensure(entry.Ext)
	accum.Bytes += entry.Size
	accum.Count++
	accum.Histogram[schema.HistogramBucket(entry.Size)]++

	if entry.Size > accum.MaxSize || (entry.Size == accum.MaxSize && entry.Path < accum.MaxPath) {
		accum.MaxSize = entry.Size
		accum.MaxPath = entry.Path
	}
}

// BuildExtensionStats ranks extensions by total size, biggest first.
func BuildExtensionStats(
	extMap ExtMap,
	classifier *extension.Classifier,
	duplicateByExtension map[string]int64,
) []schema.ExtensionStat {
	stats := make([]schema.ExtensionStat, 0, len(extMap))

	for ext, accum := range extMap {
		var mean int64
		if accum.Count > 0 {
			mean = int64(math.Round(float64(accum.Bytes) / float64(accum.Count)))
		}

		stats = append(stats, schema.ExtensionStat{
			Ext:       ext,
			Category:  classifier.Categorize(ext),
			TotalSize: accum.Bytes,
			FileCount: accum.Count,
			MeanSize:  mean,
			// Approximate: accurate to the bucket's power-of-two lower edge.
			MedianSize:     schema.HistogramQuantile(accum.Histogram, 0.5),
			P95Size:        schema.HistogramQuantile(accum.Histogram, 0.95),
			MaxSize:        accum.MaxSize,
			LargestPath:    accum.MaxPath,
			DuplicateBytes: duplicateByExtension[ext],
			Histogram:      accum.Histogram,
		})
	}

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TotalSize != stats[j].TotalSize {
			return stats[i].TotalSize > stats[j].TotalSize
		}
		return stats[i].Ext < stats[j].Ext
	})
	return stats
}

// BuildCategoryStats rolls extensions up into the ten categories, dropping the
// ones nothing landed in.
func BuildCategoryStats(extensions []schema.ExtensionStat) []schema.CategoryStat {
	index := make(map[schema.Category]int, len(schema.Categories))
	stats := make([]schema.CategoryStat, 0, len(schema.Categories))

	for _, category := range schema.Categories {
		index[category] = len(stats)
		stats = append(stats, schema.CategoryStat{Category: category, Extensions: []string{}})
	}

	// `extensions` arrives sorted by size, so each category's extension list
	// inherits that order for free.
	for _, stat := range extensions {
		at, ok := index[stat.Category]
		if !ok {
			continue
		}
		stats[at].TotalSize += stat.TotalSize
		stats[at].FileCount += stat.FileCount
		stats[at].DuplicateBytes += stat.DuplicateBytes
		stats[at].Extensions = append(stats[at].Extensions, stat.Ext)
	}

	populated := make([]schema.CategoryStat, 0, len(stats))
	for _, stat := range stats {
		if stat.FileCount > 0 {
			populated = append(populated, stat)
		}
	}

	sort.SliceStable(populated, func(i, j int) bool {
		return populated[i].TotalSize > populated[j].TotalSize
	})
	return populated
}

// SelectTopFiles caps the individually reported files: topNPerExtension biggest
// per extension, then globalTopN biggest overall. Everything dropped here still
// counts in the aggregates.
func SelectTopFiles(entries []schema.FileEntry, topNPerExtension, globalTopN int64) []schema.FileEntry {
	byExtension := map[string][]schema.FileEntry{}
	for _, entry := range entries {
		byExtension[entry.Ext] = append(byExtension[entry.Ext], entry)
	}

	kept := make([]schema.FileEntry, 0, len(entries))
	for _, list := range byExtension {
		sortBySizeThenPath(list)
		if int64(len(list)) > topNPerExtension {
			list = list[:topNPerExtension]
		}
		kept = append(kept, list...)
	}

	sortBySizeThenPath(kept)
	if int64(len(kept)) > globalTopN {
		kept = kept[:globalTopN]
	}
	return kept
}

// sortBySizeThenPath orders biggest first. The path tiebreak is what makes two
// runs over an unchanged tree produce the same file list.
func sortBySizeThenPath(entries []schema.FileEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Size != entries[j].Size {
			return entries[i].Size > entries[j].Size
		}
		return entries[i].Path < entries[j].Path
	})
}
