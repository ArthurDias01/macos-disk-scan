# Decisions

Design decisions for this tool, with the reasoning behind each. Written up front
so that six months from now the numbers on screen are explainable.

## What this is

A macOS storage-cleanup tool for a single user. A scanner walks the disk and
writes a snapshot; a static SPA reads that snapshot and ranks big files by
extension, big folders by weight, and stale files by age. The app never deletes
anything — it produces commands you review and run yourself.

---

## Scanner

### Scope: `$HOME` only

Extra roots are configurable. The walk never crosses a filesystem boundary
(`st_dev` change) and never follows symlinks.

**Why:** `$HOME` is the actionable surface. A full `/` walk costs far more (the
volume holds ~8.3M inodes) and mostly returns weight that cannot safely be
deleted. Not crossing filesystems keeps a stale network share from hanging the
scan.

### Physical size, not logical

Sizes are `blocks * 512`, not `stat.size`.

**Why:** the entire question is "what do I get back if I delete this". Logical
size overstates sparse files and APFS-compressed files, so it systematically
lies about exactly that. Physical size also reconciles with `df` and Finder's
"on disk" figure.

### Hardlink dedup

Files with `nlink > 1` are counted once, at the first path seen. Later paths are
marked `isDupInode` and contribute zero bytes.

**Why:** hardlink farms (Time Machine, Homebrew) otherwise inflate a folder
several-fold, which would mislead a cleanup decision.

### Byte-identical detection, in a second pass

**Revised after the first real scan.** The original decision was to ignore APFS
clones, on the assumption they were a marginal error confined to VM images. The
first scan of this machine reported **3.5 TB inside a 926 GB volume** — a 6×
over-count, of which 3.1 TB came from a single WhatsApp chat holding **217,639
byte-identical copies of one 15.7 MB video**. Clones were not marginal; they
dominated every chart.

The scanner now runs a second pass:

1. The walk records `(path, size)` for every file at or above
   `duplicateMinSize` (1 MB).
2. Only files whose **exact size collides** with another candidate are opened.
3. Each suspect is fingerprinted as `size + xxHash64(first 64 KB) +
   xxHash64(last 64 KB)`.
4. Files sharing a fingerprint form a `DuplicateGroup`.

**Why size-collision first:** it turns millions of files into a few thousand
reads. Full-content hashing of every large file would multiply read volume by a
hundred to find the same groups.

**What a group does *not* tell you.** Byte-identical is not the same as
block-shared, and `stat` cannot distinguish them:

- **APFS clones** share blocks. `stat` and `du` bill each copy in full;
  deleting one frees nothing. The group really occupies `size`.
- **Real duplicates** each own their blocks. The group really occupies
  `size × count`, and deleting the extras frees `reclaimableBytes`.

Because of that ambiguity, **the second pass never rewrites sizes**. Folder
totals, extension totals and `totals.bytes` stay exactly as the filesystem
reports them — matching `du` and Finder. The groups are added as a separate
dimension, and `totals.uniqueBytes` gives the floor (allocated minus every
redundant copy).

**The reconciliation check.** Each snapshot records the volume's own figures
from `df`. When the scan total exceeds the volume's used bytes, block sharing is
*proven* rather than suspected, and a warning says so. On this machine:
3.5 TB allocated, 368 GB unique, 546 GB volume used.

### Thresholds: files ≥ 100 MB, folders ≥ 500 MB

Anything smaller exists only inside aggregates, never as an individual row.

**Why:** nothing under 100 MB is worth deleting individually — it only matters in
bulk, which the per-extension aggregates already capture. This keeps the
snapshot in the hundreds of kilobytes instead of gigabytes.

**Consequence:** a 400k-file `node_modules` appears as aggregate weight, not as
400k rows. That is the intended reading.

### Folders carry both sizes

Every folder node stores `recursiveSize` (itself plus all descendants) and
`ownSize` (files directly inside it only).

**Why:** two different questions. "Where does my disk go" needs recursive size
and a tree that makes the nesting visually explicit. "What do I delete" needs a
flat comparison, which is only honest with own size — summing recursive sizes
across a flat list double-counts every ancestor.

### Bundles are atomic

`.app`, `.photoslibrary`, `.fcpbundle`, `.sparsebundle`, `.xcodeproj` and
friends are summed but never expanded.

**Why:** the deletion decision is always made at the bundle boundary. You delete
a whole Photos library, never one `.heic` inside it. A naive walk would shred a
60 GB library into 400k image rows and report the library itself as nothing.

### Extension normalization

