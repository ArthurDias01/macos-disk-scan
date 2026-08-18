package scan

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"disk-report/internal/config"
	"disk-report/internal/duplicates"
	"disk-report/internal/fixture"
	"disk-report/internal/schema"
)

func snapshotOf(t *testing.T, input SnapshotInput) schema.ScanSnapshot {
	t.Helper()

	root := fixture.Build(t)
	cfg := config.Defaults()
	cfg.Roots = []string{root}
	cfg.MinFileSize = 0
	cfg.MinFolderSize = 0
	cfg.WorkerCount = 2

	result, err := Walk(Options{Config: cfg, HomeDir: "/Users/nobody"})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if input.Config.WorkerCount == 0 {
		input.Config = cfg
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
		input.FinishedAt = input.StartedAt.Add(1234 * time.Millisecond)
	}
	if input.Duplicates.Membership == nil {
		input.Duplicates = duplicates.Empty()
	}

	return result.Snapshot(input)
}

// The id is the filename stem and the SPA's snapshot key; the timestamps are
// diffed against ones the TypeScript scanner wrote.
func TestSnapshotIdentityMatchesTheTypeScriptFormat(t *testing.T) {
	snap := snapshotOf(t, SnapshotInput{})

	if snap.ID != "scan-2026-08-17T10-00-00" {
		t.Errorf("id = %q", snap.ID)
	}
	if snap.StartedAt != "2026-08-17T10:00:00.000Z" {
		t.Errorf("startedAt = %q, want Date.toISOString shape", snap.StartedAt)
	}
	if snap.DurationMs != 1234 {
		t.Errorf("durationMs = %d, want 1234", snap.DurationMs)
	}
	if snap.SchemaVersion != schema.SchemaVersion {
		t.Errorf("schemaVersion = %d", snap.SchemaVersion)
	}

	// The id must survive being used as a filename stem.
	if !regexp.MustCompile(`^scan-[\d\-T]+$`).MatchString(snap.ID) {
		t.Errorf("id %q is not filename-safe", snap.ID)
	}
}

// A nil slice marshals to null, and the SPA maps over every one of these
// without a guard: an empty scan would produce a snapshot that crashes the page
// it was written for.
func TestSnapshotNeverMarshalsNullArrays(t *testing.T) {
	snap := snapshotOf(t, SnapshotInput{})

	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), ":null") {
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		for key, raw := range decoded {
			// volume is genuinely nullable; the SPA checks it.
			if key != "volume" && string(raw) == "null" {
				t.Errorf("%s marshalled as null", key)
			}
		}
	}

	for _, name := range []string{"extensions", "categories", "files", "duplicateGroups",
		"rechecked", "unscanned", "warnings"} {
		if !strings.Contains(string(body), `"`+name+`":[`) {
			t.Errorf("%s is not an array", name)
		}
	}
}

// Proven, not suspected: the filesystem itself says less is used than the walk
// counted, which can only happen when blocks are shared.
func TestClonesAreReportedWhenTheScanExceedsTheVolume(t *testing.T) {
	snap := snapshotOf(t, SnapshotInput{
		Volume: &schema.VolumeInfo{Path: "/", TotalBytes: 1 << 40, UsedBytes: 1},
	})

	if !hasWarning(snap.Warnings, "Blocks are shared between files") {
		t.Errorf("warnings = %v", snap.Warnings)
	}
}

func TestNoCloneWarningWhenTheTotalsReconcile(t *testing.T) {
	snap := snapshotOf(t, SnapshotInput{
		Volume: &schema.VolumeInfo{Path: "/", TotalBytes: 1 << 40, UsedBytes: 1 << 39},
	})

	if hasWarning(snap.Warnings, "Blocks are shared between files") {
		t.Errorf("warnings = %v", snap.Warnings)
	}
}

// A path that came back is the one outcome a cleanup script cannot report on
// its own: it succeeded at the time.
func TestSurvivingTrashedPathsAreWarnedAbout(t *testing.T) {
	snap := snapshotOf(t, SnapshotInput{
		Rechecked: []schema.RecheckedPath{
			{Path: "/Users/x/.npm", Present: true, Size: 4096},
			{Path: "/Users/x/.cache", Present: false},
		},
	})

	if !hasWarning(snap.Warnings, "are back on disk") {
		t.Errorf("warnings = %v", snap.Warnings)
	}
	if len(snap.Rechecked) != 2 {
		t.Errorf("rechecked = %+v", snap.Rechecked)
	}
}

func TestSnapshotEmbedsTheConfigThatProducedIt(t *testing.T) {
	snap := snapshotOf(t, SnapshotInput{})

	if len(snap.Config.Roots) != 1 {
		t.Fatalf("roots = %v", snap.Config.Roots)
	}
	if !filepath.IsAbs(snap.Config.Roots[0]) {
		t.Errorf("root %q is not absolute", snap.Config.Roots[0])
	}
	// Six months from now this is what explains the numbers in the file.
	if snap.Config.MinFileSize != 0 || len(snap.Config.CategoryMap) == 0 {
		t.Errorf("config = %+v", snap.Config)
	}
}

func hasWarning(warnings []string, substring string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, substring) {
			return true
		}
	}
	return false
}
