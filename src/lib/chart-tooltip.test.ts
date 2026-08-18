import { describe, expect, test } from 'bun:test'
import { bytesWithExact, folderTooltip, rows, share, trimPath } from './chart-tooltip'

const GB = 1024 ** 3

describe('bytesWithExact', () => {
  test('leads with a readable size and keeps the exact count', () => {
    // The reported bug: the tooltip showed "4,871,397,376" and nothing else.
    expect(bytesWithExact(4_871_397_376)).toBe('4.5 GB  (4,871,397,376 bytes)')
  })
})

describe('share', () => {
  test('states the proportion in words a reader can use', () => {
    expect(share(GB, 4 * GB)).toBe('25.0% of total')
  })

  test('collapses invisible slivers rather than printing 0.0%', () => {
    expect(share(1, 10 * GB)).toBe('<0.1% of total')
  })

  test('is omitted when there is no meaningful whole', () => {
    expect(share(0, 0)).toBeNull()
    expect(share(5, 0)).toBeNull()
  })
})

describe('trimPath', () => {
  test('keeps the end, which is what identifies the folder', () => {
    const path = '/Users/arthur/Documents/Projects/central-tech-app/node_modules/react'
    const trimmed = trimPath(path, 30)
    expect(trimmed.startsWith('…')).toBe(true)
    expect(trimmed.endsWith('node_modules/react')).toBe(true)
  })

  test('leaves short paths alone', () => {
    expect(trimPath('/Users/arthur')).toBe('/Users/arthur')
  })
})

describe('rows', () => {
  test('drops entries with nothing to say', () => {
    expect(rows([['A', '1'], ['B', null], ['C', undefined], ['D', '']])).toEqual([
      { label: 'A', value: '1' },
    ])
  })
})

describe('folderTooltip', () => {
  const datum = {
    name: 'central-tech-app',
    fullPath: '/Users/arthur/Documents/Projects/central-tech-app',
    ownSize: 1 * GB,
    recursiveSize: 4.5 * GB,
    fileCount: 12_345,
    maxMtimeMs: Date.now() - 3 * 86_400_000,
  }

  test('names the folder and formats every number', () => {
    const content = folderTooltip(datum, 4.5 * GB, 100 * GB)

    expect(content.title).toBe('central-tech-app')
    const labels = content.rows.map((row) => row.label)
    expect(labels).toEqual(['Total', 'Directly inside', 'Share', 'Files', 'Newest file', 'Path'])
    // No raw channel names, no raw byte counts standing alone.
    expect(labels).not.toContain('x')
    expect(labels).not.toContain('y')
    expect(content.rows[0].value).toContain('4.5 GB')
    expect(content.rows[3].value).toBe('12,345')
    expect(content.rows[4].value).toBe('3 days ago')
  })

  test('omits "directly inside" when it would repeat the total', () => {
    const leaf = { ...datum, ownSize: 4.5 * GB }
    const labels = folderTooltip(leaf, 4.5 * GB, 100 * GB).rows.map((row) => row.label)
    expect(labels).not.toContain('Directly inside')
  })

  test('still says something useful for a node with no datum', () => {
    const content = folderTooltip(null, 2 * GB, 10 * GB, 'Projects')
    expect(content.title).toBe('Projects')
    expect(content.rows.map((row) => row.label)).toEqual(['Total', 'Share'])
  })
})
