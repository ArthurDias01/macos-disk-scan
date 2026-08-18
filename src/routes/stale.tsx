import { useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import type { ScanSnapshot } from '@shared/schema'
import { SnapshotGate } from '~/components/snapshot-gate'
import { Card, SectionTitle, StatCard } from '~/components/ui'
import { FileTable } from '~/components/file-table'
import { StaleScatter } from '~/components/charts/stale-scatter'
import { formatAge, formatBytes, formatCount } from '~/lib/format'
import { analyseStale, DEFAULT_THRESHOLDS, type StaleThresholds } from '~/lib/stale'
import { sortFiles, type FileSort } from '~/lib/file-filters'
import { useSelection } from '~/lib/selection'
import { cn } from '~/lib/utils'

const MB = 1024 * 1024
const GB = 1024 * MB

const SIZE_CUTS = [
  { value: 100 * MB, label: '100 MB' },
  { value: 500 * MB, label: '500 MB' },
  { value: 1 * GB, label: '1 GB' },
  { value: 5 * GB, label: '5 GB' },
]

const AGE_CUTS = [
  { value: 6, label: '6 months' },
  { value: 12, label: '1 year' },
  { value: 24, label: '2 years' },
  { value: 60, label: '5 years' },
]

export const Route = createFileRoute('/stale')({
  validateSearch: (search: Record<string, unknown>) => {
    const value: Partial<StaleThresholds & { sort: FileSort; desc: boolean }> = {}
    if (typeof search.minSize === 'number') value.minSize = search.minSize
    if (typeof search.minMonths === 'number') value.minMonths = search.minMonths
    if (search.sort === 'size' || search.sort === 'age' || search.sort === 'path') {
      value.sort = search.sort
    }
    if (typeof search.desc === 'boolean') value.desc = search.desc
    return value
  },
  component: () => (
    <SnapshotGate>{(snapshot) => <Stale snapshot={snapshot} />}</SnapshotGate>
  ),
})

function Stale({ snapshot }: { snapshot: ScanSnapshot }) {
  const navigate = useNavigate({ from: '/stale' })
  const search = Route.useSearch()
  const selection = useSelection()

  const thresholds: StaleThresholds = {
    minSize: search.minSize ?? DEFAULT_THRESHOLDS.minSize,
    minMonths: search.minMonths ?? DEFAULT_THRESHOLDS.minMonths,
  }
  const sort = search.sort ?? 'size'
  const desc = search.desc ?? true

  const analysis = useMemo(
    () => analyseStale(snapshot.files, thresholds),
    [snapshot.files, thresholds.minSize, thresholds.minMonths],
  )
  const listed = useMemo(
    () => sortFiles(analysis.candidates, sort, desc),
    [analysis.candidates, sort, desc],
  )

  const set = (patch: Partial<StaleThresholds>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })

  const selectAll = () => {
    for (const file of analysis.candidates) {
      if (!selection.isSelected(file.path)) {
        selection.toggle({
          path: file.path,
          size: file.size,
          kind: 'file',
          sharesBlocks: Boolean(file.isDuplicateCopy) || file.isDupInode,
        })
      }
    }
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Stale</h1>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
          Size says what is expensive, age says what is forgotten. Neither is a
          delete list alone — a 30 GB disk image touched this morning is in use.
          The intersection is the list.
        </p>
      </header>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="Candidates"
          value={formatCount(analysis.candidates.length)}
          detail={`At or above ${formatBytes(thresholds.minSize)} and untouched ${thresholds.minMonths >= 12 ? `${thresholds.minMonths / 12} year(s)` : `${thresholds.minMonths} months`}.`}
        />
        <StatCard
          label="Allocated"
          value={formatBytes(analysis.bytes)}
          detail="What the filesystem bills for them."
        />
        <StatCard
          label="Frees if deleted"
          value={formatBytes(analysis.freeableBytes)}
          detail={
            analysis.bytes === analysis.freeableBytes
              ? 'None of these share blocks.'
              : 'The rest share blocks with another file.'
          }
          emphasis
        />
        <StatCard
          label="Oldest"
          value={analysis.oldest ? formatAge(analysis.oldest.mtimeMs) : '—'}
          detail={
            analysis.oldest
              ? analysis.oldest.path.slice(analysis.oldest.path.lastIndexOf('/') + 1)
              : 'Nothing crosses both thresholds.'
          }
        />
      </div>

      <Card className="p-4">
        <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
          <Cuts
            label="Bigger than"
            options={SIZE_CUTS}
            active={thresholds.minSize}
            onPick={(value) => set({ minSize: value })}
          />
          <Cuts
            label="Untouched for"
            options={AGE_CUTS}
            active={thresholds.minMonths}
            onPick={(value) => set({ minMonths: value })}
          />
          <button
            type="button"
            onClick={selectAll}
            disabled={analysis.candidates.length === 0}
            className={cn(
              'ml-auto rounded-md border px-2.5 py-1.5 text-xs transition-[transform,color,background-color] duration-150 ease-out active:scale-[0.97]',
              analysis.candidates.length > 0
                ? 'text-muted-foreground hover:bg-accent hover:text-foreground'
                : 'cursor-default text-muted-foreground/40',
            )}
          >
            Select all {formatCount(analysis.candidates.length)}
          </button>
        </div>
      </Card>

      <Card>
        <SectionTitle
          title="Size against age"
          hint="Thresholds are drawn, so moving one visibly moves the list."
        />
        <StaleScatter
          points={analysis.points}
          minSize={thresholds.minSize}
          minMonths={thresholds.minMonths}
        />
      </Card>

      <Card className="p-0">
        {listed.length > 0 ? (
          <FileTable
            files={listed}
            sort={sort}
            desc={desc}
            onSort={(next) =>
              void navigate({
                search: (prev) => ({
                  ...prev,
                  ...(next === sort
                    ? { desc: !desc }
                    : { sort: next, desc: next !== 'path' }),
                }),
                replace: true,
              })
            }
          />
        ) : (
          <p className="p-10 text-center text-sm text-muted-foreground">
            Nothing is both that big and that old. Loosen a threshold above.
          </p>
        )}
      </Card>

      <p className="text-xs text-muted-foreground">
        Age uses each file's modification time. macOS access times are bumped by
        Spotlight indexing and backups, so they would report much of the disk as
        freshly used and are not the axis here.
      </p>
    </div>
  )
}

function Cuts({
  label,
  options,
  active,
  onPick,
}: {
  label: string
  options: Array<{ value: number; label: string }>
  active: number
  onPick: (value: number) => void
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-muted-foreground">{label}</span>
      <div className="flex items-center gap-1 rounded-md border p-0.5">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => onPick(option.value)}
            className={cn(
              'rounded px-2 py-1 text-xs transition-colors',
              option.value === active
                ? 'bg-accent text-foreground'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  )
}
