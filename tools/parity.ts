/**
 * Compare two snapshots field by field.
 *
 * The scanner's output is numbers that cannot be independently eyeballed — if a
 * rollup double-counts, the treemap still looks convincing. That is why the
 * scanner has tests, and it is why porting it to Go needs more than "the
 * summary line looks the same": every aggregate has to agree.
 *
 *   bun run parity <a.json|dirA> <b.json|dirB>
 *
 * Two differences are expected and normalized away rather than reported:
 *
 *   - **Object key order.** Go's encoding/json sorts map keys; JavaScript keeps
 *     insertion order. JSON objects are unordered and the app looks up by key.
 *   - **Ties.** Equal-sized files and equal-sized sibling folders come back in
 *     whatever order the walk found them. The Go scanner breaks ties on path so
 *     its output is reproducible; the TypeScript one does not.
 *
 * Identity and timing fields (id, timestamps, duration, hostname) and the
 * volume figures are skipped: they describe the run, not the disk.
 */

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import type { FolderNode, ScanSnapshot } from '../shared/schema'

function load(target: string): ScanSnapshot {
  const path = statSync(target).isDirectory()
    ? join(
        target,
        readdirSync(target)
          .filter((name) => name.startsWith('scan-') && name.endsWith('.json'))
          .sort()
          .pop()!,
      )
    : target
  return JSON.parse(readFileSync(path, 'utf8')) as ScanSnapshot
}

/** Sort object keys everywhere, so key order never registers as a difference. */
function sortKeys(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortKeys)
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value as Record<string, unknown>)
        .sort()
        .map((key) => [key, sortKeys((value as Record<string, unknown>)[key])]),
    )
  }
  return value
}

/** Sort children by path at every level, so sibling tie order does not matter. */
function normalizeTree(node: FolderNode): unknown {
  return {
    ...node,
    children: [...node.children]
      .sort((a, b) => a.path.localeCompare(b.path))
      .map(normalizeTree),
  }
}

interface Difference {
  field: string
  detail: string
}

const differences: Difference[] = []

function compare(field: string, a: unknown, b: unknown): void {
  const left = JSON.stringify(sortKeys(a))
  const right = JSON.stringify(sortKeys(b))
  if (left === right) return

  differences.push({
    field,
    detail:
      left.length + right.length > 400
        ? `differs (${left.length} vs ${right.length} bytes of JSON)`
        : `\n    a=${left}\n    b=${right}`,
  })
}

/** Compare two lists as sets, and separately report what only one side has. */
function compareSets(field: string, a: string[], b: string[]): void {
  const inA = new Set(a)
  const inB = new Set(b)
  const onlyA = a.filter((value) => !inB.has(value))
  const onlyB = b.filter((value) => !inA.has(value))

  if (onlyA.length === 0 && onlyB.length === 0) return

  const sample = (values: string[]) =>
    values.slice(0, 3).join('\n      ') + (values.length > 3 ? `\n      … ${values.length - 3} more` : '')

  differences.push({
    field,
    detail:
      `${onlyA.length} only in a, ${onlyB.length} only in b` +
      (onlyA.length > 0 ? `\n    only in a:\n      ${sample(onlyA)}` : '') +
      (onlyB.length > 0 ? `\n    only in b:\n      ${sample(onlyB)}` : ''),
  })
}

const [, , targetA, targetB] = process.argv
if (!targetA || !targetB) {
  console.error('usage: bun run parity <a.json|dirA> <b.json|dirB>')
  process.exit(2)
}

const a = load(targetA)
const b = load(targetB)

/**
 * Order extensions by size, then name.
 *
 * The scanners agree on the ranking but not on how equal-sized entries fall
 * within it, and the same tie propagates into each category's extension list.
 */
function canonicalExtensions(snapshot: ScanSnapshot) {
  return [...snapshot.extensions].sort(
    (x, y) => y.totalSize - x.totalSize || x.ext.localeCompare(y.ext),
  )
}

