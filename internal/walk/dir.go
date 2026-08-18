package walk

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"disk-report/internal/extension"
	"disk-report/internal/schema"
)

// Scanner reads one directory at a time. It holds no mutable state, so any
// number of goroutines can share one.
type Scanner struct {
	config     schema.ScanConfig
	classifier *extension.Classifier
	volatile   map[string]bool
	// st_dev of each root. A different device means a mount point: another
	// volume, or a network share that could hang the walk.
	rootDevices map[int64]bool
}

// NewScanner builds the per-directory scanner. rootDevices comes from stat'ing
// the configured roots.
func NewScanner(
	config schema.ScanConfig,
	classifier *extension.Classifier,
	volatile map[string]bool,
	rootDevices map[int64]bool,
) *Scanner {
	return &Scanner{
		config:      config,
		classifier:  classifier,
		volatile:    volatile,
		rootDevices: rootDevices,
	}
}

// ScanDir reads one directory and returns its complete contribution.
//
// Subdirectories are named but not descended into: the collector owns the
// queue, because directories are discovered as their parents are read.
func (s *Scanner) ScanDir(dirPath string, dirMtimeMs float64) *DirDelta {
	delta := &DirDelta{
		Path:       dirPath,
		DirMtimeMs: dirMtimeMs,
		IsCloud:    s.classifier.IsCloudPath(dirPath),
		ExtDelta:   DirExtMap{},
	}

	dirents, err := readDir(dirPath)
	if err != nil {
		// Usually EPERM on a TCC-protected path (Mail, Messages, the Photos
		// library). Recorded rather than swallowed.
		delta.Errors = append(delta.Errors, ToScanError(dirPath, err))
		return delta
	}

	for _, dirent := range dirents {
		name := dirent.Name()
		path := filepath.Join(dirPath, name)

		if s.isExcluded(path) {
			continue
		}
		// Symlinks are never followed: cycles, and the target is counted at its
		// real location anyway.
		if dirent.Type()&fs.ModeSymlink != 0 && !s.config.FollowSymlinks {
			continue
		}

		if dirent.IsDir() {
			s.scanSubdir(delta, path, name)
			continue
		}

		if !dirent.Type().IsRegular() {
			continue // sockets, fifos, devices
		}

		info, err := os.Lstat(path)
		if err != nil {
			continue // deleted mid-scan
		}
		stat, ok := statOf(info)
		if !ok {
			continue
		}

		entry := s.makeEntry(path, name, stat, stat.Size, false)

		if stat.Nlink > 1 {
			// Held back: only the collector knows which path owns these bytes.
			delta.Hardlinks = append(delta.Hardlinks, HardlinkRecord{
				Entry: entry,
				Dev:   stat.Dev,
				Ino:   stat.Ino,
				Dir:   dirPath,
			})
			continue
		}

		delta.OwnSize += stat.Size
		delta.OwnFileCount++
		if stat.MtimeMs > delta.MaxMtimeMs {
			delta.MaxMtimeMs = stat.MtimeMs
		}
		s.noteFile(delta, entry, name, stat.Size)
	}

	return delta
}

// scanSubdir handles a directory entry: either an atomic bundle summed in
// place, or a subdirectory queued for its own visit.
func (s *Scanner) scanSubdir(delta *DirDelta, path, name string) {
	if s.classifier.IsBundleDir(name) {
		info, err := os.Lstat(path)
		if err != nil {
			return
		}
		stat, ok := statOf(info)
		if !ok {
			return
		}

		size, maxMtimeMs := s.sumBundle(path, delta)
		mtimeMs := maxMtimeMs
		if stat.MtimeMs > mtimeMs {
			mtimeMs = stat.MtimeMs
		}
		// A bundle's own mtime lags its contents, so the freshest thing inside
		// is the honest activity signal.
		stat.MtimeMs = mtimeMs

		delta.OwnSize += size
		delta.OwnFileCount++
		if mtimeMs > delta.MaxMtimeMs {
			delta.MaxMtimeMs = mtimeMs
		}

		entry := s.makeEntry(path, name, stat, size, true)
		// A bundle's contents change without changing the parent directory, so
		// the parent can never be trusted to a cache hit.
		delta.Volatile = true
		s.noteFile(delta, entry, name, size)
		return
	}

	if !s.config.CrossFilesystems {
		info, err := os.Lstat(path)
		if err != nil {
			return
		}
		stat, ok := statOf(info)
		if !ok || !s.rootDevices[stat.Dev] {
			return
		}
	}

	delta.SubdirNames = append(delta.SubdirNames, name)
}

