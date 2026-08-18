/**
 * Offline support for the report.
 *
 * The app is worth opening without its server: the snapshots are immutable
 * files, and "what is on my disk" is a question the last scan already answered.
 * What is not cached is anything that acts — a stale scan or trash request has
 * no meaning once the server is gone.
 *
 * Three rules:
 *   /api/*             never cached, never intercepted
 *   /data/scan-*.json  immutable, cache-first, capped
 *   everything else    cache-first for assets, network-first for the shell
 */

const VERSION = 'v1'
const SHELL = `disk-report-shell-${VERSION}`
const SNAPSHOTS = `disk-report-snapshots-${VERSION}`

/** Snapshots run to several megabytes each; three is a useful history. */
const MAX_SNAPSHOTS = 3

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(SHELL)
      .then((cache) => cache.addAll(['/', '/manifest.webmanifest', '/icons/icon-192.png']))
      // A failed precache must not leave a worker that never activates.
      .catch(() => undefined)
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) =>
        Promise.all(
          names
            .filter((name) => name.startsWith('disk-report-') && !name.endsWith(VERSION))
            .map((name) => caches.delete(name)),
        ),
      )
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'GET') return

  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return

  // Anything that starts a scan or moves a file is between the page and the
  // server, and has no offline meaning.
  if (url.pathname.startsWith('/api/')) return

  if (url.pathname.startsWith('/data/')) {
    event.respondWith(handleData(request, url))
    return
  }

  event.respondWith(handleApp(request))
})

/**
 * A snapshot never changes once written, so a hit is always correct. The index
 * does change on every scan, so it is fetched first and only falls back.
 */
async function handleData(request, url) {
  const isSnapshot = /\/data\/scan-.*\.json$/.test(url.pathname)

  if (isSnapshot) {
    const cached = await caches.match(request)
    if (cached) return cached

    const response = await fetch(request)
    if (response.ok) {
      const cache = await caches.open(SNAPSHOTS)
      await cache.put(request, response.clone())
      await trimSnapshots(cache)
    }
    return response
  }

  try {
    const response = await fetch(request)
    if (response.ok) {
      const cache = await caches.open(SNAPSHOTS)
      await cache.put(request, response.clone())
    }
    return response
  } catch (error) {
    const cached = await caches.match(request)
    if (cached) return cached
    throw error
  }
}

/** Keep the newest few. Snapshot ids sort chronologically by construction. */
async function trimSnapshots(cache) {
  const keys = (await cache.keys()).filter((request) => /\/data\/scan-/.test(request.url))
  if (keys.length <= MAX_SNAPSHOTS) return

  keys.sort((a, b) => a.url.localeCompare(b.url))
  for (const request of keys.slice(0, keys.length - MAX_SNAPSHOTS)) {
    await cache.delete(request)
  }
}

/**
 * Hashed assets are safe to serve from cache forever. The shell is fetched
 * first so a rebuild is picked up, and falls back so the app still opens with
 * the server down.
 */
async function handleApp(request) {
  const url = new URL(request.url)

  if (url.pathname.startsWith('/assets/') || url.pathname.startsWith('/icons/')) {
    const cached = await caches.match(request)
    if (cached) return cached
  }

  try {
    const response = await fetch(request)
    if (response.ok) {
      const cache = await caches.open(SHELL)
      await cache.put(request, response.clone())
    }
    return response
  } catch (error) {
    const cached = (await caches.match(request)) ?? (await caches.match('/'))
    if (cached) return cached
    throw error
  }
}
