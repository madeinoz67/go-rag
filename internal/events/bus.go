// Package events is go-rag's in-process document lifecycle event bus (spec 040 /
// BL-008) — the pub-sub substrate for the WatchDocuments gRPC server-stream (the
// bridge's push replacement for polling).
//
// Design: channel-per-subscriber; Publish is NON-BLOCKING (drop-behind — a full
// subscriber buffer drops for THAT subscriber only, never blocking the caller);
// subscriber isolation (one slow reader cannot stall another); in-memory only (no
// persisted state — the MVP default; a Pebble-backed follow-on would add
// cross-restart resume, migration-gated). See specs/040-watch-documents-rpc/
// research.md (R1/R2).
package events

import (
	"encoding/base64"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madeinoz67/go-rag/internal/model"
)

// DocumentEventType is the kind of a document lifecycle event. Values match the
// proto DocumentEventType enum (spec 040); RE_INGESTED is reserved (BL-010).
type DocumentEventType int

const (
	EventIngested   DocumentEventType = 0 // metadata durably committed (add)
	EventEmbedded   DocumentEventType = 1 // async embedding complete
	EventReingested DocumentEventType = 2 // reserved (BL-010 — not emitted by the MVP)
	EventDeleted    DocumentEventType = 3 // scan-detected deletion
)

// DocumentEvent is one lifecycle event on the bus. Seq is the internal monotonic
// position (encoded into the wire cursor); the proto projection drops it.
type DocumentEvent struct {
	Type        DocumentEventType
	DocumentID  string
	SourcePath  string
	Seq         uint64
	After       model.Document
	TimestampMs int64
}

// DefaultSubscriberBuffer is the per-subscriber channel capacity (spec 040 R2).
const DefaultSubscriberBuffer = 64

// subscriber is one WatchDocuments stream's state.
type subscriber struct {
	ch      chan DocumentEvent
	dropped atomic.Uint64
}

// Bus is the in-process event bus.
type Bus struct {
	mu        sync.RWMutex
	next      atomic.Uint64
	subs      map[uint64]*subscriber
	nextSubID atomic.Uint64
	closed    bool
	closeOnce sync.Once
}

// New constructs an empty Bus.
func New() *Bus { return &Bus{subs: make(map[uint64]*subscriber)} }

// Subscribe registers a subscriber with a buffered channel (cap = buf, or the
// default if buf <= 0). Returns the receive channel, the next sequence number the
// bus will publish (for from-now semantics), and an idempotent unsubscribe func.
func (b *Bus) Subscribe(buf int) (<-chan DocumentEvent, uint64, func()) {
	if buf <= 0 {
		buf = DefaultSubscriberBuffer
	}
	id := b.nextSubID.Add(1)
	sub := &subscriber{ch: make(chan DocumentEvent, buf)}
	b.mu.Lock()
	if b.closed {
		// Close already ran: hand back an already-closed channel so the
		// subscriber's !ok branch fires immediately (no event, no leak). The
		// no-op unsub keeps the caller's defer-unsub pattern safe.
		b.mu.Unlock()
		close(sub.ch)
		return sub.ch, b.next.Load() + 1, func() {}
	}
	b.subs[id] = sub
	b.mu.Unlock()
	var once sync.Once
	unsub := func() {
		once.Do(func() {
			b.mu.Lock()
			if s, ok := b.subs[id]; ok {
				delete(b.subs, id)
				close(s.ch)
			}
			b.mu.Unlock()
		})
	}
	return sub.ch, b.next.Load() + 1, unsub
}

// Publish fans ev out to every subscriber. It assigns ev.Seq the next monotonic
// sequence (and a timestamp if unset), then does a NON-BLOCKING send to each
// subscriber: a full buffer drops ev for that subscriber only (bumps its dropped
// counter) and continues — the caller is never blocked. This preserves
// Constitution Principle IV (publish does not enter the <10ms write-ACK path).
//
// Concurrency: Publish holds the RLock while iterating + sending; Unsubscribe
// holds the write Lock to remove + close — they are mutually exclusive, so a send
// can never race a close (no send-on-closed-channel).
func (b *Bus) Publish(ev DocumentEvent) {
	ev.Seq = b.next.Add(1)
	if ev.TimestampMs == 0 {
		ev.TimestampMs = time.Now().UnixMilli()
	}
	b.mu.RLock()
	for _, sub := range b.subs {
		select {
		case sub.ch <- ev:
		default:
			sub.dropped.Add(1) // drop-behind: this subscriber misses ev
		}
	}
	b.mu.RUnlock()
}

// Close shuts the bus down: every live subscriber channel is closed (so a
// blocked WatchDocuments handler unblocks via its !ok branch) and the map is
// emptied, so subsequent Publish is a silent no-op and Subscribe returns an
// already-closed channel. Idempotent (sync.Once).
//
// Used by Engine.Close so the documented "Bus closed the channel (engine
// shutdown)" handler exit path is real — and so any in-process (non-gRPC)
// subscriber unblocks on shutdown without relying on grpc.GracefulStop to
// cancel stream contexts first. Spec 040 adversarial-audit follow-up #2.
//
// Race safety: Close takes the write Lock, which excludes Publish's RLock, so
// the close(sub.ch) calls can never overlap a send (no send-on-closed). The
// per-subscriber unsubscribe's own sync.Once + the `if s, ok := b.subs[id]; ok`
// guard make a double-close impossible regardless of ordering.
func (b *Bus) Close() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		for id, sub := range b.subs {
			delete(b.subs, id)
			close(sub.ch)
		}
		b.mu.Unlock()
	})
}

// NextSeq returns the next sequence the bus will publish (the from-now baseline).
func (b *Bus) NextSeq() uint64 { return b.next.Load() + 1 }

// EncodeCursor renders an opaque, URL-safe cursor for a sequence number (little-
// endian uint64 → base64-url-no-pad). Used by the gRPC projection to build the
// wire DocumentEvent.cursor.
func EncodeCursor(seq uint64) string {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(seq >> (8 * i))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor reverses EncodeCursor; ok is false for a malformed token.
func DecodeCursor(s string) (seq uint64, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != 8 {
		return 0, false
	}
	for i := 0; i < 8; i++ {
		seq |= uint64(b[i]) << (8 * i)
	}
	return seq, true
}
