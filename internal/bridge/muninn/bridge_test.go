package muninn

import (
	"context"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
)

// testBridge builds a coordinator over a fresh FakeClient with sane config
// defaults (the Effective* methods fill the zeros).
func testBridge(t *testing.T) (*Bridge, *FakeClient) {
	t.Helper()
	f := NewFakeClient()
	b := newBridge(config.Config{BridgeEnabled: true, BridgeTargetVault: "go-rag"}, f)
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
