import { useQuery } from '@tanstack/react-query'
import { useSearch } from '@tanstack/react-router'
import type { ScanSnapshot, SnapshotIndex } from '@shared/schema'
import { snapshotIndexQuery, snapshotQuery } from './snapshots'

export interface SnapshotState {
  index: SnapshotIndex | undefined
  snapshot: ScanSnapshot | undefined
  /** Id actually loaded — the search param when valid, newest otherwise. */
  activeId: string | undefined
  isPending: boolean
  error: Error | null
  /** True when the index loaded but holds nothing: no scan has run yet. */
  isEmpty: boolean
}

/**
 * Resolve the snapshot to display.
 *
 * The `?snapshot=` search param selects one; without it the newest wins. The
 * param lives at the root route, so every page shares the selection and a
 * bookmarked URL keeps looking at the same scan.
 */
export function useSnapshot(): SnapshotState {
  const { snapshot: requestedId } = useSearch({ from: '__root__' })
  const indexResult = useQuery(snapshotIndexQuery)

  const entries = indexResult.data?.snapshots ?? []
  const entry = requestedId
    ? (entries.find((candidate) => candidate.id === requestedId) ?? entries[0])
    : entries[0]

  const snapshotResult = useQuery({
    ...snapshotQuery(entry?.file ?? ''),
    enabled: Boolean(entry),
  })

  return {
    index: indexResult.data,
    snapshot: snapshotResult.data,
    activeId: entry?.id,
    isPending: indexResult.isPending || (Boolean(entry) && snapshotResult.isPending),
    error: (indexResult.error ?? snapshotResult.error) as Error | null,
    isEmpty: indexResult.isSuccess && entries.length === 0,
  }
}
