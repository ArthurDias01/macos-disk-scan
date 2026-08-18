import { useMemo } from 'react'
import { barX } from '@tanstack/charts/bar'
import { defineChart } from '@tanstack/charts/scene'
import { scaleBand } from '@tanstack/charts/scales/band'
import { scaleLinear } from '@tanstack/charts/scales/linear'
import { tooltip } from '@tanstack/charts/tooltip'
import { Chart } from '@tanstack/charts/react'
import type { CategoryStat } from '@shared/schema'
import { sizeIn, type SizeMode } from '~/lib/size-mode'
import { CATEGORY_LABEL, categoryColor } from '~/lib/colors'
import { formatBytes, formatCount } from '~/lib/format'
import { bytesWithExact, rows as tooltipRows, share } from '~/lib/chart-tooltip'

/**
 * Horizontal bars: category names are words, and words read better along the
 * axis than rotated under vertical bars.
 */
export function CategoryChart({
  categories,
  mode = 'allocated',
}: {
  categories: CategoryStat[]
  mode?: SizeMode
}) {
  const rows = useMemo(
    () =>
      categories
        .map((stat) => ({
          label: CATEGORY_LABEL[stat.category],
          bytes: sizeIn(mode, stat.totalSize, stat.duplicateBytes),
          files: stat.fileCount,
          fill: categoryColor(stat.category),
        }))
        .sort((a, b) => b.bytes - a.bytes),
    [categories, mode],
  )
  const total = rows.reduce((sum, row) => sum + row.bytes, 0)

  const definition = useMemo(
    () =>
      defineChart({
        marks: [
          barX(rows, {
            y: 'label',
            x: 'bytes',
            fill: (row) => row.fill,
            radius: 3,
          }),
        ],
        y: { scale: () => scaleBand().padding(0.25) },
        x: {
          scale: scaleLinear,
          nice: true,
          grid: true,
          axis: { ticks: { count: 4, format: (value: number) => formatBytes(value) } },
        },
      },
      {
        tooltip: {
          use: tooltip,
          content: (points) => {
            const row = points[0]?.datum as (typeof rows)[number] | undefined
            if (!row) return { rows: [] }
            return {
              title: row.label,
              color: row.fill,
              rows: tooltipRows([
                [mode === 'unique' ? 'Unique' : 'Allocated', bytesWithExact(row.bytes)],
                ['Files', formatCount(row.files)],
                ['Share', share(row.bytes, total)],
              ]),
            }
          },
        },
      }),
    [rows, mode, total],
  )

  if (rows.length === 0) return null

  return (
    <div>
      <Chart
        definition={definition}
        height={Math.max(180, rows.length * 34)}
        ariaLabel="Allocated bytes by category"
      />
      <ul className="mt-3 grid grid-cols-2 gap-x-6 gap-y-1 text-xs text-muted-foreground sm:grid-cols-3">
        {rows.map((row) => (
          <li key={row.label} className="flex items-center gap-2">
            <span
              className="size-2 shrink-0 rounded-full"
              style={{ backgroundColor: row.fill }}
            />
            <span className="truncate">{row.label}</span>
            <span className="tabular ml-auto">{formatCount(row.files)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
