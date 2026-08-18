import { describe, expect, test } from 'bun:test'
import type { Category, FileEntry } from '@shared/schema'
import {
  DEFAULT_FILTERS,
  categoryFacets,
  filterFiles,
  sortFiles,
  summarize,
} from './file-filters'

const NOW = Date.UTC(2026, 0, 1)
const MONTH = 2_629_800_000

function file(partial: Partial<FileEntry> & { path: string }): FileEntry {
  return {
    ext: 'mp4',
    category: 'video' as Category,
    size: 1000,
    mtimeMs: NOW,
    atimeMs: NOW,
    birthtimeMs: NOW,
    isBundle: false,
    isCloud: false,
    isHardlink: false,
    isDupInode: false,
    nlink: 1,
    ...partial,
  }
}

const files: FileEntry[] = [
  file({ path: '/home/movies/big.mp4', size: 5000, mtimeMs: NOW - 24 * MONTH }),
  file({ path: '/home/movies/small.mp4', size: 500, mtimeMs: NOW }),
  file({ path: '/home/photos/pic.jpg', ext: 'jpg', category: 'image', size: 2000 }),
  file({
    path: '/home/cloud/report.pdf',
    ext: 'pdf',
    category: 'document',
    size: 3000,
    isCloud: true,
  }),
  file({ path: '/home/movies/copy.mp4', size: 5000, isDuplicateCopy: true }),
  file({ path: '/home/links/twin.bin', ext: 'bin', category: 'binary', isDupInode: true }),
]

const filters = (overrides: Partial<typeof DEFAULT_FILTERS> = {}) => ({
  ...DEFAULT_FILTERS,
  ...overrides,
})

describe('filterFiles', () => {
  test('returns everything by default', () => {
    expect(filterFiles(files, filters(), NOW)).toHaveLength(files.length)
  })

  test('filters by category', () => {
    const result = filterFiles(files, filters({ cats: ['image', 'document'] }), NOW)
    expect(result.map((entry) => entry.ext).sort()).toEqual(['jpg', 'pdf'])
  })

  test('filters by minimum size', () => {
    const result = filterFiles(files, filters({ min: 3000 }), NOW)
    expect(result.every((entry) => entry.size >= 3000)).toBe(true)
    expect(result).toHaveLength(3)
  })

  test('filters by path substring, case-insensitively', () => {
    expect(filterFiles(files, filters({ q: 'MOVIES' }), NOW)).toHaveLength(3)
    expect(filterFiles(files, filters({ q: '  photos  ' }), NOW)).toHaveLength(1)
  })

  test('filters by age, keeping only files older than the cut', () => {
    const result = filterFiles(files, filters({ olderMonths: 12 }), NOW)
    expect(result.map((entry) => entry.path)).toEqual(['/home/movies/big.mp4'])
  })

  test('isolates files that free nothing when deleted', () => {
    const result = filterFiles(files, filters({ dup: true }), NOW)
    expect(result.map((entry) => entry.path).sort()).toEqual([
      '/home/links/twin.bin',
      '/home/movies/copy.mp4',
    ])
  })

  test('hides cloud-backed files on request', () => {
    const result = filterFiles(files, filters({ hideCloud: true }), NOW)
    expect(result.some((entry) => entry.isCloud)).toBe(false)
  })

  test('combines filters', () => {
    const result = filterFiles(
      files,
      filters({ cats: ['video'], min: 1000, q: 'movies' }),
      NOW,
    )
    expect(result.map((entry) => entry.path).sort()).toEqual([
      '/home/movies/big.mp4',
      '/home/movies/copy.mp4',
    ])
  })
})

describe('sortFiles', () => {
  test('sorts by size descending by default', () => {
    const sorted = sortFiles(files, 'size', true)
    expect(sorted[0].size).toBe(5000)
    expect(sorted[sorted.length - 1].size).toBe(500)
  })

  test('sorts by age with oldest first when ascending', () => {
    const sorted = sortFiles(files, 'age', false)
    expect(sorted[0].path).toBe('/home/movies/big.mp4')
  })

  test('sorts by path alphabetically', () => {
    const sorted = sortFiles(files, 'path', false)
    expect(sorted[0].path).toBe('/home/cloud/report.pdf')
  })

  test('does not mutate the input', () => {
    const original = [...files]
    sortFiles(files, 'path', true)
    expect(files).toEqual(original)
  })
})

describe('summarize', () => {
  test('separates what deleting would actually free', () => {
    const summary = summarize(files)
    expect(summary.count).toBe(6)
    expect(summary.bytes).toBe(5000 + 500 + 2000 + 3000 + 5000 + 1000)
    // The clone copy and the hardlink twin free nothing on their own.
    expect(summary.sharedCount).toBe(2)
    expect(summary.uniqueBytes).toBe(summary.bytes - 5000 - 1000)
  })

  test('handles an empty list', () => {
    expect(summarize([])).toEqual({
      count: 0,
      bytes: 0,
      uniqueBytes: 0,
      sharedCount: 0,
    })
  })
})

describe('categoryFacets', () => {
  test('counts categories present, biggest first', () => {
    const facets = categoryFacets(files)
    expect(facets[0]).toEqual({ category: 'video', count: 3 })
    expect(facets.map((facet) => facet.category)).not.toContain('audio')
  })
})

describe('formatAge', () => {
  const HOUR = 3_600_000
  const DAY = 86_400_000

  test('reports each gap in its own unit', async () => {
    const { formatAge } = await import('./format')
    // Regression: these all used to be reported one unit too large, so twelve
    // hours read as "12 days ago" and seven months as "7 years ago".
    expect(formatAge(Date.now() - 12 * HOUR)).toBe('12 hours ago')
    expect(formatAge(Date.now() - 3 * DAY)).toBe('3 days ago')
    expect(formatAge(Date.now() - 10 * DAY)).toBe('last week')
    expect(formatAge(Date.now() - 200 * DAY)).toBe('7 months ago')
    expect(formatAge(Date.now() - 800 * DAY)).toBe('2 years ago')
  })

  test('handles the very recent and the missing', async () => {
    const { formatAge } = await import('./format')
    expect(formatAge(Date.now() - 5_000)).toBe('just now')
    expect(formatAge(0)).toBe('—')
  })

  test('a corrupt future mtime shows a date, not a relative phrase', async () => {
    const { formatAge } = await import('./format')
    expect(formatAge(Date.UTC(4854, 0, 1))).toBe('dated 4854')
  })
})
