/**
 * Generate the PWA icons.
 *
 * PNGs are written byte by byte rather than pulled in with an image library:
 * the icon is a flat background and a few bars, the encoder below is shorter
 * than the dependency's own README, and this repo already generates its Go
 * types and config the same way — from a script, checked by regenerating.
 *
 *   bun run gen:icons
 */

import { writeFileSync, mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
// node:zlib, not Bun.deflateSync: PNG's IDAT is a zlib stream, header and
// Adler-32 included, and Bun's helper emits raw deflate. The difference is
// invisible to `file`, which only reads IHDR, and fatal to every real decoder.
import { deflateSync } from 'node:zlib'

const OUT = resolve(import.meta.dir, '../public/icons')

/** Matches --background and --foreground in src/styles.css. */
const BACKGROUND = [10, 10, 11] as const
const BAR = [244, 244, 245] as const
const DIM = [113, 113, 122] as const

type RGB = readonly [number, number, number]

function crc32(bytes: Uint8Array): number {
  let crc = 0xffffffff
  for (const byte of bytes) {
    crc ^= byte
    for (let bit = 0; bit < 8; bit++) {
      crc = crc & 1 ? (crc >>> 1) ^ 0xedb88320 : crc >>> 1
    }
  }
  return (crc ^ 0xffffffff) >>> 0
}

function chunk(type: string, data: Uint8Array): Uint8Array {
  const body = new Uint8Array(type.length + data.length)
  body.set([...type].map((c) => c.charCodeAt(0)), 0)
  body.set(data, type.length)

  const out = new Uint8Array(body.length + 8)
  const view = new DataView(out.buffer)
  view.setUint32(0, data.length)
  out.set(body, 4)
  view.setUint32(out.length - 4, crc32(body))
  return out
}

/** Encode RGBA pixels as a PNG. Filter type 0 on every scanline. */
function encodePNG(width: number, height: number, pixels: Uint8Array): Uint8Array {
  const raw = new Uint8Array(height * (width * 4 + 1))
  for (let y = 0; y < height; y++) {
    raw[y * (width * 4 + 1)] = 0
    raw.set(pixels.subarray(y * width * 4, (y + 1) * width * 4), y * (width * 4 + 1) + 1)
  }

  const header = new Uint8Array(13)
  const view = new DataView(header.buffer)
  view.setUint32(0, width)
  view.setUint32(4, height)
  header[8] = 8 // bit depth
  header[9] = 6 // RGBA

  const parts = [
    new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', header),
    chunk('IDAT', new Uint8Array(deflateSync(raw))),
    chunk('IEND', new Uint8Array()),
  ]

  const total = parts.reduce((sum, part) => sum + part.length, 0)
  const png = new Uint8Array(total)
  let offset = 0
  for (const part of parts) {
    png.set(part, offset)
    offset += part.length
  }
  return png
}

/**
 * The mark: four bars of falling height, the shape every chart in the app
 * already uses for "biggest first".
 */
function draw(size: number): Uint8Array {
  const pixels = new Uint8Array(size * size * 4)

  const set = (x: number, y: number, [r, g, b]: RGB) => {
    const at = (y * size + x) * 4
    pixels[at] = r
    pixels[at + 1] = g
    pixels[at + 2] = b
    pixels[at + 3] = 255
  }

  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) set(x, y, BACKGROUND)
  }

  const unit = size / 16
  const bars = [
    { height: 10, colour: BAR },
    { height: 7, colour: BAR },
    { height: 5, colour: DIM },
    { height: 3, colour: DIM },
  ]

  bars.forEach((bar, index) => {
    const left = Math.round(unit * (2.5 + index * 3))
    const right = Math.round(left + unit * 2)
    const bottom = Math.round(size - unit * 3)
    const top = Math.round(bottom - unit * bar.height)

    for (let y = top; y < bottom; y++) {
      for (let x = left; x < right && x < size; x++) {
        if (y >= 0 && y < size && x >= 0) set(x, y, bar.colour)
      }
    }
  })

  return pixels
}

mkdirSync(OUT, { recursive: true })
for (const size of [192, 512]) {
  const path = resolve(OUT, `icon-${size}.png`)
  writeFileSync(path, encodePNG(size, size, draw(size)))
  console.log(`generated ${path}`)
}
