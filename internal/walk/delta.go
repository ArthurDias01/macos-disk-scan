package walk

import "disk-report/internal/schema"

// ExtDelta is one directory's totals for one extension.
//
// The compact shape (sparse bucket pairs, a basename rather than a full path)
// was originally forced by the cost of structured-clone across worker threads.
// Goroutines share memory, so that reason is gone — but the same rows are what
// the incremental cache stores, and at 737k directories a dense 48-slot
// histogram per extension per directory would dominate the cache file. The
// shape stays; only its justification changed.
type ExtDelta struct {
	Bytes int64 `json:"bytes"`
	Count int64 `json:"count"`
	// [bucketIndex, count] pairs — most directories touch very few buckets.
	Buckets [][2]int64 `json:"buckets"`
	MaxSize int64      `json:"maxSize"`
	MaxName string     `json:"maxName"`
}

// DirExtMap is a directory's per-extension accumulators.
type DirExtMap map[string]*ExtDelta

// HardlinkRecord is a file with nlink > 1.
//
// The walk excludes these from every local aggregate and reports them here
// instead: only the collector sees enough of the filesystem to know which
// inodes have already been counted somewhere else.
type HardlinkRecord struct {
	Entry schema.FileEntry `json:"entry"`
	Dev   int64            `json:"dev"`
	Ino   uint64           `json:"ino"`
	// Directory holding the file, so its bytes land in the right folder node.
	Dir string `json:"dir"`
}

// CandidateRecord is a file eligible for duplicate fingerprinting.
type CandidateRecord struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	// With size, decides whether a cached fingerprint can be reused.
	MtimeMs float64 `json:"mtimeMs"`
	// Carried over from the cache when the file is unchanged.
	Fingerprint *string `json:"fingerprint,omitempty"`
}

// DirDelta is one directory's contribution, complete as soon as that directory
// is done. Merging two of these is associative, which is what lets the walk
// aggregate per directory without coordinating.
type DirDelta struct {
	Path string `json:"path"`
	// The directory's own mtime, which is what the cache validates against.
	DirMtimeMs float64 `json:"dirMtimeMs"`
	// Files directly inside this directory, excluding hardlinks.
	OwnSize      int64 `json:"ownSize"`
	OwnFileCount int64 `json:"ownFileCount"`
	// Freshest mtime among this directory's own files.
	MaxMtimeMs float64 `json:"maxMtimeMs"`
	IsCloud    bool    `json:"isCloud"`
	// Rescan this directory next time regardless of its mtime: it holds a large
	// file or a volatile type, both of which can change size in place without
	// touching the directory.
	Volatile bool `json:"volatile"`
	// Subdirectory basenames. Joined onto Path — storing full paths doubles the
	// cache for no information.
	SubdirNames []string  `json:"subdirNames"`
	ExtDelta    DirExtMap `json:"extDelta"`
	// Files at or above MinFileSize, including bundles.
	Entries    []schema.FileEntry `json:"entries"`
	Hardlinks  []HardlinkRecord   `json:"hardlinks"`
	Candidates []CandidateRecord  `json:"candidates"`
	Errors     []schema.ScanError `json:"errors"`
}

// add folds one file into a directory's per-extension accumulator.
func (m DirExtMap) add(ext string, size int64, name string) {
	accum, ok := m[ext]
	if !ok {
		accum = &ExtDelta{}
		m[ext] = accum
	}

	accum.Bytes += size
	accum.Count++

	bucket := int64(schema.HistogramBucket(size))
	found := false
	for i := range accum.Buckets {
		if accum.Buckets[i][0] == bucket {
			accum.Buckets[i][1]++
			found = true
			break
		}
	}
	if !found {
		accum.Buckets = append(accum.Buckets, [2]int64{bucket, 1})
	}

	// The name tiebreak matters because this directory was read unsorted: two
	// files of the same size would otherwise make "the largest .go file" depend
	// on readdir order, and two scans of an unchanged tree would disagree.
	if size > accum.MaxSize || (size == accum.MaxSize && name < accum.MaxName) {
		accum.MaxSize = size
		accum.MaxName = name
	}
}
