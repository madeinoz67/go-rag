package muninn

import (
	"context"
	"log/slog"
	"time"

	"github.com/madeinoz67/go-rag/internal/model"
)

// ChunkSource reads go-rag's stored corpus for the (US2) backfill walker. The
// Engine supplies an adapter over ListDocuments/ListChunks; tests use a fake. The
// bridge package never imports the engine (it would cycle), so this interface is
// the seam — Extension by Interface (constitution Principle V).
type ChunkSource interface {
	// ListDocuments returns every (settled) document in the source vault.
	ListDocuments() ([]model.Document, error)
	// Chunks returns one document's chunks.
	Chunks(docID string) ([]model.Chunk, error)
}

// runBackfill is the US2 auto-on-enable corpus walker. It reads the source vault's
// documents + chunks and enqueues each doc's chunks as a ModeBackfill promotion.
// Called from Bridge.Start when BridgeBackfillAutoOnEnable is set + a ChunkSource
// is wired. Respects Pause between documents (park-checks the flag — pause holds
// the walk in place; resume continues). Stops when ctx is cancelled (Bridge.Stop).
//
// v1 is not resumable across daemon restarts — re-walking is free under the
// content-addressed UPSERT no-op (every already-promoted chunk is left alone).
func (b *Bridge) runBackfill(ctx context.Context) {
	b.bfMu.Lock()
	b.backfill.Running = true
	b.backfill.StartedAt = time.Now().UnixNano()
	b.backfill.Paused = b.paused.Load()
	b.bfMu.Unlock()
	defer func() {
		b.bfMu.Lock()
		b.backfill.Running = false
		b.bfMu.Unlock()
		slog.Info("bridge: backfill ended", "cursor", b.snapshotCursor(), "promoted", b.proc.Stats().Promoted)
	}()

	docs, err := b.source.ListDocuments()
	if err != nil {
		slog.Warn("bridge: backfill list documents failed", "err", err)
		return
	}
	slog.Info("bridge: backfill started", "documents", len(docs))

	for _, doc := range docs {
		// Stop on shutdown.
		if ctx.Err() != nil {
			return
		}
		// Park while paused (FR-014). A pause holds the walk in place; resume
		// continues from the next document. No busy-spin.
		for b.paused.Load() {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		chunks, err := b.source.Chunks(doc.ID)
		if err != nil {
			slog.Warn("bridge: backfill chunks — skipping doc", "doc", doc.ID, "err", err)
			continue
		}
		if len(chunks) == 0 {
			continue
		}
		// Enqueue directly via the processor (NOT Bridge.Submit, which pause-gates
		// ModeBackfill). The walker is the authoritative pause authority — it
		// park-checked above — so it must not double-drop at the Submit gate.
		b.proc.Submit(PromotionJob{
			Vault: b.mapper.TargetVault,
			Items: b.mapper.MapAll(chunks, doc),
			Mode:  ModeBackfill,
		})
		b.bfMu.Lock()
		b.backfill.Cursor = doc.ID
		b.bfMu.Unlock()
	}
}

// snapshotCursor returns the current backfill cursor (status helper).
func (b *Bridge) snapshotCursor() string {
	b.bfMu.Lock()
	defer b.bfMu.Unlock()
	return b.backfill.Cursor
}