- lowercased (`.JPG` → `jpg`)
- compound extensions preserved from an explicit list (`tar.gz`, not `gz`)
- leading-dot files with no other dot have **no** extension (`.zshrc`, `.DS_Store`)
- no dot at all → no extension (`Makefile`, compiled binaries)
- anything over 16 characters, containing a space, or all digits is a mis-parsed
  dotted filename → no extension. The bar started at 12 and was raised once the
  tests caught it discarding `photoslibrary` and `imovielibrary`, both real
  13-character macOS types.
- a category layer (video, image, audio, archive, diskimage, code, cache,
  document, binary, data, other) sits on top

**Why:** two hundred extensions is an unreadable chart; ten categories is a
story. Both are kept so you can zoom either way.

### Timestamps captured

`mtime`, `atime` and `birthtime` on every reported entry; folders carry the
maximum `mtime` of any descendant.

**Why:** size alone cannot answer "is this still used", and `stat` returns the
timestamps for free. Big **and** old is the real delete list.

**Caveat surfaced in the UI:** `atime` is only weakly trustworthy on macOS —
Spotlight, backups and indexers bump it. `mtime` is the default sort.

### Permission errors are data

`EPERM`/`EACCES` are captured into `unscanned[]` and shown as a banner.

Granting Full Disk Access to the host app removed all 151 of them. Note that the
grant takes effect when the app next launches, and adding an app to the list
revokes the running process's existing access immediately — a scan mid-session
will start failing until the app is restarted.

**Why:** without Full Disk Access, macOS hides `~/Library/Mail`, `~/Library/
Messages`, `~/Library/Safari` and `~/Pictures/Photos Library.photoslibrary` —
frequently the largest single item on a Mac. Silently omitting them would make
the report under-count by tens of gigabytes with no visible sign.

### Cloud folders are walked and tagged

`~/Library/CloudStorage/*` and `~/Library/Mobile Documents` are traversed;
entries are flagged `isCloud`.

**Why:** evicted placeholder files correctly report ~0 physical bytes, so they
are honest under physical sizing. `stat` never triggers a download — only
reading contents does, and the scanner never reads contents. The flag lets the
UI say "already evicted, deleting frees nothing locally".

### Worker pool, workers pre-aggregate

The main thread holds a directory queue and hands one directory at a time to an
idle worker. Each worker returns compact per-directory deltas: own bytes, file
count, a per-extension map, entries above the size floor, `nlink > 1`
candidates, and errors. The main thread merges the counters, builds the folder
tree, and owns the global inode set.

**Why:** counter merges are associative, so partial aggregation per worker is
exactly correct without coordination. Shipping every file record over IPC would
cost more than the parallel walk saves; this keeps IPC proportional to directory
count, not file count. Dedup has to stay central because the inode set is
global. Handing out one directory at a time load-balances naturally — a
`node_modules` monster does not stall one worker while eleven others idle.

### Snapshots, not a single file

Each scan writes `scan-<timestamp>.json` plus an `index.json`. The resolved
config is embedded in every snapshot.

**Why:** the stated problem is recurring bloat, which is a trend question. "What
grew since last month" is more actionable than "what is big" — a folder that
quietly gained 30 GB beats one that has been a stable 40 GB for three years. At
a few hundred kilobytes each, keeping fifty snapshots is free. Embedding the
config means an old snapshot stays self-describing and diffs can warn when two
scans used different thresholds.

**Snapshots are gitignored.** They name every large file in a personal home
directory.

---

## App

### Pure SPA

TanStack Start with `spa: { enabled: true }` and `defaultSsr: false`. Routes
fetch static JSON from `/data`. No server functions, no server at request time.

**Why:** the scan is a deliberate multi-minute act run from a terminal, not
something to trigger from a chart. Removing the server removes a whole class of
concerns from a tool that only ever runs locally.

**Revised once the scanner became a Go binary.** The app is still a static SPA —
no route renders on the server, no loader runs there, every page still reads the
same snapshot JSON from `/data`. What changed is who serves those files:
`scan serve` hosts the built app and adds the two things a static file cannot
do, start a scan and move a file to the Trash.

The original reason held for as long as the alternative was a Node server
rendering routes. It stopped holding once the scanner was already a binary with
a progress callback and a snapshot writer in it: serving a directory and
exposing two endpoints is a few hundred lines, and it removes the trip to
another window to get fresh numbers. What it costs is a process listening on
localhost that can delete things, which is why `internal/server/security.go`
exists and why it is the longest-commented file in the package.

### View state in the URL

Sorting, filters, drill path and selected snapshot are typed router search
params.

