package engine

import (
	"context"
	"fmt"
	"strings"
)

// delete.go implements spec 050 (Slice 4) US2: DeleteDoc — remove a document
// and all its chunks/embeddings from the index by content-addressed document ID.
// It is a thin read-then-delegate wrapper over pipeline.Pipeline.DeleteDoc (the
// spec 044 per-doc lock + Pebble deletes + the live FTS/Vector update live there)
// so every deletion trigger — watcher, reprocess, and now every transport — flows
// through the same chokepoint (the DELETED lifecycle event is published there).
//
// Index-only (FR-011): the source file on disk is never modified or deleted.
//
// Delete is NOT async-after-ACK (Principle IV governs ingest ACKs): it is a
// bounded synchronous read-modify-write of the live index, distinct from the
// ACK-then-embed ingest path. The doc is gone when this call returns, so the
// transport projections return 204 (REST) / empty (gRPC) / a one-line render
// (MCP) rather than an IngestSummary.

// DeleteDoc removes the document docID and all of its chunks/embeddings from the
// index. The source file on disk is untouched (index-only, FR-011).
//
// Errors:
//   - ErrInvalid: docID is empty or whitespace-only.
//   - ErrNotFound: no document with this id lives in the bound vault.
//
// The existence check makes an unknown id a real error (Pipeline.DeleteDoc is a
// silent no-op on a missing record by design — the watcher's idempotent re-scans
// rely on it; the operator-facing surface needs a 404 instead). The check is a
// single point Get over prefix 0x02; the benign TOCTOU (doc vanishes between the
// check and the delegate) collapses to a no-op delete, which is correct either
// way — the doc is gone.
func (e *Engine) DeleteDoc(_ context.Context, docID string) error {
	ws := e.db.ResolveVaultPrefix("default")
	if strings.TrimSpace(docID) == "" {
		return fmt.Errorf("document_id is required: %w", ErrInvalid)
	}
	if _, ok := lookupDoc(e.db, ws, docID); !ok {
		return fmt.Errorf("%w: document %s", ErrNotFound, docID)
	}
	p, err := e.pipeline()
	if err != nil {
		return err
	}
	return p.DeleteDoc(docID)
}
