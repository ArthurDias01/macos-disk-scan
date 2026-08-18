import { useMemo } from 'react'
import {
  createSortedRowModel,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_basic,
  tableFeatures,
  useTable,
  type ColumnDef,
} from '@tanstack/react-table'
import type { FolderNode } from '@shared/schema'
import { formatAge, formatBytes, formatCount } from '~/lib/format'
import { folderOwnSize, folderSize, truncatedSize } from '~/lib/folder-tree'
import type { SizeMode } from '~/lib/size-mode'
import { CopyCommand } from '~/components/copy-command'
import { SelectCheckbox } from '~/components/selection-bar'
import { useSelection } from '~/lib/selection'
import { cn } from '~/lib/utils'

const features = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns: { alphanumeric: sortFn_alphanumeric, basic: sortFn_basic },
})

interface Row {
  node: FolderNode
  name: string
  size: number
  ownSize: number
  fileCount: number
  duplicateSize: number
  maxMtimeMs: number
  /** Share of the parent folder, for the inline bar. */
  share: number
  hasChildren: boolean
}

export function FolderTable({
  folder,
  mode,
  onDrill,
}: {
  folder: FolderNode
  mode: SizeMode
  onDrill: (node: FolderNode) => void
}) {
  const selection = useSelection()
  const parentSize = folderSize(folder, mode)

  const data = useMemo<Row[]>(
    () =>
      folder.children.map((child) => {
        const size = folderSize(child, mode)
        return {
          node: child,
          name: child.name,
          size,
          ownSize: folderOwnSize(child, mode),
          fileCount: child.fileCount,
          duplicateSize: child.duplicateRecursiveSize,
          maxMtimeMs: child.maxMtimeMs,
          share: parentSize > 0 ? size / parentSize : 0,
          hasChildren: child.children.length > 0,
        }
      }),
    [folder, mode, parentSize],
  )

  const columns = useMemo<ColumnDef<typeof features, Row>[]>(
    () => [
      {
        accessorKey: 'name',
        header: 'Folder',
        sortFn: 'alphanumeric',
        cell: (info) => {
          const row = info.row.original
          return (
            <span className="flex items-center gap-2">
              <SelectCheckbox
                checked={selection.isSelected(row.node.path)}
                onChange={() =>
                  selection.toggle({
                    path: row.node.path,
                    size: row.size,
                    kind: 'folder',
                  })
                }
                label={`Select ${row.name}`}
              />
              <span className="truncate font-medium">{row.name}</span>
              {row.hasChildren ? (
                <span className="text-xs text-muted-foreground">
                  {row.node.children.length}
                </span>
              ) : null}
            </span>
          )
        },
      },
      {
        accessorKey: 'size',
        header: 'Size',
        cell: (info) => {
          const row = info.row.original
          return (
            <div className="flex items-center justify-end gap-2.5">
              {/* A track, not a floating pill: at a 1% share the old bar was a
                  two-pixel dot with nothing to measure it against. */}
              <span
                className="h-1 w-14 overflow-hidden rounded-full bg-foreground/10"
                title={`${(row.share * 100).toFixed(1)}% of ${folder.name || 'this folder'}`}
              >
                <span
                  className="block h-full rounded-full bg-foreground/40"
                  style={{ width: `${Math.max(2, Math.round(row.share * 100))}%` }}
                />
              </span>
              <span className="tabular w-20 text-right">{formatBytes(row.size)}</span>
            </div>
          )
        },
      },
      {
        accessorKey: 'ownSize',
        header: 'Own',
        cell: (info) => (
          <span className="tabular text-right text-muted-foreground">
            {formatBytes(info.getValue<number>())}
          </span>
        ),
      },
      {
        accessorKey: 'duplicateSize',
        header: 'Redundant',
        cell: (info) => {
          const value = info.getValue<number>()
          return (
            <span
              className={cn(
                'tabular text-right',
                value > 0 ? 'text-muted-foreground' : 'text-muted-foreground/40',
              )}
            >
              {value > 0 ? formatBytes(value) : '—'}
            </span>
          )
        },
      },
      {
        accessorKey: 'fileCount',
        header: 'Files',
        cell: (info) => (
          <span className="tabular text-right text-muted-foreground">
            {formatCount(info.getValue<number>())}
          </span>
        ),
      },
      {
        accessorKey: 'maxMtimeMs',
        header: 'Newest file',
        cell: (info) => (
          <span className="text-right text-muted-foreground">
            {formatAge(info.getValue<number>())}
          </span>
        ),
      },
      {
        id: 'actions',
        header: '',
        cell: (info) => <CopyCommand path={info.row.original.node.path} />,
      },
    ],
    [folder.name, selection],
  )

  const table = useTable({
    features,
    columns,
    data,
    initialState: { sorting: [{ id: 'size', desc: true }] },
  })

  const truncated = truncatedSize(folder, mode)

  if (data.length === 0 && truncated === 0) {
    return (
      <p className="py-6 text-center text-sm text-muted-foreground">
        Nothing inside this folder crosses the reporting threshold.
      </p>
    )
  }

  return (
    <table className="w-full text-sm">
      <thead>
        {table.getHeaderGroups().map((headerGroup) => (
          <tr key={headerGroup.id} className="border-b text-xs text-muted-foreground">
            {headerGroup.headers.map((header) => (
              <th
                key={header.id}
                onClick={header.column.getToggleSortingHandler()}
                className={cn(
                  'py-2 font-medium',
                  header.id === 'name' ? 'text-left' : 'text-right',
                  // A fixed, padded action column: without it the control sat
                  // flush against the "Newest file" value.
                  header.id === 'actions' ? 'w-24 pl-6' : '',
                  header.id === 'maxMtimeMs' ? 'pl-6' : '',
                  header.column.getCanSort() ? 'cursor-pointer select-none' : '',
                )}
              >
                {header.isPlaceholder ? null : (
                  <span className="inline-flex items-center gap-1">
                    <table.FlexRender header={header} />
                    {{ asc: '↑', desc: '↓' }[header.column.getIsSorted() as string] ??
                      null}
                  </span>
                )}
              </th>
            ))}
          </tr>
        ))}
      </thead>
      <tbody>
        {table.getRowModel().rows.map((row) => (
          <tr
            key={row.id}
            onClick={() =>
              row.original.hasChildren ? onDrill(row.original.node) : undefined
            }
            className={cn(
              'border-b border-border/50 transition-colors',
              row.original.hasChildren ? 'cursor-pointer hover:bg-accent/50' : '',
            )}
          >
            {row.getAllCells().map((cell) => (
              <td
                key={cell.id}
                className={cn(
                  'py-2',
                  cell.column.id === 'name' ? 'max-w-0 text-left' : 'text-right',
                  cell.column.id === 'actions' ? 'w-24 pl-6' : '',
                  cell.column.id === 'maxMtimeMs' ? 'pl-6' : '',
                )}
              >
                <table.FlexRender cell={cell} />
              </td>
            ))}
          </tr>
        ))}
        {truncated > 0 ? (
          <tr className="text-muted-foreground">
            <td className="py-2 text-left italic">
              {formatCount(folder.truncatedChildCount)} folders below the threshold
            </td>
            <td className="tabular py-2 text-right">{formatBytes(truncated)}</td>
            <td colSpan={5} />
          </tr>
        ) : null}
      </tbody>
    </table>
  )
}
