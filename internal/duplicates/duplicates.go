// Package duplicates finds byte-identical files.
//
// What a group does *not* tell you: byte-identical is not the same as
// block-shared, and stat cannot distinguish them.
//
//   - APFS clones share blocks. stat and du bill each copy in full; deleting
//     one frees nothing. The group really occupies `size`.
//   - Real duplicates each own their blocks. The group really occupies
//     `size * count`, and deleting the extras frees ReclaimableBytes.
//
// Because of that ambiguity this pass never rewrites sizes. Folder totals,
// extension totals and the scan total stay exactly as the filesystem reports
// them — matching du and Finder. Groups are an additional dimension.
package duplicates

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/cespare/xxhash/v2"

	"disk-report/internal/extension"
	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

// probeBytes is how much is read from each end of a file to fingerprint it.
const probeBytes = 64 * 1024

// readConcurrency caps the fingerprint reads. The work is I/O bound.
const readConcurrency = 16

// maxPathsPerGroup caps the members listed in the snapshot. The count stays
// exact — a group of 217,639 copies reports all of them, and lists 50.
const maxPathsPerGroup = 50

// Membership annotates a file entry with the group it belongs to.
type Membership struct {
	Fingerprint string
	Copies      int64
}

// Result is everything the second pass learned.
type Result struct {
	Groups []schema.DuplicateGroup
	// Paths that are a redundant copy — every member of a group but the first.
	DuplicatePaths map[string]bool
	// Fingerprint and group size for every member, for annotating file entries.
	Membership     map[string]Membership
	DuplicateBytes int64
	DuplicateFiles int64
	// Redundant bytes per containing directory, for folder-level attribution.
	ByDirectory map[string]int64
	// Redundant bytes per extension, so charts can show a unique reading.
	ByExtension map[string]int64
	// Fingerprint per suspect path, so the cache can store them for next time.
	FingerprintOf map[string]string
	// Fingerprints taken from the cache rather than read from disk.
	Reused   int
	Computed int
}

// Empty is the shape returned when duplicate detection is switched off.
func Empty() Result {
	return Result{
		Groups:         []schema.DuplicateGroup{},
		DuplicatePaths: map[string]bool{},
		Membership:     map[string]Membership{},
		ByDirectory:    map[string]int64{},
		ByExtension:    map[string]int64{},
		FingerprintOf:  map[string]string{},
	}
}

// Options configures the pass.
type Options struct {
	MinSize    int64
	Classifier *extension.Classifier
	// OnProgress may be called from several goroutines at once; it is
	// serialized internally, so an implementation needs no locking of its own.
	OnProgress func(done, total int)
}

// Fingerprint identifies a file by its size plus its first and last chunk.
// The second return is false when the file could not be read.
//
// Full-content hashing would multiply the read volume by a hundred for the
// files this targets (large media). Head and tail differ for essentially all
// real-world files of equal size; the residual risk is padded container
// formats, which is why a group is a claim of identity, not proof.
//
// The format is byte-for-byte the one the TypeScript scanner writes, so a cache
// filled by either scanner stays usable by the other.
func Fingerprint(path string, size int64) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		// Unreadable or deleted mid-scan: excluded rather than guessed at.
		return "", false
	}
	defer file.Close()

	// Short reads leave the rest of the buffer zeroed rather than failing.
	// `size` is the physical size, which can sit past the logical end of a
	// block-padded file; the padding is deterministic, so two identical files
	// still agree.
	head := make([]byte, min(int64(probeBytes), size))
	if _, err := file.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return "", false
	}

	var tailHash uint64
	if size > probeBytes {
		tailLength := min(int64(probeBytes), size-probeBytes)
		tail := make([]byte, tailLength)
		if _, err := file.ReadAt(tail, size-tailLength); err != nil && !errors.Is(err, io.EOF) {
			return "", false
		}
		tailHash = xxhash.Sum64(tail)
	}

	return fmt.Sprintf("%d-%d-%d", size, xxhash.Sum64(head), tailHash), true
}

