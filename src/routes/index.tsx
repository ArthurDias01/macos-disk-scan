import { createFileRoute } from '@tanstack/react-router'
import type { RecheckedPath, ScanSnapshot } from '@shared/schema'
import { formatBytes, formatCount, formatDuration } from '~/lib/format'
import { Banner, Card, SectionTitle, StatCard } from '~/components/ui'
import { SnapshotGate } from '~/components/snapshot-gate'
import { SizeModeToggle, useSizeMode } from '~/components/size-mode-toggle'
import { CategoryChart } from '~/components/charts/category-chart'
import { ExtensionChart } from '~/components/charts/extension-chart'
import { FolderTreemap } from '~/components/charts/folder-treemap'

export const Route = createFileRoute('/')({
  component: () => (
    <SnapshotGate>{(snapshot) => <Overview snapshot={snapshot} />}</SnapshotGate>
  ),
})

/**
 * The verdict on things you meant to delete.
 *
 * Closing this loop is the point: a cleanup that quietly failed looks exactly
 * like a cleanup that worked, because a deleted path simply stops appearing.
 */
function RecheckPanel({ rechecked }: { rechecked: RecheckedPath[] }) {
  const survivors = rechecked.filter((entry) => entry.present)
  const gone = rechecked.length - survivors.length

  if (survivors.length === 0) {
    return (
      <Banner tone="info" title={`${formatCount(gone)} previous deletions confirmed`}>
        <p>
          Every path recorded by an exported cleanup script is gone from disk.
        </p>
      </Banner>
    )
  }

  return (
    <Banner title={`${formatCount(survivors.length)} deleted item(s) are back`}>
      <p>
        These were moved to the Trash by a cleanup script but exist again —
        restored from the Trash, or recreated by the app that made them. They hold{' '}
        {formatBytes(survivors.reduce((sum, entry) => sum + entry.size, 0))}.
      </p>
      <ul className="mt-1 font-mono text-xs">
        {survivors.slice(0, 5).map((entry) => (
          <li key={entry.path} className="truncate">
            {entry.path}
          </li>
        ))}
      </ul>
      {gone > 0 ? (
        <p className="text-xs">{formatCount(gone)} other path(s) confirmed gone.</p>
      ) : null}
    </Banner>
  )
}

function Overview({ snapshot }: { snapshot: ScanSnapshot }) {
  const { totals, volume } = snapshot
  const sharesBlocks = volume ? totals.bytes > volume.usedBytes : false
  const permissionErrors = snapshot.unscanned.filter(
    (issue) => issue.code === 'EPERM' || issue.code === 'EACCES',
  )
  const reclaimable = snapshot.duplicateGroups.reduce(
    (sum, group) => sum + group.reclaimableBytes,
    0,
  )
  // When blocks are provably shared, allocated sizes make every chart unreadable:
  // one clone group swamps everything else. Unique is the honest default there.
  const mode = useSizeMode(sharesBlocks ? 'unique' : 'allocated')

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <div>
          <h1 className="text-xl font-semibold">Overview</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {snapshot.config.roots.join(', ')} · scanned{' '}
            {new Date(snapshot.startedAt).toLocaleString()} in{' '}
            {formatDuration(snapshot.durationMs)}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <p className="text-xs text-muted-foreground">
            Files at or above {formatBytes(snapshot.config.minFileSize)}, folders at
            or above {formatBytes(snapshot.config.minFolderSize)}
          </p>
          <SizeModeToggle active={mode} />
        </div>
      </header>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="Allocated"
          value={formatBytes(totals.bytes)}
          detail="As the filesystem reports it, matching du and Finder."
        />
        <StatCard
          label="Unique"
          value={formatBytes(totals.uniqueBytes)}
          detail={`Excludes ${formatCount(totals.duplicateFiles)} redundant copies of byte-identical files.`}
          emphasis
        />
        <StatCard
          label="Volume used"
          value={volume ? formatBytes(volume.usedBytes) : '—'}
          detail={
            volume
              ? `${formatBytes(volume.availableBytes)} free of ${formatBytes(volume.totalBytes)}`
              : undefined
          }
        />
        <StatCard
          label="Files"
          value={formatCount(totals.files)}
          detail={`${formatCount(totals.dirs)} directories · ${formatCount(snapshot.files.length)} above the size floor`}
        />
      </div>

      {sharesBlocks && volume ? (
        <Banner title="Some of these bytes do not exist twice">
          <p>
            The scan accounts for {formatBytes(totals.bytes)}, but the volume only
            holds {formatBytes(volume.usedBytes)}. Files are sharing blocks — on
            macOS that means APFS clones, which the filesystem bills to every copy
            in full.
          </p>
          <p>
            Deleting one clone frees nothing; only removing every copy releases the
            underlying blocks. <strong>Unique</strong> above is the honest floor.
          </p>
        </Banner>
      ) : null}

      {snapshot.rechecked && snapshot.rechecked.length > 0 ? (
        <RecheckPanel rechecked={snapshot.rechecked} />
      ) : null}

      {permissionErrors.length > 0 ? (
        <Banner title={`${permissionErrors.length} paths could not be read`}>
          <p>
            macOS withholds these from apps without Full Disk Access, so their
            contents are missing from every number on this page.
          </p>
          <ul className="mt-1 font-mono text-xs">
            {permissionErrors.slice(0, 5).map((issue) => (
              <li key={issue.path} className="truncate">
                {issue.path}
              </li>
            ))}
          </ul>
          {permissionErrors.length > 5 ? (
            <p className="text-xs">and {permissionErrors.length - 5} more</p>
          ) : null}
        </Banner>
      ) : null}

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <SectionTitle
            title="Where the weight sits"
            hint={`${mode === 'unique' ? 'Unique' : 'Allocated'} bytes by category`}
          />
          <CategoryChart categories={snapshot.categories} mode={mode} />
        </Card>

        <Card>
          <SectionTitle
            title="Biggest extensions"
            hint={`Top 15 of ${formatCount(snapshot.extensions.length)}`}
          />
          <ExtensionChart extensions={snapshot.extensions} mode={mode} />
        </Card>
      </div>

      <Card>
        <SectionTitle
          title="Folder map"
          hint={`Folders at or above ${formatBytes(snapshot.config.minFolderSize)}, three levels deep, ${mode === 'unique' ? 'excluding redundant copies' : 'as allocated'}`}
        />
        <FolderTreemap root={snapshot.folderTree} mode={mode} />
      </Card>

      {snapshot.duplicateGroups.length > 0 ? (
        <Card>
          <SectionTitle
            title="Byte-identical groups"
            hint={`${formatCount(snapshot.duplicateGroups.length)} groups holding ${formatBytes(reclaimable)} in redundant copies`}
          />
          <ul className="divide-y text-sm">
            {snapshot.duplicateGroups.slice(0, 5).map((group) => (
              <li key={group.fingerprint} className="flex items-baseline gap-3 py-2">
                <span className="tabular w-24 shrink-0 font-medium">
                  {formatBytes(group.reclaimableBytes)}
                </span>
                <span className="tabular w-28 shrink-0 text-xs text-muted-foreground">
                  {formatCount(group.count)} × {formatBytes(group.size)}
                </span>
                <span className="truncate font-mono text-xs text-muted-foreground">
                  {group.paths[0]}
                </span>
              </li>
            ))}
          </ul>
          <p className="mt-3 text-xs text-muted-foreground">
            Identical content, which means either APFS clones (deleting frees
            nothing) or real copies (deleting the extras frees the full amount).
            A group larger than the volume proves it is clones.
          </p>
        </Card>
      ) : null}
    </div>
  )
}
