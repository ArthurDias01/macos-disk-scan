import { useNavigate, useSearch } from '@tanstack/react-router'
import { cn } from '~/lib/utils'
import { SIZE_MODE_LABEL, type SizeMode } from '~/lib/size-mode'

const MODES: SizeMode[] = ['allocated', 'unique']

/**
 * Reads the mode from the URL, falling back to `fallback` — which the page sets
 * to `unique` when the scan proves blocks are shared, since `allocated` is
 * unreadable on a machine with a large clone group.
 */
export function useSizeMode(fallback: SizeMode): SizeMode {
  const { size } = useSearch({ from: '__root__' })
  return size ?? fallback
}

export function SizeModeToggle({ active }: { active: SizeMode }) {
  const navigate = useNavigate()

  return (
    <div className="flex items-center gap-1 rounded-md border p-0.5">
      {MODES.map((mode) => (
        <button
          key={mode}
          type="button"
          onClick={() =>
            void navigate({ to: '.', search: (prev) => ({ ...prev, size: mode }) })
          }
          className={cn(
            'rounded px-2 py-1 text-xs transition-colors',
            active === mode
              ? 'bg-accent text-foreground'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {SIZE_MODE_LABEL[mode]}
        </button>
      ))}
    </div>
  )
}
