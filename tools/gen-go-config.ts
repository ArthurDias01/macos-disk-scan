/**
 * Export the scanner's default config from `shared/config.ts` for the Go
 * scanner to embed.
 *
 * The category map alone is ~200 entries; transcribing it into Go by hand is
 * how the two implementations would start disagreeing about what a `.dmg` is.
 * Evaluating the module and dumping the result keeps one source of truth.
 *
 * `roots` is deliberately emptied: it defaults to `$HOME`, which belongs to the
 * machine running the scan, not to a file checked into the repo.
 */

import { writeFileSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { DEFAULT_CONFIG, VOLATILE_EXTENSIONS } from '../shared/config'

const TARGET = resolve(import.meta.dir, '../internal/config/defaults.json')

const payload = {
  _generated: 'tools/gen-go-config.ts from shared/config.ts — do not edit',
  config: { ...DEFAULT_CONFIG, roots: [] },
  volatileExtensions: VOLATILE_EXTENSIONS,
}

mkdirSync(dirname(TARGET), { recursive: true })
writeFileSync(TARGET, `${JSON.stringify(payload, null, 2)}\n`)

console.log(`generated ${TARGET}`)
