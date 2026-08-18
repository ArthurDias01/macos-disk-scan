// Package extension normalizes filenames to file types.
//
// A port of scanner/extension.ts. The rules are behavioural decisions recorded
// in docs/DECISIONS.md, not implementation details, so this file follows the
// original exactly — including the string lengths, which are counted in UTF-16
// code units because that is what the TypeScript `.length` measured.
package extension

import (
	"strings"
	"unicode"
	"unicode/utf16"

	"disk-report/internal/schema"
)

// maxExtensionLength — an "extension" longer than this is virtually always a
// mis-parse of a dotted filename ("Backup of my project.final version"), not a
// real type.
//
// The bar sits at 16 rather than something tighter because macOS ships real
// 13-character types: photoslibrary, imovielibrary, xcactivitylog. Genuine
// mis-parses are caught by the whitespace rule long before length.
const maxExtensionLength = 16

// Classifier answers the per-file questions the walk asks millions of times.
// Built once from the resolved config so the lookups are sets, not scans.
type Classifier struct {
	compound      []string
	bundles       map[string]bool
	categories    map[string]schema.Category
	cloudPrefixes []string
}

// New builds a classifier from the resolved config and the scanning user's home
// directory, which is what makes a path "cloud".
func New(config schema.ScanConfig, homeDir string) *Classifier {
	bundles := make(map[string]bool, len(config.BundleExtensions))
	for _, ext := range config.BundleExtensions {
		bundles[ext] = true
	}

	return &Classifier{
		compound:   append([]string(nil), config.CompoundExtensions...),
		bundles:    bundles,
		categories: config.CategoryMap,
		cloudPrefixes: []string{
			homeDir + "/Library/CloudStorage",
			homeDir + "/Library/Mobile Documents",
		},
	}
}

// Parse normalizes a filename to its extension.
//
// Rules (see docs/DECISIONS.md):
//   - lowercased
//   - compound extensions win over the last segment
//   - a leading dot with no other dot means no extension (.zshrc, .DS_Store)
//   - no dot at all means no extension (Makefile, compiled binaries)
//   - over 16 units, containing whitespace, or all digits means no extension
func (c *Classifier) Parse(filename string) string {
	lower := strings.ToLower(filename)

	// Dotfiles: a leading dot is not an extension separator.
	searchable := strings.TrimPrefix(lower, ".")
	if !strings.Contains(searchable, ".") {
		return schema.NoExtension
	}

	for _, compound := range c.compound {
		suffix := "." + compound
		if strings.HasSuffix(searchable, suffix) && len(searchable) > len(suffix) {
			return compound
		}
	}

	ext := searchable[strings.LastIndex(searchable, ".")+1:]

	if len(ext) == 0 {
		return schema.NoExtension
	}
	if utf16Len(ext) > maxExtensionLength {
		return schema.NoExtension
	}
	if hasSpace(ext) {
		return schema.NoExtension
	}
	// "Backup.2024" is a dated name, not a file type.
	if allDigits(ext) {
		return schema.NoExtension
	}

	return ext
}

// Categorize maps an extension to its coarse category, defaulting to other.
func (c *Classifier) Categorize(ext string) schema.Category {
	if category, ok := c.categories[ext]; ok {
		return category
	}
	return schema.CategoryOther
}

// IsBundleDir reports whether a directory should be treated as one atomic item:
// summed but never expanded. You delete a whole .photoslibrary, never one .heic
// inside it.
func (c *Classifier) IsBundleDir(dirname string) bool {
	ext := c.Parse(dirname)
	return ext != schema.NoExtension && c.bundles[ext]
}

// IsCloudPath reports whether a path is cloud-backed, and so may hold dataless
// placeholders. Under physical sizing those correctly report ~0 bytes; the flag
// lets the UI say so rather than leaving you wondering why a 200 GB Dropbox
// reads as nothing.
func (c *Classifier) IsCloudPath(path string) bool {
	for _, prefix := range c.cloudPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// utf16Len counts UTF-16 code units, matching JavaScript's String.length. An
// extension of astral characters is junk either way, but the two scanners must
// agree on where the cut falls.
func utf16Len(s string) int {
	if isASCII(s) {
		return len(s)
	}
	return len(utf16.Encode([]rune(s)))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// byteOrderMark is matched by JavaScript's `\s` but is not a Unicode space, so
// it needs naming explicitly for the two scanners to agree.
const byteOrderMark = '\ufeff'

// hasSpace mirrors JavaScript's `\s`: the Unicode space categories plus the BOM.
func hasSpace(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || r == byteOrderMark {
			return true
		}
	}
	return false
}

// allDigits mirrors `/^\d+$/`, which without the unicode flag is ASCII only.
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}
