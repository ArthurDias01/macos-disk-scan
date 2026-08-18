import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Loader2, Play, X } from 'lucide-react'
import { formatBytes, formatCount } from '~/lib/format'
import { snapshotIndexQuery } from '~/lib/snapshots'
import { fetchState, startScan, watchScan, type JobState, type ServerState } from '~/lib/api'
import { cn } from '~/lib/utils'

/**
 * Start a scan without leaving the app.
 *
 * A scan was always a deliberate act run from a terminal, and it stays
 * deliberate — this is a button and a short form, not something that fires on a
 * timer. What it removes is the trip to another window to get fresh numbers.
 */
export function ScanButton() {
  const [server, setServer] = useState<ServerState | null>(null)
  const [job, setJob] = useState<JobState | null>(null)
  const [open, setOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const lastSnapshot = useRef<string | undefined>(undefined)

  // One probe on mount decides whether any of this is available. Opened from
  // the cache with no server behind it, the control simply does not render.
  useEffect(() => {
    let cancelled = false
    fetchState()
      .then((state) => {
        if (cancelled) return
        setServer(state)
        setJob(state.job)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!server) return
    return watchScan((state) => {
      setJob(state)
      // A finished scan is a new snapshot the picker has not heard of.
      if (state.done && state.snapshotId && state.snapshotId !== lastSnapshot.current) {
        lastSnapshot.current = state.snapshotId
        void queryClient.invalidateQueries(snapshotIndexQuery)
        void navigate({ to: '.', search: (prev) => ({ ...prev, snapshot: undefined }) })
      }
    })
  }, [server, queryClient, navigate])

  if (!server) return null

  const running = job?.running ?? false

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        disabled={running}
        className={cn(
          'flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs transition-colors',
          running
            ? 'cursor-default text-muted-foreground'
            : 'text-muted-foreground hover:bg-accent hover:text-foreground',
        )}
      >
        {running ? (
          <Loader2 className="size-3.5 animate-spin" strokeWidth={2} />
        ) : (
          <Play className="size-3.5" strokeWidth={2} />
        )}
        {running ? 'Scanning' : 'Scan'}
      </button>

      {running && job ? <ScanProgress job={job} /> : null}

      {open && !running ? (
        <ScanForm
          server={server}
          error={error}
          onClose={() => setOpen(false)}
          onStart={async (request) => {
            setError(null)
            try {
              setJob(await startScan(request))
              setOpen(false)
            } catch (cause) {
              setError(cause instanceof Error ? cause.message : String(cause))
            }
          }}
        />
      ) : null}
    </div>
  )
}

/** The same numbers the CLI prints, in the corner of the header. */
function ScanProgress({ job }: { job: JobState }) {
  const path = job.currentPath ?? ''
  const trimmed = path.length > 48 ? `…${path.slice(-47)}` : path

  return (
    <div className="menu-in absolute right-0 top-9 z-50 w-96 rounded-lg border bg-popover/95 p-3 text-xs shadow-[0_16px_48px_rgba(0,0,0,0.55)] backdrop-blur">
      {job.phase === 'duplicates' ? (
        <p>
          Fingerprinting {formatCount(job.fingerprintsDone ?? 0)} of{' '}
          {formatCount(job.fingerprintsTotal ?? 0)} size-collision candidates
        </p>
      ) : (
        <>
          <p className="tabular">
            {formatCount(job.dirsDone)} dirs · {formatCount(job.dirsQueued)} queued ·{' '}
            {formatCount(job.files)} files · {formatBytes(job.bytes)}
          </p>
          <p className="mt-1 truncate text-muted-foreground" title={path}>
            {trimmed}
          </p>
        </>
      )}
      {job.cacheHits > 0 ? (
        <p className="mt-1 text-muted-foreground">
          {formatCount(job.cacheHits)} directories unchanged since the last scan
        </p>
      ) : null}
    </div>
  )
}

function ScanForm({
  server,
  error,
  onClose,
  onStart,
}: {
  server: ServerState
  error: string | null
  onClose: () => void
  onStart: (request: {
    roots?: string[]
    minFileSize?: number
    minFolderSize?: number
    full?: boolean
  }) => void
}) {
  const [roots, setRoots] = useState(server.roots.join('\n'))
  const [minFile, setMinFile] = useState(String(server.minFileSize))
  const [minFolder, setMinFolder] = useState(String(server.minFolderSize))
  const [full, setFull] = useState(false)

  // Changing a threshold changes the cache's config fingerprint, which discards
  // every stored row. On this machine that is the difference between a 15s scan
  // and a 47s one, and it is not obvious from the form.
  const thresholdsChanged =
    Number(minFile) !== server.minFileSize || Number(minFolder) !== server.minFolderSize

  return (
    <div className="menu-in absolute right-0 top-9 z-50 w-96 rounded-lg border bg-popover/95 p-3 shadow-[0_16px_48px_rgba(0,0,0,0.55)] backdrop-blur">
      <div className="mb-2 flex items-center justify-between">
        <p className="text-xs font-medium">Run a scan</p>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <X className="size-3" strokeWidth={2} />
        </button>
      </div>

      <label className="block text-xs text-muted-foreground">
        Roots, one per line
        <textarea
          value={roots}
          onChange={(event) => setRoots(event.target.value)}
          rows={2}
          spellCheck={false}
          className="mt-1 w-full rounded-md border bg-card px-2 py-1 font-mono text-xs text-foreground"
        />
      </label>

      <div className="mt-2 grid grid-cols-2 gap-2">
        <label className="block text-xs text-muted-foreground">
          Min file (bytes)
          <input
            value={minFile}
            onChange={(event) => setMinFile(event.target.value)}
            inputMode="numeric"
            className="tabular mt-1 w-full rounded-md border bg-card px-2 py-1 text-xs text-foreground"
          />
          <span className="mt-0.5 block">{formatBytes(Number(minFile) || 0)}</span>
        </label>
        <label className="block text-xs text-muted-foreground">
          Min folder (bytes)
          <input
            value={minFolder}
            onChange={(event) => setMinFolder(event.target.value)}
            inputMode="numeric"
            className="tabular mt-1 w-full rounded-md border bg-card px-2 py-1 text-xs text-foreground"
          />
          <span className="mt-0.5 block">{formatBytes(Number(minFolder) || 0)}</span>
        </label>
      </div>

      <label className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
        <input
          type="checkbox"
          checked={full}
          onChange={() => setFull((value) => !value)}
          className="size-3.5 accent-foreground"
        />
        Full rescan, ignoring the cache
      </label>

      {thresholdsChanged ? (
        <p className="mt-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-1.5 text-xs text-amber-200/90">
          Changing a threshold discards the incremental cache, so this scan walks
          everything. Snapshots taken at different thresholds are also not
          directly comparable in Trends.
        </p>
      ) : null}

      {error ? (
        <p className="mt-2 rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">
          {error}
        </p>
      ) : null}

      <button
        type="button"
        onClick={() =>
          onStart({
            roots: roots
              .split('\n')
              .map((line) => line.trim())
              .filter(Boolean),
            minFileSize: Number(minFile) || 0,
            minFolderSize: Number(minFolder) || 0,
            full,
          })
        }
        className="mt-3 w-full rounded-md border border-foreground/25 px-2 py-1.5 text-xs font-medium transition-colors hover:bg-accent"
      >
        Start scan
      </button>
    </div>
  )
}
