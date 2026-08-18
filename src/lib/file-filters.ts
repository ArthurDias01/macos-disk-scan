import type { Category, FileEntry } from '@shared/schema'

export type FileSort = 'size' | 'age' | 'path'

export interface FileFilters {
  /** Empty means every category. */
  cats: Category[]
  /** Case-insensitive substring of the full path. */
  q: string
  /** Minimum size in bytes. */
  min: number
  /** Only files whose newest change is at least this many months ago. */
  olderMonths: number
  /** Only redundant copies of byte-identical files. */
  dup: boolean
  /** Hide cloud-backed files, which are usually evicted and free nothing. */
  hideCloud: boolean
  sort: FileSort
  desc: boolean
}

export const DEFAULT_FILTERS: FileFilters = {
  cats: [],
  q: '',
  min: 0,
  olderMonths: 0,
  dup: false,
  hideCloud: false,
  sort: 'size',
  desc: true,
}

/** Roughly a month; exact calendar months are meaningless for a staleness cut. */
const MONTH_MS = 2_629_800_000

export function filterFiles(
  files: readonly FileEntry[],
  filters: FileFilters,
  now = Date.now(),
): FileEntry[] {
  const query = filters.q.trim().toLowerCase()
  const cats = filters.cats.length > 0 ? new Set(filters.cats) : null
  const cutoff = filters.olderMonths > 0 ? now - filters.olderMonths * MONTH_MS : null

  // One pass: the list can reach thousands of rows once the size floor is
  // lowered, and every filter change re-runs this.
  const result: FileEntry[] = []
  for (const file of files) {
    if (file.size < filters.min) continue
    if (cats && !cats.has(file.category)) continue
    if (cutoff !== null && file.mtimeMs > cutoff) continue
    if (filters.dup && !file.isDuplicateCopy && !file.isDupInode) continue
    if (filters.hideCloud && file.isCloud) continue
    if (query && !file.path.toLowerCase().includes(query)) continue
    result.push(file)
  }
  return result
}

export function sortFiles(
  files: FileEntry[],
  sort: FileSort,
  desc: boolean,
): FileEntry[] {
  const direction = desc ? -1 : 1
  const compare = (a: FileEntry, b: FileEntry) => {
    switch (sort) {
      case 'size':
        return (a.size - b.size) * direction
      case 'age':
        return (a.mtimeMs - b.mtimeMs) * direction
      case 'path':
        return a.path.localeCompare(b.path) * direction
    }
  }
  // toSorted keeps the snapshot's array immutable — it is shared with every
  // other view and React Query's cache.
  return files.toSorted(compare)
}

export interface FileSummary {
  count: number
  /** Allocated bytes, as the filesystem reports them. */
  bytes: number
  /** Bytes whose deletion would actually free space. */
  uniqueBytes: number
  /** Files that free nothing on their own: clones and hardlink twins. */
  sharedCount: number
}

export function summarize(files: readonly FileEntry[]): FileSummary {
  let bytes = 0
  let uniqueBytes = 0
  let sharedCount = 0
  for (const file of files) {
    bytes += file.size
    const shared = Boolean(file.isDuplicateCopy) || file.isDupInode
    if (shared) sharedCount += 1
    else uniqueBytes += file.size
  }
  return { count: files.length, bytes, uniqueBytes, sharedCount }
}

/** Categories actually present, with counts, for building the facet chips. */
export function categoryFacets(
  files: readonly FileEntry[],
): Array<{ category: Category; count: number }> {
  const counts = new Map<Category, number>()
  for (const file of files) {
    counts.set(file.category, (counts.get(file.category) ?? 0) + 1)
  }
  return [...counts.entries()]
    .map(([category, count]) => ({ category, count }))
    .sort((a, b) => b.count - a.count)
}
