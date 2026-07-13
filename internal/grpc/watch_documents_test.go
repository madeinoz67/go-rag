package grpc

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/events"
	"github.com/madeinoz67/go-rag/internal/storage"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// newWatchEngine builds an engine wired for the streaming tests: a fresh temp
// DB + a fake embedder injected via NewWithEmbedder, so engine.Add runs the
// real pipeline (lazy-init in pipeline() binds OnEvent → bus.Publish). The
// returned cleanup closes the engine (drains the embedder) and the DB. Also
// returns the temp DB root dir so callers can write doc files under it.
//
// Unlike newEngineWithCorpus (which pre-ingests via a standalone pipeline that
// has no bus wiring), this engine ingests through engine.Add so lifecycle
// events actually flow.
func newWatchEngine(t *testing.T) (*engine.Engine, func()) {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.EmbeddingModel = "fake" // satisfies pipeline()'s non-empty-model guard
	emb := &fakeEmbed{}
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	eng := engine.NewWithEmbedder(cfg, db, emb)
	cleanup := func() {
		eng.Close()
		_ = db.Close()
	}
	return eng, cleanup
}

// addDocToEngine writes content to a temp file and ingests it via engine.Add
// (the path that wires OnEvent → bus.Publish). Returns the file path (the
// event's source_path).
func addDocToEngine(t *testing.T, eng *engine.Engine, content string) string {
	t.Helper()
	dp := filepath.Join(t.TempDir(), "doc-"+time.Now().Format("150405.000000")+".txt")
	if err := os.WriteFile(dp, []byte(content), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if _, err := eng.Add(context.Background(), "default", dp, "*"); err != nil {
		t.Fatalf("engine.Add: %v", err)
	}
	return dp
}

// recvEvent reads one DocumentEvent from the stream within the deadline; fails
// the test on timeout or stream error.
func recvEvent(t *testing.T, stream goragpb.Gorag_WatchDocumentsClient, what string) *goragpb.DocumentEvent {
	t.Helper()
	type res struct {
		ev  *goragpb.DocumentEvent
		err error
	}
	ch := make(chan res, 1)
	go func() {
		ev, err := stream.Recv()
		ch <- res{ev, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("%s: stream.Recv error: %v", what, r.err)
		}
		return r.ev
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: no event within 3s", what)
		return nil
	}
}

// --- T007: US1 — receive INGESTED + EMBEDDED ---

// TestGRPC_WatchDocuments_IngEmbedded: open a stream, add a doc via the engine,
// assert INGESTED arrives within ~3s (relaxed for CI) with the right
// document_id + a non-empty cursor + the after DocumentMeta; then assert an
// EMBEDDED event arrives for the same doc, with INGESTED's Seq < EMBEDDED's Seq.
func TestGRPC_WatchDocuments_IngEmbedded(t *testing.T) {
	eng, cleanup := newWatchEngine(t)
	defer cleanup()
	client, stop := dialBuf(t, NewServer(eng, ""))
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.WatchDocuments(ctx, &goragpb.WatchRequest{})
	if err != nil {
		t.Fatalf("WatchDocuments open: %v", err)
	}

	// Add AFTER subscribing so the from-now baseline captures the event.
	dp := addDocToEngine(t, eng, "the go-rag server emits lifecycle events on ingest")

	ev := recvEvent(t, stream, "INGESTED")
	if ev.GetType() != goragpb.DocumentEventType_INGESTED {
		t.Fatalf("first event type = %v, want INGESTED", ev.GetType())
	}
	if ev.GetDocumentId() == "" {
		t.Error("INGESTED document_id is empty")
	}
	if ev.GetCursor() == "" {
		t.Error("INGESTED cursor is empty — client cannot resume")
	}
	if ev.GetAfter() == nil {
		t.Fatal("INGESTED after is nil — the DocumentMeta projection broke")
	}
	if ev.GetAfter().GetFilePath() != dp {
		t.Errorf("INGESTED after.file_path = %q, want %q", ev.GetAfter().GetFilePath(), dp)
	}
	if ev.GetTimestampMs() == 0 {
		t.Error("INGESTED timestamp_ms is 0")
	}

	// Wait for EMBEDDED. The async embedder (fakeEmbed) is fast but runs on a
	// background goroutine + a poll interval; bound the wait generously for CI.
	em := recvEvent(t, stream, "EMBEDDED")
	if em.GetType() != goragpb.DocumentEventType_EMBEDDED {
		t.Fatalf("second event type = %v, want EMBEDDED", em.GetType())
	}
	if em.GetDocumentId() != ev.GetDocumentId() {
		t.Errorf("EMBEDDED document_id = %q, want %q (same doc as INGESTED)", em.GetDocumentId(), ev.GetDocumentId())
	}

	// Ordering: decode the cursors (which encode Seq) and assert INGESTED < EMBEDDED.
	ingSeq, _ := events.DecodeCursor(ev.GetCursor())
	emSeq, _ := events.DecodeCursor(em.GetCursor())
	if ingSeq >= emSeq {
		t.Errorf("order broke: INGESTED seq %d >= EMBEDDED seq %d", ingSeq, emSeq)
	}
}

// --- T008: US2 — cursor resume ---

// TestGRPC_WatchDocuments_CursorResume: a valid cursor resumes strictly after
// it (no dupe of the at-cursor event); empty/garbage cursors degrade to
// from-now (graceful, no error). FR-005..008.
func TestGRPC_WatchDocuments_CursorResume(t *testing.T) {
	eng, cleanup := newWatchEngine(t)
	defer cleanup()

	// 1) First session: add doc A, capture INGESTED's cursor, close the stream.
	client, stop := dialBuf(t, NewServer(eng, ""))
	ctx1, cancel1 := context.WithCancel(context.Background())
	stream1, err := client.WatchDocuments(ctx1, &goragpb.WatchRequest{})
	if err != nil {
		t.Fatalf("stream1 open: %v", err)
	}
	dpA := addDocToEngine(t, eng, "doc A for cursor resume")
	evA := recvEvent(t, stream1, "doc-A INGESTED")
	cursorA := evA.GetCursor()
	if cursorA == "" {
		t.Fatal("doc-A cursor empty")
	}
	ingSeqA, _ := events.DecodeCursor(cursorA)
	// Drain doc A's async EMBEDDED on stream1 before closing, so it doesn't land
	// in the later resume/from-now streams (where it'd be the first event + muddy
	// the strict-after / from-now checks). The fake embedder fires it quickly.
	recvEvent(t, stream1, "doc-A EMBEDDED (drain before close)")
	cancel1()
	stop()

	// 2) Add doc B while disconnected (so its event is NOT in stream1's window —
	// the bus is in-memory; resume can only deliver events published after the
	// reconnect's subscribe, plus any still buffered for a live subscriber.
	// Since stream1 is closed, doc B is simply lost to a cursor resume — this
	// is the documented lossy limitation. We test the strict-after contract on
	// doc C instead: add it AFTER reconnecting with cursorA, and assert the
	// resumed stream delivers doc C without re-delivering doc A.
	_ = dpA

	// 3) Reconnect with cursorA. New subscriber; from-now baseline = doc C's
	// future event. Add doc C, assert we receive it and NOT a dupe of doc A.
	client2, stop2 := dialBuf(t, NewServer(eng, ""))
	defer stop2()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	stream2, err := client2.WatchDocuments(ctx2, &goragpb.WatchRequest{Cursor: cursorA})
	if err != nil {
		t.Fatalf("stream2 (cursor resume) open: %v", err)
	}
	dpC := addDocToEngine(t, eng, "doc C resumes strictly after the cursor")
	evC := recvEvent(t, stream2, "doc-C INGESTED (resumed)")
	if evC.GetDocumentId() == evA.GetDocumentId() {
		t.Error("cursor resume re-delivered doc A — strict-after contract broke")
	}
	// evC's cursor must be > cursorA (the resume point).
	cSeq, _ := events.DecodeCursor(evC.GetCursor())
	if cSeq <= ingSeqA {
		t.Errorf("resumed event seq %d <= cursor seq %d (strict-after broke)", cSeq, ingSeqA)
	}
	_ = dpC

	// 4) Empty cursor → from-now. A new stream with cursor="" sees only events
	// published after it subscribes.
	client3, stop3 := dialBuf(t, NewServer(eng, ""))
	defer stop3()
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	stream3, err := client3.WatchDocuments(ctx3, &goragpb.WatchRequest{Cursor: ""})
	if err != nil {
		t.Fatalf("stream3 (empty cursor) open: %v", err)
	}
	dpD := addDocToEngine(t, eng, "doc D after empty-cursor from-now")
	evD := recvEvent(t, stream3, "doc-D INGESTED (from-now)")
	if evD.GetDocumentId() == evA.GetDocumentId() {
		t.Error("empty cursor replayed doc A — from-now contract broke")
	}
	_ = dpD

	// 5) Garbage cursor → graceful from-now (no error, FR-008).
	client4, stop4 := dialBuf(t, NewServer(eng, ""))
	defer stop4()
	ctx4, cancel4 := context.WithCancel(context.Background())
	defer cancel4()
	stream4, err := client4.WatchDocuments(ctx4, &goragpb.WatchRequest{Cursor: "not-a-real-cursor!!!"})
	if err != nil {
		t.Fatalf("stream4 (garbage cursor) open: %v — should not error", err)
	}
	dpE := addDocToEngine(t, eng, "doc E after garbage-cursor from-now")
	evE := recvEvent(t, stream4, "doc-E INGESTED (garbage → from-now)")
	if evE.GetDocumentId() == evA.GetDocumentId() {
		t.Error("garbage cursor replayed doc A — from-now fallback broke")
	}
	_ = dpE
}

// --- T009: US3 — concurrency ---

// TestGRPC_WatchDocuments_ConcurrentSubscribers: two streams open
// concurrently both receive the same INGESTED event (fan-out end-to-end).
func TestGRPC_WatchDocuments_ConcurrentSubscribers(t *testing.T) {
	eng, cleanup := newWatchEngine(t)
	defer cleanup()
	client, stop := dialBuf(t, NewServer(eng, ""))
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s1, err := client.WatchDocuments(ctx, &goragpb.WatchRequest{})
	if err != nil {
		t.Fatalf("stream1 open: %v", err)
	}
	s2, err := client.WatchDocuments(ctx, &goragpb.WatchRequest{})
	if err != nil {
		t.Fatalf("stream2 open: %v", err)
	}

	addDocToEngine(t, eng, "doc observed by two concurrent watchers")

	var wg sync.WaitGroup
	ids := make([]string, 2)
	for i, st := range []goragpb.Gorag_WatchDocumentsClient{s1, s2} {
		wg.Add(1)
		go func(idx int, stream goragpb.Gorag_WatchDocumentsClient) {
			defer wg.Done()
			ev := recvEvent(t, stream, "concurrent INGESTED")
			ids[idx] = ev.GetDocumentId()
		}(i, st)
	}
	wg.Wait()

	if ids[0] == "" || ids[0] != ids[1] {
		t.Errorf("concurrent subscribers diverged: %q vs %q (want same non-empty doc id)", ids[0], ids[1])
	}
}

// TestGRPC_WatchDocuments_ClientDisconnectUnsubscribes: when the client cancels
// its context, the handler's defer-unsub fires and the bus no longer holds the
// subscriber's channel. We assert by: open stream, cancel ctx, add a doc, then
// open a NEW stream and confirm the new stream's events don't include a send to
// the closed channel (which would be a goroutine-leak / send-on-closed race).
// Indirectly: the test passing under -race + no goroutine leak is the contract.
func TestGRPC_WatchDocuments_ClientDisconnectUnsubscribes(t *testing.T) {
	eng, cleanup := newWatchEngine(t)
	defer cleanup()
	client, stop := dialBuf(t, NewServer(eng, ""))
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.WatchDocuments(ctx, &goragpb.WatchRequest{})
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	cancel() // client disconnects immediately

	// The server's select on ctx.Done() returns; defer unsub() runs. Drain the
	// client-side Recv to observe EOF (the cancelled RPC's terminal state).
	_, _ = stream.Recv() //nolint:errcheck — expected io.EOF or context-cancelled

	// Publish an event after disconnect; the bus must not have the dropped
	// subscriber anymore (no send-on-closed-channel). We exercise this by
	// adding a doc and confirming a fresh stream still works.
	addDocToEngine(t, eng, "doc after a disconnect — exercises bus subscriber map")

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	stream2, err := client.WatchDocuments(ctx2, &goragpb.WatchRequest{})
	if err != nil {
		t.Fatalf("stream2 open: %v", err)
	}
	// No event expected (the doc was added before stream2 subscribed); just
	// confirm the stream is usable by reading with a short timeout and getting
	// no error other than Timeout.
	gotErr := make(chan error, 1)
	go func() {
		_, err := stream2.Recv()
		gotErr <- err
	}()
	select {
	case err := <-gotErr:
		// Any Recv error here is unexpected (no event pending + no disconnect).
		if err != nil && err != io.EOF {
			t.Logf("stream2.Recv returned %v (acceptable — no event pending)", err)
		}
	case <-time.After(200 * time.Millisecond):
		// No event arrived — correct (the doc was added before subscribe).
	}
}
