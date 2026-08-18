// Package cache stores one row per directory so an unchanged directory costs a
// single lstat on the next scan.
//
// The file is interchangeable with the one the TypeScript scanner writes: same
// table schema, same JSON payloads, and a config fingerprint computed to the
// same bytes. Either scanner can warm the other's cache, which is what makes a
// side-by-side parity run meaningful rather than a comparison of two cold runs.
package cache

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"disk-report/internal/schema"
	"disk-report/internal/walk"
)

const ddl = `
CREATE TABLE IF NOT EXISTS dirs (
  path            TEXT PRIMARY KEY,
  dir_mtime_ms    REAL NOT NULL,
  own_size        INTEGER NOT NULL,
  own_file_count  INTEGER NOT NULL,
  max_mtime_ms    REAL NOT NULL,
  is_cloud        INTEGER NOT NULL,
  volatile        INTEGER NOT NULL,
  subdirs         TEXT NOT NULL,
  ext_map         TEXT NOT NULL,
  entries         TEXT NOT NULL,
  candidates      TEXT NOT NULL,
  hardlinks       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// Cache is an open scan cache.
type Cache struct {
	db *sql.DB
}

// fingerprintFields are the config values that change what a cached row *means*.
//
// The field order is the property order of the object literal in
// scanner/cache.ts, because JSON.stringify emits properties in insertion order
// and the two fingerprints have to be the same string.
//
// categoryMap is absent on purpose: categories are applied when the snapshot is
// assembled, never during the walk, so retagging an extension does not
// invalidate anything. roots is absent too — rows are keyed by absolute path
// and stay valid whichever root reached them. Including roots once meant that
// scanning any subtree discarded the cache for the whole home directory.
type fingerprintFields struct {
	MinFileSize        int64    `json:"minFileSize"`
	DuplicateMinSize   int64    `json:"duplicateMinSize"`
	DetectDuplicates   bool     `json:"detectDuplicates"`
	BundleExtensions   []string `json:"bundleExtensions"`
	CompoundExtensions []string `json:"compoundExtensions"`
	ExcludePaths       []string `json:"excludePaths"`
	FollowSymlinks     bool     `json:"followSymlinks"`
	CrossFilesystems   bool     `json:"crossFilesystems"`
}

func configFingerprint(config schema.ScanConfig) (string, error) {
	return encodeJSON(fingerprintFields{
		MinFileSize:        config.MinFileSize,
		DuplicateMinSize:   config.DuplicateMinSize,
		DetectDuplicates:   config.DetectDuplicates,
		BundleExtensions:   sorted(config.BundleExtensions),
		CompoundExtensions: sorted(config.CompoundExtensions),
		ExcludePaths:       sorted(config.ExcludePaths),
		FollowSymlinks:     config.FollowSymlinks,
		CrossFilesystems:   config.CrossFilesystems,
	})
}

// Open prepares the cache, discarding it wholesale when the config fingerprint
// has moved: a changed threshold or bundle list makes every stored aggregate a
// lie, and there is no partial repair worth attempting.
//
// The second return reports whether an existing cache was discarded, which is
// worth telling the user — it explains a warm scan that suddenly takes minutes.
func Open(path string, config schema.ScanConfig) (*Cache, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, false, err
	}
	// One connection: the walk writes from a single goroutine, and SQLite's
	// pure-Go driver has nothing to gain from a pool here.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, false, err
	}
	if _, err := db.Exec(ddl); err != nil {
		db.Close()
		return nil, false, err
	}

	fingerprint, err := configFingerprint(config)
	if err != nil {
		db.Close()
		return nil, false, err
	}

	var stored string
	err = db.QueryRow("SELECT value FROM meta WHERE key = 'config'").Scan(&stored)
	hadConfig := err == nil
	if err != nil && err != sql.ErrNoRows {
		db.Close()
		return nil, false, err
	}

	reset := false
	if stored != fingerprint {
		if _, err := db.Exec("DELETE FROM dirs"); err != nil {
			db.Close()
			return nil, false, err
		}
		if _, err := db.Exec(
			"INSERT OR REPLACE INTO meta (key, value) VALUES ('config', ?)", fingerprint,
		); err != nil {
			db.Close()
			return nil, false, err
		}
		reset = hadConfig
	}

	return &Cache{db: db}, reset, nil
}

// Load reads every stored row.
//
// Rows come back as DirDelta so a cache hit replays exactly what the walk would
// have produced — no parallel type, no translation step, and no way for the two
// to drift.
func (c *Cache) Load() (map[string]*walk.DirDelta, error) {
	rows, err := c.db.Query(`
		SELECT path, dir_mtime_ms, own_size, own_file_count, max_mtime_ms,
		       is_cloud, volatile, subdirs, ext_map, entries, candidates, hardlinks
		FROM dirs
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cached := map[string]*walk.DirDelta{}
	for rows.Next() {
		var (
			delta                        walk.DirDelta
			isCloud, volatile            int
			subdirs, extMap              string
			entries, candidates, hardlin string
		)

		if err := rows.Scan(
			&delta.Path, &delta.DirMtimeMs, &delta.OwnSize, &delta.OwnFileCount,
			&delta.MaxMtimeMs, &isCloud, &volatile,
			&subdirs, &extMap, &entries, &candidates, &hardlin,
		); err != nil {
			return nil, err
		}

		delta.IsCloud = isCloud == 1
		delta.Volatile = volatile == 1

		for _, decode := range []struct {
			raw    string
			target any
		}{
			{subdirs, &delta.SubdirNames},
			{extMap, &delta.ExtDelta},
			{entries, &delta.Entries},
			{candidates, &delta.Candidates},
			{hardlin, &delta.Hardlinks},
		} {
			if err := json.Unmarshal([]byte(decode.raw), decode.target); err != nil {
				return nil, fmt.Errorf("%s: %w", delta.Path, err)
			}
		}

		// Errors are transient: a cached directory was readable when stored.
		delta.Errors = nil

		row := delta
		cached[delta.Path] = &row
	}

	return cached, rows.Err()
}

