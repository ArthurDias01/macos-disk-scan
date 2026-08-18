import type { Category } from '@shared/schema'

/**
 * One hue per category, defined once in styles.css so a colour means the same
 * thing on every chart. Charts render as SVG in the document, so CSS variables
 * resolve normally in `fill`.
 */
export const CATEGORY_COLOR: Record<Category, string> = {
  video: 'var(--cat-video)',
  image: 'var(--cat-image)',
  audio: 'var(--cat-audio)',
  archive: 'var(--cat-archive)',
  diskimage: 'var(--cat-diskimage)',
  code: 'var(--cat-code)',
  cache: 'var(--cat-cache)',
  document: 'var(--cat-document)',
  binary: 'var(--cat-binary)',
  data: 'var(--cat-data)',
  other: 'var(--cat-other)',
}

export const CATEGORY_LABEL: Record<Category, string> = {
  video: 'Video',
  image: 'Images',
  audio: 'Audio',
  archive: 'Archives',
  diskimage: 'Disk images',
  code: 'Code & deps',
  cache: 'Caches',
  document: 'Documents',
  binary: 'Apps & binaries',
  data: 'Data',
  other: 'Other',
}

export function categoryColor(category: Category): string {
  return CATEGORY_COLOR[category] ?? CATEGORY_COLOR.other
}
