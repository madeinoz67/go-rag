package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// batch_get_chunks.go implements spec 038 (bridge backlog BL-003): BatchGetChunks
// — resolve up to 100 content-addressed chunk IDs in a single call, returning one
// result per requested ID in request order. The bridge sibling of GetChunk (one,
// spec 035) and GetChunkContext (a window, spec 037): it collapses N GetChunk
// round-trips into one batch — the direct enabler of the MuninnDB bridge's
// BatchRemember bulk-sync path.
//
// The key difference from GetChunk/GetChunkContext is the PER-ID ERROR MODEL
// (partial success): a missing or cross-vault chunk_id yields a result entry
// with a zero-value chunk and Err="not found" — the call itself never fails for
// one bad id. Only structurally invalid requests (empty list, >100 ids, or any
// empty/whitespace element) fail at the call level with ErrInvalid.
//
// Mechanically it is a loop of lookupChunk point-Gets over prefix 0x03 — at most
// 100 sub-millisecond Pebble point-Gets in one caller round-trip. Chunks are
// immutable once written (content-addressed, Constitution II), so the N point-
// Gets observe a consistent view with no snapshot/transaction (research.md R1).
//
// BatchGetChunks is a PURE READ. It composes the existing lookupChunk/lookupDoc
// helpers (spec 035), introduces no new stored state, and changes no on-disk
// layout (Constitution II; no migration).

// maxBatchGetChunks is the inclusive cap on requested chunk_ids (spec 038 FR-004).
const maxBatchGetChunks = 100

// MaxBatchGetChunks returns the inclusive maximum number of chunk_ids per request
// (spec 038 FR-004). Requests above this return ErrInvalid (INVALID_ARGUMENT);
// never silently truncated above the cap.
func MaxBatchGetChunks() int { return maxBatchGetChunks }

// BatchItem is one positional entry in a BatchGetChunks result (spec 038): the
// requested chunk_id, its resolved chunk (zero-value when not found — signalled
// by a non-empty Err), the parent document/source (zero-valued when absent —
// orphan-tolerant, mirroring GetChunk), and a non-empty Err when this id failed
// (currently only "not found"). Mirrors ChunkResult (spec 035) per entry.
type BatchItem struct {
	ChunkID  string
	Chunk    model.Chunk // zero-value when not found (Err != "")
	Document model.Document
	Source   model.Source
	Err      string // "" (found) or "not found"
}

// BatchResult is the ordered result of BatchGetChunks — one BatchItem per
// requested chunk_id, in request order (positional, 1:1, including duplicates
// and missing ids).
type BatchResult struct {
	Results []BatchItem
}

// BatchGetChunks resolves up to 100 content-addressed chunk_ids in a single call,
// returning one result per requested id in request order. Live ids carry their
// full chunk + parent document; missing/cross-vault ids carry a zero-value chunk
// and Err="not found". The call itself only errors for structurally invalid input.
//
// Errors:
//   - ErrInvalid: chunk_ids is empty, longer than 100, or contains any empty/
//     whitespace-only element. Validated before any lookup.
//
// Missing/cross-vault ids are NOT an error: they surface as BatchItem.Err="not
// found" (partial success — the whole point of a batch distinct from looping
// GetChunk). Cross-vault ids are simply absent from this single-vault store →
// "not found" (no leakage), mirroring GetChunk (spec 035 FR-003). Duplicate ids
// are resolved independently per position (no de-duplication). An orphan chunk
// (chunk present, parent document absent) is not an error: the chunk is returned
// with a zero-valued Document/Source.
func (e *Engine) BatchGetChunks(vault string, chunkIDs []string) (*BatchResult, error) {
	ws := e.db.ResolveVaultPrefix(vault)
	if len(chunkIDs) == 0 {
		return nil, fmt.Errorf("chunk_ids is required: %w", ErrInvalid)
	}
	if len(chunkIDs) > maxBatchGetChunks {
		return nil, fmt.Errorf("chunk_ids length %d exceeds max %d: %w", len(chunkIDs), maxBatchGetChunks, ErrInvalid)
	}
	for _, id := range chunkIDs {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("chunk_ids contains an empty/whitespace element: %w", ErrInvalid)
		}
	}

	results := make([]BatchItem, 0, len(chunkIDs))
	for _, id := range chunkIDs {
		c, ok := lookupChunk(e.db, ws, id)
		if !ok {
			results = append(results, BatchItem{ChunkID: id, Err: "not found"})
			continue
		}
		item := BatchItem{ChunkID: id, Chunk: c}
		// Parent document + source — tolerant of an orphan chunk (zero-valued,
		// not an error). Mirrors GetChunk (spec 035).
		if d, ok := lookupDoc(e.db, ws, c.DocumentID); ok {
			item.Document = d
			if raw, ok, _ := e.db.Get(keys.SourceKey(ws, d.SourceID)); ok {
				_ = json.Unmarshal(raw, &item.Source)
			}
		}
		results = append(results, item)
	}
	return &BatchResult{Results: results}, nil
}
