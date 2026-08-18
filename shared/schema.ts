/**
 * Snapshot schema shared by the scanner (Bun, worker threads) and the SPA.
 *
 * This file must stay dependency-free: it is imported by worker threads, the
 * CLI, and the browser bundle alike.
 */

/** Bumped whenever a change makes older snapshots unreadable by the SPA. */
export const SCHEMA_VERSION = 1

/**
 * Coarse grouping layered on top of raw extensions. Two hundred extensions is
 * an unreadable chart; ten categories is a story.
 */
export type Category =
  | 'video'
  | 'image'
  | 'audio'
  | 'archive'
  | 'diskimage'
  | 'code'
  | 'cache'
  | 'document'
  | 'binary'
  | 'data'
  | 'other'

export const CATEGORIES: readonly Category[] = [
  'video',
  'image',
  'audio',
  'archive',
  'diskimage',
  'code',
  'cache',
  'document',
  'binary',
  'data',
  'other',
]

/** Extension used for files that have none (`Makefile`, `.zshrc`, binaries). */
export const NO_EXTENSION = ''

/**
 * Size histograms use power-of-two buckets: index `i` counts files whose size
 * falls in `[2^i, 2^(i+1))`. Bucket 0 also absorbs zero-byte files. 48 buckets
 * reaches 281 TB, comfortably past any single file. Histograms are mergeable,
 * which is what lets each worker aggregate locally (see docs/DECISIONS.md).
 */
export const HISTOGRAM_BUCKETS = 48

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

export interface ScanConfig {
  /** Absolute roots to walk. Defaults to `[$HOME]`. */
  roots: string[]
  /** Files at or above this many bytes are reported individually. */
  minFileSize: number
  /** Folders whose recursive size is at or above this appear in the tree. */
  minFolderSize: number
  /** Cap on individually reported files per extension. */
  topNPerExtension: number
  /** Cap on individually reported files overall. */
  globalTopN: number
  /** Worker threads used for the walk. */
  workerCount: number
  /** Directory extensions treated as one atomic item (`app`, `photoslibrary`). */
  bundleExtensions: string[]
  /** Multi-segment extensions preserved whole (`tar.gz`). */
  compoundExtensions: string[]
  /** Extension -> category. Extensions absent here fall back to `other`. */
  categoryMap: Record<string, Category>
  /** Absolute paths (or path prefixes) skipped entirely. */
  excludePaths: string[]
  /** Symlinks are never followed when false — cycles and double counting. */
  followSymlinks: boolean
  /** When false the walk stops at any `st_dev` change (mounts, network shares). */
  crossFilesystems: boolean
  /**
   * Second pass that fingerprints files sharing an exact size, to find
   * byte-identical groups (APFS clones and real duplicates alike).
   */
  detectDuplicates: boolean
  /** Only files at or above this size are considered for fingerprinting. */
  duplicateMinSize: number
}

// ---------------------------------------------------------------------------
// Entries
// ---------------------------------------------------------------------------

export interface FileEntry {
  /** Absolute path. */
  path: string
  /** Normalized extension, `''` when the file has none. */
  ext: string
  category: Category
  /** Physical bytes on disk (`blocks * 512`), not logical size. */
  size: number
  /** @go float64 — stat returns sub-millisecond precision. */
  mtimeMs: number
  /**
   * Unreliable on macOS: Spotlight, backups and indexers bump it.
   * @go float64
   */
  atimeMs: number
  /** @go float64 */
  birthtimeMs: number
  /** A directory treated as one atomic item (`.app`, `.photoslibrary`). */
  isBundle: boolean
  /** Lives under iCloud Drive or a CloudStorage provider — may be evicted. */
  isCloud: boolean
  /** `st_nlink > 1`: the same bytes are reachable from another path. */
  isHardlink: boolean
  /**
   * A hardlink whose inode was already counted at another path. Its bytes are
   * excluded from every total, and deleting it alone frees nothing.
   */
  isDupInode: boolean
  nlink: number
  /** Fingerprint shared by byte-identical files, when duplicate detection ran. */
  duplicateGroup?: string
  /** How many files carry this fingerprint, including this one. */
  duplicateCopies?: number
  /** True for every copy but the first seen — the redundant ones. */
  isDuplicateCopy?: boolean
}

/**
 * A set of byte-identical files.
 *
 * Two files can be identical for two very different reasons, and `stat` cannot
 * tell them apart:
 *
 * - **APFS clones** share the same blocks. Each copy is billed its full size by
 *   `stat` and `du`, but deleting one frees nothing; the group really occupies
 *   `size` bytes, not `size * count`.
 * - **Real duplicates** each occupy their own blocks. The group really occupies
 *   `size * count`, and deleting the extra copies frees `reclaimableBytes`.
 *
 * Distinguishing them needs APFS-specific introspection (clone IDs), so the
 * snapshot reports the group and lets the UI present both readings. When the
 * scan total exceeds the volume's used bytes, clones are proven to be present.
 */
export interface DuplicateGroup {
  fingerprint: string
  /** Size of one copy, in physical bytes. */
  size: number
  count: number
  /** `size * (count - 1)` — freed by deleting the extras, if they are real copies. */
  reclaimableBytes: number
  ext: string
  category: Category
  /** Capped sample of member paths, biggest-group-first ordering preserved. */
  paths: string[]
}

/**
 * The verdict on a path the user meant to delete.
 *
 * The app cannot write files and the scanner cannot read a browser, so the
 * bridge is the cleanup script: it appends every path it actually moved to a
 * ledger, and the next scan checks those paths **before** walking anything.
 */