**Why:** this tool gets revisited over months. A filter worth building once
(".dmg and .zip, untouched 2 years, over 1 GB") should be a URL you keep, not
something rebuilt each visit.

### Treemap: the native mark

**Revised during P2.** The plan was to hand-roll a treemap from `rect` plus
d3-hierarchy, because the documentation listing of chart marks showed no
hierarchy support. The installed package does export
`@tanstack/charts/hierarchy/treemap` (and `/sunburst`), taking a flat source
keyed by a delimited `path` — a direct fit for file paths. The mark is used
as-is and the explicit d3-hierarchy dependency was dropped.

**Why a treemap at all:** it answers "where is the weight" at a glance. It is
poor at precise comparison, so the drill-down table beneath it does that work.

### Charts read allocated or unique, and pick their own default

The first render of the overview proved the calibration note right: the WhatsApp
clone group was ~90% of the treemap and flattened both bar charts, so the page
said "WhatsApp is your disk" — which is false.

Every chart now takes a `SizeMode`:

- `allocated` — what the filesystem bills, matching `du`.
- `unique` — allocated minus every redundant copy.

To support that, the scanner attributes redundant bytes per directory
(`duplicateOwnSize`, rolled up to `duplicateRecursiveSize`) and per extension
(`duplicateBytes`, rolled up into categories). The mode lives in the URL as
`?size=`, and the page defaults to `unique` whenever the scan total exceeds the
volume's used bytes — that is, whenever clones are proven present.

### The app never deletes

Rows offer a copy button producing `mv -n '<path>' ~/.Trash/`. A dropdown offers
`rm -rf`, the bare path, and `open -R`. Every path is shell-quoted. The basket
exports a reviewable script.

**Why:** review before execution is the safety mechanism, and the Trash makes
even a bad run recoverable. `rm` against a `~/Library` path can break an app
with nothing to undo it. Paths are quoted because macOS filenames routinely
contain spaces and apostrophes, where an unquoted path deletes the wrong thing.

**Stated in the UI:** trashed items still occupy disk until the Trash is
emptied.

**Revised: the app never *permanently* deletes.** With a local server there is
somewhere for a button to send a request, and Trash and Reveal became real
buttons. Permanent deletion did not.

The asymmetry is the whole point. A trashed file is recoverable from Finder,
including "Put Back" — which is why the server asks Finder to do the delete
rather than moving the file to `~/.Trash` itself, since only Finder records
where the item came from. A failed move is reported per path. `rm -rf` against a
`~/Library` path has none of those properties, so it keeps the one safety
mechanism that suits it: you read the command and run it yourself.

Every move the server makes is appended to the same `.scan-cache/deleted.log`
the exported script writes, so the next scan still reports whether the path is
really gone. That loop was built for a shell script in the middle; it turns out
not to need one.

**Where the buttons stop.** The action endpoints only accept paths under a
configured scan root, resolved through symlinks first so a link inside a root
cannot reach outside it, and never the root itself. The UI cannot widen its own
reach by scanning somewhere new: the allow-list is fixed when the server starts.

---

## Stack

| Concern | Choice | Note |
| --- | --- | --- |
| Scanner runtime | Go (porting from Bun + TypeScript) | See "The Go port" below. Types are generated from `shared/schema.ts` |
| Framework | TanStack Start (SPA mode) | |
| Routing/state | TanStack Router, typed search params | |
| Data | TanStack Query | Snapshots are immutable: `staleTime: Infinity` |
| Tables | TanStack Table + Virtual | |
| Charts | `@tanstack/charts` + `@tanstack/react-charts`, pinned `0.14.0` | Pre-1.0: exact pin, upgrades are deliberate. Native treemap mark |
| Styling | Tailwind v4 + shadcn/ui, dark default | |
| Layout | `cmd/` + `internal/` (Go), `src/` (SPA), `shared/` (canonical schema) | `shared/` stays dependency-free and is what the Go types are generated from |
| Tests | `bun:test` on scanner logic + a fixture-directory integration test | No UI tests |

**On testing:** the scanner's output is numbers that cannot be independently
eyeballed — if the rollup double-counts, the treemap still looks convincing. Its
bugs are invisible, so it gets tests. The UI's bugs are visible on every run, so
it does not.

---

## Phases

- **P0** Foundation — git, SPA scaffold, Tailwind, shared schema, config, route stubs ✅
- **P1** Scanner — worker pool, aggregation, dedup, bundles, snapshot writer, tests, first real scan ✅
- **P2** Overview — totals, categories, top extensions, treemap, unscanned banner ✅
- **P1.5** Incremental scanning — see docs/INCREMENTAL.md (designed, not built)
- **P3** Folders — treemap plus drill-down and breadcrumbs ✅
- **P4** Files — virtualized table with faceted filters ✅
- **P5** Basket — selection, copy buttons, trash-script export ✅
- **P6** Extensions — ranked list and per-extension detail ✅
- **P7** Stale — size against age ✅
- **P8** Trends — snapshot diff ✅

