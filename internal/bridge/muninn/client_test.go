package muninn

import (
	"context"
	"testing"
)

// TestFakeClient_Upsert_NoOpOnIdentical is the load-bearing contract test: the
// fake must mirror MuninnDB's shipped UPSERT semantics — a re-write with the
// SAME idempotent_id and byte-identical content is a STRICT NO-OP that mutates
// no cognitive state. NFR-002 (cognitive hygiene) rests on this. If this test
// fails, the fake is lying to every bridge test that depends on it.
func TestFakeClient_Upsert_NoOpOnIdentical(t *testing.T) {
	ctx := context.Background()
	f := NewFakeClient()
	p := WriteParams{
		Concept: "Token Refresh Flow", Content: "tokens expire after 15m",
		Vault: "go-rag", Stability: 30.0, Confidence: 1.0,
		IdempotentID: "chunk:abc", UpsertMode: true, TypeLabel: "go-rag-chunk",
	}

	// Create.
	id1, _, err := f.Write(ctx, p)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	before, err := f.Read(ctx, "go-rag", id1)
	if err != nil {
		t.Fatalf("read after create: %v", err)
	}
	if before.AccessCount != 0 {
		t.Fatalf("fresh engram AccessCount = %d, want 0", before.AccessCount)
	}
	ac, ua, la := before.AccessCount, before.UpdatedAt, before.LastAccess

	// Re-promote the byte-identical chunk N times.
	const N = 5
	for i := 0; i < N; i++ {
		id, _, err := f.Write(ctx, p)
		if err != nil {
			t.Fatalf("re-write %d: %v", i, err)
		}
		if id != id1 {
			t.Fatalf("re-write %d returned new id %q, want %q (no-op must return existing)", i, id, id1)
		}
	}

	// The three cognitive-state fields MUST be byte-identical — no bump, no refresh.
	after, err := f.Read(ctx, "go-rag", id1)
	if err != nil {
		t.Fatalf("read after re-promote: %v", err)
	}
	if after.AccessCount != ac || after.UpdatedAt != ua || after.LastAccess != la {
		t.Fatalf("no-op violated: access_count %d→%d, updated_at %d→%d, last_access %d→%d",
			ac, after.AccessCount, ua, after.UpdatedAt, la, after.LastAccess)
	}
	if f.EngramCount("go-rag") != 1 {
		t.Fatalf("no-op created duplicates: engram count = %d, want 1", f.EngramCount("go-rag"))
	}
}

// TestFakeClient_Upsert_EvolveOnChanged verifies the changed-content path
// produces a fresh engram and supersedes the predecessor. (The bridge's content-
// addressed keys never hit this — a changed chunk gets a new idempotent_id — but
// the fake must implement it faithfully for completeness.)
func TestFakeClient_Upsert_EvolveOnChanged(t *testing.T) {
	ctx := context.Background()
	f := NewFakeClient()
	p := WriteParams{
		Concept: "c", Content: "v1", Vault: "go-rag",
		Stability: 30.0, IdempotentID: "chunk:k", UpsertMode: true,
	}
	id1, _, err := f.Write(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	p.Content = "v2-changed"
	id2, _, err := f.Write(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if id2 == id1 {
		t.Fatal("changed-content write returned the same id; want a new (evolved) engram")
	}
	if f.EngramCount("go-rag") != 2 {
		t.Fatalf("evolve count = %d, want 2 (predecessor kept as history)", f.EngramCount("go-rag"))
	}
	old, _ := f.Read(ctx, "go-rag", id1)
	if old == nil || old.State != "superseded" {
		t.Fatalf("predecessor state = %q, want \"superseded\"", stateOr(old))
	}
	// The forward index now points at the new head: a third identical-to-v2 write no-ops on id2.
	id3, _, err := f.Write(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if id3 != id2 {
		t.Fatalf("post-evolve no-op returned %q, want head %q", id3, id2)
	}
}

// TestFakeClient_UnhealthyErrors confirms the degrade path: an unhealthy fake
// returns errors from every op, and Healthy() reports false — the precondition
// for the bridge's circuit-breaker + graceful-degrade tests (T013).
func TestFakeClient_UnhealthyErrors(t *testing.T) {
	ctx := context.Background()
	f := NewFakeClient()
	f.SetHealth(false)
	if f.Healthy() {
		t.Fatal("Healthy() = true after SetHealth(false)")
	}
	if _, err := f.Hello(ctx); err == nil {
		t.Fatal("Hello on unhealthy fake: want error")
	}
	if _, _, err := f.Write(ctx, WriteParams{Vault: "v", IdempotentID: "k", UpsertMode: true}); err == nil {
		t.Fatal("Write on unhealthy fake: want error")
	}
	if _, err := f.Read(ctx, "v", "x"); err == nil {
		t.Fatal("Read on unhealthy fake: want error")
	}
}

// TestFakeClient_UpsertMode_RequiresIdempotentID verifies the fail-loud contract:
// MuninnDB rejects upsert_mode without idempotent_id. The bridge must never send
// one without the other.
func TestFakeClient_UpsertMode_RequiresIdempotentID(t *testing.T) {
	ctx := context.Background()
	f := NewFakeClient()
	res, err := f.BatchWrite(ctx, "v", []WriteParams{{
		Concept: "c", Content: "x", Vault: "v", UpsertMode: true, // no IdempotentID
	}})
	if err != nil {
		t.Fatalf("BatchWrite: %v", err)
	}
	if res[0].Error == "" || res[0].ID != "" {
		t.Fatalf("bare upsert should fail loud: got %+v", res[0])
	}
}

// RED-sanity-check (performed by hand at write time): a FakeClient that FORGES
// access_count on re-promotion fails TestFakeClient_Upsert_NoOpOnIdentical — i.e.
// the no-op test genuinely catches the cognitive-hygiene regression class.

func stateOr(e *Engram) string {
	if e == nil {
		return "<nil>"
	}
	return e.State
}
