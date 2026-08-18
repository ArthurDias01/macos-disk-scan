import type { FolderNode, ScanSnapshot } from '@shared/schema'
import { sizeIn, type SizeMode } from './size-mode'

export interface Delta {
  path: string
  name: string
  before: number
  after: number
  /** `after - before`. Positive means it grew. */
  change: number
}

export interface SnapshotDiff {
  bytes: Delta
  uniqueBytes: Delta
  files: Delta
  /** Folders whose recursive size moved most, growth first. */
  folders: Delta[]
  /** Extensions whose total moved most, growth first. */
  extensions: Delta[]
  /** Reported files present in the newer scan only. */
  appeared: Array<{ path: string; size: number }>
  /** Reported files present in the older scan only — deleted, or now under the floor. */
  vanished: Array<{ path: string; size: number }>
  /** True when the two scans used different thresholds, making sizes incomparable. */
  thresholdsDiffer: boolean
}

function flattenFolders(root: FolderNode, mode: SizeMode): Map<string, FolderNode> {
  const map = new Map<string, FolderNode>()
  const stack = [root]
  while (stack.length > 0) {
    const node = stack.pop() as FolderNode
    map.set(node.path, node)
    for (const child of node.children) stack.push(child)
  }
  return map
}

function delta(path: string, name: string, before: number, after: number): Delta {
  return { path, name, before, after, change: after - before }
}

/** Biggest absolute movement first; growth wins ties, since growth is the alarm. */
function byMovement(a: Delta, b: Delta): number {
  const difference = Math.abs(b.change) - Math.abs(a.change)
  return difference !== 0 ? difference : b.change - a.change
}

/**
 * Compare two snapshots.
 *
 * The recurring question this app exists for is not "what is big" but "what got
 * bigger" — a folder that quietly gained 30 GB is a better target than one that
 * has been a stable 40 GB for three years.
 */
export function diffSnapshots(
  before: ScanSnapshot,
  after: ScanSnapshot,
  mode: SizeMode,
  limit = 15,
): SnapshotDiff {
  const beforeFolders = flattenFolders(before.folderTree, mode)
  const afterFolders = flattenFolders(after.folderTree, mode)

  const folderPaths = new Set([...beforeFolders.keys(), ...afterFolders.keys()])
  const folders: Delta[] = []
  for (const path of folderPaths) {
    const older = beforeFolders.get(path)
    const newer = afterFolders.get(path)
    const beforeSize = older
      ? sizeIn(mode, older.recursiveSize, older.duplicateRecursiveSize)
      : 0
    const afterSize = newer
      ? sizeIn(mode, newer.recursiveSize, newer.duplicateRecursiveSize)
      : 0
    if (beforeSize === afterSize) continue
    folders.push(
      delta(path, newer?.name ?? older?.name ?? path, beforeSize, afterSize),
    )
  }
  folders.sort(byMovement)

  const beforeExt = new Map(before.extensions.map((stat) => [stat.ext, stat]))
  const afterExt = new Map(after.extensions.map((stat) => [stat.ext, stat]))
  const extensions: Delta[] = []
  for (const ext of new Set([...beforeExt.keys(), ...afterExt.keys()])) {
    const older = beforeExt.get(ext)
    const newer = afterExt.get(ext)
    const beforeSize = older ? sizeIn(mode, older.totalSize, older.duplicateBytes) : 0
    const afterSize = newer ? sizeIn(mode, newer.totalSize, newer.duplicateBytes) : 0
    if (beforeSize === afterSize) continue
    extensions.push(delta(ext, ext === '' ? '(none)' : `.${ext}`, beforeSize, afterSize))
  }
  extensions.sort(byMovement)

  const beforeFiles = new Map(before.files.map((file) => [file.path, file.size]))
  const afterFiles = new Map(after.files.map((file) => [file.path, file.size]))
  const appeared: Array<{ path: string; size: number }> = []
  const vanished: Array<{ path: string; size: number }> = []
  for (const [path, size] of afterFiles) {
    if (!beforeFiles.has(path)) appeared.push({ path, size })
  }
  for (const [path, size] of beforeFiles) {
    if (!afterFiles.has(path)) vanished.push({ path, size })
  }
  appeared.sort((a, b) => b.size - a.size)
  vanished.sort((a, b) => b.size - a.size)

  return {
    bytes: delta('', 'Allocated', before.totals.bytes, after.totals.bytes),
    uniqueBytes: delta('', 'Unique', before.totals.uniqueBytes, after.totals.uniqueBytes),
    files: delta('', 'Files', before.totals.files, after.totals.files),
    folders: folders.slice(0, limit),
    extensions: extensions.slice(0, limit),
    appeared: appeared.slice(0, limit),
    vanished: vanished.slice(0, limit),
    // Comparing a 100 MB-floor scan with a 1 GB-floor scan would report
    // "vanished" files that were only filtered out.
    thresholdsDiffer:
      before.config.minFileSize !== after.config.minFileSize ||
      before.config.minFolderSize !== after.config.minFolderSize,
  }
}
