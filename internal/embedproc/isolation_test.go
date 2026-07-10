package embedproc

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// batchFlakyEmbedder simulates the spec-032 failure mode: the whole-batch Embed
// errors (as the GoMLX graph crash did for an over-length text), but per-text Embed
// succeeds for the "good" texts and errors for the "poison" text. This exercises
// approach D: per-text isolation must scatter the good ones and mark only the poison
// one terminal, so the bad text no longer kills its batch-mates.
type batchFlakyEmbedder struct {
	mu       sync.Mutex
	batchErr int // count of whole-batch Embed calls
	perText  int // count of single-text Embed calls
	poison   string
}

func (f *batchFlakyEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(texts) > 1 {
		f.batchErr++
		return nil, errors.New("simulated whole-batch failure (e.g. GoMLX graph crash)")
	}
	f.perText++
	if len(texts) == 1 && texts[0] == f.poison {
		return nil, errors.New("per-text poison failure")
	}
	return [][]float32{{1.0, 0.5}}, nil
}
func (f *batchFlakyEmbedder) Dimensions() int { return 2 }
func (f *batchFlakyEmbedder) Model() string   { return "flaky" }

// alwaysFailEmbedder fails every call (batch AND per-text). Approach D must detect
// the embedder-wide failure and leave everything pending (transient), NOT mark any
// record terminal — the embedder is down, not the texts.
type alwaysFailEmbedder struct{}

func (alwaysFailEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embedder down")
}
func (alwaysFailEmbedder) Dimensions() int { return 2 }
func (alwaysFailEmbedder) Model() string   { return "down" }

// TestProcessor_PerTextIsolation_MarksPoisonTerminal (approach D, spec 032): when the
// whole-batch Embed fails, the processor falls back to per-text embedding. Good texts
// scatter + dequeue; the poison text is marked EmbedQueueFailed (terminal) so it stops
// re-entering the retry set. Critically, the poison text does NOT kill the good ones.
func TestProcessor_PerTextIsolation_MarksPoisonTerminal(t *testing.T) {
	db := openTestDB(t)
	em := &batchFlakyEmbedder{poison: "POISON"}
	vec := index.NewVector()
	seedChunk(t, db, "good1", "good text one")
	seedChunk(t, db, "poison", "POISON")
	seedChunk(t, db, "good2", "good text two")

	p := New(db, em, nil, vec, nil)
	p.Start(context.Background())
	defer p.Stop()
	time.Sleep(400 * time.Millisecond)
	p.Stop()

	em.mu.Lock()
	batchCalls := em.batchErr
	perTextCalls := em.perText
	em.mu.Unlock()
	if batchCalls == 0 {
		t.Error("expected the whole-batch Embed to have been attempted (and failed)")
	}
	if perTextCalls == 0 {
		t.Error("expected per-text isolation fallback to have run")
	}

	// good1 + good2 must have embedding records (0x04) + be dequeued.
	for _, id := range []string{"good1", "good2"} {
		if _, ok, _ := db.GetWithPrefix(storage.PrefixEmbedding, []byte(id)); !ok {
			t.Errorf("good chunk %q should have an embedding after isolation", id)
		}
		if q, ok, _ := db.GetEmbedQueue(id); ok && q.Status == storage.EmbedQueuePending {
			t.Errorf("good chunk %q should have been dequeued (still pending)", id)
		}
	}
	// poison must be marked terminal (EmbedQueueFailed) — NOT pending, NOT dequeued.
	q, ok, _ := db.GetEmbedQueue("poison")
	if !ok {
		t.Error("poison queue record should still exist (marked terminal, not deleted)")
	} else if q.Status != storage.EmbedQueueFailed {
		t.Errorf("poison status = %q, want %q", q.Status, storage.EmbedQueueFailed)
	}
	// the good chunks' vectors were scattered: both have embedding records (checked
	// above) and were dequeued, proving per-text isolation recovered them despite the
	// batch-level failure.
}

// TestProcessor_PerTextIsolation_AllFailStaysPending: if EVERY per-text embed fails
// (embedder-wide outage), approach D must NOT mark anything terminal — all records
// stay pending so the next tick retries them once the embedder recovers.
func TestProcessor_PerTextIsolation_AllFailStaysPending(t *testing.T) {
	db := openTestDB(t)
	vec := index.NewVector()
	for i := 0; i < 3; i++ {
		seedChunk(t, db, "c"+strconv.Itoa(i), "text "+strconv.Itoa(i))
	}
	p := New(db, alwaysFailEmbedder{}, nil, vec, nil)
	p.Start(context.Background())
	defer p.Stop()
	time.Sleep(400 * time.Millisecond)
	p.Stop()

	for i := 0; i < 3; i++ {
		id := "c" + strconv.Itoa(i)
		q, ok, _ := db.GetEmbedQueue(id)
		if !ok {
			t.Errorf("chunk %q queue record vanished — must stay pending on embedder-wide failure", id)
			continue
		}
		if q.Status != storage.EmbedQueuePending {
			t.Errorf("chunk %q status = %q, want %q (nothing terminal when all fail)", id, q.Status, storage.EmbedQueuePending)
		}
	}
}
