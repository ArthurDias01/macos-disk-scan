import { formatAge, formatBytes, formatCount } from './format'

export interface TooltipRow {
  label: string
  value: string
}

/**
 * Build tooltip rows, dropping the ones with nothing to say.
 *
 * The default tooltip prints the raw channel names (`x`, `y`) and unformatted
 * values — "y 4,871,397,376" tells you nothing you can act on. Every chart here
 * names its own rows and formats its own numbers.
 */
export function rows(
  entries: Array<[label: string, value: string | null | undefined]>,
): TooltipRow[] {
  return entries
    .filter((entry): entry is [string, string] => Boolean(entry[1]))
    .map(([label, value]) => ({ label, value }))
}

/** `4.5 GB` plus the exact byte count, which matters when comparing near-equal rows. */
export function bytesWithExact(bytes: number): string {
  return `${formatBytes(bytes)}  (${formatCount(bytes)} bytes)`
}

/** A share of a whole, shown only when it is meaningful. */
export function share(part: number, whole: number): string | null {
  if (whole <= 0 || part <= 0) return null
  const percent = (part / whole) * 100
  return percent < 0.1 ? '<0.1% of total' : `${percent.toFixed(1)}% of total`
}

/**
 * Trim a long path from the left so the end — which identifies the folder — is
 * what survives. A middle ellipsis would keep a useless `/Users/arthur` prefix.
 */
export function trimPath(path: string, max = 52): string {
  return path.length <= max ? path : `…${path.slice(path.length - max)}`
}

export interface FolderTooltipDatum {
  name: string
  fullPath: string
  ownSize: number
  recursiveSize: number
  fileCount: number
  maxMtimeMs: number
}

/**
 * Content for a folder tile.
 *
 * Extracted from the chart so it can be tested: a tooltip is the one piece of a
 * chart that is pure text, and the default was printing `y 4,871,397,376`.
 */
export function folderTooltip(
  datum: FolderTooltipDatum | null,
  subtreeBytes: number,
  totalBytes: number,
  fallbackName = 'Folder',
): { title: string; rows: TooltipRow[] } {
  return {
    title: datum?.name || fallbackName,
    rows: rows([
      ['Total', bytesWithExact(subtreeBytes)],
      // Only worth saying when the folder has children; otherwise it repeats
      // the line above.
      datum && datum.ownSize !== subtreeBytes
        ? ['Directly inside', formatBytes(datum.ownSize)]
        : ['', null],
      ['Share', share(subtreeBytes, totalBytes)],
      datum ? ['Files', formatCount(datum.fileCount)] : ['', null],
      datum?.maxMtimeMs ? ['Newest file', formatAge(datum.maxMtimeMs)] : ['', null],
      datum ? ['Path', trimPath(datum.fullPath)] : ['', null],
    ]),
  }
}
