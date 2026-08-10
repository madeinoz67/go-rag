package muninn

import (
	"context"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/model"
)

// testBridge builds a coordinator over a fresh FakeClient with sane config
// defaults (the Effective* methods fill the zeros).
func testBridge(t *testing.T) (*Bridge, *FakeClient) {
	t.Helper()
	f := NewFakeClient()
	b := newBridge(config.Config{BridgeEnabled: true, BridgeTargetVault: "go-rag"}, f, nil)
	b.Start(context.Background())
	t.Cleanup(b.Stop)
	return b, f
}

func waitBridge(t *testing.T, b *Bridge, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Status().Promoted >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	s := b.Status()
	t.Fatalf("promoted = %d, want %d (skipped=%d failed=%d)", s.Promoted, want, s.Skipped, s.Failed)
}

// TestNFR002_CognitiveHygiene is the load-bearing NFR-002 assertion (spec 060):
// re-promoting an unchanged chunk through the FULL promote path (Promote → mapper
// → Submit → bridgeProc → client) MUST be a strict no-op at the memory store —
// no duplicate engrams, no access_count bump, no reinforcement. The
// content-addressed idempotent_id + MuninnDB's UPSERT no-op make this
// server-enforced; the test pins it against the FakeClient (which faithfully
// mirrors those semantics).
func TestNFR002_CognitiveHygiene(t *testing.T) {
	b, f := testBridge(t)
	chunks := []model.Chunk{
		{ID: "c1", Content: "tokens expire after 15 minutes", ChunkIndex: 0, TotalChunks: 2},
		{ID: "c2", Content: "refresh via the /token endpoint", ChunkIndex: 1, TotalChunks: 2},
	}
	doc := model.Document{FileName: "auth.md", FileType: "markdown", Metadata: map[string]any{}}

	// First promotion: two engrams created.
	b.Promote(doc, chunks)
	waitBridge(t, b, 2)
	if f.EngramCount("go-rag") != 2 {
		t.Fatalf("engram count = %d, want 2", f.EngramCount("go-rag"))
	}

	// Re-promote the byte-identical document N times (simulating daemon restart /
	// re-ingest — transport replay, not cognitive access).
	const N = 5
	for i := 0; i < N; i++ {
		b.Promote(doc, chunks)
	}
	time.Sleep(100 * time.Millisecond) // let the bridgeProc drain

	// NFR-002: still exactly 2 engrams (no duplicates) and access_count unchanged.
	if got := f.EngramCount("go-rag"); got != 2 {
		t.Fatalf("after %d re-promotes: engram count = %d, want 2 (no-op must not duplicate)", N, got)
	}
	// Both engrams keep access_count 0 (re-promotion is not a cognitive access).
	f.mu.Lock()
	for _, e := range f.engrams["go-rag"] {
		if e.AccessCount != 0 {
			t.Fatalf("engram %s AccessCount = %d after re-promotes, want 0 (no Hebbian forgery)", e.ID, e.AccessCount)
		}
	}
	f.mu.Unlock()
}

// TestBridge_DegradesGracefully is T013 / FR-009: an unreachable MuninnDB must
// never block the caller (Promote/Submit are non-blocking), the bridge must trip
// its circuit breaker (no RPC storm on a down server), and Status must report
// unhealthy. This is the bridge-level degrade contract; the engine-level "write
// ACK unaffected" is verified by the engine suite passing with the promoter wired.
func TestBridge_DegradesGracefully(t *testing.T) {
	f := NewFakeClient()
	f.SetHealth(false) // MuninnDB unreachable
	b := newBridge(config.Config{BridgeEnabled: true, BridgeTargetVault: "go-rag"}, f, nil)
	b.Start(context.Background())
	defer b.Stop()

	doc := model.Document{FileName: "x.md", FileType: "markdown", Metadata: map[string]any{}}
	chunks := []model.Chunk{{ID: "c1", Content: "body", ChunkIndex: 0, TotalChunks: 1}}

	// Promote MUST be non-blocking even with MuninnDB down — Submit is a buffered
	// send; the worker fails fast and the breaker opens after maxFails.
	for i := 0; i < 12; i++ {
		done := make(chan struct{})
		go func() { b.Promote(doc, chunks); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("Promote #%d blocked on an unhealthy MuninnDB (must be non-blocking)", i)
		}
	}

	// Wait for the worker to drain + the breaker to open.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !b.Status().CircuitOpen {
		time.Sleep(2 * time.Millisecond)
	}
	st := b.Status()
	if !st.CircuitOpen {
		t.Fatalf("breaker did not open (promoted=%d skipped=%d failed=%d)", st.Promoted, st.Skipped, st.Failed)
	}
	if st.Healthy {
		t.Error("Status().Healthy = true; want false (MuninnDB unreachable)")
	}
	if f.EngramCount("go-rag") != 0 {
		t.Errorf("engrams promoted despite unhealthy MuninnDB: %d", f.EngramCount("go-rag"))
	}
}

// TestBridge_PromotesAndStatus confirms the coordinator wires Submit through to
// the processor→client, and Status reports health + the target vault.
func TestBridge_PromotesAndStatus(t *testing.T) {
	b, f := testBridge(t)

	b.Submit("go-rag", items(5), ModeChangeEvent)
	waitBridge(t, b, 5)

	if c := f.EngramCount("go-rag"); c != 5 {
		t.Fatalf("engram count = %d, want 5", c)
	}
	st := b.Status()
	if !st.Enabled || !st.Healthy || st.TargetVault != "go-rag" || st.Promoted != 5 {
		t.Fatalf("status = %+v", st)
	}
}

// TestBridge_PauseBlocksBackfillOnly pins FR-014's core rule: Pause gates
// backfill-sourced jobs but NEVER the incremental change-event path.
func TestBridge_PauseBlocksBackfillOnly(t *testing.T) {
	b, _ := testBridge(t)
	b.Pause()
	if !b.Paused() {
		t.Fatal("Paused() = false after Pause()")
	}

	// A backfill job under pause is dropped at the coordinator gate.
	b.Submit("go-rag", items(3), ModeBackfill)
	time.Sleep(50 * time.Millisecond)
	if b.Status().Promoted != 0 {
		t.Fatalf("paused backfill promoted %d, want 0", b.Status().Promoted)
	}

	// An incremental change-event job under pause STILL promotes.
	b.Submit("go-rag", items(2), ModeChangeEvent)
	waitBridge(t, b, 2)

	// Resume → backfill jobs flow again.
	b.Resume()
	b.Submit("go-rag", items(4), ModeBackfill)
	waitBridge(t, b, 6) // 2 (change-event) + 4 (backfill after resume)
}
