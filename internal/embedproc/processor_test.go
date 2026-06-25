package embedproc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// fakeEmbedder returns deterministic vectors and records call count + texts-per-call.
type fakeEmbedder struct {
	mu       sync.Mutex
	calls    int
	textsPer []int
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	f.calls++
	f.textsPer = append(f.textsPer, len(texts))
	f.mu.Unlock()
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i + 1), 0.5}
	}
	return out, nil
}
func (f *fakeEmbedder) Dimensions() int { return 2 }
func (f *fakeEmbedder) Model() string   { return "fake" }

// failingEmbedder always errors (for the circuit-breaker test).
type failingEmbedder struct{}

func (failingEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, context.Canceled
}
func (failingEmbedder) Dimensions() int { return 2 }
func (failingEmbedder) Model() string   { return "fail" }

func openTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedChunk(t *testing.T, db *storage.DB, id, text string) {
	t.Helper()
	cj, _ := json.Marshal(model.Chunk{ID: id, Content: text})
	_ = db.SetWithPrefix(storage.PrefixChunk, []byte(id), cj)
	_ = db.PutEmbedQueueItem(id, "fake")
}

// TestProcessor_CrashRecovery (spec 030, US1 / SC-001): a chunk with a pending
// 0x14 record but no embedding (simulating a crash between ACK and embed) is
// recovered by the embedder's initial scan on Start.
func TestProcessor_CrashRecovery(t *testing.T) {
	db := openTestDB(t)
	em := &fakeEmbedder{}
	vec := index.NewVector()
	seedChunk(t, db, "chunk1", "alpha beta gamma delta epsilon")

	p := New(db, em, nil, vec, nil)
	p.Start(context.Background())
	time.Sleep(200 * time.Millisecond)
	p.Stop()

	if em.calls == 0 {
		t.Fatal("embedder never called — crash recovery failed")
	}
	if _, ok, _ := db.GetWithPrefix(storage.PrefixEmbedding, []byte("chunk1")); !ok {
		t.Error("expected 0x04 embedding record after recovery")
	}
	if q, ok, _ := db.GetEmbedQueue("chunk1"); ok && q.Status == storage.EmbedQueuePending {
		t.Error("0x14 should have been removed after embedding")
	}
}

// TestProcessor_IdempotentReEmbed (spec 030, FR-006): re-queuing an already-
// embedded chunk is harmless — the queue record is removed again.
func TestProcessor_IdempotentReEmbed(t *testing.T) {
	db := openTestDB(t)
	em := &fakeEmbedder{}
	vec := index.NewVector()
	seedChunk(t, db, "chunk1", "alpha beta gamma")

	p := New(db, em, nil, vec, nil)
	p.Start(context.Background())
	time.Sleep(150 * time.Millisecond)
	p.Stop()
	if em.calls != 1 {
		t.Fatalf("expected 1 embed call, got %d", em.calls)
	}

	_ = db.PutEmbedQueueItem("chunk1", "fake")
	p2 := New(db, em, nil, vec, nil)
	p2.Start(context.Background())
	time.Sleep(150 * time.Millisecond)
	p2.Stop()

	if q, ok, _ := db.GetEmbedQueue("chunk1"); ok && q.Status == storage.EmbedQueuePending {
		t.Error("0x14 should have been removed after the second embed")
	}
}

// TestProcessor_CrossDocBatching (spec 030, US2 / SC-004): 100 pending chunks
// embed in 4 calls (⌈100/32⌉), not 100.
func TestProcessor_CrossDocBatching(t *testing.T) {
	db := openTestDB(t)
	em := &fakeEmbedder{}
	vec := index.NewVector()
	for i := 0; i < 100; i++ {
		seedChunk(t, db, "c"+strconv.Itoa(i), "document text "+strconv.Itoa(i))
	}

	p := New(db, em, nil, vec, nil)
	p.Start(context.Background())
	time.Sleep(400 * time.Millisecond)
	p.Stop()

	em.mu.Lock()
	calls := em.calls
	em.mu.Unlock()
	if calls != 4 {
		t.Errorf("expected 4 embed calls (⌈100/32⌉), got %d", calls)
	}
	if pending := db.CountEmbedQueue(); pending != 0 {
		t.Errorf("expected 0 pending after drain, got %d", pending)
	}
}

// TestProcessor_CircuitBreaker (spec 030, US2 / SC-003): a consistently failing
// embedder opens the circuit; pending chunks stay pending (transient, not failed).
func TestProcessor_CircuitBreaker(t *testing.T) {
	db := openTestDB(t)
	vec := index.NewVector()
	for i := 0; i < 10; i++ {
		seedChunk(t, db, "f"+strconv.Itoa(i), "text "+strconv.Itoa(i))
	}

	p := New(db, failingEmbedder{}, nil, vec, nil)
	p.Start(context.Background())
	time.Sleep(400 * time.Millisecond)
	p.Stop()

	if pending := db.CountEmbedQueue(); pending != 10 {
		t.Errorf("expected 10 pending (transient failures stay pending), got %d", pending)
	}
}
