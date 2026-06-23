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

// Progress is an optional callback invoked after each file is processed during
// Ingest/Reprocess/ReprocessAll. done is 1-based; total is the pre-counted number
// of ingestible files (0 when no callback is set). status is NEW/SKIPPED/ERROR.
type Progress func(done, total int, path, status string)

// Result summarises one Ingest run.
type Result struct {
	New, Skipped, Unsupported, Errors int
}

// Pipeline runs the ingest pipeline over paths, storing synchronously and indexing
// asynchronously.
type Pipeline struct {
	db       *storage.DB
	splitter *chunk.Splitter
	embed    embed.Embedder
	prefixer *embed.Prefixer // H07: applies the document-role instruction prefix; nil = no prefixing
	fts      *index.FTS
	vec      *index.Vector

	queue chan job
	wg    sync.WaitGroup
	mu    sync.Mutex // guards markStatus read-modify-write

	// OnProgress, if non-nil, is called after each file is processed during
	// Ingest/Reprocess/ReprocessAll (enables a CLI progress bar).
	OnProgress Progress

	// OnChange, if non-nil, is called whenever the shared in-memory index is
	// mutated (chunk added in storeDocument, vector added in processJob, chunks
	// removed in DeleteDoc). The Engine binds it to its epoch-bumper so the
	// query result cache can invalidate on corpus change (audit H06/spec 016).
	// Set once, under the engine's pipeMu, before any job flows.
	OnChange func()

	// OnFirstEmbed, if non-nil, is called after a document's chunks are embedded
	// with the embedding profile (model, dim, convention). The Engine uses it to
	// persist the corpus baseline on first embed (audit H11/spec 017); it no-ops
	// once a baseline exists. Keeps internal/pipeline free of the engine + the
	// baseline store (Principle V).
	OnFirstEmbed func(model string, dim int, convention string)
}

// indexChanged fires the OnChange callback when set. Centralizing the nil guard
// keeps the mutation sites one-liners and means a pipeline constructed without a
// bound callback (e.g. some tests) simply skips the bump.
func (p *Pipeline) indexChanged() {
	if p.OnChange != nil {
		p.OnChange()
	}
}

// New returns a Pipeline with background indexing workers started. Call Close to
// drain pending work before exit. pre is the instruction-prefix resolver (audit
// H07); pass a no-op prefixer (e.g. from Config.Prefixer()) so documents are
// embedded with the model's document-role prefix. nil disables prefixing.
func New(db *storage.DB, sp *chunk.Splitter, em embed.Embedder, fts *index.FTS, vec *index.Vector, pre *embed.Prefixer) *Pipeline {
	p := &Pipeline{
		db:       db,
		splitter: sp,
		embed:    em,
		prefixer: pre,
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

// TODO(T047): add Reprocess(root, glob) — force re-ingest of an already-ingested
// directory, bypassing the SHA-256 content-hash dedup (delete each tracked doc
// under root via DeleteDoc, then re-run ingest) so reader/embedder changes apply
// without `rm -rf .go-rag`. See specs/001-local-rag-database/tasks.md (Future Work).

// Ingest walks root, processing every file whose base name matches glob. If
// p.OnProgress is set, it pre-counts ingestible files and fires the callback per
// file (done, total, path, status).
func (p *Pipeline) Ingest(ctx context.Context, root, glob string) (Result, error) {
	reader.DefaultReaders()
	total := 0
	if p.OnProgress != nil {
		total = p.countFiles(root, glob)
	}
	res := Result{}
	done := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
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
		st, _ := p.processFile(ctx, path)
		switch st {
		case "NEW":
			res.New++
		case "SKIPPED":
			res.Skipped++
		case "UNSUPPORTED":
			res.Unsupported++
		case "ERROR":
			res.Errors++
		}
		done++
		if p.OnProgress != nil {
			p.OnProgress(done, total, path, st)
		}
		return nil
	})
	return res, err
}

// countFiles returns the number of files Ingest will process under root — every
// glob-matching file outside .go-rag (including unsupported types, which Ingest
// attempts and reports as ERROR). Mirrors Ingest's walk so the progress bar total
// matches the done counter exactly.
func (p *Pipeline) countFiles(root, glob string) int {
	n := 0
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".go-rag" {
				return filepath.SkipDir
			}
			return nil
		}
		if !matchGlob(filepath.Base(path), glob) {
			return nil
		}
		n++
		return nil
	})
	return n
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
		return "UNSUPPORTED", nil // no reader for this file type — skip, not an error
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
	// H15/spec 015: populate the per-document linked list so context-window
	// retrieval can fetch sibling chunks around a hit.
	for i := range chunks {
		if i > 0 {
			chunks[i].PreviousChunkID = chunks[i-1].ID
		}
		if i < len(chunks)-1 {
			chunks[i].NextChunkID = chunks[i+1].ID
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
	// H16/spec 018 (pivoted): BM25 indexing is now ASYNC — moved off the ACK path
	// into processJob (which already embeds vectors + bumps the H06 epoch). This
	// brings go-rag into compliance with Principle IV ("BM25 indexing MUST occur
	// asynchronously on background workers AFTER the acknowledgement"). Keyword
	// search becomes eventually consistent, symmetric with vector search. The H01
	// "keyword query right after ACK" guarantee becomes "keyword query after the
	// async worker drains" — the same window vectors have (waitEmbedded covers it).
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
