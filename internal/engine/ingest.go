package engine

import (
	"context"
	"fmt"

	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/watcher"
)

// fromResult maps a pipeline.Result to an IngestSummary. Unsupported file types
// are folded into Skipped (they are not errors).
func fromResult(r pipeline.Result) IngestSummary {
	return IngestSummary{
		New:     r.New,
		Skipped: r.Skipped + r.Unsupported,
		Errors:  r.Errors,
	}
}

// Add ingests a file or directory path. It ACKs as soon as the durable store
// commit completes (async-after-ACK, Principle IV); embedding and indexing
// continue on the engine's background workers after this call returns. Call
// Close to wait for that background work to finish.
func (e *Engine) Add(ctx context.Context, path string) (*IngestSummary, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required: %w", ErrInvalid)
	}
	p, err := e.pipeline()
	if err != nil {
		return nil, err
	}
	res, err := p.Ingest(ctx, path, "*")
	if err != nil {
		return nil, err
	}
	s := fromResult(res)
	return &s, nil
}

// Scan runs a single change-detection pass over the configured watch directory
// and applies adds/modifications/deletions. Like Add, it ACKs after the durable
// store commits and embeds asynchronously.
func (e *Engine) Scan(ctx context.Context) (*IngestSummary, error) {
	root := "."
	if len(e.cfg.WatchDirs) > 0 && e.cfg.WatchDirs[0] != "" {
		root = e.cfg.WatchDirs[0]
	}
	p, err := e.pipeline()
	if err != nil {
		return nil, err
	}
	cd := watcher.New(e.db, p)
	changes, err := cd.ScanOnce(ctx, root, "*")
	if err != nil {
		return nil, err
	}
	s := IngestSummary{}
	for _, c := range changes {
		switch c.Kind {
		case "NEW":
			s.New++
		case "MODIFIED":
			s.Modified++
		case "DELETED":
			s.Deleted++
		}
	}
	return &s, nil
}

// Reprocess force-re-ingests a path, bypassing SHA-256 dedup (applies the
// current reader/embedder).
func (e *Engine) Reprocess(ctx context.Context, path string) (*IngestSummary, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required: %w", ErrInvalid)
	}
	p, err := e.pipeline()
	if err != nil {
		return nil, err
	}
	res, err := p.Reprocess(ctx, path, "*")
	if err != nil {
		return nil, err
	}
	s := fromResult(res)
	return &s, nil
}

// Migrate re-embeds documents whose embeddings use a different model than the
// configured one. If everything is current, returns a zero summary.
func (e *Engine) Migrate(ctx context.Context) (*IngestSummary, error) {
	current := e.cfg.EmbeddingModel
	stats := pipeline.EmbeddingModelStats(e.db)
	stale := 0
	for m, n := range stats {
		if m != current {
			stale += n
		}
	}
	if stale == 0 {
		return &IngestSummary{}, nil
	}
	// H06/spec 016: a model change invalidates every cached result and every
	// cached query embedding. Flush both caches up front (the ongoing re-embed
	// also bumps the index epoch via processJob, so the result cache stays
	// invalid as vectors land).
	e.flushCaches()
	p, err := e.pipeline()
	if err != nil {
		return nil, err
	}
	res, err := p.ReprocessAll(ctx)
	if err != nil {
		return nil, err
	}
	s := fromResult(res)
	return &s, nil
}