export interface RecheckedPath {
  path: string
  /** Recorded when the cleanup script moved it. */
  intendedAt: string
  /** Still on disk at scan time — the deletion did not happen or was undone. */
  present: boolean
  /** Physical bytes if still present. */
  size: number
}

/** Volume figures from `df`, used to sanity-check the scan against reality. */
export interface VolumeInfo {
  path: string
  totalBytes: number
  usedBytes: number
  availableBytes: number
}

export interface FolderNode {
  path: string
  /** Basename, for display. */
  name: string
  /** Folder plus every descendant. Overlaps ancestors — never sum siblings and parents. */
  recursiveSize: number
  /** Files directly inside this folder only. Safe to sum across a flat list. */
  ownSize: number
  fileCount: number
  ownFileCount: number
  /**
   * Freshest `mtime` anywhere beneath — the folder's real activity signal.
   * @go float64
   */
  maxMtimeMs: number
  isCloud: boolean
  /** Bytes held by redundant copies among this folder's own files. */
  duplicateOwnSize: number
  /** Same, across the whole subtree. `recursiveSize` minus this is the floor. */
  duplicateRecursiveSize: number
  /** Only children at or above `minFolderSize`. */
  children: FolderNode[]
  /** Children omitted for being under threshold, and their combined weight. */
  truncatedChildCount: number
  truncatedSize: number
}

export interface ExtensionStat {
  ext: string
  category: Category
  totalSize: number
  fileCount: number
  /** Rounded: a mean of byte counts is still reported in whole bytes. */
  meanSize: number
  /** Approximated from the histogram — accurate to a power of two. */
  medianSize: number
  p95Size: number
  maxSize: number
  /** Path of the single biggest file with this extension. */
  largestPath: string
  /** Bytes held by redundant copies of byte-identical files of this type. */
  duplicateBytes: number
  histogram: number[]
}

export interface CategoryStat {
  category: Category
  totalSize: number
  fileCount: number
  /** Bytes held by redundant copies within this category. */
  duplicateBytes: number
  /** Extensions contributing to this category, biggest first. */
  extensions: string[]
}

export interface ScanError {
  path: string
  /** errno code: `EPERM`, `EACCES`, `ELOOP`, ... */
  code: string
  message: string
}

// ---------------------------------------------------------------------------
// Snapshot
// ---------------------------------------------------------------------------

export interface ScanTotals {
  /**
   * Physical bytes as the filesystem reports them, after hardlink dedup. This
   * is what `du` and Finder show, and it over-counts APFS clones.
   */
  bytes: number
  files: number
  dirs: number
  /** Bytes skipped because another path already claimed the inode. */
  dedupedBytes: number
  dedupedFiles: number
  /** Bytes under paths that could not be read (always 0 — listed for clarity). */
  unreadablePaths: number
  /** `bytes` minus every redundant copy: the floor of what is really stored. */
  uniqueBytes: number
  /** Bytes held by redundant copies of byte-identical files. */
  duplicateBytes: number
  duplicateFiles: number
}

export interface ScanSnapshot {
  schemaVersion: number
  /** `scan-2026-08-16T14-32-05`, also the filename stem. */
  id: string
  startedAt: string
  finishedAt: string
  durationMs: number
  hostname: string
  /** The config this scan actually ran with, so old snapshots stay self-describing. */
  config: ScanConfig
  totals: ScanTotals
  extensions: ExtensionStat[]
  categories: CategoryStat[]
  /** One node per root, wrapped in a synthetic node when there are several. */
  folderTree: FolderNode
  /** Every file at or above `minFileSize`, subject to the topN caps, biggest first. */
  files: FileEntry[]
  /** Byte-identical file groups, biggest reclaimable weight first. */
  duplicateGroups: DuplicateGroup[]
  /** Volume figures at scan time, for reconciling the total against reality. */
  volume: VolumeInfo | null
  /** Paths from the deletion ledger, checked first and reported here. */
  rechecked: RecheckedPath[]
  /** Paths that could not be read — usually TCC-protected without Full Disk Access. */
  unscanned: ScanError[]
  warnings: string[]
}

export interface SnapshotIndexEntry {
  id: string
  /** Filename relative to the data directory. */
  file: string
  startedAt: string
  durationMs: number
  totalBytes: number
  totalFiles: number
  schemaVersion: number
}

export interface SnapshotIndex {
  /** Newest first. */
  snapshots: SnapshotIndexEntry[]
}

// ---------------------------------------------------------------------------
// Histogram helpers
// ---------------------------------------------------------------------------

export function histogramBucket(size: number): number {
  if (size < 2) return 0
  // clz32 only sees the low 32 bits, so anything past 4 GB uses a log instead.
  const bucket =
    size > 0xffffffff ? Math.floor(Math.log2(size)) : 31 - Math.clz32(size)
  return Math.min(HISTOGRAM_BUCKETS - 1, bucket)
}

export function emptyHistogram(): number[] {
  return new Array(HISTOGRAM_BUCKETS).fill(0)
}

/** Approximate quantile from a power-of-two histogram, to the bucket's lower edge. */
export function histogramQuantile(histogram: number[], q: number): number {
  const total = histogram.reduce((sum, count) => sum + count, 0)
  if (total === 0) return 0
  const target = total * q
  let seen = 0
  for (let i = 0; i < histogram.length; i++) {
    seen += histogram[i]
    if (seen >= target) return i === 0 ? 0 : 2 ** i
  }
  return 2 ** (histogram.length - 1)
}
