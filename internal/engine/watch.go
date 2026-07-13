package engine

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/madeinoz67/go-rag/internal/watcher"
)

// Watch runs the file-change watcher on root (spec 033: the daemon auto-watches
// cfg.WatchDirs, set via the GO_RAG_WATCH_DIRS env override).
//
// It reuses the engine's OWN pipeline + db, so ingestion is single-writer-safe
// (the watcher runs in the daemon's process — never a second Pebble writer) and
// shares the async-after-ACK pipeline and seeded index with /v1/add and the query
// path. An initial ScanOnce ingests existing content under root; then it watches
// for changes (fsnotify events + a poll safety net) until ctx is cancelled.
//
// poll <= 0 falls back to the watcher's 60 s default. The pipeline is created
// lazily here (the same path Add takes), which also starts the background
// embedder — so this requires an embedding model to be configured (bundled or
// Ollama), exactly like /v1/add.
func (e *Engine) Watch(ctx context.Context, _, root, glob string, poll time.Duration) error {
	pl, err := e.pipeline()
	if err != nil {
		return err
	}
	cd := watcher.New(e.db, pl)
	// Initial scan ingests files already present under root (the "mount a
	// populated dir → everything indexed" case); the loop below catches changes.
	if _, err := cd.ScanOnce(ctx, root, glob); err != nil {
		fmt.Fprintf(os.Stderr, "go-rag watch: initial scan %s: %v\n", root, err)
	}
	return cd.Watch(ctx, root, glob, poll)
}
