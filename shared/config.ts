import { homedir } from 'node:os'
import type { Category, ScanConfig } from './schema'

const MB = 1024 * 1024

/**
 * Directories macOS presents as single documents. The scanner sums them but
 * never descends for reporting: you delete a whole Photos library, never one
 * `.heic` inside it.
 */
export const BUNDLE_EXTENSIONS = [
  'app',
  'bundle',
  'framework',
  'kext',
  'plugin',
  'component',
  'xpc',
  'appex',
  'photoslibrary',
  'photolibrary',
  'aplibrary',
  'imovielibrary',
  'theater',
  'fcpbundle',
  'lrcat',
  'logicx',
  'band',
  'sparsebundle',
  'rtfd',
  'xcodeproj',
  'xcworkspace',
  'playground',
  'scptd',
  'download',
  'pages',
  'numbers',
  'key',
]

/** Multi-segment extensions kept whole: `archive.tar.gz` is `tar.gz`, not `gz`. */
export const COMPOUND_EXTENSIONS = [
  'tar.gz',
  'tar.bz2',
  'tar.xz',
  'tar.zst',
  'tar.lz4',
  'tar.br',
]

export const CATEGORY_MAP: Record<string, Category> = {
  // video
  mp4: 'video', mov: 'video', mkv: 'video', avi: 'video', m4v: 'video',
  webm: 'video', mpg: 'video', mpeg: 'video', wmv: 'video', flv: 'video',
  prores: 'video', braw: 'video', r3d: 'video', mts: 'video', m2ts: 'video',
  fcpbundle: 'video', imovielibrary: 'video', theater: 'video',

  // image
  jpg: 'image', jpeg: 'image', png: 'image', gif: 'image', heic: 'image',
  heif: 'image', webp: 'image', tif: 'image', tiff: 'image', bmp: 'image',
  svg: 'image', raw: 'image', cr2: 'image', cr3: 'image', nef: 'image',
  arw: 'image', dng: 'image', psd: 'image', ai: 'image', sketch: 'image',
  fig: 'image', photoslibrary: 'image', photolibrary: 'image', aplibrary: 'image',

  // audio
  mp3: 'audio', wav: 'audio', aiff: 'audio', aif: 'audio', flac: 'audio',
  m4a: 'audio', aac: 'audio', ogg: 'audio', opus: 'audio', wma: 'audio',
  logicx: 'audio', band: 'audio', als: 'audio',

  // archive
  zip: 'archive', gz: 'archive', bz2: 'archive', xz: 'archive', zst: 'archive',
  '7z': 'archive', rar: 'archive', tar: 'archive', 'tar.gz': 'archive',
  'tar.bz2': 'archive', 'tar.xz': 'archive', 'tar.zst': 'archive',
  'tar.lz4': 'archive', 'tar.br': 'archive', tgz: 'archive', jar: 'archive',
  war: 'archive', xip: 'archive',

  // disk images / VMs / containers
  dmg: 'diskimage', iso: 'diskimage', img: 'diskimage', sparsebundle: 'diskimage',
  sparseimage: 'diskimage', vdi: 'diskimage', vmdk: 'diskimage', qcow2: 'diskimage',
  vhd: 'diskimage', vhdx: 'diskimage', pkg: 'diskimage', ipa: 'diskimage',
  apk: 'diskimage', aab: 'diskimage', simdevice: 'diskimage',

  // code / dependencies
  ts: 'code', tsx: 'code', js: 'code', jsx: 'code', mjs: 'code', cjs: 'code',
  json: 'code', py: 'code', rb: 'code', go: 'code', rs: 'code', java: 'code',
  kt: 'code', swift: 'code', c: 'code', h: 'code', cpp: 'code', hpp: 'code',
  m: 'code', mm: 'code', cs: 'code', php: 'code', sh: 'code', sql: 'code',
  html: 'code', css: 'code', scss: 'code', map: 'code', lock: 'code',
  xcodeproj: 'code', xcworkspace: 'code', playground: 'code',

  // caches / build output / intermediates
  cache: 'cache', tmp: 'cache', temp: 'cache', log: 'cache', crash: 'cache',
  o: 'cache', a: 'cache', d: 'cache', pyc: 'cache', class: 'cache',
  swiftmodule: 'cache', swiftdoc: 'cache', pch: 'cache', dia: 'cache',
  bcsymbolmap: 'cache', dsym: 'cache', xcactivitylog: 'cache', tsbuildinfo: 'cache',
  download: 'cache', part: 'cache', partial: 'cache', crdownload: 'cache',

  // documents
  pdf: 'document', doc: 'document', docx: 'document', xls: 'document',
  xlsx: 'document', ppt: 'document', pptx: 'document', pages: 'document',
  numbers: 'document', key: 'document', epub: 'document', txt: 'document',
  md: 'document', rtf: 'document', rtfd: 'document', csv: 'document',

  // binaries / apps / libraries
  app: 'binary', exe: 'binary', dylib: 'binary', so: 'binary', framework: 'binary',
  bundle: 'binary', kext: 'binary', plugin: 'binary', component: 'binary',
  xpc: 'binary', appex: 'binary', bin: 'binary', wasm: 'binary',

  // structured data / databases
  db: 'data', sqlite: 'data', 'sqlite3': 'data', realm: 'data', mdb: 'data',
  parquet: 'data', avro: 'data', ndjson: 'data', xml: 'data', plist: 'data',
  yaml: 'data', yml: 'data', pack: 'data', idx: 'data', bin_data: 'data',
  safetensors: 'data', gguf: 'data', ckpt: 'data', pt: 'data', pth: 'data',
  onnx: 'data', mlmodel: 'data', mlmodelc: 'data', h5: 'data', npz: 'data',
}

