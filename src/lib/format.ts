export { formatBytes, formatCount, formatDuration, shellQuote } from '@shared/format'

const RELATIVE = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })

/** Largest unit first: the first one the gap reaches is the one to report in. */
const UNITS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ['year', 31_557_600_000],
  ['month', 2_629_800_000],
  ['week', 604_800_000],
  ['day', 86_400_000],
  ['hour', 3_600_000],
  ['minute', 60_000],
]

/**
 * Browser-only: relative time needs a "now" the scanner has no use for.
 *
 * Timestamps in the future are real — some files carry corrupt mtimes, and a
 * home directory reliably contains a few. "in 2,828 years" reads as a bug in
 * this app rather than a fact about the file, so those show an absolute year.
 */
export function formatAge(timestampMs: number): string {
  if (!timestampMs) return '—'

  const delta = timestampMs - Date.now()
  if (delta > 86_400_000) {
    return `dated ${new Date(timestampMs).getUTCFullYear()}`
  }

  const abs = Math.abs(delta)
  for (const [unit, ms] of UNITS) {
    if (abs >= ms) return RELATIVE.format(Math.round(delta / ms), unit)
  }
  return 'just now'
}