P1 lands before any chart work so every UI decision is made against real data —
real extension distribution, real folder depth, real outliers — rather than
invented fixtures that flatter the design.

---

## Scan calibration

`$HOME` on this machine, 12 workers, 100 MB / 500 MB thresholds. The second row
is the same scan after granting Full Disk Access.

| Measure | Before FDA | With FDA |
| --- | --- | --- |
| Duration | 81s | 98s |
| Files / directories | 5,459,942 / 727,954 | 5,504,018 / 737,044 |
| Allocated (as `du` sees it) | 3.5 TB | 3.5 TB |
| Unique (minus redundant copies) | 350 GB | 368 GB |
| Volume used, per `df` | 549 GB | 546 GB |
| Files at or above 100 MB | 485 | 490 |
| Distinct extensions | 11,141 | 11,244 |
| Byte-identical groups | 2,499 | 2,535 |
| Unreadable paths | 154 | **0** |
| Snapshot size | 6.6 MB | 6.7 MB |

Full Disk Access made `~/Pictures` (15.5 GB of Photos libraries), `~/.Trash`,
Mail and Messages visible. `photoslibrary` entered the extension ranking at
13 GB across 10 libraries.

Consequences carried into the UI phases:

- **490 rows** at the 100 MB floor — a comfortable table, no virtualization
  pressure. The floor is well chosen.
- **11,244 extensions** is mostly noise: random suffixes parsed out of dotted
  filenames. `/extensions` needs a long tail cut, not a full list.
- **6.7 MB snapshots**, not the few hundred KB estimated. The weight is the
  48-bucket histogram on every extension, plus up to 50 paths per duplicate
  group. Trimmable if it becomes a problem.
- **Allocated is unreadable on this machine.** `Library/Group Containers` is
  3,193.9 GB allocated against 9.7 GB unique — one WhatsApp clone group. Every
  chart defaults to unique when the scan total exceeds the volume's used bytes.

## Structure

Findings from a code-quality review after P1.5, and what changed.

**The cache stores `DirDelta` directly.** A parallel `CachedDir` type existed
alongside it, identical but for one renamed field, plus a `cachedToDelta`
translation step. Both are gone: a cache hit now replays exactly the object a
worker would have produced. One type and one function deleted, and the two can
no longer drift.

**`creditFile` is the single accounting path.** Hardlink survivors were accounted
for by 35 lines in `applyDelta` that mirrored what the worker does for ordinary
files, including a synthetic single-entry extension map. Both paths must agree
exactly or totals go wrong, so there is now one implementation and both call it.

**The worker pool moved to `pool.ts`.** `walk.ts` had grown to 469 lines doing
five jobs. The pool now owns worker lifecycle and dispatch, and asks the caller
for the next directory — the queue stays with the walk, because directories are
discovered as their parents are scanned.

**One candidate type.** `DuplicateCandidate` and `CandidateRecord` described the
same thing, bridged by a cast at the call site. The cast is gone.

**`formatBytes` had two implementations**, one in the CLI and one in the SPA,
already differing in behaviour. Now `shared/format.ts`, used by both.

**`sizeIn(mode, total, duplicate)`** replaced the allocated-versus-unique
subtraction that had been written out twelve times across charts and helpers.
"What does unique mean" now has one answer.

### From the P4 review

**Filters were defeating their own memoization.** The merged filter object was
rebuilt every render and then used as the dependency of the memos that filter
and sort the file list, so none of them ever hit. Memoized on the router's
search object, which is stable between navigations.

**`validateSearch` now omits absent keys** instead of setting them to
`undefined`. The undefined keys were overwriting defaults when spread, which had
needed a `stripUndefined` pass to undo — that pass is gone, and a default view's
URL stays clean.

**One column spec drives the file table.** Header and rows had each declared
their own widths, which is how a table quietly goes crooked: change one and the
other keeps its old value. The spec also carries the sort key, which removed a
cast in the click handler.

**`SnapshotGate` replaced per-route loading branches.** All three pages repeated
the same four states, and had already drifted into three differently-worded
empty states. Pages now receive a snapshot that is known to exist and contain no
loading branches at all — which stops the duplication before P5 through P8
multiply it.

### From a screenshot review

