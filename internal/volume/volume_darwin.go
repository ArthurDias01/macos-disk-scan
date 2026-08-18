//go:build darwin

// Package volume reads the filesystem's own figures.
//
// This is the only independent check on the scan: when the walk's total exceeds
// the volume's used bytes, block sharing (APFS clones) is proven rather than
// merely suspected. On this machine the scan reports 3.5 TB allocated against
// 546 GB actually used.
package volume

import (
	"os/exec"
	"strconv"
	"strings"

	"disk-report/internal/schema"
)

// Read returns the volume figures for the filesystem holding path, or nil when
// they cannot be read. A missing volume is not an error — the scan is still
// valid, it just loses its reconciliation check.
//
// This shells out to df rather than calling statfs, which is the reverse of
// what a port should normally do. The reason is that on APFS the two disagree:
//
//	df -k       462,610,188 KB used
//	statfs      506,168,492 KB used  (f_blocks - f_bfree)
//
// The 41 GB gap is purgeable space, which df excludes and statfs counts as
// used. df's number is the one Finder shows, the one the DECISIONS note names
// as the source of truth, and the one every existing snapshot was reconciled
// against. Taking the larger figure would quietly weaken the clone check, since
// the warning fires only when the scan total exceeds it.
//
// Reading purgeable space directly needs getattrlist with ATTR_VOL_SPACEUSED.
// One subprocess per scan is not worth that, against a walk that takes minutes.
func Read(path string) *schema.VolumeInfo {
	output, err := exec.Command("df", "-k", path).Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return nil
	}

	// Filesystem 1024-blocks Used Available Capacity iused ifree %iused Mounted
	columns := strings.Fields(lines[len(lines)-1])
	if len(columns) < 4 {
		return nil
	}

	blocks, err := strconv.ParseInt(columns[1], 10, 64)
	if err != nil {
		return nil
	}
	used, err := strconv.ParseInt(columns[2], 10, 64)
	if err != nil {
		return nil
	}
	available, err := strconv.ParseInt(columns[3], 10, 64)
	if err != nil {
		return nil
	}

	const blockSize = 1024
	return &schema.VolumeInfo{
		Path:           path,
		TotalBytes:     blocks * blockSize,
		UsedBytes:      used * blockSize,
		AvailableBytes: available * blockSize,
	}
}
