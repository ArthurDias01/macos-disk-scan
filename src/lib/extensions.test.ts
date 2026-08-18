import { describe, expect, test } from 'bun:test'
import { emptyHistogram, type ExtensionStat } from '@shared/schema'
import {
  DEFAULT_EXTENSION_FILTERS,
  displayExtension,
  histogramBars,
  rankExtensions,
} from './extensions'

function stat(partial: Partial<ExtensionStat> & { ext: string }): ExtensionStat {
  return {
    category: 'other',
    totalSize: 1000,
    fileCount: 10,
    meanSize: 100,
    medianSize: 64,
    p95Size: 512,
    maxSize: 900,
    largestPath: `/x/big.${partial.ext}`,
    duplicateBytes: 0,
    histogram: emptyHistogram(),
    ...partial,
  }
}

const extensions: ExtensionStat[] = [
  stat({ ext: 'mp4', category: 'video', totalSize: 10_000, duplicateBytes: 9_000 }),
  stat({ ext: 'jpg', category: 'image', totalSize: 5_000, fileCount: 500 }),
  stat({ ext: 'zip', category: 'archive', totalSize: 3_000, maxSize: 2_900 }),
  stat({ ext: '', totalSize: 2_000 }),
  stat({ ext: 'log', category: 'cache', totalSize: 1_000 }),
]

const filters = (overrides = {}) => ({ ...DEFAULT_EXTENSION_FILTERS, ...overrides })

describe('displayExtension', () => {
  test('names the bucket for files without one', () => {
    expect(displayExtension('')).toBe('(none)')
    expect(displayExtension('mp4')).toBe('.mp4')
  })
})

describe('rankExtensions', () => {
  test('ranks by size under the allocated reading', () => {
    const result = rankExtensions(extensions, filters(), 'allocated')
    expect(result.head.map((row) => row.stat.ext)).toEqual([
      'mp4',
      'jpg',
      'zip',
      '',
      'log',
    ])
  })

  test('unique mode reorders around redundant copies', () => {
    // mp4 is 10k allocated but only 1k unique, so it should fall behind jpg.
    const result = rankExtensions(extensions, filters(), 'unique')
    expect(result.head[0].stat.ext).toBe('jpg')
    expect(result.head.find((row) => row.stat.ext === 'mp4')?.bytes).toBe(1_000)
  })

  test('folds everything past the head into a tail summary', () => {
    const result = rankExtensions(extensions, filters(), 'allocated', 2)

    expect(result.head).toHaveLength(2)
    expect(result.tail.count).toBe(3)
    // The tail keeps the weight it represents rather than dropping it.
    expect(result.tail.bytes).toBe(3_000 + 2_000 + 1_000)
    expect(result.matched).toBe(5)
  })

  test('filters by name, including the (none) bucket', () => {
    expect(
      rankExtensions(extensions, filters({ q: 'mp' }), 'allocated').head.map(
        (row) => row.stat.ext,
      ),
    ).toEqual(['mp4'])
    expect(
      rankExtensions(extensions, filters({ q: 'none' }), 'allocated').head.map(
        (row) => row.stat.ext,
      ),
    ).toEqual([''])
  })

  test('filters by category', () => {
    const result = rankExtensions(
      extensions,
      filters({ cats: ['video', 'archive'] }),
      'allocated',
    )
    expect(result.head.map((row) => row.stat.ext)).toEqual(['mp4', 'zip'])
  })

  test('sorts by count, largest file and name', () => {
    expect(
      rankExtensions(extensions, filters({ sort: 'count' }), 'allocated').head[0].stat
        .ext,
    ).toBe('jpg')
    expect(
      rankExtensions(extensions, filters({ sort: 'largest' }), 'allocated').head[0]
        .stat.ext,
    ).toBe('zip')
    expect(
      rankExtensions(extensions, filters({ sort: 'name', desc: false }), 'allocated')
        .head[0].stat.ext,
    ).toBe('')
  })

  test('share is relative to the biggest row, so the bar always fills', () => {
    const result = rankExtensions(extensions, filters(), 'allocated')
    expect(result.head[0].share).toBe(1)
    expect(result.head[1].share).toBeCloseTo(0.5, 5)
  })
})

describe('histogramBars', () => {
  test('drops the empty range at both ends', () => {
    const histogram = emptyHistogram()
    histogram[10] = 3 // 1 KB
    histogram[12] = 1 // 4 KB
    const bars = histogramBars(histogram)

    expect(bars).toHaveLength(3)
    expect(bars[0]).toEqual({ edge: 1024, count: 3 })
    expect(bars[2]).toEqual({ edge: 4096, count: 1 })
  })

  test('is empty when nothing was recorded', () => {
    expect(histogramBars(emptyHistogram())).toEqual([])
  })
})
