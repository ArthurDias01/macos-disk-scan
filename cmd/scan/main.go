// Command scan walks the disk and writes a snapshot the report app can read.
//
// It never deletes anything. The app it feeds produces commands you review and
// run yourself, and the ledger those commands append to is read back here on
// the next scan.
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"disk-report/internal/config"
	"disk-report/internal/format"
	"disk-report/internal/scan"
	"disk-report/internal/schema"
	"disk-report/internal/snapshot"
)

const usage = `Scan the disk and write a snapshot the report app can read.

  scan [options]

Options
  --root <path>        Root to walk (repeatable). Default: $HOME
  --min-file <size>    Report files at or above this size. Default: 100MB
  --min-folder <size>  Report folders at or above this size. Default: 500MB
  --workers <n>        Concurrent readers. Default: 12
  --config <path>      Config file. Default: scan.config.json
  --out <dir>          Where to write snapshots. Default: public/data
  --dry-run            Scan and print the summary, write nothing
  --full               Ignore the cache and rescan everything
  --no-cache           Ignore the cache and do not update it either
  --verify             Run a cached scan and a full scan, then compare them
  --quiet              No progress output
  --help               This message

Subcommands
  serve                Host the report UI and run scans from it

Sizes accept B, KB, MB, GB, TB (binary units): --min-file 1GB

Paths are resolved against the working directory, so run this from the project
root — that is where scan.config.json, .scan-cache/ and public/data/ live.
`

func main() {
	// One binary, two modes. `scan` runs a scan, `scan serve` hosts the UI —
	// checked before flag parsing so the CLI's own flags are unchanged.
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "\n%v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "\n%v\n", err)
		os.Exit(1)
	}
}

type options struct {
	roots      []string
	minFile    *int64
	minFolder  *int64
	workers    *int64
	configPath string
	outDir     string
	dryRun     bool
	full       bool
	noCache    bool
	verify     bool
	quiet      bool
}

// repeatable collects a flag that may appear more than once.
type repeatable struct{ values *[]string }

func (r repeatable) String() string { return strings.Join(*r.values, ", ") }
func (r repeatable) Set(value string) error {
	expanded, err := expandHome(value)
	if err != nil {
		return err
	}
	*r.values = append(*r.values, expanded)
	return nil
}

// sizeFlag accepts "1GB" as well as a plain byte count.
type sizeFlag struct{ target **int64 }

func (s sizeFlag) String() string { return "" }
func (s sizeFlag) Set(value string) error {
	parsed, err := parseSize(value)
	if err != nil {
		return err
	}
	*s.target = &parsed
	return nil
}

func parseFlags(argv []string) (options, error) {
	var opts options

	set := flag.NewFlagSet("scan", flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	set.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	set.Var(repeatable{&opts.roots}, "root", "root to walk (repeatable)")
	set.Var(sizeFlag{&opts.minFile}, "min-file", "report files at or above this size")
	set.Var(sizeFlag{&opts.minFolder}, "min-folder", "report folders at or above this size")

	workers := set.Int64("workers", 0, "concurrent readers")
	set.StringVar(&opts.configPath, "config", "scan.config.json", "config file")
	set.StringVar(&opts.outDir, "out", filepath.Join("public", "data"), "where to write snapshots")
	set.BoolVar(&opts.dryRun, "dry-run", false, "scan and print the summary, write nothing")
	set.BoolVar(&opts.full, "full", false, "ignore the cache and rescan everything")
	set.BoolVar(&opts.noCache, "no-cache", false, "ignore the cache and do not update it")
	set.BoolVar(&opts.verify, "verify", false, "compare a cached scan against a full one")
	set.BoolVar(&opts.quiet, "quiet", false, "no progress output")

	if err := set.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		return opts, err
	}
	if set.NArg() > 0 {
		return opts, fmt.Errorf("unexpected argument: %s", set.Arg(0))
	}
	// Visit reports the flags that were actually given, which is the only way
	// to tell `--workers 0` from an unset flag. Zero is a legitimate thing to
	// ask for: the pool clamps it to one.
	set.Visit(func(f *flag.Flag) {
		if f.Name == "workers" {
			opts.workers = workers
		}
	})

	expandedOut, err := expandHome(opts.outDir)
	if err != nil {
		return opts, err
	}
	opts.outDir = expandedOut

	return opts, nil
}

