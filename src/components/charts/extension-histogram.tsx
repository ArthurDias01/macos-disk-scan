import { useMemo } from 'react'
import { barY } from '@tanstack/charts/bar'
import { defineChart } from '@tanstack/charts/scene'
import { scaleBand } from '@tanstack/charts/scales/band'
import { scaleLinear } from '@tanstack/charts/scales/linear'
import { tooltip } from '@tanstack/charts/tooltip'
import { Chart } from '@tanstack/charts/react'
import { histogramBars } from '~/lib/extensions'
import { formatBytes, formatCount } from '~/lib/format'
import { rows as tooltipRows } from '~/lib/chart-tooltip'

/**
 * The scanner records a power-of-two size histogram for every extension, which
 * covers *all* files of that type — not just the ones above the reporting
 * floor. It is the only view in the app that sees the small files.
 */
export function ExtensionHistogram({ histogram }: { histogram: readonly number[] }) {
  const rows = useMemo(
    () =>
      histogramBars(histogram).map((bar) => ({
        label: bar.edge === 0 ? '<2 B' : formatBytes(bar.edge),
        count: bar.count,
        // A bucket is a range, and saying so removes the "is 64 KB the floor or
        // the middle?" question the axis label alone leaves open.
        range:
          bar.edge === 0
            ? 'under 2 bytes'
            : `${formatBytes(bar.edge)} to ${formatBytes(bar.edge * 2)}`,
      })),
    [histogram],
  )

  if (rows.length === 0) {
    return <p className="text-sm text-muted-foreground">No size data recorded.</p>
  }

  const definition = defineChart(
    {
      marks: [barY(rows, { x: 'label', y: 'count', fill: 'var(--cat-code)', radius: 2 })],
      x: { scale: () => scaleBand().padding(0.2) },
      y: {
        scale: scaleLinear,
        nice: true,
        grid: true,
        axis: { ticks: { count: 4, format: (value: number) => formatCount(value) } },
      },
    },
    {
      tooltip: {
        use: tooltip,
        content: (points) => {
          const row = points[0]?.datum as (typeof rows)[number] | undefined
          if (!row) return { rows: [] }
          const total = rows.reduce((sum, entry) => sum + entry.count, 0)
          return {
            title: row.range,
            rows: tooltipRows([
              ['Files', formatCount(row.count)],
              [
                'Share',
                total > 0 ? `${((row.count / total) * 100).toFixed(1)}% of this type` : null,
              ],
            ]),
          }
        },
      },
    },
  )

  return (
    <div>
      <Chart definition={definition} height={200} ariaLabel="File sizes by bucket" />
      <p className="mt-2 text-xs text-muted-foreground">
        Each bar counts files from its label up to the next one. Buckets are powers
        of two, which is why the labels double.
      </p>
    </div>
  )
}
