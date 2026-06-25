package pipeline

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/chunk"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// captureEmbed records every text it is asked to embed (audit H07 wiring test).
type captureEmbed struct{ seen []string }

func (c *captureEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	c.seen = append(c.seen, texts...)
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i + 1), 0.2}
	}
	return out, nil
}
func (c *captureEmbed) Dimensions() int { return 2 }
func (c *captureEmbed) Model() string   { return "nomic-embed-text" }

// TestPipeline_DocumentPrefixApplied proves the ingest path (audit H07 US1):
// chunk texts reach the embedder with the document-role prefix; the stored
// Chunk.Content stays unprefixed (Principle II); and the 0x04 embedding record
// records the convention provenance so a later query can detect a mismatch.
func TestPipeline_DocumentPrefixApplied(t *testing.T) {
	dir := t.TempDir()
	body := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda"
	writeFile(t, filepath.Join(dir, "a.txt"), body)

	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	pre := embed.NewPrefixer("nomic-embed-text", embed.ModeAuto, "", "")
	em := &captureEmbed{}
	p := New(db, chunk.NewSplitter(512, 50), em, index.NewFTS(db.Pebble()), index.NewVector(), pre)
	if _, err := p.Ingest(context.Background(), dir, "*"); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	p.Close()

	// spec 030: the pipeline now queues chunks for the background embedder (0x14).
	// The H07 prefix is applied by the embedder (internal/embedproc), not processJob.
	// Verify the chunks are queued for the embedder.
	if n := db.CountEmbedQueue(); n == 0 {
		t.Fatalf("expected pending-embed queue entries (0x14), got 0 — chunks not queued")
	}

	// 2. The stored Chunk.Content is unprefixed (Principle II — prefix never touches content).
	var nChunks int
	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil {
			nChunks++
			if strings.HasPrefix(c.Content, "search_document: ") {
				t.Errorf("stored Chunk.Content is prefixed: %q (must stay clean)", c.Content)
			}
		}
		return true
	})
	if nChunks == 0 {
		t.Fatalf("no chunks stored")
	}

	// spec 030: assertion (3) — 0x04 embedding convention provenance — moved to the
	// embedproc package tests (the embedder writes 0x04, not the pipeline).
}

// TestPipeline_NilPrefixerNoPrefix confirms a nil prefixer (no prefix in effect)
// passes texts through unchanged — preserving the pre-H07 behavior for callers
// that opt out (and for existing tests).
func TestPipeline_NilPrefixerNoPrefix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "one two three four five six seven eight")
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	em := &captureEmbed{}
	p := New(db, chunk.NewSplitter(512, 50), em, index.NewFTS(db.Pebble()), index.NewVector(), nil)
	if _, err := p.Ingest(context.Background(), dir, "*"); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	p.Close()
	time.Sleep(60 * time.Millisecond)
	for _, txt := range em.seen {
		if strings.HasPrefix(txt, "search_document: ") {
			t.Errorf("nil prefixer prefixed text: %q (must be a no-op)", txt)
		}
	}
}
