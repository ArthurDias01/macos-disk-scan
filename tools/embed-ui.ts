/**
 * Copy the built SPA where the Go binary can embed it.
 *
 * Two things this does that a plain `cp -R` does not:
 *
 * - Skips `data/`. Vite copies `public/` wholesale, and `public/data` holds the
 *   snapshots — several megabytes each, personal, and served by the binary from
 *   the real directory anyway. Embedding them would bake one machine's scan
 *   into the binary.
 * - Keeps `.gitkeep`, so the package still compiles from a clean checkout
 *   before any UI build has happened.
 */

import { cpSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

const SOURCE = resolve(import.meta.dir, '../dist/client')
const TARGET = resolve(import.meta.dir, '../internal/webui/assets')

rmSync(TARGET, { recursive: true, force: true })
mkdirSync(TARGET, { recursive: true })

cpSync(SOURCE, TARGET, {
  recursive: true,
  filter: (source) => !source.startsWith(resolve(SOURCE, 'data')),
})

writeFileSync(
  resolve(TARGET, '.gitkeep'),
  'The built SPA is copied here by `bun run build` so it can be embedded.\n',
)

console.log(`embedded ${SOURCE} -> ${TARGET}`)
