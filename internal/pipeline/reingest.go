package pipeline

import (
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/model"
)

// reingest.go threads the OLD chunk set from Reprocess/ReprocessAll (which delete
// then re-ingest) into processFile, so a re-ingest can emit RE_INGESTED with the
// chunk delta instead of INGESTED+DELETED (spec 043 / BL-010). The map is
// transient — populated by Reprocess before DeleteDoc, drained by processFile.
// Reprocess fully precedes Ingest, so populate/consume are sequential; p.mu
// guards the map for safety against future concurrent-ingest changes.

// captureReingest stores the old chunk set for a path before DeleteDoc runs.
// Called by Reprocess/ReprocessAll. The chunksOfDoc scan happens outside the
// lock (I/O); only the map write is under p.mu.
func (p *Pipeline) captureReingest(path, docID string) {
	old := p.chunksOfDoc(docID)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reingest == nil {
		p.reingest = map[string][]model.Chunk{}
		p.reingestDocs = map[string]bool{}
	}
	p.reingest[filepath.Clean(path)] = old
	p.reingestDocs[docID] = true // spec 043 / BL-010 FR-005: suppress DELETED for this re-ingest
}

// takeReingest pops + returns the old chunk set for a path (if captured) + an
// "is a re-ingest" flag. Called once at the top of processFile so the entry is
// drained even on an early return (SKIPPED/UNSUPPORTED/ERROR). The two-value
// lookup distinguishes "path not in map" (first ingest → INGESTED) from "in map,
// empty old set" (re-ingest of a doc that had no chunks → RE_INGESTED).
func (p *Pipeline) takeReingest(path string) ([]model.Chunk, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := filepath.Clean(path)
	old, ok := p.reingest[key]
	if ok {
		delete(p.reingest, key)
	}
	return old, ok
}
