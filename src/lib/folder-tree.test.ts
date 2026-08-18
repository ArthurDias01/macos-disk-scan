import { describe, expect, test } from 'bun:test'
import type { FolderNode } from '@shared/schema'
import {
  findFolder,
  folderChain,
  folderOwnSize,
  folderSize,
  truncatedSize,
} from './folder-tree'

function node(path: string, partial: Partial<FolderNode> = {}): FolderNode {
  return {
    path,
    name: path.split('/').pop() ?? path,
    recursiveSize: 0,
    ownSize: 0,
    fileCount: 0,
    ownFileCount: 0,
    maxMtimeMs: 0,
    isCloud: false,
    duplicateOwnSize: 0,
    duplicateRecursiveSize: 0,
    children: [],
    truncatedChildCount: 0,
    truncatedSize: 0,
    ...partial,
  }
}

const tree = node('/home', {
  recursiveSize: 1000,
  ownSize: 100,
  children: [
    node('/home/media', {
      recursiveSize: 600,
      ownSize: 200,
      duplicateRecursiveSize: 300,
      duplicateOwnSize: 50,
      children: [node('/home/media/clips', { recursiveSize: 400 })],
    }),
    node('/home/code', { recursiveSize: 250 }),
  ],
})

describe('folderChain', () => {
  test('returns just the root for no path', () => {
    expect(folderChain(tree).map((entry) => entry.path)).toEqual(['/home'])
  })

  test('walks to a nested path', () => {
    expect(folderChain(tree, '/home/media/clips').map((entry) => entry.path)).toEqual([
      '/home',
      '/home/media',
      '/home/media/clips',
    ])
  })

  test('degrades to the deepest known ancestor for an unknown path', () => {
    // A stale bookmark should land on the parent, not error.
    expect(folderChain(tree, '/home/media/gone/deeper').map((e) => e.path)).toEqual([
      '/home',
      '/home/media',
    ])
  })

  test('does not match a sibling sharing a name prefix', () => {
    const siblings = node('/home', {
      children: [node('/home/app'), node('/home/app-cache')],
    })
    expect(findFolder(siblings, '/home/app-cache').path).toBe('/home/app-cache')
  })
})

describe('size readings', () => {
  test('allocated returns the raw figures', () => {
    const media = tree.children[0]
    expect(folderSize(media, 'allocated')).toBe(600)
    expect(folderOwnSize(media, 'allocated')).toBe(200)
  })

  test('unique subtracts redundant copies', () => {
    const media = tree.children[0]
    expect(folderSize(media, 'unique')).toBe(300)
    expect(folderOwnSize(media, 'unique')).toBe(150)
  })

  test('never goes negative', () => {
    const odd = node('/x', { recursiveSize: 10, duplicateRecursiveSize: 50 })
    expect(folderSize(odd, 'unique')).toBe(0)
  })
})

describe('truncatedSize', () => {
  test('accounts for the weight of unlisted children', () => {
    // 1000 total - 100 own - (600 + 250) listed = 50 hidden below the threshold.
    expect(truncatedSize(tree, 'allocated')).toBe(50)
  })

  test('is zero when the parts already add up', () => {
    const exact = node('/p', {
      recursiveSize: 300,
      ownSize: 100,
      children: [node('/p/a', { recursiveSize: 200 })],
    })
    expect(truncatedSize(exact, 'allocated')).toBe(0)
  })
})
