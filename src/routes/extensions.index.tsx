import { useMemo } from 'react'
import { createFileRoute, useNavigate, useSearch, Link } from '@tanstack/react-router'
import { CATEGORIES, type Category, type ScanSnapshot } from '@shared/schema'
import { SnapshotGate } from '~/components/snapshot-gate'
import { SizeModeToggle, useSizeMode } from '~/components/size-mode-toggle'
import { Card, SectionTitle, StatCard } from '~/components/ui'
import { formatBytes, formatCount } from '~/lib/format'
import { CATEGORY_LABEL, categoryColor } from '~/lib/colors'
import {
  DEFAULT_EXTENSION_FILTERS,
  displayExtension,
  rankExtensions,
  type ExtensionFilters,
  type ExtensionSort,
} from '~/lib/extensions'
import { cn } from '~/lib/utils'

const SORTS: ExtensionSort[] = ['size', 'count', 'largest', 'name']

function isCategory(value: unknown): value is Category {
  return typeof value === 'string' && (CATEGORIES as readonly string[]).includes(value)
}

export const Route = createFileRoute('/extensions/')({
  validateSearch: (search: Record<string, unknown>): Partial<ExtensionFilters> => {
    const filters: Partial<ExtensionFilters> = {}
    if (Array.isArray(search.cats)) {
      const cats = search.cats.filter(isCategory)
      if (cats.length > 0) filters.cats = cats
    }
    if (typeof search.q === 'string' && search.q !== '') filters.q = search.q
    if (SORTS.includes(search.sort as ExtensionSort)) {
      filters.sort = search.sort as ExtensionSort
    }
    if (typeof search.desc === 'boolean') filters.desc = search.desc
    return filters
  },
  component: () => (
    <SnapshotGate>{(snapshot) => <Extensions snapshot={snapshot} />}</SnapshotGate>
  ),
})

const COLUMNS: Array<{ id: ExtensionSort | 'median'; label: string; sortable: boolean }> = [
  { id: 'name', label: 'Extension', sortable: true },
  { id: 'size', label: 'Size', sortable: true },
  { id: 'count', label: 'Files', sortable: true },
  { id: 'median', label: 'Median', sortable: false },
  { id: 'largest', label: 'Largest', sortable: true },
]

