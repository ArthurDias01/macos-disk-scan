import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown, Clipboard } from 'lucide-react'
import { shellQuote } from '~/lib/format'
import { cn } from '~/lib/utils'

/** Roughly the menu's height, used to decide whether it opens up or down. */
const MENU_HEIGHT = 230
const MENU_WIDTH = 288

/**
 * Commands offered for a path. The default moves the item to the Trash, which
 * is recoverable from Finder; `rm -rf` is available but never the default.
 *
 * Every path is shell-quoted: macOS filenames routinely contain spaces and
 * apostrophes ("Photos Library.photoslibrary"), and an unquoted path in a
 * delete command targets the wrong thing.
 */
function commandsFor(path: string) {
  const quoted = shellQuote(path)
  return [
    {
      id: 'trash',
      label: 'Move to Trash',
      hint: 'Recoverable from Finder',
      value: `mv -n ${quoted} ~/.Trash/`,
    },
    {
      id: 'reveal',
      label: 'Reveal in Finder',
      hint: 'Opens the enclosing folder',
      value: `open -R ${quoted}`,
    },
    {
      id: 'path',
      label: 'Copy path',
      hint: 'Unquoted, for pasting elsewhere',
      value: path,
    },
    {
      id: 'rm',
      label: 'Delete permanently',
      hint: 'No undo',
      value: `rm -rf ${quoted}`,
      danger: true,
    },
  ] as const
}

interface MenuPosition {
  top: number
  left: number
  origin: 'top' | 'bottom'
}

export function CopyCommand({
  path,
  variant = 'icon',
}: {
  path: string
  /** `icon` for table rows, where 20 repetitions of a word is noise. */
  variant?: 'icon' | 'labelled'
}) {
  const [position, setPosition] = useState<MenuPosition | null>(null)
  const [copied, setCopied] = useState(false)
  const trigger = useRef<HTMLDivElement>(null)
  const commands = commandsFor(path)
  const open = position !== null

  const place = () => {
    const rect = trigger.current?.getBoundingClientRect()
    if (!rect) return
    const openUp = rect.bottom + MENU_HEIGHT > window.innerHeight
    setPosition({
      top: openUp ? rect.top - MENU_HEIGHT - 4 : rect.bottom + 4,
      left: Math.max(8, Math.min(rect.right - MENU_WIDTH, window.innerWidth - MENU_WIDTH - 8)),
      origin: openUp ? 'bottom' : 'top',
    })
  }

  useEffect(() => {
    if (!open) return
    const close = () => setPosition(null)
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (!trigger.current?.contains(target) && !menuRef.current?.contains(target)) {
        close()
      }
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close()
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    // The menu is fixed-positioned, so it cannot follow a scrolling row.
    window.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('scroll', close, true)
      window.removeEventListener('resize', close)
    }
  }, [open])

  const menuRef = useRef<HTMLDivElement>(null)

  const copy = async (value: string) => {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    setPosition(null)
    window.setTimeout(() => setCopied(false), 1400)
  }

  return (
    <div ref={trigger} className="relative flex justify-end">
      <div
        className={cn(
          'flex h-7 items-stretch overflow-hidden rounded-md border transition-colors duration-150',
          copied ? 'border-foreground/30' : 'border-border hover:border-foreground/25',
        )}
      >
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            void copy(commands[0].value)
          }}
          title={commands[0].value}
          aria-label="Copy Trash command"
          className={cn(
            'flex items-center gap-1.5 whitespace-nowrap px-2 text-xs transition-[transform,color,background-color] duration-150 ease-out active:scale-[0.97]',
            copied
              ? 'text-foreground'
              : 'text-muted-foreground hover:bg-accent hover:text-foreground',
          )}
        >
          {copied ? (
            <Check className="size-3.5" strokeWidth={2.5} />
          ) : (
            <Clipboard className="size-3.5" strokeWidth={2} />
          )}
          {variant === 'labelled' ? (
            <span>{copied ? 'Copied' : 'Trash cmd'}</span>
          ) : null}
        </button>

        <button
          type="button"
          aria-label="More commands"
          aria-expanded={open}
          onClick={(event) => {
            event.stopPropagation()
            if (open) setPosition(null)
            else place()
          }}
          className={cn(
            'flex items-center border-l px-1 text-muted-foreground transition-[transform,color,background-color] duration-150 ease-out active:scale-[0.97] hover:bg-accent hover:text-foreground',
            open ? 'bg-accent text-foreground' : '',
          )}
        >
          <ChevronDown className="size-3" strokeWidth={2.5} />
        </button>
      </div>

      {/* Rendered into <body>: virtualized rows are transformed, and a transform
          creates a stacking context — inside one, no z-index can lift the menu
          above the rows that come after it. */}
      {position && typeof document !== 'undefined'
        ? createPortal(
            <div
              ref={menuRef}
              role="menu"
              style={{
                top: position.top,
                left: position.left,
                width: MENU_WIDTH,
                transformOrigin: position.origin === 'top' ? 'top right' : 'bottom right',
              }}
              // Entering from scale(0.96) rather than 0, scaled from the corner
              // nearest its trigger: nothing in the real world appears out of
              // nothing, and the menu should look like it grew from the button.
              className="menu-in fixed z-50 overflow-hidden rounded-lg border border-border bg-popover shadow-[0_16px_48px_rgba(0,0,0,0.55)]"
              onClick={(event) => event.stopPropagation()}
            >
              {commands.map((command) => (
                <button
                  key={command.id}
                  type="button"
                  role="menuitem"
                  onClick={(event) => {
                    event.stopPropagation()
                    void copy(command.value)
                  }}
                  className="block w-full border-b border-border/50 px-3 py-2 text-left transition-colors duration-100 last:border-b-0 hover:bg-accent"
                >
                  <span
                    className={cn(
                      'block text-xs font-medium',
                      'danger' in command && command.danger
                        ? 'text-destructive'
                        : 'text-foreground',
                    )}
                  >
                    {command.label}
                  </span>
                  <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">
                    {command.hint}
                  </span>
                </button>
              ))}
              <p className="bg-muted/40 px-3 py-2 text-[11px] leading-snug text-muted-foreground">
                Copies a command — nothing is deleted here. Trashed items keep using
                disk until the Trash is emptied.
              </p>
            </div>,
            document.body,
          )
        : null}
    </div>
  )
}