**The row action control was breaking rows.** "Trash cmd" wrapped onto two lines
in every row of the folders table, next to a sliver of a caret. Table rows now
get an icon-only control that cannot wrap; the page header keeps a labelled
variant, where one word is information rather than twenty repetitions of noise.
The dropdown gained each command's actual effect as a subtitle.

**`formatAge` reported every gap one unit too large.** It paired each unit with
the previous unit's divisor, so twelve hours read as "12 days ago", five days as
"5 weeks ago", and seven months as "7 years ago" — visible throughout the app
and wrong everywhere. Found by a test written for an unrelated fix.

**A file dated year 4854 rendered as "in 2,828 years".** Corrupt mtimes are real
and a home directory has a few; the phrase reads as a bug in this app rather
than a fact about the file, so future timestamps now show an absolute year.

**The share bar was a floating pill.** At a 1% share it was a two-pixel dot with
nothing to measure against. It is now a fixed track with a filled portion, and
carries the exact percentage as a tooltip.

**The breadcrumb root read as a bare `arthur`.** The home directory renders as
`~`.

**The file list was a fixed 600px**, leaving dead space on a laptop and wasting
half of a larger display. It now fills the viewport.

All of the above was then confirmed in a browser: single-line rows, correct
ages, a working dropdown, and live filtering — `?dup=true` narrows to the 54
files that share blocks and reports "Frees if deleted: 0 B", which is the whole
point of the distinction.

### Tooltips say what they mean

The library's default tooltip prints channel names and raw values — hovering a
treemap tile gave `x /Documents/Projects/central-tech-app` and
`y 4,871,397,376`, which names neither quantity and formats neither number.

Every chart now supplies its own content:

- **Treemap** — folder name, total with the exact byte count in parentheses,
  what sits directly inside (omitted when it would repeat the total), share of
  the whole, file count, newest file, and the path trimmed from the left so the
  identifying end survives.
- **Category and extension bars** — the reading in use (unique or allocated),
  file count, and average file size.
- **Histogram** — the bucket as a *range* ("64 KB to 128 KB"), since an axis
  label alone leaves "is this the floor or the middle?" open.
- **Scatter** — file name, size, how long ago it changed in the unit that suits
  the gap, whether it is a candidate, and a warning when it shares blocks.

Sizes read as `4.5 GB  (4,871,397,376 bytes)`: the first number is for judging,
the second for comparing two rows that round to the same thing.

The builders live in `chart-tooltip.ts` rather than inside the chart
components, because a tooltip is the one part of a chart that is pure text and
can therefore be tested.

### The deletion ledger closes the loop

**Requested after P7.** A cleanup that quietly failed looks exactly like one
that worked, because a deleted path simply stops appearing in the report.

The obstacle is that the browser cannot write files and the scanner cannot read
a browser. The bridge is the cleanup script the basket already exports: each
line now records itself **only if the move succeeded**.

```sh
mv -n '/Users/arthur/.npm' ~/.Trash/ && printf '%s\t%s\n' "$STAMP" '/Users/arthur/.npm' >> "$LEDGER"
```

`set -e` was dropped deliberately: one failed move must not abandon the rest,
and the `&&` means a failure is simply never logged.

Every scan reads `.scan-cache/deleted.log` **before walking anything**, stats
each path, and reports the verdict in the snapshot as `rechecked[]`. This has to
happen deliberately: an absent path never appears in a walk, so its absence
cannot be inferred from the results.

The Overview then says either "N previous deletions confirmed" or names the
ones that came back — restored from the Trash, or recreated by the app that made
them. The ledger is plain `ISO_TIMESTAMP<TAB>PATH`, appendable from a shell
script and fixable by hand.

### Trends compares two scans

Folder and extension movements, plus files that appeared or stopped being
reported. Sizes are mode-aware, and the diff refuses to be quietly misleading:
if the two scans used different thresholds it says so, because items can appear
or vanish purely because the floor moved.

Bars are centred on zero — growth right in red, shrinkage left — so direction
reads before the number does.

### Stale is an intersection, and the thresholds are drawn

Size says what is expensive; age says what is forgotten. Neither is a delete
list alone — a 30 GB disk image touched this morning is in use, and a 2 KB note
from 2011 is not worth finding. `/stale` plots every reported file as size
against age and draws both threshold lines, so "big and old" is a visible region
rather than a claim: moving a cut visibly moves the list.

The y axis is `log2(size)` with its ticks relabelled as bytes. Sizes span nine
orders of magnitude, so a linear axis would pin every point to the floor.

Two details the data forced:

