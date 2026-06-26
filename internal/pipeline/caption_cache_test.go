package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/reader"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// countingCaptioner tracks how many times Caption is called (to verify the cache
// skips redundant vision calls).
type countingCaptioner struct {
	model  string
	result string
	calls  *int
}

func (c *countingCaptioner) Model() string { return c.model }
func (c *countingCaptioner) Caption(_ context.Context, _ []byte, _ string) (string, error) {
	*c.calls++
	return c.result, nil
}

// TestPipeline_CaptionImages_ImageCache (spec 031): the cross-document image-caption
// cache deduplicates identical images — the same image bytes (a logo on every page,
// or the same chart across documents) are captioned ONCE; subsequent occurrences hit
// the cache and skip the vision call entirely. This test verifies within-document
// dedup (two identical images on different pages → 1 vision call).
func TestPipeline_CaptionImages_ImageCache(t *testing.T) {
	p, db := newCaptionPipeline(t)
	defer db.Close()
	defer p.Close()

	calls := 0
	p.SetCaptioner(&countingCaptioner{model: "fake-vision", result: "a chart showing revenue", calls: &calls})
	docID, oc := storeDocWithOneChunk(t, db)

	// Two IDENTICAL images (same bytes) on different pages.
	sameBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	p.captionImages(job{
		docID:  docID,
		chunks: []model.Chunk{oc},
		images: []reader.ImageRef{
			{PageNr: 1, Bytes: sameBytes, FileType: "jpeg"},
			{PageNr: 2, Bytes: sameBytes, FileType: "jpeg"}, // identical → cache hit
		},
		mimeType: "application/pdf",
	})

	// The vision model should be called ONCE (second image hits the cache).
	if calls != 1 {
		t.Errorf("expected 1 vision call (second image cached), got %d", calls)
	}

	// The caption chunk should reference BOTH pages (both captioned — one from the
	// call, one from the cache).
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
		t.Fatal("expected a caption chunk")
	}
	if !strings.Contains(caption.Content, "page 1") || !strings.Contains(caption.Content, "page 2") {
		t.Errorf("caption should reference both pages; got: %s", caption.Content)
	}
}
