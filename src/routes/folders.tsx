import { createFileRoute, useNavigate } from '@tanstack/react-router'
import type { FolderNode, ScanSnapshot } from '@shared/schema'
import { folderChain, findFolder, folderOwnSize, folderSize } from '~/lib/folder-tree'
import { formatAge, formatBytes, formatCount } from '~/lib/format'
import { Card, SectionTitle, StatCard } from '~/components/ui'
import { SnapshotGate } from '~/components/snapshot-gate'
import { SizeModeToggle, useSizeMode } from '~/components/size-mode-toggle'
import { FolderTreemap } from '~/components/charts/folder-treemap'
import { FolderTable } from '~/components/folder-table'
import { CopyCommand } from '~/components/copy-command'
import { cn } from '~/lib/utils'

export const Route = createFileRoute('/folders')({
  validateSearch: (search: Record<string, unknown>) => ({
    path: typeof search.path === 'string' ? search.path : undefined,
  }),
  component: () => (
    <SnapshotGate>{(snapshot) => <Folders snapshot={snapshot} />}</SnapshotGate>
  ),
})

function Folders({ snapshot }: { snapshot: ScanSnapshot }) {
  const navigate = useNavigate({ from: '/folders' })
  const { path } = Route.useSearch()
  const sharesBlocks = snapshot.volume
    ? snapshot.totals.bytes > snapshot.volume.usedBytes
    : false
  const mode = useSizeMode(sharesBlocks ? 'unique' : 'allocated')

  const root = snapshot.folderTree
  const chain = folderChain(root, path)
  const folder = findFolder(root, path)

  const goTo = (node: FolderNode) =>
    void navigate({
      search: (prev) => ({
        ...prev,
        // The root needs no param, so the default view has a clean URL.
        path: node.path === root.path ? undefined : node.path,
      }),
    })

  const size = folderSize(folder, mode)
  const own = folderOwnSize(folder, mode)

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-baseline justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold">Folders</h1>
          <Breadcrumb chain={chain} onNavigate={goTo} />
        </div>
        <div className="flex items-center gap-3">
          <CopyCommand path={folder.path} variant="labelled" />
          <SizeModeToggle active={mode} />
        </div>
      </header>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label={mode === 'unique' ? 'Unique size' : 'Allocated size'}
          value={formatBytes(size)}
          detail="This folder and everything beneath it."
          emphasis
        />
        <StatCard
          label="Own files"
          value={formatBytes(own)}
          detail={`${formatCount(folder.ownFileCount)} files directly inside — safe to compare across folders.`}
        />
        <StatCard
          label="Redundant"
          value={formatBytes(folder.duplicateRecursiveSize)}
          detail="Held by byte-identical copies beneath this folder."
        />
        <StatCard
          label="Newest file"
          value={formatAge(folder.maxMtimeMs)}
          detail={`${formatCount(folder.fileCount)} files in the subtree.`}
        />
      </div>

      <Card>
        <SectionTitle
          title="Inside this folder"
          hint={
            folder.children.length > 0
              ? 'Click a row to drill in. Size includes descendants; Own counts only direct files.'
              : 'No sub-folders cross the reporting threshold.'
          }
        />
        <FolderTable folder={folder} mode={mode} onDrill={goTo} />
      </Card>

      <Card>
        <SectionTitle
          title="Map"
          hint={`${mode === 'unique' ? 'Unique' : 'Allocated'} bytes, two levels below ${folder.name}`}
        />
        <FolderTreemap root={folder} mode={mode} maxDepth={2} height={360} />
      </Card>
    </div>
  )
}

/** `/Users/arthur` is the home directory, and "arthur" alone reads as a stray word. */
function rootLabel(path: string): string {
  return /^\/Users\/[^/]+$/.test(path) ? '~' : path
}

function Breadcrumb({
  chain,
  onNavigate,
}: {
  chain: FolderNode[]
  onNavigate: (node: FolderNode) => void
}) {
  return (
    <nav className="mt-1 flex flex-wrap items-center gap-1 text-sm">
      {chain.map((node, index) => {
        const isLast = index === chain.length - 1
        return (
          <span key={node.path || 'root'} className="flex items-center gap-1">
            {index > 0 ? <span className="text-muted-foreground/60">/</span> : null}
            <button
              type="button"
              onClick={() => onNavigate(node)}
              disabled={isLast}
              className={cn(
                'max-w-[280px] truncate rounded px-1 py-0.5',
                isLast
                  ? 'font-medium text-foreground'
                  : 'text-muted-foreground hover:bg-accent hover:text-foreground',
              )}
            >
              {index === 0 ? rootLabel(node.path) : node.name || node.path}
            </button>
          </span>
        )
      })}
    </nav>
  )
}
