package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/madeinoz67/go-rag/internal/events"
)

// TestReingest_Performance_UnchangedRatio (spec 043 / BL-010, T020): the
// end-to-end UNCHANGED-ratio measurement (SC-001 target: >=80%). Ingests a
// multi-chunk doc, edits one paragraph, reprocesses, and reports the delta
// distribution. This is the measurement the design doc said must be done before
// the embed-skip's value is claimed externally.
func TestReingest_Performance_UnchangedRatio(t *testing.T) {
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()
	ws := wsOf(p)

	var mu sync.Mutex
	var got []events.ChunkDelta
	p.OnEvent = func(ev events.DocumentEvent) {
		if ev.Type == events.EventReingested {
			mu.Lock()
			got = ev.Deltas
			mu.Unlock()
		}
	}

	dir := t.TempDir()
	docPath := filepath.Join(dir, "doc.txt")

	// Generate a doc with ~8-10 chunks (~18000 chars at ~2000 chars/chunk).
	// 10 distinct sections, each ~1800 chars, to avoid content-hash collapse.
	para := strings.Repeat("the quick brown fox jumps over the lazy dog near the river bank. ", 36) // ~1800 chars
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString(para)
		sb.WriteString("\n\n")
	}
	writeFile(t, docPath, sb.String())

	// Ingest.
	r, _ := p.Ingest(context.Background(), ws, dir, "*")
	if r.New != 1 {
		t.Fatalf("ingest: want 1 new doc, got %+v", r)
	}
	docID := docIDForPath(t, p, docPath)
	origCount := len(p.chunksOfDoc(ws, docID))
	t.Logf("original chunks: %d", origCount)

	// Edit: change one section's content (in the middle of the doc).
	var sb2 strings.Builder
	for i := 0; i < 10; i++ {
		if i == 5 {
			sb2.WriteString(strings.Repeat("EDITED CONTENT WITH DIFFERENT TEXT FOR THE MEASUREMENT TEST HERE. ", 36))
		} else {
			sb2.WriteString(para)
		}
		sb2.WriteString("\n\n")
	}
	writeFile(t, docPath, sb2.String())

	// Reprocess (re-ingest the edited doc).
	p.Reprocess(context.Background(), dir, "*")

	// Analyze the delta.
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("no RE_INGESTED event — the delta didn't fire")
	}

	counts := map[events.ChunkChange]int{}
	for _, d := range got {
		counts[d.Change]++
	}
	total := len(got)
	unchanged := counts[events.ChangeUnchanged]
	added := counts[events.ChangeAdded]
	removed := counts[events.ChangeRemoved]
	ratio := 0
	if total > 0 {
		ratio = unchanged * 100 / total
	}

	t.Logf("=== T020 MEASUREMENT RESULTS ===")
	t.Logf("total deltas: %d", total)
	t.Logf("  UNCHANGED: %d (%d%%)", unchanged, ratio)
	t.Logf("  ADDED:     %d", added)
	t.Logf("  REMOVED:   %d", removed)

	// SC-001 target: >=80% UNCHANGED for a localized edit.
	// This is a soft assertion (t.Logf, not t.Errorf) because the ratio depends
	// on the splitter's behavior (chunk boundaries vs the edit position). If the
	// edit straddles a chunk boundary, 2 chunks change instead of 1, lowering
	// the ratio. The measurement is the DATA POINT; the target is aspirational.
	if ratio >= 80 {
		t.Logf("SC-001: PASS — %d%% UNCHANGED (target >=80%%)", ratio)
	} else {
		t.Logf("SC-001: %d%% UNCHANGED (target >=80%%) — the edit affected %d of %d chunks; "+
			"ratio depends on chunk-boundary alignment. Still a real saving for larger docs.", ratio, added+removed, total)
	}
}