// sumBundle totals a bundle directory in place. Bundles are atomic, so nothing
// inside is reported individually and nothing inside reaches the extension
// aggregates.
//
// Hardlinks are deduped within the bundle only. Cross-bundle dedup would need
// the global inode set, which lives with the collector; a hardlink shared
// between two bundles is rare enough to accept.
func (s *Scanner) sumBundle(bundlePath string, delta *DirDelta) (int64, float64) {
	type inode struct {
		dev int64
		ino uint64
	}

	seen := map[inode]bool{}
	stack := []string{bundlePath}
	var size int64
	var maxMtimeMs float64

	for len(stack) > 0 {
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		dirents, err := readDir(dir)
		if err != nil {
			delta.Errors = append(delta.Errors, ToScanError(dir, err))
			continue
		}

		for _, dirent := range dirents {
			if dirent.Type()&fs.ModeSymlink != 0 && !s.config.FollowSymlinks {
				continue
			}
			path := filepath.Join(dir, dirent.Name())

			if dirent.IsDir() {
				stack = append(stack, path)
				continue
			}
			if !dirent.Type().IsRegular() {
				continue
			}

			info, err := os.Lstat(path)
			if err != nil {
				continue // deleted mid-scan
			}
			stat, ok := statOf(info)
			if !ok {
				continue
			}

			if stat.Nlink > 1 {
				key := inode{stat.Dev, stat.Ino}
				if seen[key] {
					continue
				}
				seen[key] = true
			}

			size += stat.Size
			if stat.MtimeMs > maxMtimeMs {
				maxMtimeMs = stat.MtimeMs
			}
		}
	}

	return size, maxMtimeMs
}

// noteFile records a file in the aggregates that do not depend on anything
// outside this directory.
func (s *Scanner) noteFile(delta *DirDelta, entry schema.FileEntry, name string, size int64) {
	delta.ExtDelta.add(entry.Ext, size, name)

	if size >= s.config.MinFileSize {
		delta.Entries = append(delta.Entries, entry)
		// A large file can grow in place without touching the directory's
		// mtime, so this directory must never be trusted to a cache hit.
		delta.Volatile = true
	}
	if s.volatile[entry.Ext] {
		delta.Volatile = true
	}
	// Bundles are excluded: a candidate is fingerprinted by opening it, and a
	// .photoslibrary is a directory. The TypeScript scanner offered them up and
	// relied on the open failing, which spent an errno on every large bundle
	// and made "unreadable" mean two different things.
	if s.config.DetectDuplicates && size >= s.config.DuplicateMinSize && !entry.IsBundle {
		delta.Candidates = append(delta.Candidates, CandidateRecord{
			Path:    entry.Path,
			Size:    size,
			MtimeMs: entry.MtimeMs,
		})
	}
}

func (s *Scanner) makeEntry(path, name string, stat statInfo, size int64, isBundle bool) schema.FileEntry {
	ext := s.classifier.Parse(name)
	return schema.FileEntry{
		Path:        path,
		Ext:         ext,
		Category:    s.classifier.Categorize(ext),
		Size:        size,
		MtimeMs:     stat.MtimeMs,
		AtimeMs:     stat.AtimeMs,
		BirthtimeMs: stat.BirthtimeMs,
		IsBundle:    isBundle,
		IsCloud:     s.classifier.IsCloudPath(path),
		IsHardlink:  stat.Nlink > 1,
		// Decided centrally: only the collector sees every inode.
		IsDupInode: false,
		Nlink:      stat.Nlink,
	}
}

func (s *Scanner) isExcluded(path string) bool {
	for _, excluded := range s.config.ExcludePaths {
		if path == excluded || strings.HasPrefix(path, excluded+"/") {
			return true
		}
	}
	return false
}

// readDir lists a directory without sorting. os.ReadDir sorts by name, which at
// 5.5M files is real time spent on an order nothing here depends on.
func readDir(path string) ([]os.DirEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return file.ReadDir(-1)
}
