/**
 * Formatting shared by the scanner CLI and the SPA.
 *
 * Lives in `shared/` because both sides print the same numbers, and two
 * implementations of "how big is this" drift the moment one is tweaked.
 */

/** Binary units, because that is what Finder and `df` report on macOS. */
const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const

export function formatBytes(bytes: number, precision?: number): string {
  if (!Number.isFinite(bytes)) return '—'
  if (bytes === 0) return '0 B'
  const sign = bytes < 0 ? '-' : ''
  let value = Math.abs(bytes)
  let unit = 0
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024
    unit++
  }
  const digits = precision ?? (value < 10 && unit > 0 ? 1 : 0)
  return `${sign}${value.toFixed(digits)} ${UNITS[unit]}`
}

export function formatCount(count: number): string {
  return new Intl.NumberFormat('en-US').format(count)
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${Math.round(seconds % 60)}s`
}

/**
 * Quote a path for a POSIX shell. macOS filenames routinely contain spaces and
 * apostrophes ("Photos Library.photoslibrary", "Arthur's Mac"); an unquoted
 * path in a delete command targets the wrong thing.
 */
export function shellQuote(path: string): string {
  return `'${path.replaceAll("'", `'\\''`)}'`
}