function Extensions({ snapshot }: { snapshot: ScanSnapshot }) {
  const navigate = useNavigate({ from: '/extensions/' })
  const search = Route.useSearch()
  // Snapshot and size mode belong to the root route; links must carry them
  // forward explicitly or a click would silently reset the view.
  const globals = useSearch({ from: '__root__' })
  const sharesBlocks = snapshot.volume
    ? snapshot.totals.bytes > snapshot.volume.usedBytes
    : false
  const mode = useSizeMode(sharesBlocks ? 'unique' : 'allocated')

  const filters = useMemo<ExtensionFilters>(
    () => ({ ...DEFAULT_EXTENSION_FILTERS, ...search }),
    [search],
  )

  const ranked = useMemo(
    () => rankExtensions(snapshot.extensions, filters, mode),
    [snapshot.extensions, filters, mode],
  )

  const setFilters = (patch: Partial<ExtensionFilters>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })

  const toggleSort = (next: ExtensionSort) =>
    setFilters(
      next === filters.sort
        ? { desc: !filters.desc }
        : { sort: next, desc: next !== 'name' },
    )

  const present = useMemo(() => {
    const counts = new Map<Category, number>()
    for (const stat of snapshot.extensions) {
      counts.set(stat.category, (counts.get(stat.category) ?? 0) + 1)
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1])
  }, [snapshot.extensions])

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-baseline justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Extensions</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {formatCount(snapshot.extensions.length)} distinct extensions. Most are
            noise parsed out of dotted filenames — the weight is in the first few.
          </p>
        </div>
        <SizeModeToggle active={mode} />
      </header>

      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard
          label="Listed"
          value={formatCount(ranked.head.length)}
          detail={`of ${formatCount(ranked.matched)} matching`}
        />
        <StatCard
          label={mode === 'unique' ? 'Unique bytes' : 'Allocated bytes'}
          value={formatBytes(ranked.head.reduce((sum, row) => sum + row.bytes, 0))}
          detail="Held by the extensions listed below."
          emphasis
        />
        <StatCard
          label="The tail"
          value={formatBytes(ranked.tail.bytes)}
          detail={`${formatCount(ranked.tail.count)} further extensions, ${formatCount(ranked.tail.files)} files.`}
        />
      </div>

      <Card className="p-4">
        <div className="flex flex-wrap items-center gap-2">
          <input
            type="search"
            value={filters.q}
            onChange={(event) => setFilters({ q: event.target.value })}
            placeholder="Find an extension…"
            className="h-8 min-w-56 flex-1 rounded-md border bg-card px-3 text-sm outline-none placeholder:text-muted-foreground focus:ring-1 focus:ring-ring"
          />
          <button
            type="button"
            onClick={() =>
              void navigate({ search: { ...globals }, replace: true })
            }
            className="h-8 rounded-md px-2 text-xs text-muted-foreground hover:text-foreground"
          >
            Reset
          </button>
        </div>
        <div className="mt-3 flex flex-wrap gap-1.5">
          {present.map(([category, count]) => {
            const selected = filters.cats.includes(category)
            return (
              <button
                key={category}
                type="button"
                onClick={() =>
                  setFilters({
                    cats: selected
                      ? filters.cats.filter((entry) => entry !== category)
                      : [...filters.cats, category],
                  })
                }
                className={cn(
                  'flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors',
                  selected
                    ? 'border-foreground/30 bg-accent text-foreground'
                    : 'text-muted-foreground hover:text-foreground',
                )}
              >
                <span
                  className="size-2 rounded-full"
                  style={{ backgroundColor: categoryColor(category) }}
                />
                {CATEGORY_LABEL[category]}
                <span className="tabular text-muted-foreground">{count}</span>
              </button>
            )
          })}
        </div>
      </Card>

      <Card className="p-0">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b text-xs text-muted-foreground">
              {COLUMNS.map((column) => (
                <th
                  key={column.id}
                  onClick={() =>
                    column.sortable && toggleSort(column.id as ExtensionSort)
                  }
                  className={cn(
                    'px-4 py-2 font-medium',
                    column.id === 'name' ? 'text-left' : 'text-right',
                    column.sortable ? 'cursor-pointer select-none' : '',
                  )}
                >
                  {column.label}
                  {column.sortable && filters.sort === column.id
                    ? filters.desc
                      ? ' ↓'
                      : ' ↑'
                    : ''}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {ranked.head.map((row) => (
              <tr
                key={row.stat.ext}
                className="border-b border-border/50 transition-colors hover:bg-accent/40"
              >
                <td className="px-4 py-2">
                  <Link
                    to="/extensions/$ext"
                    params={{ ext: row.stat.ext === '' ? '_none' : row.stat.ext }}
                    // The detail route also owns file sort params; start it at
                    // its defaults rather than inheriting this table's sort.
                    search={{ ...globals, sort: undefined, desc: undefined }}
                    className="flex items-center gap-2 hover:underline"
                  >
                    <span
                      className="size-2 shrink-0 rounded-full"
                      style={{ backgroundColor: categoryColor(row.stat.category) }}
                      title={CATEGORY_LABEL[row.stat.category]}
                    />
                    <span className="font-medium">{displayExtension(row.stat.ext)}</span>
                  </Link>
                </td>
                <td className="px-4 py-2">
                  <div className="flex items-center justify-end gap-2.5">
                    <span className="h-1 w-16 overflow-hidden rounded-full bg-foreground/10">
                      <span
                        className="block h-full rounded-full bg-foreground/40"
                        style={{ width: `${Math.max(2, Math.round(row.share * 100))}%` }}
                      />
                    </span>
                    <span className="tabular w-20 text-right font-medium">
                      {formatBytes(row.bytes)}
                    </span>
                  </div>
                </td>
                <td className="tabular px-4 py-2 text-right text-muted-foreground">
                  {formatCount(row.stat.fileCount)}
                </td>
                <td className="tabular px-4 py-2 text-right text-muted-foreground">
                  {formatBytes(row.stat.medianSize)}
                </td>
                <td className="tabular px-4 py-2 text-right text-muted-foreground">
                  {formatBytes(row.stat.maxSize)}
                </td>
              </tr>
            ))}
            {ranked.tail.count > 0 ? (
              <tr className="text-muted-foreground">
                <td className="px-4 py-2 italic">
                  {formatCount(ranked.tail.count)} further extensions
                </td>
                <td className="tabular px-4 py-2 text-right">
                  {formatBytes(ranked.tail.bytes)}
                </td>
                <td className="tabular px-4 py-2 text-right">
                  {formatCount(ranked.tail.files)}
                </td>
                <td colSpan={2} className="px-4 py-2 text-right text-xs">
                  Search to find one by name
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </Card>
    </div>
  )
}
