import type { FolderNode } from '@shared/schema'
import { sizeIn, type SizeMode } from './size-mode'

/**
 * Walk from the root to `path`, returning every node along the way.
 *
 * The chain is what the breadcrumb renders, and its last element is the folder
 * being viewed. An unknown path resolves to the deepest ancestor that does
 * exist, so a stale bookmark degrades to its parent instead of erroring.
 */
export function folderChain(root: FolderNode, path?: string): FolderNode[] {
  const chain: FolderNode[] = [root]
  if (!path || path === root.path) return chain

  let current = root
  while (current.path !== path) {
    const next = current.children.find(
      (child) => path === child.path || path.startsWith(`${child.path}/`),
    )
    if (!next) break
    chain.push(next)
    current = next
  }
  return chain
}

export function findFolder(root: FolderNode, path?: string): FolderNode {
  const chain = folderChain(root, path)
  return chain[chain.length - 1]
}

/** Size under the active reading: allocated as-is, or minus redundant copies. */
export function folderSize(node: FolderNode, mode: SizeMode): number {
  return sizeIn(mode, node.recursiveSize, node.duplicateRecursiveSize)
}

export function folderOwnSize(node: FolderNode, mode: SizeMode): number {
  return sizeIn(mode, node.ownSize, node.duplicateOwnSize)
}

/**
 * Weight of children too small to be listed individually. Shown as its own row
 * so a folder's parts always add up to its total.
 */
export function truncatedSize(node: FolderNode, mode: SizeMode): number {
  const listed = node.children.reduce((sum, child) => sum + folderSize(child, mode), 0)
  const own = folderOwnSize(node, mode)
  return Math.max(0, folderSize(node, mode) - listed - own)
}
