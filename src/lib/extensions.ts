import { HISTOGRAM_BUCKETS, type Category, type ExtensionStat } from '@shared/schema'
import { sizeIn, type SizeMode } from './size-mode'

export type ExtensionSort = 'size' | 'count' | 'largest' | 'name'

export interface ExtensionFilters {
  q: string
  cats: Category[]
  sort: ExtensionSort
  desc: boolean
}

export const DEFAULT_EXTENSION_FILTERS: ExtensionFilters = {
  q: '',
  cats: [],
  sort: 'size',
  desc: true,
}

/** Rows shown before the long tail is folded into a single summary. */
export const HEAD_COUNT = 40

export interface ExtensionRow {
  stat: ExtensionStat
  /** Size under the active reading. */
  bytes: number
  /** Share of all reported bytes, for the inline bar. */
  share: number
}

export interface RankedExtensions {
  head: ExtensionRow[]
  /** Everything past the head: real, counted, but not worth a row each. */
  tail: { count: number; bytes: number; files: number }
  /** Rows matching the filters, before the head/tail split. */
  matched: number
  totalBytes: number
}

export function displayExtension(ext: string): string {
  return ext === '' ? '(none)' : `.${ext}`
}

/**
 * Rank extensions and fold the long tail.
 *
 * A real home directory produces around eleven thousand distinct extensions,
 * almost all of them junk parsed out of dotted filenames. Listing them is
 * useless; hiding their weight would be dishonest. So the head is listed and
 * the tail is summarised with its combined size.
 */
export function rankExtensions(
  extensions: readonly ExtensionStat[],
  filters: ExtensionFilters,
  mode: SizeMode,
  headCount = HEAD_COUNT,
): RankedExtensions {
  const query = filters.q.trim().toLowerCase()
  const cats = filters.cats.length > 0 ? new Set(filters.cats) : null

  const rows: ExtensionRow[] = []
  let totalBytes = 0

  for (const stat of extensions) {
    const bytes = sizeIn(mode, stat.totalSize, stat.duplicateBytes)
    totalBytes += bytes
    if (cats && !cats.has(stat.category)) continue
    if (query && !displayExtension(stat.ext).toLowerCase().includes(query)) continue
    rows.push({ stat, bytes, share: 0 })
  }

  rows.sort((a, b) => {
    const direction = filters.desc ? -1 : 1
    switch (filters.sort) {
      case 'size':
        return (a.bytes - b.bytes) * direction
      case 'count':
        return (a.stat.fileCount - b.stat.fileCount) * direction
      case 'largest':
        return (a.stat.maxSize - b.stat.maxSize) * direction
      case 'name':
        return a.stat.ext.localeCompare(b.stat.ext) * direction
    }
  })

  const biggest = rows.length > 0 ? Math.max(...rows.map((row) => row.bytes)) : 0
  for (const row of rows) {
    row.share = biggest > 0 ? row.bytes / biggest : 0
  }

  const head = rows.slice(0, headCount)
  const tailRows = rows.slice(headCount)

  return {
    head,
    tail: {
      count: tailRows.length,
      bytes: tailRows.reduce((sum, row) => sum + row.bytes, 0),
      files: tailRows.reduce((sum, row) => sum + row.stat.fileCount, 0),
    },
    matched: rows.length,
    totalBytes,
  }
}

export interface HistogramBar {
  /** Lower edge of the bucket in bytes: files here are `[edge, edge*2)`. */
  edge: number
  count: number
}

/**
 * Turn the stored power-of-two histogram into chart rows.
 *
 * Empty buckets at either end are dropped: a distribution running from 1 byte
 * to 256 TB is mostly whitespace, and the interesting range is narrow.
 */
export function histogramBars(histogram: readonly number[]): HistogramBar[] {
  let first = histogram.findIndex((count) => count > 0)
  if (first === -1) return []
  let last = histogram.length - 1
  while (last > first && histogram[last] === 0) last--

  const bars: HistogramBar[] = []
  for (let i = first; i <= last && i < HISTOGRAM_BUCKETS; i++) {
    bars.push({ edge: i === 0 ? 0 : 2 ** i, count: histogram[i] })
  }
  return bars
}
