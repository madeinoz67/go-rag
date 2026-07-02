package pipeline

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/madeinoz67/go-rag/internal/events"
)

// TestReingest_EmitsReingestedWithDelta (spec 043 / BL-010, US1): a Reprocess
// of an already-ingested doc emits a RE_INGESTED event carrying the chunk delta.
// Re-ingesting an UNCHANGED doc yields all-UNCHANGED deltas (the diff matched old
// vs new content on ContentHash). Proves the wiring: capture-before-delete →
// processFile diff → RE_INGESTED emission (replacing INGESTED).
func TestReingest_EmitsReingestedWithDelta(t *testing.T) {
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	var got *events.DocumentEvent
	var mu sync.Mutex
	p.OnEvent = func(ev events.DocumentEvent) {
		if ev.Type == events.EventReingested {
			mu.Lock()
			got = &ev
			mu.Unlock()
		}
	}

	dir := t.TempDir()
	content := ""
	for i := 0; i < 60; i++ {
		content += "the quick brown fox jumps over the lazy dog. "
	}
	writeFile(t, filepath.Join(dir, "doc.txt"), content)

	r, _ := p.Ingest(context.Background(), dir, "*")
	if r.New != 1 {
		t.Fatalf("first ingest: want 1 new doc, got %+v", r)
	}

	// Reprocess = re-ingest (capture old chunks → delete → re-ingest → RE_INGESTED).
	if _, err := p.Reprocess(context.Background(), dir, "*"); err != nil {
		t.Fatalf("Reprocess: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("no RE_INGESTED event fired on re-ingest")
	}
	if len(got.Deltas) == 0 {
		t.Fatal("RE_INGESTED has no chunk deltas")
	}
	for _, d := range got.Deltas {
		if d.Change != events.ChangeUnchanged {
			t.Errorf("delta change = %v, want UNCHANGED (content didn't change)", d.Change)
		}
	}
}
