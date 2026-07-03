package pipeline

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/events"
)

// TestSerialization_NoDoubleEvent (spec 044 / US1, T008): two concurrent
// operations on the SAME docID must produce exactly one lifecycle event —
// not a DELETED + RE_INGESTED double. The per-document lock serializes them.
func TestSerialization_NoDoubleEvent(t *testing.T) {
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	dir := t.TempDir()
	content := ""
	for i := 0; i < 60; i++ {
		content += "the quick brown fox jumps over the lazy dog. "
	}
	path := filepath.Join(dir, "doc.txt")
	writeFile(t, path, content)
	p.Ingest(context.Background(), dir, "*")
	docID := docIDForPath(t, p, path)

	var mu sync.Mutex
	var eventTypes []events.DocumentEventType
	p.OnEvent = func(ev events.DocumentEvent) {
		if ev.DocumentID != "" {
			mu.Lock()
			eventTypes = append(eventTypes, ev.Type)
			mu.Unlock()
		}
	}

	// Concurrent: one Reprocess (the re-ingest path) + one bare DeleteDoc.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		p.Reprocess(context.Background(), dir, "*")
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // slight delay to race with Reprocess
		p.DeleteDoc(docID)
	}()
	wg.Wait()
	time.Sleep(50 * time.Millisecond) // let async events settle

	mu.Lock()
	defer mu.Unlock()
	// Count DELETED + RE_INGESTED for the same docID. The per-document lock
	// ensures at most one of the two operations emits an event (the other is
	// serialized after — Reprocess's DeleteDoc suppresses DELETED via
	// reingestDocs, or the bare DeleteDoc runs first and Reprocess re-ingests
	// normally). No double DELETED + RE_INGESTED.
	deletedCount := 0
	reingestedCount := 0
	for _, et := range eventTypes {
		if et == events.EventDeleted {
			deletedCount++
		}
		if et == events.EventReingested {
			reingestedCount++
		}
	}
	if deletedCount > 0 && reingestedCount > 0 {
		t.Errorf("double event: %d DELETED + %d RE_INGESTED (the per-document lock should prevent this)", deletedCount, reingestedCount)
	}
}

// TestSerialization_NonBlockingQueuePush (spec 044 / US2, T004):
// processFile must return immediately even when the job queue is full —
// the detached sender parks instead.
func TestSerialization_NonBlockingQueuePush(t *testing.T) {
	p, cleanup := newTestTestPipelineWithFullQueue(t)
	defer cleanup()

	dir := t.TempDir()
	path := filepath.Join(dir, "extra.txt")
	content := ""
	for i := 0; i < 60; i++ {
		content += "an extra document that pushes processFile through the non-blocking path. "
	}
	writeFile(t, path, content)

	// processFile should return quickly even with a full queue.
	start := time.Now()
	r, err := p.Ingest(context.Background(), dir, "*")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Ingest error: %v", err)
	}
	if r.New != 1 {
		t.Fatalf("want 1 new, got %+v", r)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Ingest took %v with a full queue — the non-blocking push should prevent blocking", elapsed)
	}
}

// newTestTestPipelineWithFullQueue creates a pipeline whose job queue is pre-filled
// to capacity (so processFile's queue push hits the default branch → detached sender).
func newTestTestPipelineWithFullQueue(t *testing.T) (*Pipeline, func()) {
	t.Helper()
	p, cleanup := newTestPipeline(t, 0)
	// Fill the queue with dummy jobs that workers won't process quickly
	// (they reference non-existent doc IDs — processJob will skip them).
	for i := 0; i < 64; i++ {
		p.queue <- job{docID: "dummy-fill-" + string(rune(i)), chunks: nil}
	}
	return p, cleanup
}
