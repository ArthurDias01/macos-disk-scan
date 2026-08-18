import { useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { CATEGORIES, type Category, type ScanSnapshot } from '@shared/schema'
import { formatBytes, formatCount } from '~/lib/format'
import {
  DEFAULT_FILTERS,
  categoryFacets,
  filterFiles,
  sortFiles,
  summarize,
  type FileFilters,
  type FileSort,
} from '~/lib/file-filters'
import { Card, StatCard } from '~/components/ui'
import { SnapshotGate } from '~/components/snapshot-gate'
import { FileFilterBar } from '~/components/file-filter-bar'
import { FileTable } from '~/components/file-table'

const SORTS: FileSort[] = ['size', 'age', 'path']

function isCategory(value: unknown): value is Category {
  return typeof value === 'string' && (CATEGORIES as readonly string[]).includes(value)
}

export const Route = createFileRoute('/files')({
  // Filters live in the URL so a view worth building once — ".dmg and .zip,
  // untouched two years, over 1 GB" — is a link you keep, not something you
  // rebuild on every visit.
  validateSearch: (search: Record<string, unknown>): Partial<FileFilters> => {
    // Keys are omitted rather than set to undefined, so spreading over the
    // defaults is enough and no key-stripping pass is needed. Omitted keys
    // also stay out of the URL, keeping a default view's link clean.
    const filters: Partial<FileFilters> = {}
    if (Array.isArray(search.cats)) {
      const cats = search.cats.filter(isCategory)
      if (cats.length > 0) filters.cats = cats
    }
    if (typeof search.q === 'string' && search.q !== '') filters.q = search.q
    if (typeof search.min === 'number' && search.min > 0) filters.min = search.min
    if (typeof search.olderMonths === 'number' && search.olderMonths > 0) {
      filters.olderMonths = search.olderMonths
    }
    if (search.dup === true) filters.dup = true
    if (search.hideCloud === true) filters.hideCloud = true
    if (SORTS.includes(search.sort as FileSort)) filters.sort = search.sort as FileSort
    if (typeof search.desc === 'boolean') filters.desc = search.desc
    return filters
  },
  component: () => <SnapshotGate>{(snapshot) => <Files snapshot={snapshot} />}</SnapshotGate>,
})

function Files({ snapshot }: { snapshot: ScanSnapshot }) {
  const navigate = useNavigate({ from: '/files' })
  const search = Route.useSearch()

  // A fresh object here would defeat every memo below, since it is their
  // dependency. `search` keeps a stable identity between navigations.
  const filters = useMemo<FileFilters>(
    () => ({ ...DEFAULT_FILTERS, ...search }),
    [search],
  )

  const setFilters = (patch: Partial<FileFilters>) =>
    void navigate({
      search: (prev) => ({ ...prev, ...patch }),
      // Typing in the path box would otherwise push a history entry per
      // keystroke, making the back button useless.
      replace: true,
    })

  const visible = useMemo(
    () => sortFiles(filterFiles(snapshot.files, filters), filters.sort, filters.desc),
    [snapshot.files, filters],
  )
  const summary = useMemo(() => summarize(visible), [visible])
  const facets = useMemo(() => categoryFacets(snapshot.files), [snapshot.files])
  const total = useMemo(() => summarize(snapshot.files), [snapshot.files])

  const toggleSort = (next: FileSort) =>
    setFilters(
      next === filters.sort
        ? { desc: !filters.desc }
        : // Size and age read newest/biggest first; paths read A-Z.
          { sort: next, desc: next !== 'path' },
    )

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Files</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Every file at or above {formatBytes(snapshot.config.minFileSize)} in this
          scan — {formatCount(total.count)} of them, {formatBytes(total.bytes)}{' '}
          allocated.
        </p>
      </header>

      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard
          label="Matching"
          value={formatCount(summary.count)}
          detail={`of ${formatCount(total.count)} reported files`}
        />
        <StatCard
          label="Allocated"
          value={formatBytes(summary.bytes)}
          detail="What the filesystem bills for these files."
        />
        <StatCard
          label="Frees if deleted"
          value={formatBytes(summary.uniqueBytes)}
          detail={
            summary.sharedCount > 0
              ? `${formatCount(summary.sharedCount)} of these share blocks with another file and free nothing on their own.`
              : 'None of these share blocks with another file.'
          }
          emphasis
        />
      </div>

      <Card className="p-4">
        <FileFilterBar
          filters={filters}
          facets={facets}
          onChange={setFilters}
          onReset={() =>
            // Clears the file filters but keeps the snapshot and size-mode
            // selection, which belong to the whole app rather than this page.
            void navigate({
              search: (prev) => ({ snapshot: prev.snapshot, size: prev.size }),
              replace: true,
            })
          }
        />
      </Card>

      <Card className="p-0">
        <FileTable
          files={visible}
          sort={filters.sort}
          desc={filters.desc}
          onSort={toggleSort}
        />
      </Card>
    </div>
  )
}
