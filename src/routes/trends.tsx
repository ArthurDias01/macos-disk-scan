import { useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import type { ScanSnapshot } from '@shared/schema'
import { SnapshotGate } from '~/components/snapshot-gate'
import { SizeModeToggle, useSizeMode } from '~/components/size-mode-toggle'
import { Banner, Card, SectionTitle, StatCard } from '~/components/ui'
import { CopyCommand } from '~/components/copy-command'
import { snapshotIndexQuery, snapshotQuery } from '~/lib/snapshots'
import { diffSnapshots, type Delta } from '~/lib/trends'
import { formatBytes, formatCount } from '~/lib/format'
import { cn } from '~/lib/utils'

export const Route = createFileRoute('/trends')({
  validateSearch: (search: Record<string, unknown>) => ({
    against: typeof search.against === 'string' ? search.against : undefined,
  }),
  component: () => (
    <SnapshotGate>{(snapshot) => <Trends latest={snapshot} />}</SnapshotGate>
  ),
})

function Trends({ latest }: { latest: ScanSnapshot }) {
  const navigate = useNavigate({ from: '/trends' })
  const { against } = Route.useSearch()
  const { data: index } = useQuery(snapshotIndexQuery)
  const sharesBlocks = latest.volume
    ? latest.totals.bytes > latest.volume.usedBytes
    : false
  const mode = useSizeMode(sharesBlocks ? 'unique' : 'allocated')

  const others = (index?.snapshots ?? []).filter((entry) => entry.id !== latest.id)
  const baseline = against
    ? (others.find((entry) => entry.id === against) ?? others[0])
    : others[0]

  const { data: previous } = useQuery({
    ...snapshotQuery(baseline?.file ?? ''),
    enabled: Boolean(baseline),
  })

  const diff = useMemo(
    () => (previous ? diffSnapshots(previous, latest, mode) : null),
    [previous, latest, mode],
  )

  if (others.length === 0) {
    return (
      <Banner tone="info" title="Only one snapshot so far">
        <p>
          Trends compare two scans. Run <code>bun run scan</code> again later — a
          warm scan takes about 20 seconds — and this page will show what moved.
        </p>
      </Banner>
    )
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-baseline justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Trends</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            What moved between two scans. A folder that quietly gained 30 GB is a
            better target than one that has been a stable 40 GB for years.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={baseline?.id ?? ''}
            onChange={(event) =>
              void navigate({
                search: (prev) => ({ ...prev, against: event.target.value }),
                replace: true,
              })
            }
            aria-label="Compare against"
            className="h-8 rounded-md border bg-card px-2 text-xs text-muted-foreground"
          >
            {others.map((entry) => (
              <option key={entry.id} value={entry.id}>
                vs {new Date(entry.startedAt).toLocaleString()}
              </option>
            ))}
          </select>
          <SizeModeToggle active={mode} />
        </div>
      </header>

      {!diff || !previous ? (
        <p className="text-sm text-muted-foreground">Loading the earlier scan…</p>
      ) : (
        <>
          {diff.thresholdsDiffer ? (
            <Banner title="These scans used different thresholds">
              <p>
                One reported files at or above{' '}
                {formatBytes(previous.config.minFileSize)} and the other at{' '}
                {formatBytes(latest.config.minFileSize)}. Items can appear or vanish
                purely because the floor moved.
              </p>
            </Banner>
          ) : null}

          <div className="grid gap-4 sm:grid-cols-3">
            <ChangeCard label="Allocated" delta={diff.bytes} format={formatBytes} />
            <ChangeCard label="Unique" delta={diff.uniqueBytes} format={formatBytes} />
            <ChangeCard label="Files" delta={diff.files} format={formatCount} />
          </div>

          <p className="text-xs text-muted-foreground">
            {new Date(previous.startedAt).toLocaleString()} →{' '}
            {new Date(latest.startedAt).toLocaleString()}
          </p>

          <div className="grid gap-6 lg:grid-cols-2">
            <Card className="p-0">
              <div className="p-5 pb-0">
                <SectionTitle title="Folders that moved" hint="Biggest change first" />
              </div>
              <DeltaTable rows={diff.folders} />
            </Card>

            <Card className="p-0">
              <div className="p-5 pb-0">
                <SectionTitle
                  title="Extensions that moved"
                  hint="Which kinds of file account for it"
                />
              </div>
              <DeltaTable rows={diff.extensions} />
            </Card>
          </div>

          <div className="grid gap-6 lg:grid-cols-2">
            <Card className="p-0">
              <div className="p-5 pb-0">
                <SectionTitle
                  title="Newly reported files"
                  hint="Above the size floor in the newer scan only"
                />
              </div>
              <FileList rows={diff.appeared} tone="up" />
            </Card>

            <Card className="p-0">
              <div className="p-5 pb-0">
                <SectionTitle
                  title="No longer reported"
                  hint="Deleted, moved, or shrunk below the floor"
                />
              </div>
              <FileList rows={diff.vanished} tone="down" />
            </Card>
          </div>
        </>
      )}
    </div>
  )
}

function ChangeCard({
  label,
  delta,
  format,
}: {
  label: string
  delta: Delta
  format: (value: number) => string
}) {
  const grew = delta.change > 0
  return (
    <StatCard
      label={label}
      value={format(delta.after)}
      detail={
        delta.change === 0
          ? 'Unchanged.'
          : `${grew ? '+' : '−'}${format(Math.abs(delta.change))} since ${format(delta.before)}`
      }
      emphasis={grew}
    />
  )
}

function DeltaTable({ rows }: { rows: Delta[] }) {
  if (rows.length === 0) {
    return <p className="p-6 text-center text-sm text-muted-foreground">No change.</p>
  }
  const widest = Math.max(...rows.map((row) => Math.abs(row.change)))

  return (
    <table className="w-full text-sm">
      <tbody>
        {rows.map((row) => (
          <tr key={row.path || row.name} className="border-t border-border/50">
            <td className="max-w-0 px-4 py-2">
              <span className="block truncate" title={row.path || row.name}>
                {row.name}
              </span>
            </td>
            <td className="w-28 px-2 py-2">
              {/* A bar centred on zero: growth right, shrinkage left, so the
                  direction reads before the number does. */}
              <span className="flex h-1.5 w-24 items-center">
                <span className="flex h-full w-1/2 justify-end">
                  {row.change < 0 ? (
                    <span
                      className="block h-full rounded-l-full bg-foreground/40"
                      style={{ width: `${(Math.abs(row.change) / widest) * 100}%` }}
                    />
                  ) : null}
                </span>
                <span className="flex h-full w-1/2">
                  {row.change > 0 ? (
                    <span
                      className="block h-full rounded-r-full bg-destructive/70"
                      style={{ width: `${(row.change / widest) * 100}%` }}
                    />
                  ) : null}
                </span>
              </span>
            </td>
            <td
              className={cn(
                'tabular w-24 px-4 py-2 text-right font-medium',
                row.change > 0 ? 'text-destructive' : 'text-foreground',
              )}
            >
              {row.change > 0 ? '+' : '−'}
              {formatBytes(Math.abs(row.change))}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function FileList({
  rows,
  tone,
}: {
  rows: Array<{ path: string; size: number }>
  tone: 'up' | 'down'
}) {
  if (rows.length === 0) {
    return <p className="p-6 text-center text-sm text-muted-foreground">None.</p>
  }
  return (
    <ul className="text-sm">
      {rows.map((row) => (
        <li
          key={row.path}
          className="flex items-center gap-3 border-t border-border/50 px-4 py-2"
        >
          <span className="min-w-0 flex-1 truncate" title={row.path}>
            {row.path.slice(row.path.lastIndexOf('/') + 1)}
            <span className="block truncate text-xs text-muted-foreground">
              {row.path.slice(0, row.path.lastIndexOf('/'))}
            </span>
          </span>
          <span
            className={cn(
              'tabular w-20 text-right',
              tone === 'up' ? 'text-destructive' : 'text-muted-foreground',
            )}
          >
            {tone === 'up' ? '+' : '−'}
            {formatBytes(row.size)}
          </span>
          {tone === 'up' ? <CopyCommand path={row.path} /> : null}
        </li>
      ))}
    </ul>
  )
}
