package pipeline

import (
	"encoding/json"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/reader"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// TestPipeline_CaptionImages_SectionContext (spec 031): the caption chunk carries
// the heading breadcrumb (SectionContext) of the section where the image appears —
// so captions report document hierarchy, same as text + table chunks.
func TestPipeline_CaptionImages_SectionContext(t *testing.T) {
	p, db := newCaptionPipeline(t)
	defer db.Close()
	defer p.Close()
	p.SetCaptioner(fakeCaptioner{modelName: "fake-vision", caption: "a chart showing revenue growth"})
	docID, oc := storeDocWithOneChunk(t, db)

	// Heading spans + page offsets: the image on page 1 is under "Financial Results".
	spans := []reader.HeadingSpan{
		{Level: 1, Text: "Financial Results", Offset: 0},
	}
	pageOffsets := map[int]int{1: 0}

	p.captionImages(job{
		docID:       docID,
		chunks:      []model.Chunk{oc},
		images:      []reader.ImageRef{{PageNr: 1, Bytes: []byte("fake-jpeg"), FileType: "jpeg"}},
		mimeType:    "application/pdf",
		spans:       spans,
		pageOffsets: pageOffsets,
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
		t.Fatal("expected a caption chunk")
	}
	if len(caption.SectionContext) == 0 || caption.SectionContext[0] != "Financial Results" {
		t.Errorf("caption SectionContext: got %v, want [Financial Results]", caption.SectionContext)
	}
}
