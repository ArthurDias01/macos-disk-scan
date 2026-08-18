# Disk Report

A macOS storage tool that answers three questions honestly: **where did my disk
go**, **what is safe to delete**, and **did the last cleanup actually work**.

A Go scanner walks your home directory and writes a snapshot. A local web app
reads that snapshot and ranks big files by type, big folders by weight, and
stale files by age. Deleting is one button — to the Trash, recoverable — and
every deletion is checked on the next scan.

<!-- A screenshot belongs here. -->

## Why another disk tool

Most of them lie to you, and it is not their fault — the filesystem does it
first.

**They report logical size.** The question you are asking is "what do I get back
if I delete this", and `stat.size` systematically overstates that for sparse and
compressed files. This reports physical size, `blocks * 512`, which reconciles
with `du` and Finder's "on disk" figure.

**They count shared blocks more than once.** The first real scan of the author's
machine reported **3.5 TB inside a 926 GB volume** — a 6× over-count, of which
3.1 TB came from one chat folder holding 217,639 byte-identical copies of a
single 15.7 MB video. APFS clones share blocks; `stat` bills every copy in full
and cannot tell you they are clones.

This scanner finds byte-identical files and reports them as a separate
dimension, without ever rewriting the sizes. Charts read either **allocated**
(what the filesystem bills, matching `du`) or **unique** (allocated minus every
redundant copy), and default to whichever is honest — if the scan total exceeds
what the volume says is used, block sharing is *proven*, and it says so.

**They shred bundles.** A naive walk turns a 60 GB Photos library into 400,000
image rows and reports the library itself as nothing. `.app`, `.photoslibrary`,
`.xcodeproj` and friends are summed and never expanded, because that is the
boundary you actually delete at.

**They forget.** A cleanup that quietly failed looks exactly like one that
worked, because a deleted path simply stops appearing in the report. Every
deletion is written to a ledger, and the next scan checks each path and tells
you whether it is really gone.

## Install

Requires macOS, [Go](https://go.dev) 1.25+, and [Bun](https://bun.sh).

```sh
git clone https://github.com/ArthurDias01/mac-disk-report.git && cd mac-disk-report
bun install
bun run build                      # builds the app and embeds it in the binary
go build -o bin/scan ./cmd/scan
```

Grant **Full Disk Access** to your terminal (or to `bin/scan`) in System
Settings → Privacy & Security. Without it macOS hides `~/Library/Mail`,
`~/Library/Messages`, Safari's data and your Photos library — frequently the
largest single item on the machine — and the report under-counts by tens of
gigabytes with no visible sign. Paths that could not be read are listed in the
snapshot rather than silently skipped.

## Use it

Scan from the terminal:

```sh
./bin/scan                          # walks $HOME, writes public/data/scan-*.json
./bin/scan --root ~/Downloads --min-file 1GB
./bin/scan --help
```

Or run the app and scan from a button:

```sh
./bin/scan serve --open             # http://localhost:7777
```

The app is installable — Chrome's "Install app" or Safari's "Add to Dock" — and
opens on your last scan even with the server down.

To have it there whenever you are logged in:

```sh
./bin/scan serve --install-agent    # LaunchAgent, starts at login
./bin/scan serve --uninstall-agent
```

## How it decides things

The design notes live in [`docs/DECISIONS.md`](docs/DECISIONS.md) — every choice
with the reasoning behind it, including the ones that turned out to be wrong and
were revised. A few that shape what you see:

**Only files ≥ 100 MB and folders ≥ 500 MB get their own row.** Everything
smaller still counts in the totals and the per-type aggregates. A 400,000-file
`node_modules` is weight, not 400,000 rows.

**Hardlinks are counted once**, at the first path seen. Later paths are shown as
freeing nothing, which is what stops a Time Machine or Homebrew farm from
inflating a folder several-fold.

**Folders carry two sizes.** `recursiveSize` answers "where does my disk go";
`ownSize` answers "what do I delete", because summing recursive sizes across a
flat list double-counts every ancestor.

**Scans are incremental.** An unchanged directory costs one `lstat`. A warm scan
of 2.9M files takes ~15s against ~47s cold. Directories holding a large file or
a type that grows in place are never trusted to the cache, because a file can
change size without touching its directory's mtime.

**Each scan is its own snapshot.** Recurring bloat is a trend question: what
*grew* since last month is more actionable than what is big. `/trends` diffs two
scans and refuses to be quietly misleading — if they used different thresholds
it says so.

## Deleting

**Move to Trash** and **Reveal in Finder** are real buttons when the server is
running. Trash goes through Finder, so items keep "Put Back".

**Permanent deletion is never a button.** It produces a shell-quoted command you
read and run yourself. The asymmetry is deliberate: a trashed file is
recoverable and a failed move is reported, while `rm -rf` against a `~/Library`
path has neither property.

The server binds `127.0.0.1` only and is not a boundary on its own — every page
you visit can reach localhost. Requests must carry a per-run token in a custom
header (which forces a CORS preflight the server refuses), come from the
expected origin, and arrive with a matching `Host` (which is what stops DNS
rebinding). File actions are restricted to paths under a configured scan root,
resolved through symlinks first so a link cannot reach outside it, and never a
root itself.

## Development

```sh
bun run dev        # Vite on :3000, proxying /api to the Go server on :7777
bun run scan       # go run ./cmd/scan
bun test           # both suites: bun:test and go test
bun run gen        # regenerate Go types from shared/schema.ts
bun run parity a b # compare two snapshots field by field
```

`shared/schema.ts` is the single source of truth for the snapshot format. The Go
structs and the embedded config defaults are generated from it, and `bun run
gen:check` fails on any drift — two hand-maintained copies of a 200-entry
category map is how a scanner and its app start disagreeing about what a `.dmg`
is, with nothing to catch it.

The scanner has tests and the UI mostly does not, on purpose: the scanner's
output is numbers you cannot eyeball, so a double-counted rollup still renders a
convincing chart. The UI's bugs are visible on every run.

## Layout

```
cmd/scan/          CLI and `serve` subcommand
internal/
  walk/            directory walk, bundles, hardlinks
  aggregate/       folder tree and per-type rollups
  duplicates/      byte-identical detection
  cache/           incremental scan cache (SQLite)
  scan/            drives a walk, assembles a snapshot
  server/          local HTTP server and its guards
shared/            schema and config — canonical, shared with the app
src/               the SPA (TanStack Start, client-only)
tools/             code generation and the parity harness
```

## License

MIT
