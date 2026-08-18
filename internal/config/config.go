// Package config resolves the scan configuration.
//
// Defaults are generated from `shared/config.ts` (see tools/gen-go-config.ts)
// and embedded, so the Go scanner and the SPA cannot disagree about what a
// `.dmg` is or which extensions grow in place.
package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"disk-report/internal/schema"
)

//go:embed defaults.json
var defaultsJSON []byte

type embedded struct {
	Config             schema.ScanConfig `json:"config"`
	VolatileExtensions []string          `json:"volatileExtensions"`
}

var loaded = func() embedded {
	var e embedded
	if err := json.Unmarshal(defaultsJSON, &e); err != nil {
		panic(fmt.Sprintf("embedded defaults.json is malformed: %v", err))
	}
	return e
}()

// VolatileExtensions are types that change size in place. Writing to an
// existing file does not touch its directory's mtime, so a directory holding
// one of these can never be trusted to an incremental cache hit.
func VolatileExtensions() map[string]bool {
	set := make(map[string]bool, len(loaded.VolatileExtensions))
	for _, ext := range loaded.VolatileExtensions {
		set[ext] = true
	}
	return set
}

// Defaults returns the embedded defaults with `roots` resolved to $HOME.
//
// Roots are left empty in the generated file on purpose: they belong to the
// machine running the scan, not to a file in the repo.
func Defaults() schema.ScanConfig {
	config := loaded.Config
	config.Roots = append([]string(nil), config.Roots...)
	config.BundleExtensions = append([]string(nil), config.BundleExtensions...)
	config.CompoundExtensions = append([]string(nil), config.CompoundExtensions...)
	config.ExcludePaths = append([]string(nil), config.ExcludePaths...)

	categories := make(map[string]schema.Category, len(config.CategoryMap))
	for ext, category := range config.CategoryMap {
		categories[ext] = category
	}
	config.CategoryMap = categories

	if len(config.Roots) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			config.Roots = []string{home}
		}
	}
	return config
}

// Overrides mirrors `scan.config.json`. Every field is a pointer so that an
// absent key falls back to the default rather than resetting it to zero — the
// difference between "leave minFileSize alone" and "report every byte".
type Overrides struct {
	Roots              *[]string                   `json:"roots"`
	MinFileSize        *int64                      `json:"minFileSize"`
	MinFolderSize      *int64                      `json:"minFolderSize"`
	TopNPerExtension   *int64                      `json:"topNPerExtension"`
	GlobalTopN         *int64                      `json:"globalTopN"`
	WorkerCount        *int64                      `json:"workerCount"`
	BundleExtensions   *[]string                   `json:"bundleExtensions"`
	CompoundExtensions *[]string                   `json:"compoundExtensions"`
	CategoryMap        *map[string]schema.Category `json:"categoryMap"`
	ExcludePaths       *[]string                   `json:"excludePaths"`
	FollowSymlinks     *bool                       `json:"followSymlinks"`
	CrossFilesystems   *bool                       `json:"crossFilesystems"`
	DetectDuplicates   *bool                       `json:"detectDuplicates"`
	DuplicateMinSize   *int64                      `json:"duplicateMinSize"`
}

// Resolve layers overrides onto the defaults.
//
// Lists replace wholesale — narrowing `bundleExtensions` has to be possible.
// The category map merges, because it is a lookup table that a user extends
// with their own types rather than redefines.
func Resolve(overrides Overrides) schema.ScanConfig {
	config := Defaults()

	if overrides.Roots != nil {
		config.Roots = expandPaths(*overrides.Roots)
	}
	if overrides.MinFileSize != nil {
		config.MinFileSize = *overrides.MinFileSize
	}
	if overrides.MinFolderSize != nil {
		config.MinFolderSize = *overrides.MinFolderSize
	}
	if overrides.TopNPerExtension != nil {
		config.TopNPerExtension = *overrides.TopNPerExtension
	}
	if overrides.GlobalTopN != nil {
		config.GlobalTopN = *overrides.GlobalTopN
	}
	if overrides.WorkerCount != nil {
		config.WorkerCount = *overrides.WorkerCount
	}
	if overrides.BundleExtensions != nil {
		config.BundleExtensions = *overrides.BundleExtensions
	}
	if overrides.CompoundExtensions != nil {
		config.CompoundExtensions = *overrides.CompoundExtensions
	}
	if overrides.CategoryMap != nil {
		for ext, category := range *overrides.CategoryMap {
			config.CategoryMap[ext] = category
		}
	}
	if overrides.ExcludePaths != nil {
		config.ExcludePaths = expandPaths(*overrides.ExcludePaths)
	}
	if overrides.FollowSymlinks != nil {
		config.FollowSymlinks = *overrides.FollowSymlinks
	}
	if overrides.CrossFilesystems != nil {
		config.CrossFilesystems = *overrides.CrossFilesystems
	}
	if overrides.DetectDuplicates != nil {
		config.DetectDuplicates = *overrides.DetectDuplicates
	}
	if overrides.DuplicateMinSize != nil {
		config.DuplicateMinSize = *overrides.DuplicateMinSize
	}
	return config
}

// expandPaths resolves a leading `~`.
//
// `scan.config.ts` could call homedir(); JSON cannot, so a config saying
// "~/Downloads" would otherwise name a directory literally called "~" and the
// root would simply fail to resolve. Only a leading `~` is special — a path
// segment may legitimately contain one anywhere else.
func expandPaths(paths []string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return paths
	}

	expanded := make([]string, len(paths))
	for i, path := range paths {
		switch {
		case path == "~":
			expanded[i] = home
		case strings.HasPrefix(path, "~/"):
			expanded[i] = filepath.Join(home, path[2:])
		default:
			expanded[i] = path
		}
	}
	return expanded
}

// LoadFile reads `scan.config.json`. A missing file is not an error: the
// defaults are a complete, usable configuration on their own.
func LoadFile(path string) (Overrides, error) {
	var overrides Overrides

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return overrides, nil
	}
	if err != nil {
		return overrides, err
	}
	if err := json.Unmarshal(data, &overrides); err != nil {
		return overrides, fmt.Errorf("%s: %w", path, err)
	}
	return overrides, nil
}
