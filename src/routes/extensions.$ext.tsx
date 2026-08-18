import { useMemo } from 'react'
import { createFileRoute, Link, useNavigate, useSearch } from '@tanstack/react-router'
import type { ScanSnapshot } from '@shared/schema'
import { SnapshotGate } from '~/components/snapshot-gate'
import { Banner, Card, SectionTitle, StatCard } from '~/components/ui'
import { FileTable } from '~/components/file-table'
import { ExtensionHistogram } from '~/components/charts/extension-histogram'
import { formatBytes, formatCount } from '~/lib/format'
import { CATEGORY_LABEL, categoryColor } from '~/lib/colors'
import { displayExtension } from '~/lib/extensions'
import { sortFiles, type FileSort } from '~/lib/file-filters'
import { sizeIn } from '~/lib/size-mode'
import { useSizeMode } from '~/components/size-mode-toggle'

/** `(none)` has no spelling in a URL, so the empty extension travels as `_none`. */
const NONE = '_none'

export const Route = createFileRoute('/extensions/$ext')({
  validateSearch: (search: Record<string, unknown>) => ({
    sort: search.sort === 'age' || search.sort === 'path' ? (search.sort as FileSort) : undefined,
    desc: typeof search.desc === 'boolean' ? search.desc : undefined,
  }),
  component: () => (
    <SnapshotGate>{(snapshot) => <ExtensionDetail snapshot={snapshot} />}</SnapshotGate>
  ),
})

function ExtensionDetail({ snapshot }: { snapshot: ScanSnapshot }) {
  const { ext } = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: '/extensions/$ext' })
  const globals = useSearch({ from: '__root__' })
  const key = ext === NONE ? '' : ext

  const sharesBlocks = snapshot.volume
    ? snapshot.totals.bytes > snapshot.volume.usedBytes
    : false
  const mode = useSizeMode(sharesBlocks ? 'unique' : 'allocated')

  const stat = snapshot.extensions.find((entry) => entry.ext === key)

  const sort = search.sort ?? 'size'
  const desc = search.desc ?? true
  const files = useMemo(
    () =>
      sortFiles(
        snapshot.files.filter((file) => file.ext === key),
        sort,
        desc,
      ),
    [snapshot.files, key, sort, desc],
  )

  if (!stat) {
    return (
      <Banner title={`No extension ${displayExtension(key)} in this scan`}>
        <p>
          It may exist in another snapshot.{' '}
          <Link to="/extensions" search={{ ...globals }} className="underline">
            Back to all extensions
          </Link>
          .
        </p>
      </Banner>
    )
  }

  const bytes = sizeIn(mode, stat.totalSize, stat.duplicateBytes)

  return (
    <div className="space-y-6">
      <header>
        <Link
          to="/extensions"
          search={{ ...globals }}
          className="text-sm text-muted-foreground hover:text-foreground"
        >
          ← Extensions
        </Link>
        <h1 className="mt-1 flex items-center gap-2 text-xl font-semibold">
          <span
            className="size-3 rounded-full"
            style={{ backgroundColor: categoryColor(stat.category) }}
          />
          {displayExtension(stat.ext)}
          <span className="text-sm font-normal text-muted-foreground">
            {CATEGORY_LABEL[stat.category]}
          </span>
        </h1>
      </header>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label={mode === 'unique' ? 'Unique bytes' : 'Allocated bytes'}
          value={formatBytes(bytes)}
          detail={
            stat.duplicateBytes > 0
              ? `${formatBytes(stat.duplicateBytes)} of it is redundant copies.`
              : 'No byte-identical copies among these.'
          }
          emphasis
        />
        <StatCard
          label="Files"
          value={formatCount(stat.fileCount)}
          detail={`Mean ${formatBytes(stat.meanSize)}.`}
        />
        <StatCard
          label="Median"
          value={formatBytes(stat.medianSize)}
          detail={`95% are under ${formatBytes(stat.p95Size)}. Both are accurate to a power of two.`}
        />
        <StatCard
          label="Largest"
          value={formatBytes(stat.maxSize)}
          detail={stat.largestPath.slice(stat.largestPath.lastIndexOf('/') + 1)}
        />
      </div>

      <Card>
        <SectionTitle
          title="Size distribution"
          hint="Every file of this type, in power-of-two buckets — including the ones below the reporting floor."
        />
        <ExtensionHistogram histogram={stat.histogram} />
      </Card>

      <Card className="p-0">
        {files.length > 0 ? (
          <FileTable
            files={files}
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
            No individual file of this type reaches{' '}
            {formatBytes(snapshot.config.minFileSize)}, so none is listed. The
            distribution above still counts every one of them.
          </p>
        )}
      </Card>
    </div>
  )
}
