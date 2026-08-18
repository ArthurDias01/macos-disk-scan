package volume

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// The volume figure is the scan's only independent check, so it has to be the
// same number df reports — not merely a plausible one. statfs disagrees with df
// on APFS by the size of the purgeable space, which on this machine is 41 GB.
func TestReadMatchesDf(t *testing.T) {
	info := Read(".")
	if info == nil {
		t.Fatal("no volume information for the working directory")
	}

	output, err := exec.Command("df", "-k", ".").Output()
	if err != nil {
		t.Skipf("df unavailable: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	columns := strings.Fields(lines[len(lines)-1])

	used, err := strconv.ParseInt(columns[2], 10, 64)
	if err != nil {
		t.Fatal(err)
	}

	// A live filesystem moves between the two calls, so compare within a
	// tolerance rather than exactly. The failure this guards against is the
	// 41 GB kind, not the megabyte kind.
	const tolerance = 512 * 1024 * 1024
	if diff := info.UsedBytes - used*1024; diff > tolerance || diff < -tolerance {
		t.Errorf("used = %d bytes, df says %d (diff %d)", info.UsedBytes, used*1024, diff)
	}
}

func TestReadReportsPlausibleFigures(t *testing.T) {
	info := Read(".")
	if info == nil {
		t.Fatal("no volume information")
	}

	if info.TotalBytes <= 0 {
		t.Errorf("total = %d", info.TotalBytes)
	}
	if info.UsedBytes < 0 || info.UsedBytes > info.TotalBytes {
		t.Errorf("used = %d of %d", info.UsedBytes, info.TotalBytes)
	}
	if info.AvailableBytes < 0 || info.AvailableBytes > info.TotalBytes {
		t.Errorf("available = %d of %d", info.AvailableBytes, info.TotalBytes)
	}
}

// A missing volume is not an error: the scan is still valid, it just loses its
// reconciliation check.
func TestReadOfAMissingPathIsNil(t *testing.T) {
	if info := Read("/no/such/path/anywhere"); info != nil {
		t.Errorf("info = %+v, want nil", info)
	}
}
