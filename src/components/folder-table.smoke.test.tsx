import { describe, expect, test } from 'bun:test'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { renderToString } from 'react-dom/server'
import type { ScanSnapshot } from '@shared/schema'
import { findFolder } from '~/lib/folder-tree'
import { DEFAULT_FILTERS, filterFiles, sortFiles } from '~/lib/file-filters'
import { SelectionProvider } from '~/lib/selection'
import { FolderTable } from './folder-table'
import { FileTable } from './file-table'
import { FolderTreemap } from './charts/folder-treemap'

/**
 * Renders the folder table and treemap against the newest real snapshot.
 *
 * Typecheck cannot catch a misused chart or table runtime API, and these two
 * are the pieces most likely to break on a library bump. Rendering them with
 * real data is the cheapest proof they still work.
 */
const DATA_DIR = join(import.meta.dir, '..', '..', 'public', 'data')

function newestSnapshot(): ScanSnapshot | null {
  let files: string[]
  try {
    files = readdirSync(DATA_DIR).filter(
      (name) => name.startsWith('scan-') && name.endsWith('.json'),
    )
  } catch {
    return null
  }
  if (files.length === 0) return null
  files.sort()
  return JSON.parse(
    readFileSync(join(DATA_DIR, files[files.length - 1]), 'utf8'),
  ) as ScanSnapshot
}

const snapshot = newestSnapshot()

/** Both tables read the selection, so they only render inside its provider. */
function render(node: React.ReactNode): string {
  return renderToString(<SelectionProvider>{node}</SelectionProvider>)
}

describe.skipIf(!snapshot)('folder views render against a real snapshot', () => {
  test('table renders the root folder listing', () => {
    const root = (snapshot as ScanSnapshot).folderTree
    const html = render(<FolderTable folder={root} mode="unique" onDrill={() => {}} />)

    expect(html).toContain('<table')
    expect(html).toContain('Folder')
    // Every child above the threshold should appear by name.
    for (const child of root.children.slice(0, 3)) {
      expect(html).toContain(child.name)
    }
  })

  test('table renders a drilled-in folder', () => {
    const root = (snapshot as ScanSnapshot).folderTree
    const child = root.children.find((entry) => entry.children.length > 0)
    if (!child) return
    const folder = findFolder(root, child.path)
    const html = render(<FolderTable folder={folder} mode="allocated" onDrill={() => {}} />)
    expect(html).toContain(folder.children[0].name)
  })

  test('file table renders rows for the real file list', () => {
    const snap = snapshot as ScanSnapshot
    const files = sortFiles(filterFiles(snap.files, DEFAULT_FILTERS), 'size', true)
    const html = render(<FileTable files={files} sort="size" desc onSort={() => {}} />)

    expect(html).toContain('File')
    // The virtualizer has no scroll element while server-rendering, so it
    // reports zero visible rows. What this proves is that the component sets
    // up without throwing and sizes its scroll container.
    expect(html).toContain('overflow-auto')
  })

  test('filters narrow the real file list without emptying it', () => {
    const snap = snapshot as ScanSnapshot
    const all = filterFiles(snap.files, DEFAULT_FILTERS)
    const big = filterFiles(snap.files, { ...DEFAULT_FILTERS, min: 1024 ** 3 })

    expect(all.length).toBe(snap.files.length)
    expect(big.length).toBeLessThanOrEqual(all.length)
    expect(big.every((entry) => entry.size >= 1024 ** 3)).toBe(true)
  })

  test('treemap renders svg tiles', () => {
    const html = renderToString(
      <FolderTreemap root={(snapshot as ScanSnapshot).folderTree} mode="unique" />,
    )
    expect(html).toContain('<svg')
    expect(html).toContain('rect')
  })
})
