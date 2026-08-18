/**
 * Which reading of "size" a view shows.
 *
 * - `allocated`: what the filesystem bills, matching `du` and Finder. Every
 *   copy of a cloned file counts in full.
 * - `unique`: allocated minus every redundant copy of byte-identical files. On
 *   a machine with heavy APFS cloning this is the only readable view — a single
 *   clone group can otherwise be 90% of the picture.
 */
export type SizeMode = 'allocated' | 'unique'

export const SIZE_MODE_LABEL: Record<SizeMode, string> = {
  allocated: 'Allocated',
  unique: 'Unique',
}

/**
 * The one place the two readings differ.
 *
 * Every chart and table asks this rather than repeating the subtraction, so
 * "what does unique mean" has a single answer. Clamped at zero: duplicate
 * attribution and totals are computed in separate passes, and a folder whose
 * contents changed mid-scan can otherwise produce a negative.
 */
export function sizeIn(mode: SizeMode, total: number, duplicate: number): number {
  return mode === 'unique' ? Math.max(0, total - duplicate) : total
}
