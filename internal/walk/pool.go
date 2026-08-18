package walk

import (
	"os"
	"sync"
)

// MaxWorkers caps the pool. Past this the walk is contending for the disk
// rather than using more of it.
const MaxWorkers = 32

// Handler is the collector side of the pool: the queue and every aggregate that
// cannot be decided from one directory alone.
//
// Every method is called from the driver goroutine, never from a worker, so an
// implementation needs no locking. CachedMtime is the exception — it runs on
// worker goroutines and must only read state that is immutable for the whole
// walk.
type Handler struct {
	// Next hands out the next directory to visit, or false when the queue is
	// empty. The queue lives with the collector because directories are
	// discovered as their parents are read.
	Next func() (string, bool)
	// CachedMtime returns the stored mtime for a directory when its row may be
	// trusted. Called from worker goroutines: read-only state only.
	CachedMtime func(path string) (float64, bool)
	// OnScanned folds in a directory that was actually read.
	OnScanned func(delta *DirDelta)
	// OnUnchanged reports a directory whose mtime still matches the cache, so
	// the stored row can be replayed. Also covers a directory that vanished
	// between being queued and being visited.
	OnUnchanged func(path string)
}

type dirResult struct {
	path string
	// nil when the directory was answered from the cache, or had vanished.
	delta *DirDelta
}

// Run walks until the queue drains.
//
// Handing out one directory at a time load-balances naturally: a node_modules
// monster does not stall one worker while eleven others idle.
func Run(scanner *Scanner, workers int, handler Handler) {
	if workers < 1 {
		workers = 1
	}
	if workers > MaxWorkers {
		workers = MaxWorkers
	}

	dispatch := make(chan string)
	results := make(chan dirResult, workers*2)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for path := range dispatch {
				results <- visit(scanner, handler, path)
			}
		}()
	}

	// One directory is held between being popped and being accepted by a
	// worker, because a select that does not fire must not drop it.
	var pending string
	var havePending bool
	outstanding := 0

	for {
		if !havePending {
			pending, havePending = handler.Next()
		}
		if !havePending && outstanding == 0 {
			break
		}

		var send chan<- string
		if havePending {
			send = dispatch
		}

		select {
		case send <- pending:
			havePending = false
			outstanding++
		case result := <-results:
			outstanding--
			if result.delta != nil {
				handler.OnScanned(result.delta)
			} else {
				handler.OnUnchanged(result.path)
			}
		}
	}

	close(dispatch)
	wg.Wait()
}

// visit is the whole of a worker's decision: one lstat, then either a cache
// answer or a full read.
func visit(scanner *Scanner, handler Handler, path string) dirResult {
	info, err := os.Lstat(path)
	if err != nil {
		// Vanished between being queued and being visited.
		return dirResult{path: path}
	}
	stat, ok := statOf(info)
	if !ok {
		return dirResult{path: path}
	}

	// The whole point of the cache: an unchanged directory costs one lstat, no
	// readdir and no per-file stats.
	if handler.CachedMtime != nil {
		if cached, ok := handler.CachedMtime(path); ok && cached == stat.MtimeMs {
			return dirResult{path: path}
		}
	}

	return dirResult{path: path, delta: scanner.ScanDir(path, stat.MtimeMs)}
}
