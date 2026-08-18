/**
 * Stand-in for routes not yet built. The app is built in phases (see
 * docs/DECISIONS.md); every route exists from day one so navigation is never
 * broken while a phase is still pending.
 */
export function PhasePlaceholder({
  title,
  phase,
  description,
}: {
  title: string
  phase: string
  description: string
}) {
  return (
    <div className="rounded-lg border border-dashed p-10 text-center">
      <p className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
        {phase}
      </p>
      <h1 className="mt-2 text-xl font-semibold">{title}</h1>
      <p className="mx-auto mt-2 max-w-lg text-sm text-muted-foreground">
        {description}
      </p>
    </div>
  )
}
