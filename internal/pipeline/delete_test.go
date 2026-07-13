package pipeline

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/events"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// docIDForPath resolves the content-addressed document ID for an ingested file
// path by scanning the PrefixDocument key space (Ingest's Result carries counts,
// not IDs). Used by the delete-event tests to target DeleteDoc.
func docIDForPath(t *testing.T, p *Pipeline, filePath string) string {
	t.Helper()
	ws := wsOf(p)
	var found string
	scanVaultKind(t, p.db, storage.PrefixDocument, ws, func(_ []byte, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil && d.FilePath == filePath {
			found = d.ID
			return false // stop scan
		}
		return true
	})
	if found == "" {
		t.Fatalf("no document stored for path %q", filePath)
	}
	return found
}

// drainNonBlocking empties any buffered events without blocking.
func drainNonBlocking(ch <-chan events.DocumentEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// recvOrTimeout reads one event before the deadline; ok is false on timeout.
func recvOrTimeout(t *testing.T, ch <-chan events.DocumentEvent, timeout time.Duration) (events.DocumentEvent, bool) {
	t.Helper()
	select {
	case ev := <-ch:
		return ev, true
	case <-time.After(timeout):
		return events.DocumentEvent{}, false
	}
}

// TestDeleteDoc_PublishesDeletedEvent: spec 040 / BL-008 (FR-004). Deleting a
// document publishes a DELETED lifecycle event carrying the document_id + the
// source file path, after the durable document-record delete. The gRPC stream
// projection of this event is the same Send path proven for INGESTED/EMBEDDED
// (TestGRPC_WatchDocuments_IngEmbedded); this test pins the publish wiring at
// the DeleteDoc chokepoint every deletion trigger (watcher, reprocess) shares.
func TestDeleteDoc_PublishesDeletedEvent(t *testing.T) {
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()
	ws := wsOf(p)

	// Mirror the engine's wiring (pipeline(): e.pipe.OnEvent = e.bus.Publish).
	bus := events.New()
	p.OnEvent = bus.Publish
	ch, _, unsub := bus.Subscribe(32)
	defer unsub()

	dir := t.TempDir()
	path := filepath.Join(dir, "delete-me.txt")
	writeFile(t, path, "the watcher removes this file and the bus must report it as deleted")

	if _, err := p.Ingest(context.Background(), ws, dir, "*"); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	docID := docIDForPath(t, p, path)

	// Let INGESTED + the async EMBEDDED land, then drain so the next event read
	// targets the DELETED one. A straggler EMBEDDED is still tolerated below.
	time.Sleep(80 * time.Millisecond)
	drainNonBlocking(ch)

	if err := p.DeleteDoc(docID); err != nil {
		t.Fatalf("DeleteDoc: %v", err)
	}

	// Read until DELETED for our doc arrives (skip a racing EMBEDDED, if any).
	var got events.DocumentEvent
	deadline := time.After(2 * time.Second)
loop:
	for {
		select {
		case ev := <-ch:
			if ev.Type == events.EventDeleted && ev.DocumentID == docID {
				got = ev
				break loop
			}
		case <-deadline:
			t.Fatalf("DELETED event for %q not received within 2s", docID)
		}
	}
	if got.SourcePath != path {
		t.Errorf("DELETED SourcePath = %q, want %q", got.SourcePath, path)
	}
	if got.TimestampMs == 0 {
		t.Error("DELETED TimestampMs is 0 — Publish should stamp it")
	}
}

// TestDeleteDoc_MissingDocDoesNotPublish: a no-op delete (double-scan, or a doc
// that was already removed) must NOT emit a DELETED event — the publish is gated
// on a record actually existing. Prevents bridge consumers from seeing phantom
// removals on the watcher's idempotent re-scans.
func TestDeleteDoc_MissingDocDoesNotPublish(t *testing.T) {
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	bus := events.New()
	p.OnEvent = bus.Publish
	ch, _, unsub := bus.Subscribe(8)
	defer unsub()

	if err := p.DeleteDoc("never-existed"); err != nil {
		t.Fatalf("DeleteDoc on a missing doc returned err: %v", err)
	}
	if ev, ok := recvOrTimeout(t, ch, 150*time.Millisecond); ok {
		t.Errorf("DeleteDoc on a missing doc published an event: %+v", ev)
	}
}
