package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// get_chunk.go implements spec 035 (bridge backlog BL-001): GetChunk — resolve a
// content-addressed chunk_id to its full stored chunk plus its parent document's
// metadata. It is the primitive that makes chunk_id a usable foreign key from
// MuninnDB (or any client) back into go-rag, unblocking the bridge's
// ActivateWithRAG pattern.
//
// GetChunk is a PURE READ. It composes the existing lookupChunk / lookupDoc
// helpers (and one optional source read) into one engine method — two/three
// Pebble point Gets over the EXISTING 0x03 / 0x02 / 0x01 prefixes. No scan,
// corpus-size-independent, no new stored state, no migration (Constitution
// storage-discipline + schema-evolution). It mirrors ReleaseChunk / ResetChunk
// in arity (a single chunkID string) — there is NO vault parameter, because the
// engine is single-vault-per-process (a chunk_id from another vault is simply
// absent here, which is also how cross-vault isolation — FR-003 — falls out).

// ChunkResult is the engine projection returned by GetChunk: the resolved chunk,
// its parent document, and the source the document was ingested from. Document
// and Source are zero-valued when absent — GetChunk tolerates an orphan chunk
// (a present chunk whose parent document/source was removed) rather than failing,
// mirroring ListChunks' tolerant FilePath="" behaviour (eval/run.go).
type ChunkResult struct {
	Chunk    model.Chunk
	Document model.Document
	Source   model.Source
}

// GetChunk resolves a content-addressed chunk_id to its full stored chunk plus
// its parent document's metadata (and, for source context, the source path) in a
// single call. Constant-time: two/three Pebble point Gets, no scan.
//
// Errors:
//   - ErrInvalid: chunkID is empty or whitespace-only.
//   - ErrNotFound: no chunk with this id lives in the bound vault. This is also
//     the cross-vault-isolation path (FR-003): a chunk_id that belongs to a
//     different vault is simply not present in this single-vault store, so it
//     resolves to ErrNotFound and the other vault's chunk is never disclosed.
//
// An orphan chunk (chunk present, parent document absent) is NOT an error: the
// chunk is returned with a zero-valued Document/Source.
func (e *Engine) GetChunk(chunkID string) (*ChunkResult, error) {
	if strings.TrimSpace(chunkID) == "" {
		return nil, fmt.Errorf("chunk_id is required: %w", ErrInvalid)
	}
	c, ok := lookupChunk(e.db, chunkID)
	if !ok {
		return nil, fmt.Errorf("%w: chunk %s", ErrNotFound, chunkID)
	}
	res := &ChunkResult{Chunk: c}
	// Parent document — tolerant: an orphan chunk (doc deleted/stale) yields a
	// zero Document, not an error. The chunk carries DocumentID inline, so this
	// is one point Get with no scan.
	if d, ok := lookupDoc(e.db, c.DocumentID); ok {
		res.Document = d
		// Optional source read for source_path (absolute source dir). Constant-
		// time point Get over prefix 0x01; tolerant of a missing/bad row.
		if raw, ok, _ := e.db.GetWithPrefix(storage.PrefixSource, []byte(d.SourceID)); ok {
			_ = json.Unmarshal(raw, &res.Source)
		}
	}
	return res, nil
}
