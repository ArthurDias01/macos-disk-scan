package extension

import (
	"testing"

	"disk-report/internal/config"
	"disk-report/internal/schema"
)

func classifier() *Classifier {
	return New(config.Defaults(), "/Users/test")
}

func TestParseLowercases(t *testing.T) {
	c := classifier()
	assertParse(t, c, "IMG_1234.JPG", "jpg")
	assertParse(t, c, "Movie.MoV", "mov")
}

func TestParseKeepsCompoundExtensionsWhole(t *testing.T) {
	c := classifier()
	assertParse(t, c, "archive.tar.gz", "tar.gz")
	assertParse(t, c, "backup.TAR.BZ2", "tar.bz2")
	assertParse(t, c, "data.tar.zst", "tar.zst")
}

func TestParseTakesOnlyLastSegment(t *testing.T) {
	c := classifier()
	assertParse(t, c, "my.project.v2.mp4", "mp4")
	assertParse(t, c, "report.final.pdf", "pdf")
}

func TestParseDotfilesHaveNoExtension(t *testing.T) {
	c := classifier()
	assertParse(t, c, ".zshrc", "")
	assertParse(t, c, ".DS_Store", "")
	assertParse(t, c, ".gitignore", "")
}

func TestParseNamesWithoutDotHaveNoExtension(t *testing.T) {
	c := classifier()
	assertParse(t, c, "Makefile", "")
	assertParse(t, c, "node", "")
	assertParse(t, c, "LICENSE", "")
}

func TestParseRejectsJunkExtensions(t *testing.T) {
	c := classifier()
	assertParse(t, c, "Some file.this is not an extension", "")
	assertParse(t, c, "notes.averyverylongsuffix", "")
	assertParse(t, c, "Backup.2024", "")
}

func TestParseHandlesTrailingDots(t *testing.T) {
	c := classifier()
	assertParse(t, c, "weird.", "")
	assertParse(t, c, "..", "")
}

func TestParseDotfileWithRealSuffix(t *testing.T) {
	assertParse(t, classifier(), ".eslintrc.json", "json")
}

// The bar was raised from 12 to 16 once this case was found: both are real
// 13-character macOS types that a tighter cut discarded.
func TestParseKeepsThirteenCharacterMacTypes(t *testing.T) {
	c := classifier()
	assertParse(t, c, "Photos Library.photoslibrary", "photoslibrary")
	assertParse(t, c, "Home Movies.imovielibrary", "imovielibrary")
}

func TestCategorize(t *testing.T) {
	c := classifier()
	cases := map[string]schema.Category{
		"mp4":    schema.CategoryVideo,
		"dmg":    schema.CategoryDiskimage,
		"tar.gz": schema.CategoryArchive,
		"qqq":    schema.CategoryOther,
		"":       schema.CategoryOther,
	}
	for ext, want := range cases {
		if got := c.Categorize(ext); got != want {
			t.Errorf("Categorize(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestIsBundleDir(t *testing.T) {
	c := classifier()
	bundles := []string{"Safari.app", "Photos Library.photoslibrary", "Project.xcodeproj"}
	for _, name := range bundles {
		if !c.IsBundleDir(name) {
			t.Errorf("IsBundleDir(%q) = false, want true", name)
		}
	}

	// A directory that merely looks like a media file is not a bundle.
	plain := []string{"Documents", "node_modules", "holiday.mp4"}
	for _, name := range plain {
		if c.IsBundleDir(name) {
			t.Errorf("IsBundleDir(%q) = true, want false", name)
		}
	}
}

func TestIsCloudPath(t *testing.T) {
	c := classifier()
	cloud := []string{
		"/Users/test/Library/CloudStorage/Dropbox/file.zip",
		"/Users/test/Library/Mobile Documents/Notes",
	}
	for _, path := range cloud {
		if !c.IsCloudPath(path) {
			t.Errorf("IsCloudPath(%q) = false, want true", path)
		}
	}
	if c.IsCloudPath("/Users/test/Downloads/file.zip") {
		t.Error("IsCloudPath(local) = true, want false")
	}
}

func assertParse(t *testing.T, c *Classifier, name, want string) {
	t.Helper()
	if got := c.Parse(name); got != want {
		t.Errorf("Parse(%q) = %q, want %q", name, got, want)
	}
}
