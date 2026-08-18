import { queryOptions } from '@tanstack/react-query'
import type { ScanSnapshot, SnapshotIndex } from '@shared/schema'

/** Snapshots are static files written by `bun run scan` into public/data. */
const DATA_BASE = '/data'

async function fetchJson<T>(url: string): Promise<T> {
  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`${url} — ${response.status} ${response.statusText}`)
  }
  return (await response.json()) as T
}

export const snapshotIndexQuery = queryOptions({
  queryKey: ['snapshot-index'],
  queryFn: () => fetchJson<SnapshotIndex>(`${DATA_BASE}/index.json`),
})

export function snapshotQuery(file: string) {
  return queryOptions({
    queryKey: ['snapshot', file],
    queryFn: () => fetchJson<ScanSnapshot>(`${DATA_BASE}/${file}`),
  })
}
