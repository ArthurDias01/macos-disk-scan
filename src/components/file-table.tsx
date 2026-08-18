import { useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { FileEntry } from '@shared/schema'
import { formatAge, formatBytes } from '~/lib/format'
import { categoryColor } from '~/lib/colors'
import type { FileSort } from '~/lib/file-filters'
import { CopyCommand } from '~/components/copy-command'
import { SelectCheckbox } from '~/components/selection-bar'
import { useSelection } from '~/lib/selection'
import { cn } from '~/lib/utils'

/** Row height in pixels, fixed so the virtualizer never has to measure. */
const ROW_HEIGHT = 44

/**
 * One column spec drives both the header and the rows.
 *
 * Declaring widths twice is how a table quietly goes crooked: change the header
 * and the body keeps its old width. `sort` marks the columns the header can
 * order by, and doubles as the sort key — no cast at the click handler.
 */
const COLUMNS = [
  { id: 'path', label: 'File', width: 'min-w-0 flex-1', align: 'text-left', sort: 'path' },
  { id: 'size', label: 'Size', width: 'w-24', align: 'text-right', sort: 'size' },
  { id: 'age', label: 'Modified', width: 'w-28', align: 'text-right', sort: 'age' },
  { id: 'flags', label: '', width: 'w-16 shrink-0', align: 'text-left', sort: null },
  { id: 'actions', label: '', width: 'w-20 shrink-0', align: 'text-right', sort: null },
] as const satisfies ReadonlyArray<{
  id: string
  label: string
  width: string
  align: string
  sort: FileSort | null
}>

export function FileTable({
  files,
  sort,
  desc,
  onSort,
}: {
  files: FileEntry[]
  sort: FileSort
  desc: boolean
  onSort: (next: FileSort) => void
}) {
  const selection = useSelection()
  const scrollRef = useRef<HTMLDivElement>(null)

  const virtualizer = useVirtualizer({
    count: files.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 12,
  })

  if (files.length === 0) {
    return (
      <p className="py-10 text-center text-sm text-muted-foreground">
        No files match these filters.
      </p>
    )
  }

  return (
    <div>
      <div className="flex items-center gap-4 border-b px-4 py-2 text-xs text-muted-foreground">
        {COLUMNS.map((column) => (
          <button
            key={column.id}
            type="button"
            disabled={column.sort === null}
            onClick={() => column.sort !== null && onSort(column.sort)}
            className={cn(
              'font-medium',
              column.width,
              column.align,
              column.sort !== null ? 'cursor-pointer hover:text-foreground' : '',
            )}
          >
            {column.label}
            {column.sort === sort ? (desc ? ' ↓' : ' ↑') : ''}
          </button>
        ))}
      </div>

      {/* Fills the viewport rather than a fixed 600px, which left dead space
          on a laptop and wasted half a large display. */}
      <div
        ref={scrollRef}
        className="h-[calc(100vh-23rem)] min-h-80 overflow-auto"
      >
        <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
          {virtualizer.getVirtualItems().map((row) => {
            const file = files[row.index]
            return (
              <div
                key={file.path}
                className="absolute inset-x-0 top-0 flex items-center gap-4 border-b border-border/50 px-4"
                style={{
                  height: `${row.size}px`,
                  transform: `translateY(${row.start}px)`,
                }}
              >
                <span className={cn(COLUMNS[0].width, 'flex items-center gap-2')}>
                  <SelectCheckbox
                    checked={selection.isSelected(file.path)}
                    onChange={() =>
                      selection.toggle({
                        path: file.path,
                        size: file.size,
                        kind: 'file',
                        sharesBlocks: Boolean(file.isDuplicateCopy) || file.isDupInode,
                      })
                    }
                    label={`Select ${file.path}`}
                  />
                  <FileCell file={file} />
                </span>
                <span
                  className={cn(COLUMNS[1].width, COLUMNS[1].align, 'tabular text-sm font-medium')}
                >
                  {formatBytes(file.size)}
                </span>
                <span
                  className={cn(COLUMNS[2].width, COLUMNS[2].align, 'text-xs text-muted-foreground')}
                >
                  {formatAge(file.mtimeMs)}
                </span>
                <span className={cn(COLUMNS[3].width, 'flex flex-wrap gap-1')}>
                  <Flags file={file} />
                </span>
                <span className={COLUMNS[4].width}>
                  <CopyCommand path={file.path} />
                </span>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function FileCell({ file }: { file: FileEntry }) {
  const cut = file.path.lastIndexOf('/')
  const directory = file.path.slice(0, cut)
  const name = file.path.slice(cut + 1)

  return (
    <>
      <span
        className="size-2 shrink-0 rounded-full"
        style={{ backgroundColor: categoryColor(file.category) }}
        title={file.category}
      />
      <span className="min-w-0">
        <span className="block truncate text-sm">{name}</span>
        {/* Directory second and dimmed: the filename identifies a row, but the
            path is what tells you whether it is safe to delete. */}
        <span className="block truncate text-xs text-muted-foreground" title={file.path}>
          {directory}
        </span>
      </span>
    </>
  )
}

/**
 * Why a row might free less than its size suggests. Data-driven so a new flag
 * is one entry rather than another branch.
 */
const FLAGS: ReadonlyArray<{
  id: string
  applies: (file: FileEntry) => boolean
  title: (file: FileEntry) => string
}> = [
  {
    id: 'copy',
    applies: (file) => Boolean(file.isDuplicateCopy),
    title: (file) =>
      `One of ${file.duplicateCopies} byte-identical files. If these are APFS clones, deleting this frees nothing.`,
  },
  {
    id: 'link',
    applies: (file) => file.isDupInode,
    title: () => 'A hardlink to bytes counted elsewhere. Deleting it frees nothing.',
  },
  {
    id: 'cloud',
    applies: (file) => file.isCloud,
    title: () => 'Cloud-backed. If evicted it already occupies no local space.',
  },
  {
    id: 'bundle',
    applies: (file) => file.isBundle,
    title: () => 'A macOS bundle, counted as one item.',
  },
]

function Flags({ file }: { file: FileEntry }) {
  return (
    <>
      {FLAGS.filter((flag) => flag.applies(file)).map((flag) => (
        <span
          key={flag.id}
          title={flag.title(file)}
          className="rounded border px-1 py-0.5 text-[10px] text-muted-foreground"
        >
          {flag.id}
        </span>
      ))}
    </>
  )
}
