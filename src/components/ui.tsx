import type { ReactNode } from 'react'
import { cn } from '~/lib/utils'

export function Card({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cn('rounded-lg border bg-card p-5', className)}>{children}</div>
  )
}

export function SectionTitle({
  title,
  hint,
  action,
}: {
  title: string
  hint?: string
  action?: ReactNode
}) {
  return (
    <div className="mb-3 flex items-baseline justify-between gap-4">
      <div>
        <h2 className="text-sm font-semibold">{title}</h2>
        {hint ? <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p> : null}
      </div>
      {action}
    </div>
  )
}

export function StatCard({
  label,
  value,
  detail,
  emphasis,
}: {
  label: string
  value: string
  detail?: string
  emphasis?: boolean
}) {
  return (
    <Card className={cn(emphasis && 'border-foreground/25')}>
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="tabular mt-1.5 text-2xl font-semibold">{value}</p>
      {detail ? (
        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{detail}</p>
      ) : null}
    </Card>
  )
}

export function Banner({
  tone = 'warning',
  title,
  children,
}: {
  tone?: 'warning' | 'info'
  title: string
  children: ReactNode
}) {
  return (
    <div
      className={cn(
        'rounded-lg border p-4 text-sm',
        tone === 'warning'
          ? 'border-destructive/40 bg-destructive/5'
          : 'border-border bg-card',
      )}
    >
      <p className="font-medium">{title}</p>
      <div className="mt-1.5 space-y-1 text-muted-foreground">{children}</div>
    </div>
  )
}
