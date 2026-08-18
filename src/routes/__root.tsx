import { useEffect, type ReactNode } from 'react'
import {
  Outlet,
  HeadContent,
  Scripts,
  Link,
  createRootRouteWithContext,
} from '@tanstack/react-router'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { SnapshotPicker } from '~/components/snapshot-picker'
import { ScanButton } from '~/components/scan-button'
import { SelectionProvider } from '~/lib/selection'
import { SelectionBar } from '~/components/selection-bar'
import type { SizeMode } from '~/lib/size-mode'
import appCss from '../styles.css?url'

interface RouterContext {
  queryClient: QueryClient
}

export const Route = createRootRouteWithContext<RouterContext>()({
  // Declared at the root so every page shares one snapshot selection and any
  // view can be bookmarked against a specific scan.
  validateSearch: (search: Record<string, unknown>) => ({
    snapshot: typeof search.snapshot === 'string' ? search.snapshot : undefined,
    size:
      search.size === 'allocated' || search.size === 'unique'
        ? (search.size as SizeMode)
        : undefined,
  }),
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Disk Report' },
      { name: 'theme-color', content: '#0a0a0b' },
      { name: 'apple-mobile-web-app-capable', content: 'yes' },
    ],
    links: [
      { rel: 'stylesheet', href: appCss },
      { rel: 'manifest', href: '/manifest.webmanifest' },
      { rel: 'apple-touch-icon', href: '/icons/icon-192.png' },
    ],
  }),
  component: RootComponent,
})

const NAV = [
  { to: '/', label: 'Overview' },
  { to: '/folders', label: 'Folders' },
  { to: '/files', label: 'Files' },
  { to: '/extensions', label: 'Extensions' },
  { to: '/stale', label: 'Stale' },
  { to: '/trends', label: 'Trends' },
  { to: '/basket', label: 'Basket' },
] as const

function RootComponent() {
  const { queryClient } = Route.useRouteContext()
  useServiceWorker()

  return (
    <RootDocument>
      <QueryClientProvider client={queryClient}>
        <SelectionProvider>
        <div className="min-h-screen">
          <header className="sticky top-0 z-10 border-b bg-background/85 backdrop-blur">
            <nav className="mx-auto flex max-w-[1600px] items-center gap-1 px-6 py-3">
              <span className="mr-4 text-sm font-semibold tracking-tight">
                Disk Report
              </span>
              {NAV.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  search={(prev) => ({ snapshot: prev.snapshot, size: prev.size })}
                  activeOptions={{ exact: item.to === '/' }}
                  className="rounded-md px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  activeProps={{ className: 'bg-accent text-foreground' }}
                >
                  {item.label}
                </Link>
              ))}
              <div className="ml-auto flex items-center gap-2">
                <ScanButton />
                <SnapshotPicker />
              </div>
            </nav>
          </header>
          <main className="mx-auto max-w-[1600px] px-6 py-8 pb-28">
            <Outlet />
          </main>
          <SelectionBar />
        </div>
        </SelectionProvider>
      </QueryClientProvider>
    </RootDocument>
  )
}

/**
 * Register the service worker so the app opens without its server.
 *
 * Dev is excluded: Vite serves modules the worker would happily cache, and a
 * stale module is a confusing way to spend an afternoon.
 */
function useServiceWorker() {
  useEffect(() => {
    if (!('serviceWorker' in navigator) || import.meta.env.DEV) return
    navigator.serviceWorker.register('/sw.js').catch(() => undefined)
  }, [])
}

function RootDocument({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en" className="dark">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}
