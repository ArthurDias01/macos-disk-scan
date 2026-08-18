import { useMemo } from 'react'
import { defineChart } from '@tanstack/charts/scene'
import { treemap } from '@tanstack/charts/hierarchy/treemap'
import { tooltip } from '@tanstack/charts/tooltip'
import { Chart } from '@tanstack/charts/react'
import type { FolderNode } from '@shared/schema'
import { sizeIn, type SizeMode } from '~/lib/size-mode'
import { formatAge, formatBytes, formatCount } from '~/lib/format'
import { folderTooltip } from '~/lib/chart-tooltip'

interface TreemapRow {
  path: string
  /** Absolute path, for the tooltip — the relative one loses the context. */
  fullPath: string
  name: string
  /** Own files plus the weight of children too small to be listed. */
  value: number
  recursiveSize: number
  ownSize: number
  fileCount: number
  maxMtimeMs: number
  fill: string
}

/** Distinct hues per top-level folder, so siblings stay visually separable. */
const PALETTE = [
  'var(--cat-code)',
  'var(--cat-video)',
  'var(--cat-archive)',
  'var(--cat-image)',
  'var(--cat-diskimage)',
  'var(--cat-document)',
  'var(--cat-audio)',
  'var(--cat-cache)',
  'var(--cat-data)',
  'var(--cat-binary)',
]

/**
 * Flatten the folder tree into path rows.
 *
 * Each row carries only its **own** weight (direct files, plus children pruned
 * for being under the folder threshold). The treemap sums descendants itself,
 * so passing recursive sizes would count every ancestor twice.
 */
function flatten(root: FolderNode, maxDepth: number, mode: SizeMode): TreemapRow[] {
  const weigh = (node: FolderNode) =>
    sizeIn(mode, node.recursiveSize, node.duplicateRecursiveSize)
  const weighOwn = (node: FolderNode) =>
    sizeIn(mode, node.ownSize + node.truncatedSize, node.duplicateOwnSize)

  const rows: TreemapRow[] = []
  const rootPath = root.path

  const walk = (node: FolderNode, depth: number, topIndex: number) => {
    if (depth > 0) {
      const relative = node.path.startsWith(`${rootPath}/`)
        ? node.path.slice(rootPath.length + 1)
        : node.name
      rows.push({
        path: relative,
        fullPath: node.path,
        name: node.name,
        value: weighOwn(node),
        recursiveSize: weigh(node),
        ownSize: sizeIn(mode, node.ownSize, node.duplicateOwnSize),
        fileCount: node.fileCount,
        maxMtimeMs: node.maxMtimeMs,
        fill: PALETTE[topIndex % PALETTE.length],
      })
    }

    if (depth >= maxDepth) {
      // Deeper folders are folded into this one so the drawing stays readable.
      const listed = node.children.reduce((sum, child) => sum + weigh(child), 0)
      if (listed > 0 && depth > 0) {
        const last = rows[rows.length - 1]
        last.value += listed
      }
      return
    }

    node.children.forEach((child, index) => {
      walk(child, depth + 1, depth === 0 ? index : topIndex)
    })
  }

  walk(root, 0, 0)
  return rows.filter((row) => row.value > 0)
}

export function FolderTreemap({
  root,
  maxDepth = 3,
  height = 420,
  mode = 'allocated',
}: {
  root: FolderNode
  maxDepth?: number
  height?: number
  mode?: SizeMode
}) {
  const rows = useMemo(() => flatten(root, maxDepth, mode), [root, maxDepth, mode])
  const total = sizeIn(mode, root.recursiveSize, root.duplicateRecursiveSize)

  const definition = useMemo(
    () =>
      defineChart({
        marks: [
          treemap(rows, {
            path: 'path',
            delimiter: '/',
            value: 'value',
            paddingInner: 2,
            paddingOuter: 1,
            radius: 2,
            fill: (node) => node.data?.fill ?? 'var(--cat-other)',
            fillOpacity: 0.85,
            stroke: 'var(--background)',
            strokeWidth: 1,
            label: (node) => node.name,
            labelFontSize: 11,
          }),
        ],
      },
      {
        tooltip: {
          use: tooltip,
          content: (points) => {
            const node = points[0]?.datum as
              | { name?: string; value?: number; data?: TreemapRow | null }
              | undefined
            const row = node?.data ?? null
            return folderTooltip(
              row,
              node?.value ?? row?.recursiveSize ?? 0,
              total,
              node?.name,
            )
          },
        },
      }),
    [rows, total],
  )

  if (rows.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No folder crosses the reporting threshold.
      </p>
    )
  }

  return (
    <div>
      <Chart definition={definition} height={height} ariaLabel="Folder sizes" />
      <p className="mt-2 text-xs text-muted-foreground">
        Area is {mode === 'unique' ? 'unique' : 'allocated'} bytes. Nesting is real
        containment, so a tile inside another is part of it — never add the two
        together. Total{' '}
        {formatBytes(total)}.
      </p>
    </div>
  )
}
