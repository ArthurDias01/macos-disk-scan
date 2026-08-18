import { useMemo } from 'react'
import { barX } from '@tanstack/charts/bar'
import { defineChart } from '@tanstack/charts/scene'
import { scaleBand } from '@tanstack/charts/scales/band'
import { scaleLinear } from '@tanstack/charts/scales/linear'
import { tooltip } from '@tanstack/charts/tooltip'
import { Chart } from '@tanstack/charts/react'
import type { ExtensionStat } from '@shared/schema'
import { sizeIn, type SizeMode } from '~/lib/size-mode'
import { categoryColor } from '~/lib/colors'
import { formatBytes, formatCount } from '~/lib/format'
import { bytesWithExact, rows as tooltipRows } from '~/lib/chart-tooltip'

export function ExtensionChart({
  extensions,
  limit = 15,
  mode = 'allocated',
}: {
  extensions: ExtensionStat[]
  limit?: number
  mode?: SizeMode
}) {
  const rows = useMemo(() => {
    // There are ~11k extensions and this chart shows 15. Mapping and sorting
    // the whole list to throw away 99.9% of it is wasted work on every mode
    // flip, so keep a running top-N instead. Subtracting duplicates reorders
    // the ranking, so the sort cannot be skipped — only its input.
    const top: Array<{ stat: ExtensionStat; bytes: number }> = []
    let smallestKept = 0

    for (const stat of extensions) {
      const bytes = sizeIn(mode, stat.totalSize, stat.duplicateBytes)
      if (top.length === limit && bytes <= smallestKept) continue

      const at = top.findIndex((row) => bytes > row.bytes)
      top.splice(at === -1 ? top.length : at, 0, { stat, bytes })
      if (top.length > limit) top.pop()
      smallestKept = top[top.length - 1].bytes
    }

    return top.map(({ stat, bytes }) => ({
      // The bucket for files with no extension needs a visible name.
      label: stat.ext === '' ? '(none)' : `.${stat.ext}`,
      bytes,
      files: stat.fileCount,
      fill: categoryColor(stat.category),
    }))
  }, [extensions, limit, mode])

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
        y: { scale: () => scaleBand().padding(0.2) },
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
                ['Average', formatBytes(row.files > 0 ? row.bytes / row.files : 0)],
              ]),
            }
          },
        },
      }),
    [rows, mode],
  )

  if (rows.length === 0) return null

  return (
    <Chart
      definition={definition}
      height={Math.max(200, rows.length * 26)}
      ariaLabel="Largest extensions by allocated bytes"
    />
  )
}
