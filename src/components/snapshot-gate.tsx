import type { ReactNode } from 'react'
import type { ScanSnapshot } from '@shared/schema'
import { useSnapshot } from '~/lib/use-snapshot'
import { Banner } from '~/components/ui'

/**
 * Resolves the active snapshot, or explains why there isn't one.
 *
 * Every page needs the same four states — no scans, load failure, loading,
 * loaded — and writing them per route produced three subtly different empty
 * states. The render-prop hands the page a snapshot that is already known to
 * exist, so pages carry no loading branches at all.
 */
export function SnapshotGate({
  children,
}: {
  children: (snapshot: ScanSnapshot) => ReactNode
}) {
  const { snapshot, isPending, error, isEmpty } = useSnapshot()

  if (isEmpty) return <NoScans />

  if (error) {
    return (
      <Banner title="Could not load the snapshot">
        <p>{error.message}</p>
        <p>
          The file may have been deleted from <code>public/data</code>. Running a
          scan writes a fresh one.
        </p>
      </Banner>
    )
  }

  if (isPending || !snapshot) {
    return <p className="text-sm text-muted-foreground">Loading snapshot…</p>
  }

  return <>{children(snapshot)}</>
}

function NoScans() {
  return (
    <div className="rounded-lg border border-dashed p-10 text-center">
      <h1 className="text-xl font-semibold">No scans yet</h1>
      <p className="mx-auto mt-2 max-w-md text-sm text-muted-foreground">
        Run a scan to produce the first snapshot. It walks your home directory and
        reports files at or above 100 MB and folders at or above 500 MB.
      </p>
      <pre className="mx-auto mt-4 w-fit rounded-md bg-muted px-4 py-2 text-sm">
        bun run scan
      </pre>
    </div>
  )
}
