import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { snapshotIndexQuery } from '~/lib/snapshots'
import { formatBytes } from '~/lib/format'

export function SnapshotPicker() {
  const navigate = useNavigate()
  const { snapshot: selected } = useSearch({ from: '__root__' })
  const { data } = useQuery(snapshotIndexQuery)
  const snapshots = data?.snapshots ?? []

  if (snapshots.length === 0) return null

  return (
    <select
      value={selected ?? snapshots[0].id}
      onChange={(event) => {
        const next = event.target.value
        void navigate({
          to: '.',
          search: (prev) => ({
            ...prev,
            // The newest scan is the default view: no param needed for it.
            snapshot: next === snapshots[0].id ? undefined : next,
          }),
        })
      }}
      className="rounded-md border bg-card px-2 py-1 text-xs text-muted-foreground"
      aria-label="Snapshot"
    >
      {snapshots.map((entry, position) => (
        <option key={entry.id} value={entry.id}>
          {new Date(entry.startedAt).toLocaleString()} · {formatBytes(entry.totalBytes)}
          {position === 0 ? ' (latest)' : ''}
        </option>
      ))}
    </select>
  )
}
