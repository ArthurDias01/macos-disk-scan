import type { Category, FileEntry } from '@shared/schema'

/** Roughly a month; calendar precision is meaningless for a staleness cut. */
const MONTH_MS = 2_629_800_000
export const DAY_MS = 86_400_000

export interface StaleThresholds {
  /** Minimum size in bytes to count as worth deleting. */
  minSize: number
  /** Minimum age in months since the file last changed. */
  minMonths: number
}

export const DEFAULT_THRESHOLDS: StaleThresholds = {
  minSize: 1024 ** 3,
  minMonths: 12,
}

export interface StalePoint {
  path: string
  name: string
  category: Category
  size: number
  /** Days since the file last changed. */
  ageDays: number
  /** log2(size), so the axis can be linear while sizes span nine orders. */
  sizeLog: number
  /** Deleting it alone frees nothing: a clone copy or hardlink twin. */
  sharesBlocks: boolean
  /** Inside the big-and-old quadrant. */
  candidate: boolean
}

export interface StaleSummary {
  /** Files that are both big enough and old enough. */
  candidates: FileEntry[]
  points: StalePoint[]
  /** Allocated bytes in the quadrant. */
  bytes: number
  /** Bytes that deleting the quadrant would actually free. */
  freeableBytes: number
  /** The oldest candidate, which is usually the most obviously abandoned. */
  oldest: FileEntry | null
}

/**
 * Split files into the big-and-old quadrant and everything else.
 *
 * Size alone says what is expensive; age alone says what is forgotten. Neither
 * is a delete list on its own — a 30 GB VM image touched this morning is in use,
 * and a 2 KB note from 2011 is not worth finding. The intersection is the list.
 *
 * `mtime` is the axis rather than `atime`: macOS bumps access times during
 * Spotlight indexing and backups, so `atime` would report half the disk as
 * freshly used.
 */
export function analyseStale(
  files: readonly FileEntry[],
  thresholds: StaleThresholds,
  now = Date.now(),
): StaleSummary {
  const cutoff = now - thresholds.minMonths * MONTH_MS
  const candidates: FileEntry[] = []
  const points: StalePoint[] = []
  let bytes = 0
  let freeableBytes = 0
  let oldest: FileEntry | null = null

  for (const file of files) {
    // A future mtime is corrupt data, not a fresh file; clamp so it cannot
    // drag the axis 2,800 years to the right.
    const ageDays = Math.max(0, (now - file.mtimeMs) / DAY_MS)
    const sharesBlocks = Boolean(file.isDuplicateCopy) || file.isDupInode
    const candidate = file.size >= thresholds.minSize && file.mtimeMs <= cutoff

    points.push({
      path: file.path,
      name: file.path.slice(file.path.lastIndexOf('/') + 1),
      category: file.category,
      size: file.size,
      ageDays,
      sizeLog: file.size > 0 ? Math.log2(file.size) : 0,
      sharesBlocks,
      candidate,
    })

    if (!candidate) continue
    candidates.push(file)
    bytes += file.size
    if (!sharesBlocks) freeableBytes += file.size
    if (!oldest || file.mtimeMs < oldest.mtimeMs) oldest = file
  }

  candidates.sort((a, b) => b.size - a.size)
  return { candidates, points, bytes, freeableBytes, oldest }
}

/** Axis ticks are powers of two, so the label is the byte value they stand for. */
export function sizeFromLog(value: number): number {
  return 2 ** value
}

export function monthsToDays(months: number): number {
  return (months * MONTH_MS) / DAY_MS
}
