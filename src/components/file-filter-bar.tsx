import type { Category } from '@shared/schema'
import { CATEGORY_LABEL, categoryColor } from '~/lib/colors'
import { formatCount } from '~/lib/format'
import type { FileFilters } from '~/lib/file-filters'
import { cn } from '~/lib/utils'

const MB = 1024 * 1024
const GB = 1024 * MB

const SIZE_STEPS = [
  { value: 0, label: 'Any size' },
  { value: 100 * MB, label: '100 MB+' },
  { value: 500 * MB, label: '500 MB+' },
  { value: 1 * GB, label: '1 GB+' },
  { value: 5 * GB, label: '5 GB+' },
]

const AGE_STEPS = [
  { value: 0, label: 'Any age' },
  { value: 6, label: '6 months+' },
  { value: 12, label: '1 year+' },
  { value: 24, label: '2 years+' },
  { value: 60, label: '5 years+' },
]

export function FileFilterBar({
  filters,
  facets,
  onChange,
  onReset,
}: {
  filters: FileFilters
  facets: Array<{ category: Category; count: number }>
  onChange: (patch: Partial<FileFilters>) => void
  onReset: () => void
}) {
  const toggleCategory = (category: Category) => {
    const next = filters.cats.includes(category)
      ? filters.cats.filter((entry) => entry !== category)
      : [...filters.cats, category]
    onChange({ cats: next })
  }

  const active =
    filters.cats.length > 0 ||
    filters.q !== '' ||
    filters.min > 0 ||
    filters.olderMonths > 0 ||
    filters.dup ||
    filters.hideCloud

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <input
          type="search"
          value={filters.q}
          onChange={(event) => onChange({ q: event.target.value })}
          placeholder="Filter by path…"
          className="h-8 min-w-56 flex-1 rounded-md border bg-card px-3 text-sm outline-none placeholder:text-muted-foreground focus:ring-1 focus:ring-ring"
        />

        <Select
          value={filters.min}
          options={SIZE_STEPS}
          onChange={(value) => onChange({ min: value })}
        />
        <Select
          value={filters.olderMonths}
          options={AGE_STEPS}
          onChange={(value) => onChange({ olderMonths: value })}
        />

        <Toggle
          active={filters.dup}
          onClick={() => onChange({ dup: !filters.dup })}
          title="Only files that free nothing on their own: clone copies and hardlink twins."
        >
          Frees nothing
        </Toggle>
        <Toggle
          active={filters.hideCloud}
          onClick={() => onChange({ hideCloud: !filters.hideCloud })}
          title="Hide cloud-backed files, which may already occupy no local space."
        >
          Hide cloud
        </Toggle>

        <button
          type="button"
          onClick={onReset}
          disabled={!active}
          className={cn(
            'h-8 rounded-md px-2 text-xs',
            active
              ? 'text-muted-foreground hover:text-foreground'
              : 'cursor-default text-muted-foreground/40',
          )}
        >
          Reset
        </button>
      </div>

      <div className="flex flex-wrap gap-1.5">
        {facets.map((facet) => {
          const selected = filters.cats.includes(facet.category)
          return (
            <button
              key={facet.category}
              type="button"
              onClick={() => toggleCategory(facet.category)}
              className={cn(
                'flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors',
                selected
                  ? 'border-foreground/30 bg-accent text-foreground'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              <span
                className="size-2 rounded-full"
                style={{ backgroundColor: categoryColor(facet.category) }}
              />
              {CATEGORY_LABEL[facet.category]}
              <span className="tabular text-muted-foreground">
                {formatCount(facet.count)}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function Select({
  value,
  options,
  onChange,
}: {
  value: number
  options: Array<{ value: number; label: string }>
  onChange: (value: number) => void
}) {
  return (
    <select
      value={value}
      onChange={(event) => onChange(Number(event.target.value))}
      className="h-8 rounded-md border bg-card px-2 text-xs text-muted-foreground outline-none focus:ring-1 focus:ring-ring"
    >
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  )
}

function Toggle({
  active,
  onClick,
  title,
  children,
}: {
  active: boolean
  onClick: () => void
  title: string
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      className={cn(
        'h-8 rounded-md border px-2.5 text-xs transition-colors',
        active
          ? 'border-foreground/30 bg-accent text-foreground'
          : 'text-muted-foreground hover:text-foreground',
      )}
    >
      {children}
    </button>
  )
}
