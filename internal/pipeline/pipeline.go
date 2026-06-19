// Package pipeline orchestrates ingestion (PRD §4.4):
//
//	walk → read → hash → dedup → store(Sync, <10ms) → ACK → [embed + index async]
//
// The async-after-ACK write model keeps the write path under 10ms (PRD §10.1);
// all embedding and indexing work happens on background workers after the user is
// acknowledged.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/reader"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// Document lifecycle statuses.
const (
	StatusPending  = "pending"
	StatusEmbedded = "embedded"
	StatusError    = "error"
)

// Result summarises one Ingest run.
type Result struct {
	New, Skipped, Errors int
}

// Pipeline runs the ingest pipeline over paths, storing synchronously and indexing
// asynchronously.
type Pipeline struct {
	db       *storage.DB
	splitter *chunk.Splitter
	embed    embed.Embedder
	fts      *index.FTS
	vec      *index.Vector

	queue chan job
	wg    sync.WaitGroup
	mu    sync.Mutex // guards markStatus read-modify-write
}

// New returns a Pipeline with background indexing workers started. Call Close to
// drain pending work before exit.
func New(db *storage.DB, sp *chunk.Splitter, em embed.Embedder, fts *index.FTS, vec *index.Vector) *Pipeline {
	p := &Pipeline{
		db:       db,
		splitter: sp,
		embed:    em,
		fts:      fts,
		vec:      vec,
		queue:    make(chan job, 64),
	}
	const numWorkers = 2
	for i := 0; i < numWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// Close drains the async queue and stops workers.
func (p *Pipeline) Close() {
	close(p.queue)
	p.wg.Wait()
}

// Ingest walks root, processing every file whose base name matches glob.
func (p *Pipeline) Ingest(ctx context.Context, root, glob string) (Result, error) {
	reader.DefaultReaders()
	res := Result{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			res.Errors++
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".go-rag" {
				return filepath.SkipDir // never ingest the database's own directory
			}
			return nil
		}
		if !matchGlob(filepath.Base(path), glob) {
			return nil
		}
		switch st, _ := p.processFile(ctx, path); st {
		case "NEW":
			res.New++
		case "SKIPPED":
			res.Skipped++
		case "ERROR":
			res.Errors++
		}
		return nil
	})
	return res, err
}

// processFile reads, dedups by content hash, chunks, stores synchronously, then
// enqueues chunks for async embedding+indexing. Returns NEW/SKIPPED/ERROR.
func (p *Pipeline) processFile(ctx context.Context, path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "ERROR", err
	}
	ch := model.ContentHash(raw)

	// Idempotent dedup: content hash already ingested -> skip (Principle II).
	if _, ok, _ := p.db.GetWithPrefix(storage.PrefixContentHash, []byte(ch)); ok {
		return "SKIPPED", nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	rd, ok := reader.Get(ext)
	if !ok {
		return "ERROR", fmt.Errorf("no reader for extension %q", ext)
	}
	content, metadata, err := rd.Read(ctx, raw, path)
	if err != nil {
		return "ERROR", err
	}

	docID := model.GenerateID(content, mimeType(ext), metadata)
	now := time.Now().UTC()
	doc := model.Document{
		ID:          docID,
		FilePath:    path,
		FileName:    filepath.Base(path),
		FileType:    extType(ext),
		MimeType:    mimeType(ext),
		ContentHash: ch,
		Metadata:    metadata,
		FileSize:    int64(len(raw)),
		IngestedAt:  now,
		UpdatedAt:   now,
		Status:      StatusPending,
	}

	segs := p.splitter.Split(content)
	chunks := make([]model.Chunk, len(segs))
	for i, s := range segs {
		cid := model.GenerateID(s.Text, doc.MimeType, map[string]any{"doc": docID, "idx": i})
		chunks[i] = model.Chunk{
			ID:           cid,
			DocumentID:   docID,
			Content:      s.Text,
			ChunkIndex:   i,
			TotalChunks:  len(segs),
			StartCharIdx: s.StartCharIdx,
			EndCharIdx:   s.EndCharIdx,
			TokenCount:   s.TokenCount,
			CreatedAt:    now,
		}
	}
	doc.ChunkCount = len(chunks)

	// Synchronous, durable store -> the <10ms ACK (Principle IV).
	if err := p.storeDocument(doc, chunks, ch); err != nil {
		return "ERROR", err
	}

	// Async embed + index after the ACK.
	p.queue <- job{docID: docID, chunks: chunks}
	return "NEW", nil
}

// storeDocument writes the Document, its Chunks, and the dedup/path indexes with
// Sync durability.
func (p *Pipeline) storeDocument(doc model.Document, chunks []model.Chunk, contentHash string) error {
	dbj, _ := json.Marshal(doc)
	if err := p.db.SetWithPrefix(storage.PrefixDocument, []byte(doc.ID), dbj); err != nil {
		return err
	}
	if err := p.db.SetWithPrefix(storage.PrefixContentHash, []byte(contentHash), []byte(doc.ID)); err != nil {
		return err
	}
	if err := p.db.SetWithPrefix(storage.PrefixPathDoc, []byte(doc.FilePath), []byte(doc.ID)); err != nil {
		return err
	}
	for _, c := range chunks {
		cj, _ := json.Marshal(c)
		if err := p.db.SetWithPrefix(storage.PrefixChunk, []byte(c.ID), cj); err != nil {
			return err
		}
	}
	return nil
}

// CountDocuments returns the number of stored Documents (0x02 prefix).
func (p *Pipeline) CountDocuments() int {
	n := 0
	_ = p.db.PrefixScanByte(storage.PrefixDocument, func(_, _ []byte) bool {
		n++
		return true
	})
	return n
}

func matchGlob(name, glob string) bool {
	if glob == "" || glob == "*" {
		return true
	}
	m, _ := filepath.Match(glob, name)
	return m
}

func extType(ext string) string {
	switch ext {
	case ".pdf":
		return "pdf"
	case ".txt", ".log", ".csv":
		return "text"
	case ".md", ".markdown":
		return "markdown"
	case ".docx":
		return "docx"
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".png":
		return "png"
	}
	return "unknown"
}

func mimeType(ext string) string {
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".txt", ".log", ".csv":
		return "text/plain"
	case ".md", ".markdown":
		return "text/markdown"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	}
	return "application/octet-stream"
}