// Detect finds byte-identical files among the candidates.
//
// Only files whose exact size collides with another candidate are read: on a
// real home directory that turns millions of files into a few thousand reads.
// Full-content hashing of every large file would multiply read volume by a
// hundred to find the same groups.
func Detect(candidates []walk.CandidateRecord, options Options) Result {
	suspects := collideOnSize(candidates, options.MinSize)
	fingerprints, reused, computed := fingerprintAll(suspects, options)

	byFingerprint := map[string][]walk.CandidateRecord{}
	fingerprintOf := make(map[string]string, len(fingerprints))

	for i, value := range fingerprints {
		if value == "" {
			continue
		}
		fingerprintOf[suspects[i].Path] = value
		byFingerprint[value] = append(byFingerprint[value], suspects[i])
	}

	result := Empty()
	result.FingerprintOf = fingerprintOf
	result.Reused = reused
	result.Computed = computed

	for value, members := range byFingerprint {
		if len(members) < 2 {
			continue
		}
		result.addGroup(value, members, options.Classifier)
	}

	// Biggest reclaimable weight first. The fingerprint tiebreak keeps two runs
	// over an unchanged tree in the same order.
	sort.Slice(result.Groups, func(i, j int) bool {
		if result.Groups[i].ReclaimableBytes != result.Groups[j].ReclaimableBytes {
			return result.Groups[i].ReclaimableBytes > result.Groups[j].ReclaimableBytes
		}
		return result.Groups[i].Fingerprint < result.Groups[j].Fingerprint
	})

	return result
}

// addGroup records one set of byte-identical files.
func (r *Result) addGroup(
	fingerprint string,
	members []walk.CandidateRecord,
	classifier *extension.Classifier,
) {
	// Deterministic ordering so repeated scans keep the same "original".
	sort.Slice(members, func(i, j int) bool { return members[i].Path < members[j].Path })

	size := members[0].Size
	ext := classifier.Parse(filepath.Base(members[0].Path))

	for _, member := range members {
		r.Membership[member.Path] = Membership{Fingerprint: fingerprint, Copies: int64(len(members))}
	}

	// Every member but the first is redundant: deleting it frees `size`, unless
	// the group is really a set of clones sharing blocks.
	for _, member := range members[1:] {
		r.DuplicatePaths[member.Path] = true
		r.DuplicateBytes += size
		r.DuplicateFiles++
		r.ByDirectory[filepath.Dir(member.Path)] += size
		r.ByExtension[ext] += size
	}

	paths := make([]string, 0, min(len(members), maxPathsPerGroup))
	for _, member := range members[:min(len(members), maxPathsPerGroup)] {
		paths = append(paths, member.Path)
	}

	r.Groups = append(r.Groups, schema.DuplicateGroup{
		Fingerprint:      fingerprint,
		Size:             size,
		Count:            int64(len(members)),
		ReclaimableBytes: size * int64(len(members)-1),
		Ext:              ext,
		Category:         classifier.Categorize(ext),
		Paths:            paths,
	})
}

// collideOnSize keeps only the candidates that share an exact size with another
// candidate. This is the step that makes the pass affordable.
func collideOnSize(candidates []walk.CandidateRecord, minSize int64) []walk.CandidateRecord {
	bySize := map[int64][]walk.CandidateRecord{}
	for _, candidate := range candidates {
		if candidate.Size < minSize {
			continue
		}
		bySize[candidate.Size] = append(bySize[candidate.Size], candidate)
	}

	var suspects []walk.CandidateRecord
	for _, group := range bySize {
		if len(group) > 1 {
			suspects = append(suspects, group...)
		}
	}

	// Map iteration is randomized, and the read order decides nothing else —
	// but a stable order makes progress output and profiles comparable.
	sort.Slice(suspects, func(i, j int) bool { return suspects[i].Path < suspects[j].Path })
	return suspects
}

// fingerprintAll reads the suspects, returning one fingerprint per suspect
// (empty where the file could not be read).
func fingerprintAll(suspects []walk.CandidateRecord, options Options) ([]string, int, int) {
	fingerprints := make([]string, len(suspects))

	var (
		mutex    sync.Mutex
		done     int
		reused   int
		computed int
	)

	work := make(chan int)
	var wg sync.WaitGroup

	workers := min(readConcurrency, len(suspects))
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for index := range work {
				candidate := suspects[index]

				// A cached fingerprint is trustworthy while size and mtime both
				// match: the walk already proved the file was untouched, and
				// re-reading it is the most expensive thing a warm scan does.
				if candidate.Fingerprint != nil && *candidate.Fingerprint != "" {
					fingerprints[index] = *candidate.Fingerprint
					mutex.Lock()
					reused++
					done++
					report(options.OnProgress, done, len(suspects))
					mutex.Unlock()
					continue
				}

				value, ok := Fingerprint(candidate.Path, candidate.Size)
				if ok {
					fingerprints[index] = value
				}
				mutex.Lock()
				computed++
				done++
				report(options.OnProgress, done, len(suspects))
				mutex.Unlock()
			}
		}()
	}

	for index := range suspects {
		work <- index
	}
	close(work)
	wg.Wait()

	return fingerprints, reused, computed
}

func report(onProgress func(done, total int), done, total int) {
	if onProgress != nil {
		onProgress(done, total)
	}
}
