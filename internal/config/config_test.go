package config

import (
	"os"
	"path/filepath"
	"testing"

	"disk-report/internal/schema"
)

func TestDefaultsResolveRootsToHome(t *testing.T) {
	cfg := Defaults()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if len(cfg.Roots) != 1 || cfg.Roots[0] != home {
		t.Errorf("roots = %v, want [%s]", cfg.Roots, home)
	}
	if len(cfg.CategoryMap) == 0 || len(cfg.BundleExtensions) == 0 {
		t.Error("the embedded defaults are empty")
	}
}

// Callers mutate the returned config; one caller must not reach another's.
func TestDefaultsAreNotShared(t *testing.T) {
	first := Defaults()
	first.CategoryMap["mp4"] = schema.CategoryOther
	first.BundleExtensions[0] = "clobbered"

	second := Defaults()
	if second.CategoryMap["mp4"] != schema.CategoryVideo {
		t.Error("the category map is shared between calls")
	}
	if second.BundleExtensions[0] == "clobbered" {
		t.Error("the bundle list is shared between calls")
	}
}

// An absent key means "leave this alone", not "reset it to zero" — the
// difference between keeping minFileSize and reporting every byte on the disk.
func TestAbsentOverridesKeepTheDefaults(t *testing.T) {
	cfg := Resolve(Overrides{})
	defaults := Defaults()

	if cfg.MinFileSize != defaults.MinFileSize || cfg.WorkerCount != defaults.WorkerCount {
		t.Errorf("empty overrides changed the config: %+v", cfg)
	}
	if !cfg.DetectDuplicates {
		t.Error("detectDuplicates was reset to false by an absent key")
	}
}

// The category map is a lookup table a user extends with their own types, not
// one they redefine. Lists are the opposite: narrowing them has to be possible.
func TestCategoryMapMergesWhileListsReplace(t *testing.T) {
	custom := map[string]schema.Category{"blend": schema.CategoryData}
	bundles := []string{"app"}

	cfg := Resolve(Overrides{CategoryMap: &custom, BundleExtensions: &bundles})

	if cfg.CategoryMap["blend"] != schema.CategoryData {
		t.Error("the custom mapping was dropped")
	}
	if cfg.CategoryMap["mp4"] != schema.CategoryVideo {
		t.Error("the defaults were replaced rather than extended")
	}
	if len(cfg.BundleExtensions) != 1 {
		t.Errorf("bundleExtensions = %v, want it replaced wholesale", cfg.BundleExtensions)
	}
}

// scan.config.ts could call homedir(); JSON cannot.
func TestTildeInConfiguredPathsIsExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}

	roots := []string{"~/Downloads", "~", "/absolute", "/has~tilde/inside"}
	excludes := []string{"~/Library/Caches"}

	cfg := Resolve(Overrides{Roots: &roots, ExcludePaths: &excludes})

	want := []string{filepath.Join(home, "Downloads"), home, "/absolute", "/has~tilde/inside"}
	for i, expected := range want {
		if cfg.Roots[i] != expected {
			t.Errorf("root %d = %q, want %q", i, cfg.Roots[i], expected)
		}
	}
	if cfg.ExcludePaths[0] != filepath.Join(home, "Library", "Caches") {
		t.Errorf("exclude = %q", cfg.ExcludePaths[0])
	}
}

// The defaults alone are a complete, usable configuration.
func TestAMissingConfigFileIsNotAnError(t *testing.T) {
	overrides, err := LoadFile(filepath.Join(t.TempDir(), "scan.config.json"))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if overrides.MinFileSize != nil {
		t.Error("a missing file produced overrides")
	}
}

func TestLoadFileIgnoresTheSchemaPointer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.config.json")
	body := `{"$schema": "./scan.config.schema.json", "minFileSize": 2048}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	overrides, err := LoadFile(path)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if overrides.MinFileSize == nil || *overrides.MinFileSize != 2048 {
		t.Errorf("minFileSize = %v", overrides.MinFileSize)
	}
}

// A malformed config is worth failing on: silently scanning with defaults would
// produce a snapshot that does not match what the file asked for.
func TestAMalformedConfigFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFile(path); err == nil {
		t.Error("err = nil, want a parse failure")
	}
}
