package pipeline

import (
	"context"
	"encoding/json"
	"time"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// job is a unit of async indexing work: embed a document's chunks and add them to
// the FTS and vector indexes.
type job struct {
	docID  string
	chunks []model.Chunk
}

// worker drains the queue, embedding and indexing chunks, then updates the
// Document status (pending -> embedded | error).
func (p *Pipeline) worker() {
	defer p.wg.Done()
	for j := range p.queue {
		p.processJob(j)
	}
}

func (p *Pipeline) processJob(j job) {
	texts := make([]string, len(j.chunks))
	for i, c := range j.chunks {
		texts[i] = c.Content
	}

	status := StatusEmbedded
	vecs, err := p.embed.Embed(context.Background(), texts)
	if err != nil {
		status = StatusError
	} else {
		for i, c := range j.chunks {
			p.fts.Index(c.ID, map[string]string{"body": c.Content})
			if i < len(vecs) {
				p.vec.Add(c.ID, vecs[i])
			}
		}
	}
	p.markStatus(j.docID, status)
}

// markStatus reads a Document, updates its status, and writes it back. The pipeline
// mutex serialises read-modify-write across concurrent workers.
func (p *Pipeline) markStatus(docID, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	raw, ok, _ := p.db.GetWithPrefix(storage.PrefixDocument, []byte(docID))
	if !ok {
		return
	}
	var doc model.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return
	}
	doc.Status = status
	doc.UpdatedAt = time.Now().UTC()
	dbj, _ := json.Marshal(doc)
	_ = p.db.SetWithPrefix(storage.PrefixDocument, []byte(docID), dbj)
}