/**
 * Types that change size in place. Writing to an existing file does not touch
 * its directory's mtime, so a directory holding one of these can never be
 * trusted to an incremental cache hit.
 */
export const VOLATILE_EXTENSIONS = [
  'log',
  'db',
  'sqlite',
  'sqlite3',
  'db-wal',
  'db-shm',
  // Write-ahead logs and journals churn constantly and grow in place. Found by
  // `--verify`: sqlite-wal alone accounted for most of the drift on a live home
  // directory because only the `db-wal` spelling was listed.
  'sqlite-wal',
  'sqlite-shm',
  'wal',
  'shm',
  'journal',
  'db-journal',
  'tracev3',
  'jsonl',
  'sst',
  'vmdk',
  'qcow2',
  'vdi',
  'vhd',
  'vhdx',
  'img',
  'sparsebundle',
  'sparseimage',
  'realm',
  'pack',
]

export const DEFAULT_CONFIG: ScanConfig = {
  roots: [homedir()],
  minFileSize: 100 * MB,
  minFolderSize: 500 * MB,
  topNPerExtension: 200,
  globalTopN: 5000,
  workerCount: 12,
  bundleExtensions: BUNDLE_EXTENSIONS,
  compoundExtensions: COMPOUND_EXTENSIONS,
  categoryMap: CATEGORY_MAP,
  excludePaths: [],
  followSymlinks: false,
  crossFilesystems: false,
  detectDuplicates: true,
  // 1 MB: below this, a duplicate group is not worth the read or the row.
  duplicateMinSize: 1024 * 1024,
}

/** Identity helper that gives `scan.config.ts` autocomplete and type checking. */
export function defineScanConfig(config: Partial<ScanConfig>): Partial<ScanConfig> {
  return config
}

export function resolveConfig(overrides: Partial<ScanConfig> = {}): ScanConfig {
  return {
    ...DEFAULT_CONFIG,
    ...overrides,
    bundleExtensions: overrides.bundleExtensions ?? DEFAULT_CONFIG.bundleExtensions,
    compoundExtensions: overrides.compoundExtensions ?? DEFAULT_CONFIG.compoundExtensions,
    categoryMap: { ...DEFAULT_CONFIG.categoryMap, ...overrides.categoryMap },
  }
}
