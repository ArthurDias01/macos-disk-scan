//go:build darwin

package walk

import (
	"os"
	"syscall"
)

// statInfo is the subset of stat(2) the scanner reads, in the units the
// snapshot reports.
type statInfo struct {
	// Physical bytes on disk (blocks * 512), not logical size. The whole
	// question is "what do I get back if I delete this", and logical size
	// systematically overstates sparse and APFS-compressed files.
	Size        int64
	MtimeMs     float64
	AtimeMs     float64
	BirthtimeMs float64
	Dev         int64
	Ino         uint64
	Nlink       int64
}

func statOf(info os.FileInfo) (statInfo, bool) {
	raw, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return statInfo{}, false
	}

	return statInfo{
		Size:        raw.Blocks * 512,
		MtimeMs:     timespecMs(raw.Mtimespec),
		AtimeMs:     timespecMs(raw.Atimespec),
		BirthtimeMs: timespecMs(raw.Birthtimespec),
		Dev:         int64(raw.Dev),
		Ino:         raw.Ino,
		Nlink:       int64(raw.Nlink),
	}, true
}

// DeviceOf returns a path's st_dev, which is what identifies a filesystem. The
// walk stops at any change: another volume, or a network share that could hang
// the scan.
func DeviceOf(info os.FileInfo) (int64, bool) {
	stat, ok := statOf(info)
	return stat.Dev, ok
}

// SizeOf returns a path's physical size in bytes (blocks * 512), which is what
// deleting it would actually free.
func SizeOf(info os.FileInfo) (int64, bool) {
	stat, ok := statOf(info)
	return stat.Size, ok
}

// timespecMs matches Node's `stats.mtimeMs`: milliseconds since the epoch,
// carrying the sub-millisecond remainder.
func timespecMs(ts syscall.Timespec) float64 {
	return float64(ts.Sec)*1000 + float64(ts.Nsec)/1e6
}