- **Future mtimes are clamped to zero age.** A file dated 2262 would otherwise
  drag the axis centuries to the right and squash every real point into a line.
- **Colour is a channel, not a per-point fill.** `dot` takes a single fill
  string, so category colour goes through an ordinal scale.

On this machine the default cut (1 GB, 1 year) finds 4 files worth 7.1 GB;
loosening to 500 MB finds 21 worth 20 GB, of which 19 GB would actually free.

### The extension long tail is folded, not dropped

A real home directory yields **11,244 distinct extensions**, and 11,110 of them
fall in the `other` category — junk parsed out of dotted filenames. Listing them
is useless; hiding their weight would be dishonest.

So `/extensions` lists the top 40 and folds the rest into one summary row that
keeps its numbers: *11,204 further extensions, 34 GB, 1.2M files*. Search reaches
anything by name, so nothing is unreachable.

The ranking is mode-aware, and that changes the answer completely: `.mp4` is
first by allocated bytes at 3.1 TB, and **eighth** by unique bytes at 11 GB, once
the WhatsApp clones are excluded.

### The per-extension histogram finally uses the buckets

The scanner has recorded a power-of-two size histogram per extension since P1,
and nothing displayed it. The detail page does: it is the only view that sees
files *below* the 100 MB reporting floor, because the histogram counts every
file of that type. `.jpg` turns out to peak at 64 KB across 21,476 files, with a
long tail out to 538 MB.

Empty buckets at both ends are trimmed — a chart spanning 1 byte to 256 TB is
mostly whitespace.

### Selection lives in localStorage, not the URL

Every other view state is a search param (Q17), but the selection is not. A list
of absolute paths makes an unusable link, and the point of a basket is to gather
items across several pages before acting — so it persists in `localStorage` and
survives navigation and reloads.

**One command for the whole selection.** `mv`, `open` and `rm` all accept many
sources, so ticking twenty folders yields a single pasteable line rather than
twenty copies:

```
mv -n '/Users/arthur/.npm' '/Users/arthur/.cache' '/Users/arthur/.gradle' ~/.Trash/
```

The basket also exports a script with **one `mv` per line**, because a one-liner
stops at the first failure and a line-per-item does not — and because a script is
reviewable in a way a 2,000-character line is not.

**Two things the UI says out loud**, both of which would otherwise look like the
command worked:

- `mv -n` silently skips the second of two items sharing a basename — the
  selection is checked for collisions and names them.
- Selected items that share blocks free nothing on their own, so the bar reports
  "would actually be freed" separately from the allocated total.

### The transparent menu

The dropdown looked see-through on `/files` but was fine on `/folders`, and its
background was provably opaque (`oklch(0.185 0 0)` at runtime). The cause was
stacking, not colour: **virtualized rows are `position: absolute` with a
`transform`, and a transform creates a stacking context.** Inside one, `z-index`
is relative to that row — so rows later in the DOM painted straight over the
menu. The real table on `/folders` has no transforms, which is why it never
showed the problem.

The menu is now rendered into `document.body` through a portal with fixed
positioning, and flips above its trigger when the space below is too small.
That also fixes menus on the last rows, which previously opened off-screen.

Two smaller things surfaced with it:

- `shadow-xl` computed to a fully transparent shadow, so the menu had no
  elevation at all. Replaced with an explicit shadow value.
- The entry animation was driven by `requestAnimationFrame`, which is throttled
  in an unfocused window — the menu could stay stuck at `opacity: 0`. It is now
  a CSS animation, which needs no frame callback, entering at `scale(0.96)` from
  the corner nearest its trigger over 150ms.

### A bug the refactor exposed

Re-running the scan after these changes reported **0% cache hits**. The cause was
not the refactor: the config fingerprint included `roots`, and stale-row pruning
deleted every row not visited. So scanning any subtree discarded the cache for
the whole home directory — twice over.

Fixed by dropping `roots` from the fingerprint (rows are keyed by absolute path
and stay valid whichever root reached them) and scoping pruning to the roots
actually scanned. Proven: a subtree scan now hits 100% on its own rows and
leaves the rest intact, and the next home scan still hits 99%.

## Environment note

macOS TCC blocks processes from `~/Documents` unless the hosting terminal app
holds a Files-and-Folders or Full Disk Access grant. Without it, `getcwd()`
returns `EPERM` and no Node or Bun process can start in this directory. Full
Disk Access is required anyway for the scanner to see `~/Library/Mail`,
`~/Library/Messages` and the Photos library.

## The Go port

The scanner is being rewritten in Go. Four things drove it: a single
distributable binary, raw walk speed, deleting the worker-IPC layer, and a
preference for Go in this kind of code.

