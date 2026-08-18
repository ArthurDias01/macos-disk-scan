import { useMemo } from 'react'
import { dot } from '@tanstack/charts/dot'
import { ruleX, ruleY } from '@tanstack/charts/rule'
import { defineChart } from '@tanstack/charts/scene'
import { scaleLinear } from '@tanstack/charts/scales/linear'
import { scaleOrdinal } from '@tanstack/charts/scales/ordinal'
import { tooltip } from '@tanstack/charts/tooltip'
import { Chart } from '@tanstack/charts/react'
import { CATEGORIES } from '@shared/schema'
import { categoryColor } from '~/lib/colors'
import { formatBytes } from '~/lib/format'
import { bytesWithExact, rows as tooltipRows, trimPath } from '~/lib/chart-tooltip'
import { monthsToDays, sizeFromLog, type StalePoint } from '~/lib/stale'

/**
 * Size against age, with the thresholds drawn.
 *
 * The lines matter more than the dots: they make "big and old" a visible
 * region rather than a claim, so moving a threshold visibly moves the list.
 * Sizes span nine orders of magnitude, so the y axis is log2 with its ticks
 * relabelled as bytes.
 */
export function StaleScatter({
  points,
  minSize,
  minMonths,
  height = 380,
}: {
  points: StalePoint[]
  minSize: number
  minMonths: number
  height?: number
}) {
  const definition = useMemo(() => {
    const ageCut = monthsToDays(minMonths)
    const sizeCut = Math.log2(Math.max(1, minSize))

    return defineChart(
      {
        marks: [
          // Thresholds first so the dots sit above them.
          ruleX([ageCut], { stroke: 'var(--foreground)', strokeOpacity: 0.25 }),
          ruleY([sizeCut], { stroke: 'var(--foreground)', strokeOpacity: 0.25 }),
          dot(points, {
            x: 'ageDays',
            y: 'sizeLog',
            r: (point) => (point.candidate ? 4 : 2.5),
            // Colour is a channel rather than a per-point fill: `dot` takes one
            // fill string, and a scale is what carries category meaning anyway.
            color: 'category',
            fillOpacity: 0.85,
            stroke: 'var(--background)',
            strokeWidth: 0.5,
          }),
        ],
        color: {
          scale: scaleOrdinal,
          domain: [...CATEGORIES],
          range: CATEGORIES.map(categoryColor),
        },
        x: {
          scale: scaleLinear,
          nice: true,
          grid: true,
          axis: {
            label: 'Days since last change',
            ticks: {
              count: 6,
              format: (value: number) =>
                value >= 365
                  ? `${(value / 365).toFixed(value >= 1095 ? 0 : 1)}y`
                  : `${Math.round(value)}d`,
            },
          },
        },
        y: {
          scale: scaleLinear,
          nice: true,
          grid: true,
          axis: {
            ticks: {
              count: 5,
              format: (value: number) => formatBytes(sizeFromLog(value)),
            },
          },
        },
      },
      {
        tooltip: {
          use: tooltip,
          content: (chartPoints) => {
            const point = chartPoints[0]?.datum as StalePoint | undefined
            if (!point) return { rows: [] }
            const years = point.ageDays / 365
            return {
              title: point.name,
              rows: tooltipRows([
                ['Size', bytesWithExact(point.size)],
                [
                  'Last changed',
                  years >= 1
                    ? `${years.toFixed(1)} years ago`
                    : `${Math.round(point.ageDays)} days ago`,
                ],
                point.candidate ? ['Verdict', 'Big and old — a candidate'] : ['', null],
                point.sharesBlocks
                  ? ['Careful', 'Shares blocks; deleting frees nothing']
                  : ['', null],
                ['Path', trimPath(point.path)],
              ]),
            }
          },
        },
      },
    )
  }, [points, minSize, minMonths])

  if (points.length === 0) {
    return <p className="text-sm text-muted-foreground">No files to plot.</p>
  }

  return (
    <div>
      <Chart definition={definition} height={height} ariaLabel="File size against age" />
      <p className="mt-2 text-xs text-muted-foreground">
        Each dot is one file; larger dots are past both thresholds. Up is bigger,
        right is older, so the top-right corner is the delete list. Sizes double
        with every gridline.
      </p>
    </div>
  )
}
