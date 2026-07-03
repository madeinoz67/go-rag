package pipeline

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/events"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// reingest.go threads the OLD chunk set + their PrefixEmbedding records from
// Reprocess/ReprocessAll/ReingestPath (which delete then re-ingest) into
// processFile, so a re-ingest can emit RE_INGESTED with the chunk delta +
// preserve embeddings for UNCHANGED chunks (spec 043 / BL-010).

// reingestCapture holds the old chunk set + their raw PrefixEmbedding JSON,
// captured before DeleteDoc, consumed by processFile.
type reingestCapture struct {
	chunks []model.Chunk
	embeds map[string][]byte // old chunk ID → raw PrefixEmbedding JSON (for copy)
}

// captureReingest stores the old chunks + embeddings for a path before
// DeleteDoc. Called by Reprocess/ReprocessAll + ReingestPath; consumed by
// processFile via takeReingest.
func (p *Pipeline) captureReingest(path, docID string) {
	chunks := p.chunksOfDoc(docID)
	embeds := map[string][]byte{}
	for _, c := range chunks {
		if raw, ok, _ := p.db.GetWithPrefix(storage.PrefixEmbedding, []byte(c.ID)); ok {
			embeds[c.ID] = raw
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reingest == nil {
		p.reingest = map[string]reingestCapture{}
		p.reingestDocs = map[string]bool{}
	}
	p.reingest[filepath.Clean(path)] = reingestCapture{chunks: chunks, embeds: embeds}
	p.reingestDocs[docID] = true
}

// takeReingest pops + returns the capture for a path (if any) + "is reingest" flag.
func (p *Pipeline) takeReingest(path string) (reingestCapture, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := filepath.Clean(path)
	capt, ok := p.reingest[key]
	if ok {
		delete(p.reingest, key)
	}
	return capt, ok
}

// ReingestPath captures the old chunks + embeddings, deletes the old doc, and
// re-ingests — the single-file re-ingest path (spec 043 / BL-010). Used by the
// watcher for MODIFIED files.
func (p *Pipeline) ReingestPath(ctx context.Context, path, docID string) error {
	p.captureReingest(path, docID)
	if err := p.DeleteDoc(docID); err != nil {
		return err
	}
	_, err := p.Ingest(ctx, path, "*")
	return err
}

// preserveEmbeds copies old PrefixEmbedding records to the new chunk IDs for
// UNCHANGED chunks whose embedding model matches the current embedder (spec 043
// / BL-010 US2, T013+T014). The embedder (T015) finds the copied record →
// vec.Add + skip the Ollama call. If the model drifted or there's no old
// embedding, the chunk is left for normal embedding.
func (p *Pipeline) preserveEmbeds(oldEmbeds map[string][]byte, remap map[string]string) {
	curModel := ""
	if p.embed != nil {
		curModel = p.embed.Model()
	}
	for oldCID, newCID := range remap {
		raw, ok := oldEmbeds[oldCID]
		if !ok {
			continue // no old embedding → normal embed
		}
		// Gate (T013): only copy if the model matches (no stale vectors).
		var rec struct{ Model string }
		if json.Unmarshal(raw, &rec) != nil || rec.Model != curModel {
			continue // model drifted → normal embed
		}
		// Copy the PrefixEmbedding record (includes the vector JSON) to the new cid.
		_ = p.db.SetWithPrefix(storage.PrefixEmbedding, []byte(newCID), raw)
	}
}

// reingestEarlyReturn emits a RE_INGESTED with all-REMOVED deltas when a
// re-ingest's processFile returns early (SKIPPED/UNSUPPORTED/ERROR). The old
// doc was already deleted by DeleteDoc (with DELETED suppressed via
// reingestDocs), so without this the doc silently vanishes from a WatchDocuments
// consumer's mirror. The old chunks (captured before delete) are all REMOVED
// (no new chunks were created). (spec 043 adversarial-review finding.)
func (p *Pipeline) reingestEarlyReturn(capture reingestCapture, path string) {
	if len(capture.chunks) == 0 || p.OnEvent == nil {
		return
	}
	deltas := make([]events.ChunkDelta, len(capture.chunks))
	for i, c := range capture.chunks {
		deltas[i] = events.ChunkDelta{Change: events.ChangeRemoved, PrevChunkID: c.ID}
	}
	p.OnEvent(events.DocumentEvent{
		Type:       events.EventReingested,
		DocumentID: capture.chunks[0].DocumentID,
		SourcePath: path,
		Deltas:     deltas,
	})
}
