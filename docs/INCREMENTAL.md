# Incremental scanning

Reusing the previous scan instead of re-walking everything. **Implemented**;
measured results are at the bottom.

## What a scan costs

Full `$HOME` scan: **81s** — 53s walking, ~28s fingerprinting duplicate
candidates. 5.46M files across 728k directories.

Single-threaded traversal benchmarks:

| Traversal | Time | Syscalls |
| --- | --- | --- |
| `readdir` + stat directories only | 71s | 751k stats, 751k readdirs |
| `readdir` + stat every file | 208s | 6.3M stats, 751k readdirs |

File stats are roughly two thirds of the walk. But an unchanged directory does
not need its `readdir` either — the subdirectory list can come from cache — so
the real floor is **one `lstat` per directory**, around 751k calls, a few
seconds across twelve workers.

Measured incremental scan: **22.7s against 115.3s**, a 5.1x speedup, with 99%
of directories unchanged. Slower than the "under 10s" projection because the
directory stat pass is not the only fixed cost — see Results.

## The invariant this rests on

A directory's `mtime` changes when its **direct entries** change: a file created,
deleted or renamed. It does **not** change when:

- a file inside it is modified in place (that updates the file's `mtime`)
- anything changes in a subdirectory

The second point is harmless: every directory is stat'ed anyway, so deeper
changes are found on their own. The first point is the blind spot, handled
below.

## Cache

`.scan-cache/tree.sqlite` via `bun:sqlite` (gitignored — it maps a personal home
directory, same as snapshots).

JSON was rejected: at 728k directories the file would run to hundreds of
megabytes and be parsed in full on every scan. SQLite gives keyed lookups, a
fast bulk read, and row-level upserts.

```sql
CREATE TABLE dirs (
  path           TEXT PRIMARY KEY,
  dir_mtime_ms   REAL NOT NULL,  -- the validity check
  own_size       INTEGER NOT NULL,
  own_file_count INTEGER NOT NULL,
  max_mtime_ms   REAL NOT NULL,
  is_cloud       INTEGER NOT NULL,
  subdirs        TEXT NOT NULL,  -- JSON array, so unchanged dirs skip readdir
  ext_map        TEXT NOT NULL,  -- JSON: ext -> [bytes, count, maxSize, maxPath]
  entries        TEXT NOT NULL,  -- JSON: files at or above minFileSize
  candidates     TEXT NOT NULL,  -- JSON: [path, size, mtimeMs, fingerprint|null]
  hardlinks      TEXT NOT NULL   -- JSON: nlink > 1 records
);

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
```

`meta` holds a **config fingerprint**. Thresholds, bundle extensions, compound
extensions and the exclude list all change what a cached row means, so a change
to any of them invalidates the whole cache. `categoryMap` does not: categories
are applied when the snapshot is assembled, not during the walk.

## Flow

1. Main thread opens the cache and loads every row into a `Map<path, CachedDir>`
   (one query, ~1-2s at this scale).
2. When dispatching a directory it includes the cached `dirMtimeMs`:
   `{ type: 'dir', path, cachedMtimeMs }`.
3. The worker calls `lstat` on the directory.
   - **mtime matches** → replies `{ type: 'unchanged', path }` and stops. One
     syscall, no `readdir`, no file stats.
   - **mtime differs, or no cache entry** → full scan as today.
4. On `unchanged`, the main thread applies the cached row it already holds: adds
   the aggregates, enqueues the cached subdirectory list, replays cached entries
   and hardlink records. Nothing crosses the worker boundary but the verdict.
5. Changed directories overwrite their row; directories that vanished have their
   rows deleted (any cached path not visited this run).

## Fingerprint reuse

The duplicate pass is the other 28s. Cached candidates carry
`(path, size, mtimeMs, fingerprint)`. A candidate whose size **and** mtime match
the cache reuses its fingerprint and is never read again. Only genuinely new or
modified files get opened, which should take that pass to near zero on a quiet
rescan.

## The blind spot, and what is done about it

A file modified **in place without changing size or being recreated** leaves its
directory's `mtime` untouched, so an incremental scan would keep the stale size.
This is real for log files, SQLite databases, VM disk images and `.sst` files.

Three mitigations:

1. **Always rescan a directory that held anything large.** If a cached row
   contains an entry at or above `minFileSize`, the directory is rescanned
   regardless of its mtime. Few directories qualify (485 files cross the floor
   on this machine), so the cost is negligible — and these are exactly the files
   the report is about.
2. **Volatile extensions force a rescan** of their directory: `log`, `db`,
   `sqlite`, `sst`, `vmdk`, `qcow2`, `img`, `sparsebundle`.
3. **`--full` bypasses the cache** entirely.

The residual error is confined to files that are below the floor, of a
non-volatile type, and modified without changing size. Those cannot move the
report meaningfully.

## Verification

A tool whose bugs are invisible needs a way to prove the shortcut is honest, so
`--verify` runs a cached scan and a full scan back to back and diffs totals,
the folder tree and every extension aggregate, exiting non-zero on any
difference.

On a **quiet tree** (`~/go/pkg/mod`, 31,611 directories, 179,500 files) the two
agree **exactly**.

On a **live home directory** they differ by ~13 MB in 3.85 TB (0.0003%), because
the filesystem changes underneath the two runs: logs, browser caches, and
write-ahead logs are all being written while the scan runs. Comparing two full
scans of a live tree would show the same drift. The fixture tests in
`cache.test.ts` are the deterministic proof; `--verify` on a quiet subtree is
the proof on real data.

That first live run did find a genuine gap: `sqlite-wal` accounted for most of
the drift because only the `db-wal` spelling was in the volatile list. Fixed,
along with `sqlite-shm`, `wal`, `shm`, `journal`, `tracev3` and `jsonl`.

## Results

| | Cold | Warm |
| --- | --- | --- |
| Duration | 115.3s | **22.7s** |
| Directories unchanged | 0 of 737,236 | 730,669 of 737,231 (99%) |
| Fingerprints reused | 0 | 239,204 of 244,102 |

Two costs are worth knowing about:

**Cold scans got ~17% slower** (98s to 115s). Caching per-directory aggregates
means workers must send per-directory extension deltas, which the original
design deliberately avoided. The deltas are compressed — sparse histogram
buckets, and a basename instead of a full path for each extension's largest
file — but they are not free. The trade is deliberate: warm scans are the common
case.

**The cache file is 422 MB.** Larger than the 150-250 MB estimated, and ironic
for a disk-cleanup tool. The weight is 737k rows of JSON. It could be cut
roughly in half by encoding rows as positional arrays rather than keyed objects,
which is not yet done.

## Rejected alternatives

**FSEvents replay.** macOS keeps a persistent event log per volume, and
`FSEventStreamCreate` can replay everything since a stored event ID — which is
how Spotlight and Time Machine do incremental work. It would beat directory
mtimes outright, invalidating exact subtrees instead of stat'ing all 751k
directories. Rejected for now because reaching it from Bun means FFI into
CoreServices or a native helper: the most fragile dependency in the project, for
a saving measured in a few seconds. Worth revisiting if the directory stat pass
turns out to dominate.

**A watcher daemon.** Keeping a live tree in a background process is the fastest
possible answer and the wrong shape for a tool that runs occasionally, on
purpose, from a terminal.