### `shared/schema.ts` stays canonical

`tools/gen-go-types.ts` reads the TypeScript declarations with the compiler API
and emits `internal/schema/schema_gen.go`; `tools/gen-go-config.ts` evaluates
`shared/config.ts` and emits the embedded defaults. `bun run gen:check`
regenerates and fails on any diff.

**Why generate rather than transcribe:** the category map alone is ~200 entries.
Two hand-maintained copies is how the scanner and the app start disagreeing
about what a `.dmg` is, and nothing would catch it — the chart still renders.

Numbers map to `int64`, because nearly every number in this schema is a byte
count or a tally. The five genuinely fractional fields carry a `@go float64`
JSDoc tag.

### The worker protocol survives, for a different reason

`protocol.ts` compressed its payloads — sparse bucket pairs, basenames rather
than paths — because structured-clone across worker threads was expensive.
Goroutines share memory, so that reason is gone. The same rows are what the
incremental cache stores, though, and at 452k directories a dense 48-slot
histogram per extension per directory would dominate the cache file. The shape
stays; only its justification changed.

### Config moved to JSON

Go cannot read `scan.config.ts`. `scan.config.json` carries a `$schema` pointer
to a generated JSON Schema, so the editor still autocompletes and validates, and
changing a number is still one edit with no build step.

A leading `~` is expanded by the scanner, because JSON cannot call `homedir()`.

### `df`, not `statfs`

The port replaced the `df -k` subprocess with `syscall.Statfs`, and that was
wrong. On APFS the two disagree:

| Source | Used |
| --- | --- |
| `df -k` | 462,610,188 KB |
| `statfs` (`f_blocks - f_bfree`) | 506,168,492 KB |

The 41 GB gap is purgeable space, which `df` excludes and `statfs` counts as
used. `df`'s figure is the one Finder shows and the one every existing snapshot
was reconciled against — and the larger number would quietly weaken the clone
check, which only fires when the scan total *exceeds* it. The subprocess is back.

### Every tie is broken deliberately

The TypeScript scanner leaned on V8's stable sort over insertion-ordered
objects. Go map iteration is randomized, so the same scan would have ranked
equal-sized extensions differently on every run. Extensions tie-break on name;
files and folders on path; the largest file of a type on path.

**Consequence:** two Go scans of an unchanged tree are byte-identical. The
TypeScript scanner never was.

### Divergences from the TypeScript scanner

Three, all deliberate:

- **Duplicate group members sort byte-wise, not by `localeCompare`.** Collation
  is locale- and ICU-dependent, so the TypeScript ordering is not even stable
  across machines. This decides which of *n* byte-identical copies is called
  "the original", and which 50 of them the capped `paths` sample lists. Counts,
  sizes and `reclaimableBytes` are unaffected.
- **Bundles are never duplicate candidates.** `noteFile` offered them up and
  relied on the `open()` failing, which spent an errno on every large bundle and
  made "unreadable" mean two different things.
- **`mergeExtMaps` and `addFileToExtMap` were not ported.** Both are dead in the
  TypeScript scanner: the first is referenced only by its own tests, the second
  by nothing.

### Parity, measured

`bun run parity <a> <b>` compares two snapshots field by field, normalizing
object key order and reporting ties separately from defects.

On static trees the two scanners agree exactly:

| Tree | Result |
| --- | --- |
| `/Applications` — 28 GB, 225 bundles | **Identical**, 2 ties |
| The project repo — 13,509 files, 1,068 dirs | **Identical**, 3 ties |

A tie is a `largestPath` where several files share the maximum size and there is
nothing to choose between them.

On a live `$HOME` exact equality is not achievable — by either scanner, against
itself. Three back-to-back cold scans of 2.89M files:

| Pair | File delta | Reported rows differing | Extension rows differing |
| --- | --- | --- | --- |
| Go vs Go | 781 | 2 / 372 | 63 / 11,165 |
| Go vs TypeScript | 1,337 | 2 / 372 | 48 / 11,165 |
| TypeScript vs Go | 2,118 | 0 / 372 | 61 / 11,169 |

Go differs from TypeScript by *less* than Go differs from itself, and the
magnitudes track the elapsed time between runs rather than which scanner ran.
Duplicate group counts were identical across all three.

Every difference on a quiet tree was traced to a cause: junk extensions from
temporary files that existed at one moment and not the next, `maxMtimeMs` moving
as a simulator wrote, and the `localeCompare` divergence above.

### Speed, measured

Warm page cache, alternating runs:

