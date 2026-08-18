import { useEffect, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

/**
 * A blocking confirmation for the one action that touches your disk.
 *
 * Rendered through a portal for the same reason the row menus are: virtualized
 * rows carry a transform, and a transform creates a stacking context that
 * anything rendered inside it cannot escape.
 */
export function ConfirmDialog({
  title,
  confirmLabel,
  onConfirm,
  onCancel,
  busy,
  children,
}: {
  title: string
  confirmLabel: string
  onConfirm: () => void
  onCancel: () => void
  busy?: boolean
  children: ReactNode
}) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busy) onCancel()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onCancel, busy])

  if (typeof document === 'undefined') return null

  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="menu-in w-full max-w-lg rounded-xl border bg-popover p-4 shadow-[0_24px_64px_rgba(0,0,0,0.6)]"
      >
        <h2 className="text-sm font-semibold">{title}</h2>
        <div className="mt-2 text-xs leading-relaxed text-muted-foreground">{children}</div>

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            className="rounded-md border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy}
            className="rounded-md border border-foreground/25 px-3 py-1.5 text-xs font-medium transition-colors hover:bg-accent disabled:opacity-50"
          >
            {busy ? 'Working…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
