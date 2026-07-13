package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// get_chunk_context.go implements spec 037 (bridge backlog BL-002): GetChunkContext
// — resolve a content-addressed chunk_id to that chunk plus up to `window`
// neighbours on each side in one call, so the bridge's ActivateWithRAG pattern
// (and any context-expansion consumer) stops chaining N GetChunk calls.
//
// The window is fetched by following the per-document linked list
// (Chunk.PreviousChunkID / NextChunkID, written atomically at ingest — spec 015):
// lookupChunk(target), then up to `window` hops back and forward. That is at most
// 1 + 2*window Pebble point-gets (max 21 at window=10), each sub-millisecond — one
// caller round-trip returning a consistent view of an existing document's chunks.
// Chunks are keyed by content-addressed chunkID (prefix 0x03), not by
// (DocumentID, ChunkIndex), so there is no range key to fetch the window in a
// single read; the linked list exists for exactly this (research R1).
//
// GetChunkContext is a PURE READ. It composes the existing lookupChunk / lookupDoc
// helpers (spec 035), introduces no new stored state, and changes no on-disk
// layout (Constitution II; no migration).

// ContextResult is the engine projection returned by GetChunkContext: the ordered
// context window centred on the requested chunk, the requested chunk's index
// within that window, and the parent document (+ optional source for source_path).
// Mirrors ChunkResult (spec 035). Document and Source are zero-valued when absent
// — GetChunkContext tolerates an orphan chunk (a present chunk whose parent
// document/source was removed) rather than failing, mirroring GetChunk.
type ContextResult struct {
	Chunks      []model.Chunk // ordered [predecessors…] [target] [successors…]
	TargetIndex int           // index of the requested chunk within Chunks
	Document    model.Document
	Source      model.Source
}

// Window bounds for GetChunkContext (spec 037 FR-003/FR-004). The default is
// applied by the transport layer when the caller omits `window`; the engine
// treats 0 as "exactly the target" (equivalent to GetChunk).
const (
	defaultChunkContextWindow = 2
	maxChunkContextWindow     = 10
)

// DefaultChunkContextWindow returns the default window applied by transports when
// the caller omits it (spec 037 FR-003). Exposed for the transport adapters.
func DefaultChunkContextWindow() int { return defaultChunkContextWindow }

// MaxChunkContextWindow returns the maximum allowed window (spec 037 FR-004).
// Requests above this return ErrInvalid (INVALID_ARGUMENT); it is never silently
// truncated above the cap.
func MaxChunkContextWindow() int { return maxChunkContextWindow }

// GetChunkContext resolves a content-addressed chunk_id to that chunk plus up to
// `window` predecessor and successor chunks (following the per-document linked
// list) in a single call, with the requested chunk at TargetIndex and the parent
// document's metadata alongside. Constant-time-per-hop: at most 1+2*window
// Pebble point-Gets, no scan.
//
// `window` is taken as-is in [0, maxChunkContextWindow]; 0 returns exactly the
// target (equivalent to GetChunk). Transports apply the default when the caller
// omits it.
//
// Errors:
//   - ErrInvalid: chunkID is empty/whitespace-only, or window is <0 or >10.
//   - ErrNotFound: no chunk with this id lives in the bound vault. This is also
//     the cross-vault-isolation path (FR-006): a chunk_id that belongs to a
//     different vault is simply not present in this single-vault store, so it
//     resolves to ErrNotFound and the other vault's chunk is never disclosed.
//
// An orphan chunk (chunk present, parent document absent) is NOT an error: the
// window is returned with a zero-valued Document/Source. A broken linked-list
// hop (a neighbour ID that does not resolve) is tolerated — the unbroken run up
// to the requested window is returned, not an error.
func (e *Engine) GetChunkContext(vault, chunkID string, window int) (*ContextResult, error) {
	ws := e.db.ResolveVaultPrefix(vault)
	if strings.TrimSpace(chunkID) == "" {
		return nil, fmt.Errorf("chunk_id is required: %w", ErrInvalid)
	}
	if window < 0 || window > maxChunkContextWindow {
		return nil, fmt.Errorf("window must be 0..%d, got %d: %w", maxChunkContextWindow, window, ErrInvalid)
	}
	target, ok := lookupChunk(e.db, ws, chunkID)
	if !ok {
		return nil, fmt.Errorf("%w: chunk %s", ErrNotFound, chunkID)
	}

	// Walk backward via PreviousChunkID (collect in reverse, then they're ascending).
	predecessors := make([]model.Chunk, 0, window)
	cur := target
	for i := 0; i < window && cur.PreviousChunkID != ""; i++ {
		p, ok := lookupChunk(e.db, ws, cur.PreviousChunkID)
		if !ok {
			break // defensive: linked list writes are atomic at ingest; degrade gracefully
		}
		predecessors = append([]model.Chunk{p}, predecessors...) // prepend → ascending document order
		cur = p
	}
	// Walk forward via NextChunkID.
	successors := make([]model.Chunk, 0, window)
	cur = target
	for i := 0; i < window && cur.NextChunkID != ""; i++ {
		nx, ok := lookupChunk(e.db, ws, cur.NextChunkID)
		if !ok {
			break
		}
		successors = append(successors, nx)
		cur = nx
	}

	chunks := make([]model.Chunk, 0, len(predecessors)+1+len(successors))
	chunks = append(chunks, predecessors...)
	chunks = append(chunks, target)
	chunks = append(chunks, successors...)

	res := &ContextResult{Chunks: chunks, TargetIndex: len(predecessors)}
	// Parent document + source — tolerant of an orphan chunk (zero-valued, not an
	// error). Mirrors GetChunk (spec 035): one lookupDoc + an optional source Get.
	if d, ok := lookupDoc(e.db, ws, target.DocumentID); ok {
		res.Document = d
		if raw, ok, _ := e.db.Get(keys.SourceKey(ws, d.SourceID)); ok {
			_ = json.Unmarshal(raw, &res.Source)
		}
	}
	return res, nil
}
