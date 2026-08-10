package muninn

import (
	"context"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/model"
)

// fakeSource is a deterministic ChunkSource for backfill tests: N documents, each
// with M distinct chunks.
type fakeSource struct {
	docs   []model.Document
	chunks map[string][]model.Chunk
}

func newFakeSource(numDocs, chunksPer int) *fakeSource {
	s := &fakeSource{chunks: map[string][]model.Chunk{}}
	for d := 0; d < numDocs; d++ {
		doc := model.Document{
			ID: "doc" + string(rune('a'+d)), FileName: "f" + string(rune('a'+d)) + ".md",
			FileType: "markdown", Metadata: map[string]any{},
		}
		s.docs = append(s.docs, doc)
		cs := make([]model.Chunk, chunksPer)
		for c := 0; c < chunksPer; c++ {
			cs[c] = model.Chunk{
				ID: doc.ID + "_c" + string(rune('a'+c)), DocumentID: doc.ID,
				Content:    "content " + string(rune('a'+d)) + string(rune('a'+c)),
				ChunkIndex: c, TotalChunks: chunksPer,
			}
		}
		s.chunks[doc.ID] = cs
	}
	return s
}

func (s *fakeSource) ListDocuments() ([]model.Document, error) { return s.docs, nil }
func (s *fakeSource) Chunks(docID string) ([]model.Chunk, error) {
	return s.chunks[docID], nil
}

func waitBackfill(t *testing.T, b *Bridge, want int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b.Status().Promoted >= want && !b.Status().Backfill.Running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	st := b.Status()
	t.Fatalf("promoted = %d, want %d (backfill running=%v skipped=%d failed=%d)",
		st.Promoted, want, st.Backfill.Running, st.Skipped, st.Failed)
}

// TestBackfill_PromotesCorpus (T015/T017): the auto-on-enable walker promotes every
// document's chunks into the target vault, bounded by the processor's rate caps.
func TestBackfill_PromotesCorpus(t *testing.T) {
	f := NewFakeClient()
	src := newFakeSource(3, 2) // 3 docs × 2 chunks = 6
	b := newBridge(config.Config{BridgeEnabled: true, BridgeBackfillAutoOnEnable: true, BridgeTargetVault: "go-rag"}, f, src)
	b.Start(context.Background())
	defer b.Stop()

	waitBackfill(t, b, 6)
	if got := f.EngramCount("go-rag"); got != 6 {
		t.Fatalf("engram count = %d, want 6", got)
	}
}

// TestBackfill_NoDuplicatesOnRewalk (T018): a second backfill pass over the same
// corpus MUST produce zero new engrams — the content-addressed idempotent_id +
// MuninnDB's UPSERT no-op leave every already-promoted chunk alone. This is the
// resume/restart safety property (NFR-002 generalized to the backfill path).
func TestBackfill_NoDuplicatesOnRewalk(t *testing.T) {
	f := NewFakeClient()
	src := newFakeSource(4, 3) // 4 × 3 = 12
	b := newBridge(config.Config{BridgeEnabled: true, BridgeBackfillAutoOnEnable: true, BridgeTargetVault: "go-rag"}, f, src)
	b.Start(context.Background())
	defer b.Stop()

	waitBackfill(t, b, 12)
	if got := f.EngramCount("go-rag"); got != 12 {
		t.Fatalf("after first walk: engram count = %d, want 12", got)
	}
	// Re-walk the same corpus synchronously (simulating a daemon restart re-running
	// backfill). The processor counts the no-op submits as "promoted" cumulatively,
	// but the FAKE's engram count MUST stay at 12 (no-op leaves existing engrams).
	b.runBackfill(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && b.Status().Promoted < 24 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := f.EngramCount("go-rag"); got != 12 {
		t.Fatalf("after re-walk: engram count = %d, want 12 (no-op must not duplicate)", got)
	}
}

// TestBackfill_StatusReportsBackfill confirms the Backfill state block is populated
// while running and clears on completion (FR-017).
func TestBackfill_StatusReportsBackfill(t *testing.T) {
	f := NewFakeClient()
	src := newFakeSource(2, 1)
	b := newBridge(config.Config{BridgeEnabled: true, BridgeBackfillAutoOnEnable: true, BridgeTargetVault: "go-rag"}, f, src)
	b.Start(context.Background())
	defer b.Stop()

	waitBackfill(t, b, 2)
	st := b.Status()
	if st.Backfill.Running {
		t.Errorf("Backfill.Running = true after completion")
	}
	// The cursor advanced past at least one document.
	if st.Backfill.Cursor == "" {
		t.Errorf("Backfill.Cursor empty after a completed walk")
	}
}
