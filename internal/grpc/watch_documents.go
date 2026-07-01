package grpc

import (
	"github.com/madeinoz67/go-rag/internal/events"
	"github.com/madeinoz67/go-rag/internal/model"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// watch_documents.go is the gRPC server-streaming projection of the engine's
// document lifecycle event bus (spec 040 / BL-008): the bridge's push replacement
// for the 60–90s ListDocuments poll. The stream is gRPC-ONLY by design (FR-009):
// REST/MCP/CLI do not carry it — a server-stream over HTTP/2 is the natural fit
// and the first operation not projected onto all four transports (research.md R3
// justifies the scope under Principle V).
//
// The handler subscribes to the engine-owned events.Bus, then loops: each event
// whose Seq >= startSeq is projected to the wire and Sent; ctx cancellation
// (client disconnect) tears down the subscription via the deferred unsub, so a
// disconnect never leaks a channel. From-now is the default; a valid cursor
// resumes strictly after it; an unrecognized cursor degrades to from-now
// (graceful — never an error, FR-008).

// watchSubscriberBuffer matches the bus default (events.DefaultSubscriberBuffer,
// 64). Hard-coded here rather than imported to keep the grpc package's surface
// narrow; the bus caps the channel either way and the value is load-bearing only
// as the slow-consumer isolation bound (drop-behind, FR-011).
const watchSubscriberBuffer = 64

// WatchDocuments is the gRPC server-streaming handler for the document lifecycle
// event stream. It blocks for the lifetime of the stream (one RPC = one
// subscriber), projecting each event onto the wire as a DocumentEvent proto.
//
// Cursor semantics:
//   - empty/unset → from-now: only events published after subscribe arrive
//     (startSeq = nextSeq the bus will publish).
//   - a recognized cursor → resume-strictly-after: events with Seq > cursor
//     arrive (startSeq = cursor+1). The at-cursor event is NOT re-delivered.
//   - unrecognized/garbage → degrades to from-now (no error, FR-008): a client
//     passing a stale/corrupted token simply rejoins the live stream.
//
// The handler returns nil when the client disconnects (ctx.Done) or when the bus
// closes the subscriber channel (engine shutdown). A stream.Send error propagates
// as the RPC's terminal status.
func (a *Adapter) WatchDocuments(req *goragpb.WatchRequest, stream goragpb.Gorag_WatchDocumentsServer) error {
	bus := a.eng.Events()
	if bus == nil {
		// Defensive: an engine constructed by hand without a bus cannot serve
		// the stream. Treat as an internal fault rather than panicking.
		return errNoEventBus
	}
	ch, nextSeq, unsub := bus.Subscribe(watchSubscriberBuffer)
	defer unsub()

	startSeq := nextSeq // from-now default
	if c := req.GetCursor(); c != "" {
		if seq, ok := events.DecodeCursor(c); ok {
			startSeq = seq + 1 // resume strictly after the cursor
		}
		// unrecognized → fall through with startSeq = nextSeq (from-now, FR-008)
	}

	ctx := stream.Context()

	// A sender goroutine owns stream.Send and is the sole reader of ch. This
	// decouples Send — which can block indefinitely on HTTP/2 flow control when
	// a client is connected but stops reading — from the ctx.Done select below.
	// Without it, a Send blocked in the select's send-branch makes ctx.Done
	// un-selectable for that iteration, wedging the handler + subscriber until
	// grpc tears the stream down (spec 040 adversarial-audit follow-up #1).
	// With Send on its own goroutine, ctx cancellation (client disconnect or
	// engine shutdown via grpc stream-ctx cancel) always unwinds the handler
	// promptly → defer unsub() runs → no leak.
	//
	// term is buffered (1) so the sender's terminal signal never blocks, even
	// if the main select already exited via ctx.Done. The cursor / from-now
	// filter (ev.Seq < startSeq) stays with the send.
	term := make(chan error, 1)
	go func() {
		for ev := range ch {
			if ev.Seq < startSeq {
				continue // before the resume point — skip (cursor / from-now)
			}
			if err := stream.Send(toEventProto(ev)); err != nil {
				term <- err // transport error → terminal status
				return
			}
		}
		// range over ch exited → Engine.Close → Bus.Close closed the channel.
		// Clean exit; nil preserves the OK terminal status.
		term <- nil
	}()

	// Wait for cancellation (client disconnect / shutdown) or the sender
	// finishing (bus-closed clean exit / Send error). Either way defer unsub()
	// runs on return, closing the subscriber channel + removing it from the bus
	// (no leak). The sender winds down on its own: unsub closes ch → the
	// sender's next receive hits !ok → exit; or grpc cancels the stream ctx →
	// the in-flight Send errors → exit. Bounded by grpc stream teardown.
	select {
	case <-ctx.Done():
		return nil
	case err := <-term:
		return err
	}
}

// toEventProto maps an internal events.DocumentEvent to its proto projection.
// The cursor is the opaque base64-url encoding of ev.Seq (events.EncodeCursor)
// so a client can round-trip it on reconnect. The Type mapping is explicit
// (not a raw cast) so the RE_INGESTED reserved value is unambiguous — the proto
// and internal enums share numeric values 0/1/3 today, but a future enum reorder
// on either side would silently break a cast. `after` reuses toDocumentMetaPB
// (the same projection GetChunk/ListDocuments emit) with a zero Source — the
// event carries the document, not its source record.
func toEventProto(ev events.DocumentEvent) *goragpb.DocumentEvent {
	return &goragpb.DocumentEvent{
		Type:        toEventTypePB(ev.Type),
		DocumentId:  ev.DocumentID,
		SourcePath:  ev.SourcePath,
		Cursor:      events.EncodeCursor(ev.Seq),
		After:       toDocumentMetaPB(ev.After, model.Source{}),
		TimestampMs: ev.TimestampMs,
	}
}

// toEventTypePB maps the internal events.DocumentEventType to the proto enum.
// Explicit switch (not a raw int32 cast) so RE_INGESTED handling is visible and
// a future enum reorder on either side surfaces as a compile-time gap rather
// than a silent mislabel. The MVP emits INGESTED + EMBEDDED + DELETED (DELETED
// publishes from Pipeline.DeleteDoc — the shared deletion chokepoint); the
// other values are mapped for completeness (a bus caller could publish them
// before the gRPC handler grows a typed switch over them).
func toEventTypePB(t events.DocumentEventType) goragpb.DocumentEventType {
	switch t {
	case events.EventIngested:
		return goragpb.DocumentEventType_INGESTED
	case events.EventEmbedded:
		return goragpb.DocumentEventType_EMBEDDED
	case events.EventReingested:
		return goragpb.DocumentEventType_RE_INGESTED
	case events.EventDeleted:
		return goragpb.DocumentEventType_DELETED
	default:
		// Unknown internal value → default to INGESTED (proto's zero value).
		// Defensive: the bus never publishes a value outside the four above,
		// but a raw cast of an out-of-range int would produce a proto-decoder
		// warning on the wire. Defaulting keeps the stream usable.
		return goragpb.DocumentEventType_INGESTED
	}
}