func run(argv []string) error {
	opts, err := parseFlags(argv)
	if err != nil {
		return err
	}

	cfg, err := resolveConfig(opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Scanning %s\n", strings.Join(cfg.Roots, ", "))
	fmt.Fprintf(os.Stderr,
		"Files at or above %s, folders at or above %s, %d workers\n",
		format.Bytes(cfg.MinFileSize), format.Bytes(cfg.MinFolderSize), cfg.WorkerCount)

	runOptions := scan.RunOptions{
		Config:      cfg,
		IgnoreCache: opts.full,
		LedgerPath:  filepath.Join(scan.CacheDirName, scan.LedgerName),
	}
	if !opts.noCache {
		runOptions.CachePath = filepath.Join(scan.CacheDirName, scan.CacheFileName)
	}
	if !opts.quiet {
		runOptions.OnProgress = renderProgress
	}

	if opts.full && !opts.noCache {
		// --full still writes the cache, so the next run is warm again.
		fmt.Fprintln(os.Stderr, "Full rescan: the cache is ignored for reads.")
	}

	result, err := scan.Run(runOptions)
	if err != nil {
		return err
	}
	if !opts.quiet {
		clearLine()
	}
	if result.Stats.CacheDiscardedOnOpen {
		fmt.Fprintln(os.Stderr, "Cache discarded: the scan config changed since it was written.")
	}

	printSummary(result.Snapshot)
	printCacheStats(result.Stats, opts)

	mismatch := false
	if opts.verify {
		mismatch, err = verify(cfg, result.Snapshot)
		if err != nil {
			return err
		}
	}

	if opts.dryRun {
		fmt.Fprintln(os.Stderr, "\nDry run: nothing written.")
		return nil
	}

	if _, err := snapshot.ReadIndex(opts.outDir); err != nil {
		// Not fatal: the snapshots are all still on disk. But anything the
		// index no longer lists has just become invisible to the app.
		fmt.Fprintf(os.Stderr, "! %v\n", err)
	}

	path, err := snapshot.Write(opts.outDir, result.Snapshot)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nWrote %s\n", path)

	// The snapshot is written either way: a verify mismatch is a finding about
	// the cache, not a reason to lose the scan that found it.
	if mismatch {
		return errVerifyMismatch
	}
	return nil
}

// resolveConfig layers the config file and then the CLI flags onto the
// defaults, so the most specific instruction wins.
func resolveConfig(opts options) (schema.ScanConfig, error) {
	overrides, err := config.LoadFile(opts.configPath)
	if err != nil {
		return schema.ScanConfig{}, err
	}

	if len(opts.roots) > 0 {
		overrides.Roots = &opts.roots
	}
	if opts.minFile != nil {
		overrides.MinFileSize = opts.minFile
	}
	if opts.minFolder != nil {
		overrides.MinFolderSize = opts.minFolder
	}
	if opts.workers != nil {
		overrides.WorkerCount = opts.workers
	}

	return config.Resolve(overrides), nil
}

// errVerifyMismatch reports that --verify found a difference. It is returned
// only after the snapshot has been written, and its message has already been
// printed in full.
var errVerifyMismatch = errors.New("the cached scan does not match a full scan")

// verify proves the cache on real data rather than on a fixture. It reports
// whether a difference was found.
func verify(cfg schema.ScanConfig, cached schema.ScanSnapshot) (bool, error) {
	fmt.Fprintln(os.Stderr, "\nVerifying against a full rescan…")

	control, err := scan.Run(scan.RunOptions{Config: cfg})
	if err != nil {
		return false, err
	}

	problems := scan.Diff(cached, control.Snapshot)
	if len(problems) == 0 {
		fmt.Fprintln(os.Stderr, "Verified: the cached scan matches a full scan exactly.")
		return false, nil
	}

	fmt.Fprintf(os.Stderr, "! %d difference(s) between cached and full:\n", len(problems))
	for _, problem := range problems[:min(len(problems), 20)] {
		fmt.Fprintf(os.Stderr, "  %s\n", problem)
	}
	return true, nil
}

func printSummary(snap schema.ScanSnapshot) {
	duration := time.Duration(snap.DurationMs) * time.Millisecond

	fmt.Fprintf(os.Stderr, "\nDone in %s — %s across %s files in %s directories\n",
		format.Duration(duration), format.Bytes(snap.Totals.Bytes),
		format.Count(snap.Totals.Files), format.Count(snap.Totals.Dirs))

	fmt.Fprintf(os.Stderr,
		"%s files at or above the floor · %s extensions · %s hardlinks deduped (%s)\n",
		format.Count(int64(len(snap.Files))), format.Count(int64(len(snap.Extensions))),
		format.Count(snap.Totals.DedupedFiles), format.Bytes(snap.Totals.DedupedBytes))

	if snap.Volume != nil {
		fmt.Fprintf(os.Stderr, "Volume: %s used of %s\n",
			format.Bytes(snap.Volume.UsedBytes), format.Bytes(snap.Volume.TotalBytes))
	}

	if len(snap.DuplicateGroups) > 0 {
		var reclaimable int64
		for _, group := range snap.DuplicateGroups {
			reclaimable += group.ReclaimableBytes
		}
		fmt.Fprintf(os.Stderr, "%s byte-identical groups · %s held by redundant copies\n",
			format.Count(int64(len(snap.DuplicateGroups))), format.Bytes(reclaimable))
	}

	if len(snap.Rechecked) > 0 {
		gone := 0
		for _, entry := range snap.Rechecked {
			if !entry.Present {
				gone++
			}
		}
		fmt.Fprintf(os.Stderr,
			"Ledger: %d of %d previously trashed path(s) confirmed gone\n",
			gone, len(snap.Rechecked))
	}

	for _, warning := range snap.Warnings {
		fmt.Fprintf(os.Stderr, "! %s\n", warning)
	}

	top := snap.Extensions[:min(len(snap.Extensions), 10)]
	if len(top) > 0 {
		fmt.Fprintln(os.Stderr, "\nBiggest extensions")
		for _, stat := range top {
			label := stat.Ext
			if label == "" {
				label = "(none)"
			}
			fmt.Fprintf(os.Stderr, "  %-16s %10s  %s files\n",
				label, format.Bytes(stat.TotalSize), format.Count(stat.FileCount))
		}
	}
}

func printCacheStats(stats scan.Stats, opts options) {
	if opts.noCache || opts.full {
		return
	}

	total := stats.CacheHits + stats.CacheMisses
	share := 0
	if total > 0 {
		share = int(math.Round(float64(stats.CacheHits) / float64(total) * 100))
	}

	fmt.Fprintf(os.Stderr,
		"Cache: %s of %s directories unchanged (%d%%) · %s fingerprints reused, %s read\n",
		format.Count(stats.CacheHits), format.Count(total), share,
		format.Count(int64(stats.FingerprintsReused)),
		format.Count(int64(stats.FingerprintsComputed)))
}

func renderProgress(report scan.Report) {
	if report.Phase == scan.PhaseDuplicates {
		fmt.Fprintf(os.Stderr, "\r\x1b[2K  fingerprinting %d/%d size-collision candidates",
			report.FingerprintsDone, report.FingerprintsTotal)
		return
	}

	path := report.CurrentPath
	if len(path) > 60 {
		path = "…" + path[len(path)-59:]
	}

	fmt.Fprintf(os.Stderr, "\r\x1b[2K  %s dirs · %s queued · %s files · %s  %s",
		format.Count(report.DirsDone), format.Count(report.DirsQueued),
		format.Count(report.Files), format.Bytes(report.Bytes), path)
}

func clearLine() {
	fmt.Fprint(os.Stderr, "\r\x1b[2K")
}

var sizePattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(b|kb|mb|gb|tb)?$`)

func parseSize(input string) (int64, error) {
	match := sizePattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(input)))
	if match == nil {
		return 0, fmt.Errorf("cannot parse size: %s", input)
	}

	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse size: %s", input)
	}

	multipliers := map[string]float64{
		"":   1,
		"b":  1,
		"kb": 1 << 10,
		"mb": 1 << 20,
		"gb": 1 << 30,
		"tb": 1 << 40,
	}
	return int64(math.Round(value * multipliers[match[2]])), nil
}

// expandHome resolves ~ and makes a path absolute, so the config embedded in
// the snapshot names real locations rather than whatever the shell was in.
func expandHome(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[1:]), nil
	}
	return filepath.Abs(path)
}
