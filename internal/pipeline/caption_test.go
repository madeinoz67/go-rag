package pipeline

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/caption"
	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/reader"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// fakeCaptioner returns a fixed caption (hermetic — no vision model). If err is
// set, Caption returns it (used to exercise the error taxonomy).
type fakeCaptioner struct {
	modelName string
	caption   string
	err       error
}

func (f fakeCaptioner) Model() string { return f.modelName }
func (f fakeCaptioner) Caption(_ context.Context, _ []byte, _ string) (string, error) {
	return f.caption, f.err
}

// storeDocWithOneChunk stores a minimal document + one original chunk for a
// caption test, returning the docID + the original chunk (the caption links
// after it). Mirrors the post-ACK state processJob leaves behind.
func storeDocWithOneChunk(t *testing.T, db *storage.DB) (string, model.Chunk) {
	t.Helper()
	docID := "doc-caption-test"
	doc := model.Document{ID: docID, MimeType: "application/pdf", ChunkCount: 1, Status: StatusEmbedded}
	dj, _ := json.Marshal(doc)
	if err := db.SetWithPrefix(storage.PrefixDocument, []byte(docID), dj); err != nil {
		t.Fatalf("store doc: %v", err)
	}
	oc := model.Chunk{ID: "chunk-orig-1", DocumentID: docID, Content: "body text here", ChunkIndex: 0, TotalChunks: 1}
	ocj, _ := json.Marshal(oc)
	if err := db.SetWithPrefix(storage.PrefixChunk, []byte(oc.ID), ocj); err != nil {
		t.Fatalf("store chunk: %v", err)
	}
	return docID, oc
}

func newCaptionPipeline(t *testing.T) (*Pipeline, *storage.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	p := New(db, chunk.NewSplitter(512, 50), fakeEmbedPI{}, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	return p, db
}

// TestPipeline_CaptionImages (spec 031 US4, SC-004 wiring): with a captioner
// bound, captionImages writes a synthetic caption chunk that is FTS-searchable
// AND embed-queued, linked into the chunk list, with the document ChunkCount
// bumped. Hermetic (fake captioner — no vision model). The real SC-004 E2E
// (vector search over a live vision model's caption) is an operator smoke test.
func TestPipeline_CaptionImages(t *testing.T) {
	p, db := newCaptionPipeline(t)
	defer db.Close()
	defer p.Close()
	p.SetCaptioner(fakeCaptioner{modelName: "fake-vision", caption: "bar chart showing revenue rising from 10k to 50k"})
	docID, oc := storeDocWithOneChunk(t, db)

	p.captionImages(job{
		docID:    docID,
		chunks:   []model.Chunk{oc},
		images:   []reader.ImageRef{{PageNr: 1, Bytes: []byte("fake-jpeg"), FileType: "jpeg"}},
		mimeType: "application/pdf",
	})

	var caption *model.Chunk
	db.PrefixScanByte(storage.PrefixChunk, func(_ []byte, v []byte) bool {
		var c model.Chunk
		if json.Unmarshal(v, &c) == nil && c.Kind == "caption" && c.DocumentID == docID {
			caption = &c
			return false
		}
		return true
	})
	if caption == nil {
		t.Fatal("expected a caption chunk (Kind=caption)")
	}
	if !strings.Contains(caption.Content, "revenue rising") {
		t.Errorf("caption content: %q", caption.Content)
	}
	if caption.Caption == nil || caption.Caption.Model != "fake-vision" || caption.Caption.Status != "done" {
		t.Errorf("caption sidecar: %+v", caption.Caption)
	}

	// Linked-list integrity: original → caption → (none).
	var oc2 model.Chunk
	if raw, ok, _ := db.GetWithPrefix(storage.PrefixChunk, []byte(oc.ID)); ok {
		_ = json.Unmarshal(raw, &oc2)
	}
	if oc2.NextChunkID != caption.ID {
		t.Errorf("original NextChunkID: %q, want %q", oc2.NextChunkID, caption.ID)
	}
	if caption.PreviousChunkID != oc.ID {
		t.Errorf("caption PreviousChunkID: %q, want %q", caption.PreviousChunkID, oc.ID)
	}
	if caption.NextChunkID != "" {
		t.Errorf("caption NextChunkID: %q, want empty (tail)", caption.NextChunkID)
	}

	// ChunkCount bumped (non-identity statistic).
	var d model.Document
	if raw, ok, _ := db.GetWithPrefix(storage.PrefixDocument, []byte(docID)); ok {
		_ = json.Unmarshal(raw, &d)
	}
	if d.ChunkCount != 2 {
		t.Errorf("ChunkCount: %d, want 2", d.ChunkCount)
	}

	// Embed-queued (the vector-search half of SC-004 — without this, silently
	// BM25-only).
	if _, ok, _ := db.GetEmbedQueue(caption.ID); !ok {
		t.Error("caption chunk was not queued for embedding (vector half would silently fail)")
	}

	// FTS-searchable (the keyword half of SC-004).
	hits := p.fts.Search("revenue", 10)
	found := false
	for _, h := range hits {
		if h.ChunkID == caption.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("caption not FTS-searchable; hits: %+v", hits)
	}
}

// TestPipeline_CaptionImages_Disabled (spec 031 US4, SC-006): no captioner bound
// → no caption chunk, ChunkCount unchanged, original chunk untouched. Proves the
// opt-in default-off invariant (byte-identical to today when captioning is off).
func TestPipeline_CaptionImages_Disabled(t *testing.T) {
	p, db := newCaptionPipeline(t)
	defer db.Close()
	defer p.Close()
	docID, oc := storeDocWithOneChunk(t, db)
	p.captionImages(job{
		docID:    docID,
		chunks:   []model.Chunk{oc},
		images:   []reader.ImageRef{{PageNr: 1, Bytes: []byte("x")}},
		mimeType: "application/pdf",
	})
	count := 0
	db.PrefixScanByte(storage.PrefixChunk, func(_ []byte, v []byte) bool {
		var c model.Chunk
		if json.Unmarshal(v, &c) == nil && c.Kind == "caption" {
			count++
		}
		return true
	})
	if count != 0 {
		t.Errorf("expected no caption chunk when disabled, got %d", count)
	}
}

// TestPipeline_CaptionImages_Transient (spec 031 US4, SC-006): a transient
// captioner error (circuit open / network) → no caption chunk written, no panic,
// no inline retry loop. The document completes normally; a later reprocess retries.
func TestPipeline_CaptionImages_Transient(t *testing.T) {
	p, db := newCaptionPipeline(t)
	defer db.Close()
	defer p.Close()
	p.SetCaptioner(fakeCaptioner{modelName: "fake-vision", err: caption.ErrCircuitOpen})
	docID, oc := storeDocWithOneChunk(t, db)
	p.captionImages(job{
		docID:    docID,
		chunks:   []model.Chunk{oc},
		images:   []reader.ImageRef{{PageNr: 1, Bytes: []byte("x")}},
		mimeType: "application/pdf",
	})
	count := 0
	db.PrefixScanByte(storage.PrefixChunk, func(_ []byte, v []byte) bool {
		var c model.Chunk
		if json.Unmarshal(v, &c) == nil && c.Kind == "caption" {
			count++
		}
		return true
	})
	if count != 0 {
		t.Errorf("expected no caption chunk on transient error, got %d", count)
	}
}
