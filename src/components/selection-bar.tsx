import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { Check, Copy, FolderOpen, Loader2, Trash2, X } from 'lucide-react'
import { formatBytes, formatCount } from '~/lib/format'
import { useSelection } from '~/lib/selection'
import { snapshotIndexQuery } from '~/lib/snapshots'
import {
  COMMAND_HINT,
  COMMAND_LABEL,
  buildCommand,
  collidingBasenames,
  type CommandKind,
} from '~/lib/commands'
import { fetchState, revealPaths, trashPaths, type ActionResult } from '~/lib/api'
import { ConfirmDialog } from '~/components/confirm-dialog'
import { cn } from '~/lib/utils'

/** Copy-only commands. Permanent deletion stays something you run yourself. */
const COPY_KINDS: CommandKind[] = ['paths', 'remove']

/**
 * Appears once anything is selected and follows you across pages.
 *
 * Trash and Reveal act directly when the local server is running; without it
 * they fall back to producing the same pasteable command they always did, so
 * the app opened from its cache is still useful.
 *
 * Permanent deletion is never a button here. The Trash is recoverable and a
 * failed move is visible; `rm -rf` against a ~/Library path is neither, and
 * review before execution is the only thing standing between a misclick and a
 * broken app.
 */
export function SelectionBar() {
  const { items, totalBytes, freeableBytes, clear, remove } = useSelection()
  const [copied, setCopied] = useState<CommandKind | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [results, setResults] = useState<ActionResult[] | null>(null)
  const [serverUp, setServerUp] = useState(false)

  const queryClient = useQueryClient()

  useEffect(() => {
    let cancelled = false
    fetchState()
      .then(() => !cancelled && setServerUp(true))
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  if (items.length === 0) return null

  const paths = items.map((item) => item.path)
  const collisions = collidingBasenames(paths)
  const shared = items.filter((item) => item.sharesBlocks).length

  const copy = async (kind: CommandKind) => {
    await navigator.clipboard.writeText(buildCommand(kind, paths))
    setCopied(kind)
    window.setTimeout(() => setCopied(null), 1600)
  }

  const runTrash = async () => {
    setBusy(true)
    try {
      const response = await trashPaths(paths)
      setResults(response.results)
      // Anything that moved is gone from the snapshot's point of view, so it
      // should not sit in the basket claiming to be selectable.
      for (const result of response.results) {
        if (result.ok) remove(result.path)
      }
      void queryClient.invalidateQueries(snapshotIndexQuery)
    } catch (cause) {
      setResults([
        { path: '', ok: false, error: cause instanceof Error ? cause.message : String(cause) },
      ])
    } finally {
      setBusy(false)
      setConfirming(false)
    }
  }

  const runReveal = async () => {
    try {
      await revealPaths(paths)
    } catch {
      await copy('reveal')
    }
  }

  const failures = results?.filter((result) => !result.ok) ?? []
  const succeeded = results?.filter((result) => result.ok).length ?? 0

  return (
    <>
      <div className="pointer-events-none fixed inset-x-0 bottom-0 z-40 flex justify-center p-4">
        <div className="menu-in pointer-events-auto w-full max-w-5xl rounded-xl border bg-popover/95 p-3 shadow-[0_16px_48px_rgba(0,0,0,0.55)] backdrop-blur">
          <div className="flex flex-wrap items-center gap-3">
            <div className="min-w-40">
              <p className="text-sm font-medium">
                {formatCount(items.length)} selected ·{' '}
                <span className="tabular">{formatBytes(totalBytes)}</span>
              </p>
              <p className="text-xs text-muted-foreground">
                {freeableBytes === totalBytes
                  ? 'All of it would be freed'
                  : `${formatBytes(freeableBytes)} would actually be freed`}
              </p>
            </div>

            <div className="ml-auto flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                onClick={() => (serverUp ? setConfirming(true) : void copy('trash'))}
                title={
                  serverUp
                    ? 'Move to Trash — recoverable from Finder'
                    : 'Server not running — copies the command instead'
                }
                className="flex items-center gap-1.5 whitespace-nowrap rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground transition-[transform,color,background-color] duration-150 ease-out hover:bg-accent hover:text-foreground active:scale-[0.97]"
              >
                {serverUp ? (
                  <Trash2 className="size-3.5" strokeWidth={2} />
                ) : copied === 'trash' ? (
                  <Check className="size-3.5" strokeWidth={2.5} />
                ) : (
                  <Copy className="size-3.5" strokeWidth={2} />
                )}
                Move to Trash
              </button>

              <button
                type="button"
                onClick={() => (serverUp ? void runReveal() : void copy('reveal'))}
                title="Reveal in Finder — opens each enclosing folder"
                className="flex items-center gap-1.5 whitespace-nowrap rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground transition-[transform,color,background-color] duration-150 ease-out hover:bg-accent hover:text-foreground active:scale-[0.97]"
              >
                {serverUp ? (
                  <FolderOpen className="size-3.5" strokeWidth={2} />
                ) : copied === 'reveal' ? (
                  <Check className="size-3.5" strokeWidth={2.5} />
                ) : (
                  <Copy className="size-3.5" strokeWidth={2} />
                )}
                Reveal in Finder
              </button>

              {COPY_KINDS.map((kind) => (
                <button
                  key={kind}
                  type="button"
                  onClick={() => void copy(kind)}
                  title={`${COMMAND_LABEL[kind]} — ${COMMAND_HINT[kind]}`}
                  className={cn(
                    'flex items-center gap-1.5 whitespace-nowrap rounded-md border px-2.5 py-1.5 text-xs transition-[transform,color,background-color] duration-150 ease-out active:scale-[0.97]',
                    kind === 'remove'
                      ? 'border-destructive/40 text-destructive hover:bg-destructive/10'
                      : 'text-muted-foreground hover:bg-accent hover:text-foreground',
                  )}
                >
                  {copied === kind ? (
                    <Check className="size-3.5" strokeWidth={2.5} />
                  ) : (
                    <Copy className="size-3.5" strokeWidth={2} />
                  )}
                  {COMMAND_LABEL[kind]}
                </button>
              ))}

              <Link
                to="/basket"
                search={(prev) => ({ snapshot: prev.snapshot, size: prev.size })}
                className="rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                Review
              </Link>

              <button
                type="button"
                onClick={clear}
                aria-label="Clear selection"
                className="rounded-md border p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <X className="size-3.5" strokeWidth={2} />
              </button>
            </div>
          </div>

          {collisions.length > 0 || shared > 0 || results ? (
            <div className="mt-2 space-y-1 border-t pt-2 text-xs text-muted-foreground">
              {!serverUp && collisions.length > 0 ? (
                <p>
                  {collisions.length} name{collisions.length === 1 ? '' : 's'} appear
                  twice ({collisions.slice(0, 3).join(', ')}
                  {collisions.length > 3 ? '…' : ''}) — <code>mv -n</code> skips the
                  second silently.
                </p>
              ) : null}
              {shared > 0 ? (
                <p>
                  {shared} selected item{shared === 1 ? '' : 's'} share blocks with
                  another file and free nothing alone.
                </p>
              ) : null}
              {results ? (
                <p className={failures.length > 0 ? 'text-destructive' : undefined}>
                  {succeeded > 0 ? `${formatCount(succeeded)} moved to the Trash. ` : ''}
                  {failures.length > 0
                    ? `${failures.length} failed: ${failures[0].error ?? 'unknown error'}`
                    : 'They keep using disk until the Trash is emptied.'}
                </p>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>

      {confirming ? (
        <ConfirmDialog
          title={`Move ${formatCount(items.length)} item${items.length === 1 ? '' : 's'} to the Trash?`}
          confirmLabel="Move to Trash"
          busy={busy}
          onCancel={() => setConfirming(false)}
          onConfirm={() => void runTrash()}
        >
          <p>
            <span className="tabular">{formatBytes(totalBytes)}</span> selected,{' '}
            <span className="tabular">{formatBytes(freeableBytes)}</span> of which
            would actually be freed. Items stay in the Trash and keep using disk
            until it is emptied, and Finder can put them back.
          </p>
          {shared > 0 ? (
            <p className="mt-2">
              {shared} of them share blocks with another file, so deleting them
              alone frees nothing.
            </p>
          ) : null}
          <ul className="mt-2 max-h-40 space-y-0.5 overflow-y-auto font-mono text-[11px]">
            {items.slice(0, 40).map((item) => (
              <li key={item.path} className="truncate" title={item.path}>
                {item.path}
              </li>
            ))}
          </ul>
          {items.length > 40 ? <p className="mt-1">and {items.length - 40} more.</p> : null}
        </ConfirmDialog>
      ) : null}
    </>
  )
}

/** The checkbox used in both tables. */
export function SelectCheckbox({
  checked,
  onChange,
  label,
}: {
  checked: boolean
  onChange: () => void
  label: string
}) {
  return (
    <input
      type="checkbox"
      checked={checked}
      aria-label={label}
      onClick={(event) => event.stopPropagation()}
      onChange={onChange}
      className="size-3.5 shrink-0 cursor-pointer accent-foreground"
    />
  )
}
