// Package fixture builds the directory tree the scanner tests walk.
//
// It lives outside the test files because both the walk and the collector need
// the same tree, and two copies of a fixture drift: a file added to one and not
// the other turns a real failure into a puzzle about which layout was assumed.
package fixture

import (
	"os"
	"path/filepath"
	"testing"
)

// KB is the unit the fixture's sizes are written in.
const KB = 1024

// Build creates the tree below and returns its root. The directory is removed
// when the test ends.
//
//	root/
//	  big.mp4                 512 KB
//	  small.mp4                 4 KB
//	  notes.txt                 1 KB
//	  Makefile                  1 KB
//	  .zshrc                    1 KB
//	  archive.tar.gz           16 KB
//	  linked-a.bin             64 KB  (hardlinked to linked-b.bin)
//	  linked-b.bin                    same inode as linked-a.bin
//	  shortcut.mp4                    symlink to big.mp4
//	  Sample.app/                     bundle: 2 files, 96 KB total
//	    Contents/MacOS/binary          64 KB
//	    Contents/Resources/icon.png    32 KB
//	  nested/
//	    clip.mov                256 KB
//	    deeper/
//	      data.json              8 KB
//
// Sizes are physical (blocks * 512), so exact byte totals depend on the
// filesystem's block size. Assertions built on this tree should check counts,
// grouping and reconciliation — the things a rollup bug breaks — plus lower
// bounds on bytes.
func Build(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(path string, size int) {
		t.Helper()
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatalf("fixture: write %s: %v", path, err)
		}
	}
	mkdir := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("fixture: mkdir %s: %v", path, err)
		}
	}

	write(filepath.Join(root, "big.mp4"), 512*KB)
	write(filepath.Join(root, "small.mp4"), 4*KB)
	write(filepath.Join(root, "notes.txt"), 1*KB)
	write(filepath.Join(root, "Makefile"), 1*KB)
	write(filepath.Join(root, ".zshrc"), 1*KB)
	write(filepath.Join(root, "archive.tar.gz"), 16*KB)
	write(filepath.Join(root, "linked-a.bin"), 64*KB)

	if err := os.Link(filepath.Join(root, "linked-a.bin"), filepath.Join(root, "linked-b.bin")); err != nil {
		t.Fatalf("fixture: link: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "big.mp4"), filepath.Join(root, "shortcut.mp4")); err != nil {
		t.Fatalf("fixture: symlink: %v", err)
	}

	mkdir(filepath.Join(root, "Sample.app", "Contents", "MacOS"))
	mkdir(filepath.Join(root, "Sample.app", "Contents", "Resources"))
	write(filepath.Join(root, "Sample.app", "Contents", "MacOS", "binary"), 64*KB)
	write(filepath.Join(root, "Sample.app", "Contents", "Resources", "icon.png"), 32*KB)

	mkdir(filepath.Join(root, "nested", "deeper"))
	write(filepath.Join(root, "nested", "clip.mov"), 256*KB)
	write(filepath.Join(root, "nested", "deeper", "data.json"), 8*KB)

	return root
}
