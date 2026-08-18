import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Check, Copy, Download, X } from 'lucide-react'
import { formatBytes, formatCount } from '~/lib/format'
import { useSelection } from '~/lib/selection'
import {
  COMMAND_HINT,
  COMMAND_LABEL,
  buildCommand,
  buildScript,
  collidingBasenames,
  type CommandKind,
} from '~/lib/commands'
import { Banner, Card, SectionTitle } from '~/components/ui'
import { cn } from '~/lib/utils'

const KINDS: CommandKind[] = ['trash', 'reveal', 'paths', 'remove']

export const Route = createFileRoute('/basket')({
  component: BasketRoute,
})

function BasketRoute() {
  const { items, totalBytes, freeableBytes, remove, clear } = useSelection()
  const [kind, setKind] = useState<CommandKind>('trash')
  const [copied, setCopied] = useState(false)

  if (items.length === 0) {
    return (
      <div className="rounded-lg border border-dashed p-10 text-center">
        <h1 className="text-xl font-semibold">Nothing selected</h1>
        <p className="mx-auto mt-2 max-w-md text-sm text-muted-foreground">
          Tick folders on the Folders page or files on the Files page. The selection
          follows you between pages and survives a reload.
        </p>
      </div>
    )
  }

  const paths = items.map((item) => item.path)
  const command = buildCommand(kind, paths)
  const script = buildScript(items, formatBytes(totalBytes))
  const collisions = collidingBasenames(paths)
  const shared = items.filter((item) => item.sharesBlocks)

  const copy = async (value: string) => {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }

  const download = () => {
    const blob = new Blob([script], { type: 'text/x-shellscript' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `cleanup-${new Date().toISOString().slice(0, 10)}.sh`
    anchor.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-baseline justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Basket</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {formatCount(items.length)} item{items.length === 1 ? '' : 's'} ·{' '}
            {formatBytes(totalBytes)} allocated ·{' '}
            <strong className="text-foreground">{formatBytes(freeableBytes)}</strong>{' '}
            would actually be freed
          </p>
        </div>
        <button
          type="button"
          onClick={clear}
          className="rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          Clear all
        </button>
      </header>

      {collisions.length > 0 ? (
        <Banner title="Two items share a name">
          <p>
            <code>mv -n</code> will not move the second of{' '}
            {collisions.map((name) => (
              <code key={name} className="mr-1">
                {name}
              </code>
            ))}{' '}
            into the Trash, and says nothing about it. Move those one at a time, or
            rename first.
          </p>
        </Banner>
      ) : null}

      <Card>
        <SectionTitle
          title="One command for the whole selection"
          hint="Nothing runs here — this only copies text for your terminal."
        />

        <div className="flex flex-wrap gap-1.5">
          {KINDS.map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => setKind(option)}
              className={cn(
                'rounded-md border px-2.5 py-1.5 text-xs transition-[transform,color,background-color] duration-150 ease-out active:scale-[0.97]',
                option === kind
                  ? 'border-foreground/30 bg-accent text-foreground'
                  : 'text-muted-foreground hover:text-foreground',
                option === 'remove' && option !== kind ? 'text-destructive/80' : '',
              )}
            >
              {COMMAND_LABEL[option]}
            </button>
          ))}
        </div>
        <p className="mt-2 text-xs text-muted-foreground">{COMMAND_HINT[kind]}</p>

        <div className="mt-3 rounded-lg border bg-muted/30">
          <pre className="max-h-48 overflow-auto p-3 font-mono text-xs leading-relaxed text-foreground">
            {command}
          </pre>
          <div className="flex items-center gap-2 border-t p-2">
            <button
              type="button"
              onClick={() => void copy(command)}
              className="flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground transition-[transform,color,background-color] duration-150 ease-out active:scale-[0.97] hover:bg-accent hover:text-foreground"
            >
              {copied ? (
                <Check className="size-3.5" strokeWidth={2.5} />
              ) : (
                <Copy className="size-3.5" strokeWidth={2} />
              )}
              {copied ? 'Copied' : 'Copy command'}
            </button>
            <button
              type="button"
              onClick={download}
              className="flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              <Download className="size-3.5" strokeWidth={2} />
              Download script
            </button>
            <p className="ml-auto text-xs text-muted-foreground">
              The script runs one <code>mv</code> per line, so one failure does not
              stop the rest.
            </p>
          </div>
        </div>
      </Card>

      <Card className="p-0">
        <ul className="divide-y">
          {items.map((item) => (
            <li key={item.path} className="flex items-center gap-3 px-4 py-2.5">
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm">{item.path}</span>
                <span className="text-xs text-muted-foreground">
                  {item.kind === 'folder' ? 'Folder' : 'File'}
                  {item.sharesBlocks ? ' · shares blocks, frees nothing alone' : ''}
                </span>
              </span>
              <span className="tabular w-24 text-right text-sm">
                {formatBytes(item.size)}
              </span>
              <button
                type="button"
                onClick={() => remove(item.path)}
                aria-label={`Remove ${item.path}`}
                className="rounded-md border p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <X className="size-3.5" strokeWidth={2} />
              </button>
            </li>
          ))}
        </ul>
      </Card>

      {shared.length > 0 ? (
        <p className="text-xs text-muted-foreground">
          {shared.length} of these share blocks with another file. Removing one frees
          nothing; the space returns only when every copy is gone.
        </p>
      ) : null}
    </div>
  )
}
