package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// get_document.go implements spec 047 US2 (Documents detail view): GetDocument —
// resolve a content-addressed document_id to its full stored document plus the
// source it was ingested from (so the detail view can show source_path, which
// the listing deliberately omits for performance). It is the document analogue
// of GetChunk (spec 035).
//
// Pure read: two Pebble point Gets over the EXISTING 0x02 / 0x01 prefixes
// (document, then source). No scan, corpus-size-independent, no new stored
// state, no migration (Constitution storage-discipline + schema-evolution).

// DocumentResult is the engine projection returned by GetDocument: the resolved
// document and (when present) its source. Source is zero-valued when absent —
// GetDocument tolerates an orphan document (source removed) rather than failing.
type DocumentResult struct {
	Document model.Document
	Source   model.Source
}

// GetDocument resolves a document_id to its full stored document plus the source
// it was ingested from, in a single call. Constant-time: two point Gets, no scan.
//
// Errors:
//   - ErrInvalid: docID is empty or whitespace-only.
//   - ErrNotFound: no document with this id lives in the bound vault (also the
//     cross-vault-isolation path — a foreign id is simply absent here).
//
// A document whose source record is missing is NOT an error: the document is
// returned with a zero-valued Source (source_path empty).
func (e *Engine) GetDocument(docID string) (*DocumentResult, error) {
	if strings.TrimSpace(docID) == "" {
		return nil, fmt.Errorf("document_id is required: %w", ErrInvalid)
	}
	d, ok := lookupDoc(e.db, docID)
	if !ok {
		return nil, fmt.Errorf("%w: document %s", ErrNotFound, docID)
	}
	res := &DocumentResult{Document: d}
	// Optional source read for source_path (absolute source dir). Constant-time
	// point Get over prefix 0x01; tolerant of a missing/bad row.
	if raw, ok, _ := e.db.GetWithPrefix(storage.PrefixSource, []byte(d.SourceID)); ok {
		_ = json.Unmarshal(raw, &res.Source)
	}
	return res, nil
}