function canonicalCategories(snapshot: ScanSnapshot) {
  const rank = new Map(canonicalExtensions(snapshot).map((stat, index) => [stat.ext, index]))
  return [...snapshot.categories]
    .sort((x, y) => y.totalSize - x.totalSize || x.category.localeCompare(y.category))
    .map((stat) => ({
      ...stat,
      extensions: [...stat.extensions].sort(
        (x, y) => (rank.get(x) ?? 0) - (rank.get(y) ?? 0),
      ),
    }))
}

/**
 * `largestPath` names one file, and when several share the maximum size there
 * is nothing to choose between them. The Go scanner breaks that tie on path so
 * its own runs are reproducible; the TypeScript one keeps whichever the walk
 * reached first. Reported, but not counted as a defect — unless the sizes
 * themselves disagree, which would be a real one.
 */
const ties: string[] = []

function compareExtensions(): void {
  const left = canonicalExtensions(a)
  const right = canonicalExtensions(b)

  const strip = (stats: typeof left) =>
    stats.map(({ largestPath, ...rest }) => rest)
  compare('extensions', strip(left), strip(right))

  const rightByExt = new Map(right.map((stat) => [stat.ext, stat]))
  for (const stat of left) {
    const other = rightByExt.get(stat.ext)
    if (!other || stat.largestPath === other.largestPath) continue

    if (stat.maxSize === other.maxSize) {
      ties.push(`${stat.ext || '(none)'}: two files of ${stat.maxSize} bytes — ` +
        `a chose ${stat.largestPath}, b chose ${other.largestPath}`)
    } else {
      differences.push({
        field: `extensions.${stat.ext || '(none)'}.largestPath`,
        detail: `\n    a=${stat.largestPath} (${stat.maxSize})\n    b=${other.largestPath} (${other.maxSize})`,
      })
    }
  }
}

compare('schemaVersion', a.schemaVersion, b.schemaVersion)
compare('config', a.config, b.config)
compare('totals', a.totals, b.totals)
compareExtensions()
compare('categories', canonicalCategories(a), canonicalCategories(b))
compare('folderTree', normalizeTree(a.folderTree), normalizeTree(b.folderTree))
compare('warnings', [...a.warnings].sort(), [...b.warnings].sort())
compare(
  'duplicateGroups',
  [...a.duplicateGroups].sort((x, y) => x.fingerprint.localeCompare(y.fingerprint)),
  [...b.duplicateGroups].sort((x, y) => x.fingerprint.localeCompare(y.fingerprint)),
)

// Files carry every flag the app filters on, so compare the whole record.
const fileKey = (entry: ScanSnapshot['files'][number]) =>
  JSON.stringify([
    entry.path,
    entry.size,
    entry.ext,
    entry.category,
    entry.isBundle,
    entry.isCloud,
    entry.isHardlink,
    entry.isDupInode,
    entry.nlink,
    entry.duplicateGroup ?? null,
    entry.duplicateCopies ?? null,
    entry.isDuplicateCopy ?? null,
  ])
compareSets('files', a.files.map(fileKey), b.files.map(fileKey))

compareSets(
  'unscanned',
  a.unscanned.map((error) => `${error.code} ${error.path}`),
  b.unscanned.map((error) => `${error.code} ${error.path}`),
)
compareSets(
  'rechecked',
  a.rechecked.map((entry) => `${entry.present} ${entry.path}`),
  b.rechecked.map((entry) => `${entry.present} ${entry.path}`),
)

console.log(`a: ${a.id}  ${a.totals.files} files, ${a.totals.bytes} bytes, ${a.durationMs}ms`)
console.log(`b: ${b.id}  ${b.totals.files} files, ${b.totals.bytes} bytes, ${b.durationMs}ms`)

if (ties.length > 0) {
  console.log(`\n${ties.length} tie(s), where the two scanners had nothing to choose between:`)
  for (const tie of ties) console.log(`  ${tie}`)
}

if (differences.length === 0) {
  console.log('\nIDENTICAL')
  process.exit(0)
}

console.log(`\n${differences.length} field(s) differ:\n`)
for (const difference of differences) {
  console.log(`  ${difference.field}: ${difference.detail}`)
}
process.exit(1)
