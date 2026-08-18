/**
 * The local server, from the page's side.
 *
 * The app is still a static SPA that reads snapshot JSON — this covers only the
 * two things a static file cannot do: start a scan, and act on a file. When the
 * server is not running (the app was opened from its cache, or served by Vite
 * with nothing behind it) every call here fails and the UI disables the
 * controls rather than pretending they work.
 */

import type { ScanConfig } from '@shared/schema'

const TOKEN_HEADER = 'X-Disk-Report-Token'

/**
 * The token that proves a request came from the page this server served.
 *
 * It travels in a custom header, and a custom header forces a CORS preflight
 * the server refuses — so no other origin can reach the mutating endpoints even
 * though they sit on localhost. Fetching it rather than reading it from the
 * HTML matters because the service worker caches that HTML: a baked-in token
 * would be a dead one after the next restart, and every action would fail with
 * a 403 that looked like a permissions problem.
 *
 * Fetched once and reused. A restart invalidates it, which surfaces as a 403 —
 * see `request` below.
 */
let pending: Promise<string> | null = null

function token(): Promise<string> {
  pending ??= fetch('/api/token')
    .then((response) => {
      if (!response.ok) throw new Error('no token')
      return response.json() as Promise<{ token: string }>
    })
    .then((body) => body.token)
    .catch((cause) => {
      // Do not cache the failure: the server may simply not be up yet.
      pending = null
      throw cause
    })
  return pending
}

export interface JobState {
  running: boolean
  phase?: 'walk' | 'duplicates'
  dirsDone: number
  dirsQueued: number
  files: number
  bytes: number
  currentPath?: string
  cacheHits: number
  fingerprintsDone?: number
  fingerprintsTotal?: number
  done: boolean
  snapshotId?: string
  snapshotFile?: string
  durationMs?: number
  error?: string
}

export interface ServerState {
  job: JobState
  roots: string[]
  minFileSize: number
  minFolderSize: number
  version: string
}

export interface ActionResult {
  path: string
  ok: boolean
  error?: string
  method?: string
}

async function send<T>(path: string, body: unknown, authorization: string): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers:
      body === undefined
        ? undefined
        : { 'Content-Type': 'application/json', [TOKEN_HEADER]: authorization },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (!response.ok) {
    const detail = (await response.text()).trim()
    const error = new Error(detail || `${response.status} ${response.statusText}`)
    ;(error as Error & { status?: number }).status = response.status
    throw error
  }
  return (await response.json()) as T
}

async function request<T>(path: string, body?: unknown): Promise<T> {
  if (body === undefined) return send<T>(path, undefined, '')

  try {
    return await send<T>(path, body, await token())
  } catch (cause) {
    // A 403 after a server restart means the token we held is from the previous
    // process. Drop it and try once with a fresh one, rather than making the
    // user reload to fix something they cannot see.
    if ((cause as { status?: number }).status !== 403) throw cause
    pending = null
    return send<T>(path, body, await token())
  }
}

/** Whether the server is reachable, and what it is currently doing. */
export function fetchState(): Promise<ServerState> {
  return request<ServerState>('/api/state')
}

export interface ScanRequest {
  roots?: string[]
  minFileSize?: number
  minFolderSize?: number
  full?: boolean
}

/** Rejects with a 409 message when a scan is already running. */
export function startScan(options: ScanRequest): Promise<JobState> {
  return request<JobState>('/api/scan', options)
}

export function trashPaths(paths: string[]): Promise<{ results: ActionResult[]; error?: string }> {
  return request('/api/actions/trash', { paths })
}

export function revealPaths(paths: string[]): Promise<{ results: ActionResult[]; error?: string }> {
  return request('/api/actions/reveal', { paths })
}

/**
 * Subscribe to scan progress.
 *
 * EventSource rather than polling: the walk already reports every 250ms, and
 * the browser reconnects on its own if the server restarts mid-scan.
 */
export function watchScan(onState: (state: JobState) => void): () => void {
  const source = new EventSource('/api/scan/events')
  source.onmessage = (event) => {
    try {
      onState(JSON.parse(event.data) as JobState)
    } catch {
      // A malformed frame is not worth tearing the stream down for.
    }
  }
  return () => source.close()
}

/** The subset of the config the scan form edits. */
export type ScanForm = Pick<ScanConfig, 'roots' | 'minFileSize' | 'minFolderSize'>
