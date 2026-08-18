import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

export interface SelectedItem {
  path: string
  /** Bytes as reported by the scan, for the running total. */
  size: number
  kind: 'file' | 'folder'
  /** True when deleting it frees nothing: a clone copy or hardlink twin. */
  sharesBlocks?: boolean
}

interface SelectionValue {
  items: SelectedItem[]
  paths: Set<string>
  totalBytes: number
  /** Bytes that would actually be freed, ignoring block-sharing items. */
  freeableBytes: number
  toggle: (item: SelectedItem) => void
  remove: (path: string) => void
  clear: () => void
  isSelected: (path: string) => boolean
}

const SelectionContext = createContext<SelectionValue | null>(null)

const STORAGE_KEY = 'disk-report.selection.v1'

function load(): SelectedItem[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as SelectedItem[]) : []
  } catch {
    return []
  }
}

/**
 * The selection is a working set that outlives navigation, so it lives in
 * localStorage rather than the URL: a list of absolute paths would make an
 * unusable link, and the point is to gather items across several pages before
 * acting on them.
 */
export function SelectionProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<SelectedItem[]>(load)

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(items))
    } catch {
      // A full or disabled localStorage must not break the app; the selection
      // simply stops surviving reloads.
    }
  }, [items])

  const toggle = useCallback((item: SelectedItem) => {
    setItems((previous) =>
      previous.some((entry) => entry.path === item.path)
        ? previous.filter((entry) => entry.path !== item.path)
        : [...previous, item],
    )
  }, [])

  const remove = useCallback((path: string) => {
    setItems((previous) => previous.filter((entry) => entry.path !== path))
  }, [])

  const clear = useCallback(() => setItems([]), [])

  const value = useMemo<SelectionValue>(() => {
    const paths = new Set(items.map((item) => item.path))
    let totalBytes = 0
    let freeableBytes = 0
    for (const item of items) {
      totalBytes += item.size
      if (!item.sharesBlocks) freeableBytes += item.size
    }
    return {
      items,
      paths,
      totalBytes,
      freeableBytes,
      toggle,
      remove,
      clear,
      isSelected: (path: string) => paths.has(path),
    }
  }, [items, toggle, remove, clear])

  return <SelectionContext value={value}>{children}</SelectionContext>
}

export function useSelection(): SelectionValue {
  const value = useContext(SelectionContext)
  if (!value) throw new Error('useSelection must be used inside SelectionProvider')
  return value
}
