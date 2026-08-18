import { describe, expect, test } from 'bun:test'
import type { Category, FileEntry } from '@shared/schema'
import { DAY_MS, DEFAULT_THRESHOLDS, analyseStale, monthsToDays, sizeFromLog } from './stale'

const NOW = Date.UTC(2026, 0, 1)
const GB = 1024 ** 3

function file(partial: Partial<FileEntry> & { path: string }): FileEntry {
  return {
    ext: 'mp4',
    category: 'video' as Category,
    size: 2 * GB,
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

const bigOld = file({ path: '/old-big.mp4', size: 3 * GB, mtimeMs: NOW - 800 * DAY_MS })
const bigFresh = file({ path: '/fresh-big.mp4', size: 5 * GB, mtimeMs: NOW - 2 * DAY_MS })
const smallOld = file({
  path: '/old-small.txt',
  size: 200 * 1024 ** 2,
  mtimeMs: NOW - 900 * DAY_MS,
})
const oldestBig = file({
  path: '/ancient.mov',
  size: 2 * GB,
  mtimeMs: NOW - 2000 * DAY_MS,
})
const sharedBigOld = file({
  path: '/clone.mp4',
  size: 4 * GB,
  mtimeMs: NOW - 700 * DAY_MS,
  isDuplicateCopy: true,
})

const files = [bigOld, bigFresh, smallOld, oldestBig, sharedBigOld]

describe('analyseStale', () => {
  test('keeps only files past both thresholds', () => {
    const result = analyseStale(files, DEFAULT_THRESHOLDS, NOW)
    // Big-but-fresh and old-but-small are both excluded: neither alone is a
    // reason to delete anything.
    expect(result.candidates.map((entry) => entry.path).sort()).toEqual([
      '/ancient.mov',
      '/clone.mp4',
      '/old-big.mp4',
    ])
  })

  test('separates allocated bytes from what deleting would free', () => {
    const result = analyseStale(files, DEFAULT_THRESHOLDS, NOW)
    expect(result.bytes).toBe(3 * GB + 2 * GB + 4 * GB)
    // The clone copy frees nothing on its own.
    expect(result.freeableBytes).toBe(3 * GB + 2 * GB)
  })

  test('reports the oldest candidate', () => {
    const result = analyseStale(files, DEFAULT_THRESHOLDS, NOW)
    expect(result.oldest?.path).toBe('/ancient.mov')
  })

  test('plots every file, marking only the candidates', () => {
    const result = analyseStale(files, DEFAULT_THRESHOLDS, NOW)
    expect(result.points).toHaveLength(files.length)
    expect(result.points.filter((point) => point.candidate)).toHaveLength(3)
  })

  test('a corrupt future mtime cannot drag the axis backwards', () => {
    const result = analyseStale(
      [file({ path: '/future.mp4', mtimeMs: NOW + 900 * DAY_MS })],
      DEFAULT_THRESHOLDS,
      NOW,
    )
    expect(result.points[0].ageDays).toBe(0)
    expect(result.candidates).toHaveLength(0)
  })

  test('loosening a threshold widens the list', () => {
    const loose = analyseStale(files, { minSize: 100 * 1024 ** 2, minMonths: 6 }, NOW)
    expect(loose.candidates.length).toBeGreaterThan(
      analyseStale(files, DEFAULT_THRESHOLDS, NOW).candidates.length,
    )
  })

  test('candidates come back biggest first', () => {
    const sizes = analyseStale(files, DEFAULT_THRESHOLDS, NOW).candidates.map(
      (entry) => entry.size,
    )
    expect(sizes).toEqual([...sizes].sort((a, b) => b - a))
  })
})

describe('axis helpers', () => {
  test('log sizes round-trip to bytes', () => {
    expect(sizeFromLog(Math.log2(GB))).toBeCloseTo(GB, 0)
  })

  test('months convert to days for the threshold line', () => {
    expect(Math.round(monthsToDays(12))).toBe(365)
  })
})
