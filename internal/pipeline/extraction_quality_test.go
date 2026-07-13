package pipeline

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// TestIngest_ExtractionQuality_DefaultNative (spec 042 / BL-006): a non-PDF
// ingest (no reader extraction signal) defaults to method=native / quality=1.0
// — the clean-text default. (The PDF coverage classifier is unit-tested in
// internal/reader/pdfquality_test.go; the reader wiring is exercised by the
// existing PDF fixture tests.)
func TestIngest_ExtractionQuality_DefaultNative(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "d.txt"), "a plain text document with no pdf extraction to consider here at all\n")
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()
	ws := wsOf(p)

	r, _ := p.Ingest(context.Background(), ws, dir, "*")
	if r.New != 1 {
		t.Fatalf("want 1 new doc, got %+v", r)
	}
	var method string
	var quality float64
	scanVaultKind(t, p.db, storage.PrefixChunk, ws, func(_ []byte, v []byte) bool {
		var ch model.Chunk
		if json.Unmarshal(v, &ch) == nil {
			method = ch.ExtractionMethod
			quality = ch.ExtractionQuality
		}
		return true
	})
	if method != "native" {
		t.Errorf("ExtractionMethod = %q, want native (the non-PDF default)", method)
	}
	if quality != 1.0 {
		t.Errorf("ExtractionQuality = %v, want 1.0 (the non-PDF default)", quality)
	}
}
