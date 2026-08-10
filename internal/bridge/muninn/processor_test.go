package muninn

import (
	"context"
	"testing"
	"time"
)

// items builds n WriteParams (distinct content → distinct idempotent_ids → CREATED).
func items(n int) []WriteParams {
	out := make([]WriteParams, n)
	for i := range out {
		out[i] = WriteParams{
			Concept: "c", Content: string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Vault: "go-rag", Stability: 30.0,
			IdempotentID: "chunk:k" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			UpsertMode:   true,
		}
	}
	return out
}

// waitPromoted polls Stats().Promoted until want is reached or the deadline passes.
func waitPromoted(t *testing.T, p *Processor, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Stats().Promoted >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	s := p.Stats()
	t.Fatalf("promoted = %d, want %d (skipped=%d failed=%d)", s.Promoted, want, s.Skipped, s.Failed)
}

// TestProcessor_Promotes confirms a submitted job's items land at MuninnDB and are
// counted as promoted. Uses the FakeClient (UPSERT semantics — distinct content
// ⇒ CREATED).
func TestProcessor_Promotes(t *testing.T) {
	f := NewFakeClient()
	p := NewProcessor(f, ProcConfig{Workers: 1, MaxInFlight: 2, BatchSize: 3})
	p.Start(context.Background())
	defer p.Stop()

	const n = 7
	p.Submit(PromotionJob{Vault: "go-rag", Items: items(n), Mode: ModeChangeEvent})
	waitPromoted(t, p, int64(n))

	if c := f.EngramCount("go-rag"); c != n {
		t.Fatalf("engram count = %d, want %d", c, n)
	}
}

// TestProcessor_ShedsWhenFull verifies FR-011: a full queue sheds (not blocks).
// Without Start the queue is never drained, so a depth-1 queue holds one job and
// sheds the rest synchronously.
func TestProcessor_ShedsWhenFull(t *testing.T) {
	f := NewFakeClient()
	p := NewProcessor(f, ProcConfig{Workers: 1, QueueDepth: 1})
	// Deliberately NOT started — queue stays full.

	p.Submit(PromotionJob{Vault: "v", Items: items(1)}) // fills the queue
	p.Submit(PromotionJob{Vault: "v", Items: items(3)}) // shed
	p.Submit(PromotionJob{Vault: "v", Items: items(2)}) // shed

	if got := p.Stats().Skipped; got != 5 {
		t.Fatalf("skipped = %d, want 5 (3+2 shed)", got)
	}
	// Submit after Stop must not panic and must shed (closed queue). job1's fate
	// during Start→Stop is timing-dependent (Stop cancels ctx, so an in-flight
	// job1 skips at the sem-acquire — correct shutdown behavior, recovered by the
	// next backfill), so assert only the post-stop submit's contribution.
	p.Start(context.Background())
	p.Stop()
	pre := p.Stats().Skipped
	p.Submit(PromotionJob{Vault: "v", Items: items(4)})
	if got := p.Stats().Skipped; got != pre+4 {
		t.Fatalf("skipped after stop = %d, want %d+4 (closed queue sheds)", got, pre)
	}
}

// TestProcessor_StopIsBounded confirms Stop returns within procDrainTimeout + a
// sane margin even when the client is unreachable (NFR-005 — the embedproc-drain
// lesson: never wedge `go-rag stop`). The FakeClient when unhealthy returns
// errors fast, so this mainly guards the drain-timeout path exists.
func TestProcessor_StopIsBounded(t *testing.T) {
	f := NewFakeClient()
	p := NewProcessor(f, ProcConfig{Workers: 2})
	p.Start(context.Background())
	p.Submit(PromotionJob{Vault: "go-rag", Items: items(5)})

	start := time.Now()
	p.Stop()
	elapsed := time.Since(start)
	if elapsed > procDrainTimeout+time.Second {
		t.Fatalf("Stop took %v, want <= %v+1s", elapsed, procDrainTimeout)
	}
}

// TestProcessor_BreakerSkipsOnFailure confirms that a failing MuninnDB trips the
// breaker and subsequent jobs are skipped at the allow() gate (no RPC storm on a
// down server). Single worker for determinism.
func TestProcessor_BreakerSkipsOnFailure(t *testing.T) {
	f := NewFakeClient()
	f.SetHealth(false) // all writes error → breaker accumulates fails
	p := NewProcessor(f, ProcConfig{Workers: 1, BatchSize: 2})
	p.Start(context.Background())
	defer p.Stop()

	// Submit enough small jobs to exceed maxFails (5). Each failing job trips the
	// breaker toward open; once open, later jobs skip at the gate (skipped++).
	for i := 0; i < 12; i++ {
		p.Submit(PromotionJob{Vault: "go-rag", Items: items(1)})
	}
	// Wait for the worker to drain the queue.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !p.CircuitOpen() {
		// Failures drive the breaker open; keep submitting cheaply if needed.
		time.Sleep(2 * time.Millisecond)
	}
	if !p.CircuitOpen() {
		s := p.Stats()
		t.Fatalf("breaker did not open (promoted=%d skipped=%d failed=%d)", s.Promoted, s.Skipped, s.Failed)
	}
	// With the breaker open, a fresh submit is skipped at the gate (no RPC issued).
	// The job queues behind the pending failing jobs, so poll for the skip rather
	// than sleeping a fixed interval (which is what made this test flaky).
	before := p.Stats()
	p.Submit(PromotionJob{Vault: "go-rag", Items: items(1)})
	skDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(skDeadline) && p.Stats().Skipped <= before.Skipped {
		time.Sleep(2 * time.Millisecond)
	}
	if p.Stats().Skipped <= before.Skipped {
		s := p.Stats()
		t.Fatalf("expected open breaker to skip the new job (before=%d now=%d)", before.Skipped, s.Skipped)
	}
}