| Tree | Go | TypeScript | Ratio |
| --- | --- | --- | --- |
| `$HOME` — 2.89M files, 452k dirs | 70.6s | 146s | 2.1× |
| `~/Library/Developer` — 163k files, 42k dirs | 1.7s | 3.1s | 1.8× |
| `/Applications` — I/O-bound fingerprinting | 10.1s | 11.0s | 1.1× |

Roughly 2× where the work is walking directories, and near parity where it is
waiting on the disk. The estimate before starting was "2–4×, not 20×", which
holds — the walk was always syscall-bound, and Go does not make a read faster.

**The first `$HOME` run cost 139s against the second's 70s**, purely from the
page cache. Any timing comparison that does not control for run order is
measuring the cache.

### The cutover

`scanner/` and `scan.config.ts` are gone. `bun run scan` now runs
`go run ./cmd/scan`, and `bun test` runs both suites.

The 64 `bun:test` cases that covered the TypeScript scanner were not deleted so
much as relocated: every one of them has a Go counterpart, several pinned
against output captured from the TypeScript implementation before it was
removed — the histogram bucket table, the duplicate fingerprint format, the
config fingerprint, and the three formatters. Those pins are what stop the Go
scanner from drifting away from a reference that no longer exists to check
against.

### Calibration has moved

The table above this section describes a machine holding 5.5M files and 3.5 TB
allocated against 546 GB used. It now holds **2.89M files and 286 GB allocated
against 441 GB used** — the WhatsApp clone farm that made allocated unreadable
is gone, and with it the reason `unique` was the default reading. The numbers in
"Scan calibration" are kept as the record of what the design was built against,
not as a description of the current disk.

## The local server

`scan serve` hosts the built SPA, runs scans, and performs the two file actions.
It binds `127.0.0.1` only.

### Localhost is not a boundary

A server on localhost is reachable from every web page you visit. It is not
routable from anywhere else, but the browser you are reading this in can post to
it — and this one moves files to the Trash. Four checks stack, and a request has
to pass all of them:

1. **A token in a custom header.** Custom headers force a CORS preflight, and
   the preflight is refused, so a cross-origin page cannot send one at all.
2. **An Origin allow-list.** Exactly `http://localhost:PORT` and
   `http://127.0.0.1:PORT`. A request with no `Origin` is refused rather than
   trusted.
3. **A Host check, on every request including plain GETs.** DNS rebinding points
   an attacker's domain at 127.0.0.1, which satisfies an Origin check and fails
   this one.
4. **A path allow-list**, described under "The app never deletes" above.

The token is fetched from `GET /api/token` rather than baked into the served
HTML. That endpoint needs no token itself, which sounds like a hole and is not:
without CORS headers a cross-origin caller cannot read the response, a `no-cors`
fetch gets an opaque body, and the one way around that — rebinding — is refused
by check 3. Serving it separately matters because the service worker caches the
HTML: a token baked into a cached document would be a dead one after the next
restart, and every action would fail with a 403 that looked like a permissions
problem.

### Always-on, by choice

`scan serve --install-agent` writes a LaunchAgent so the app is there when you
click its icon. That means a process that can trash files is listening whenever
you are logged in. `--uninstall-agent` removes it.

### Scanning from the UI

One scan at a time. Two walks would fight over the same cache file and the same
output directory, and a second scan started thirty seconds after the first has
nothing new to say. A second request gets a 409.

Roots and thresholds are editable in the form, with one warning: changing a
threshold changes the cache's config fingerprint and discards every stored row.
On this machine that is 15s becoming 47s. Snapshots taken at different
thresholds are also not comparable, which `/trends` already detects and says.

## PWA

A manifest, two generated icons, and a service worker, so the report is a dock
icon rather than a tab.

**What is cached, and why the split matters.** Snapshots are immutable once
written, so they are cache-first and capped at the three most recent — they run
to several megabytes each. `index.json` changes on every scan, so it is fetched
first and only falls back to the cache. Anything under `/api/` is never cached
and never intercepted: a stale scan or trash request has no meaning once the
server it was going to is gone.

Offline the app opens on the last snapshot you looked at, and the scan and trash
controls disable themselves rather than failing when pressed.

**The icons are generated, not drawn.** `tools/gen-icons.ts` writes the PNGs
byte by byte — signature, IHDR, a zlib-framed IDAT, IEND. The encoder is shorter
than an image library's README, and it keeps the dependency list where it is.
One thing it does have to get right: `Bun.deflateSync` emits raw deflate, and
PNG's IDAT is a zlib stream. The difference is invisible to `file`, which reads
only the header, and fatal to every real decoder.