// Save replaces the rows for directories visited this run and drops the ones
// that have vanished.
//
// Pruning is scoped to roots: a scan of one subtree must not delete rows for
// directories it never looked at.
func (c *Cache) Save(deltas []*walk.DirDelta, visited map[string]bool, roots []string) error {
	if err := c.upsert(deltas); err != nil {
		return err
	}
	return c.prune(visited, roots)
}

func (c *Cache) upsert(deltas []*walk.DirDelta) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO dirs
			(path, dir_mtime_ms, own_size, own_file_count, max_mtime_ms, is_cloud,
			 volatile, subdirs, ext_map, entries, candidates, hardlinks)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, delta := range deltas {
		payloads := make([]string, 0, 5)
		for _, value := range []any{
			emptyList(delta.SubdirNames),
			emptyMap(delta.ExtDelta),
			emptyList(delta.Entries),
			emptyList(delta.Candidates),
			emptyList(delta.Hardlinks),
		} {
			encoded, err := encodeJSON(value)
			if err != nil {
				return fmt.Errorf("%s: %w", delta.Path, err)
			}
			payloads = append(payloads, encoded)
		}

		if _, err := stmt.Exec(
			delta.Path, delta.DirMtimeMs, delta.OwnSize, delta.OwnFileCount,
			delta.MaxMtimeMs, boolToInt(delta.IsCloud), boolToInt(delta.Volatile),
			payloads[0], payloads[1], payloads[2], payloads[3], payloads[4],
		); err != nil {
			return fmt.Errorf("%s: %w", delta.Path, err)
		}
	}

	return tx.Commit()
}

// prune drops rows under a scanned root that were not seen this run: those
// directories are gone from disk. Rows outside those roots were never
// candidates and are left alone.
func (c *Cache) prune(visited map[string]bool, roots []string) error {
	rows, err := c.db.Query("SELECT path FROM dirs")
	if err != nil {
		return err
	}

	var stale []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return err
		}
		if inScope(path, roots) && !visited[path] {
			stale = append(stale, path)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}

	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("DELETE FROM dirs WHERE path = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, path := range stale {
		if _, err := stmt.Exec(path); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Close releases the database.
func (c *Cache) Close() error {
	return c.db.Close()
}

func inScope(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// encodeJSON matches JSON.stringify: no HTML escaping, no trailing newline.
// Go's default encoder rewrites `<`, `>` and `&` as escapes, which would make
// the config fingerprint differ from the TypeScript one for any path holding an
// ampersand — and silently discard the cache on every alternate run.
func encodeJSON(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimRight(buffer.String(), "\n"), nil
}

func sorted(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// emptyList keeps nil slices out of the stored JSON. A nil slice encodes as
// `null`, and the TypeScript scanner iterates these lists straight after
// JSON.parse — reading a row this scanner wrote would throw.
func emptyList[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

// emptyMap is emptyList for the extension map, which has the same problem:
// `Object.entries(null)` throws.
func emptyMap(values walk.DirExtMap) walk.DirExtMap {
	if values == nil {
		return walk.DirExtMap{}
	}
	return values
}
