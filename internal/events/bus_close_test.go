package events

import (
	"testing"
	"time"
)

// TestBus_Close_ClosesAllSubscriberChannels: Close shuts every live subscriber
// channel so a WatchDocuments handler's !ok branch fires (spec 040 audit
// follow-up #2 — the documented "Bus closed the channel (engine shutdown)"
// path must be real, not dead code).
func TestBus_Close_ClosesAllSubscriberChannels(t *testing.T) {
	b := New()
	ch1, _, unsub1 := b.Subscribe(8)
	ch2, _, unsub2 := b.Subscribe(8)
	defer unsub1()
	defer unsub2()

	b.Close()

	for i, ch := range []<-chan DocumentEvent{ch1, ch2} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("subscriber %d: channel still open after Close", i)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: <-ch blocked after Close (shutdown leak)", i)
		}
	}
}

// TestBus_Close_Idempotent: Close is sync.Once-gated — repeat calls must not
// double-close (panic). Asserted via a post-Close Subscribe returning a closed
// channel, proving the closed flag persisted.
func TestBus_Close_Idempotent(t *testing.T) {
	b := New()
	_, _, unsub := b.Subscribe(8)
	defer unsub()

	b.Close()
	b.Close()
	b.Close()

	ch, _, lateUnsub := b.Subscribe(8)
	defer lateUnsub()
	if _, ok := <-ch; ok {
		t.Fatal("Subscribe after repeated Close returned an open channel")
	}
}

// TestBus_Close_LateSubscribeReturnsClosedChannel: a Subscribe that lands after
// Close must hand back an already-closed channel + a no-op unsub, so the
// handler's !ok branch fires immediately instead of blocking forever on a
// channel nothing will ever close.
func TestBus_Close_LateSubscribeReturnsClosedChannel(t *testing.T) {
	b := New()
	b.Close()

	ch, _, unsub := b.Subscribe(8)
	defer unsub() // must be a no-op, not a close-of-never-tracked-channel

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("Subscribe after Close returned an open channel")
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe after Close blocked — expected an immediately-closed channel")
	}
}

// TestBus_Close_PublishIsNoOp: after Close the subscriber map is empty, so a
// Publish (e.g. an in-flight markStatus publishing EMBEDDED during Engine.Close)
// is a silent no-op — no send-on-closed, no panic, and no phantom event lands.
func TestBus_Close_PublishIsNoOp(t *testing.T) {
	b := New()
	ch, _, unsub := b.Subscribe(8)
	defer unsub()
	b.Close()

	b.Publish(DocumentEvent{Type: EventIngested, DocumentID: "post-close"})

	// Channel stays closed with no buffered event — Publish neither sent nor
	// reopened it.
	if _, ok := <-ch; ok {
		t.Fatal("Publish after Close delivered an event or reopened the channel")
	}
}

// TestBus_Close_UnblocksBlockedReceiver: the load-bearing shutdown invariant. A
// subscriber blocked on <-ch (no event published) must unblock promptly when
// Close runs — this is what lets a live WatchDocuments handler exit on
// Engine.Close without grpc.GracefulStop cancelling its ctx first.
func TestBus_Close_UnblocksBlockedReceiver(t *testing.T) {
	b := New()
	ch, _, unsub := b.Subscribe(8)
	defer unsub()

	closed := make(chan struct{})
	go func() {
		if _, ok := <-ch; !ok {
			close(closed)
		}
	}()

	time.Sleep(20 * time.Millisecond) // let the receiver block on <-ch

	b.Close()

	select {
	case <-closed:
		// Close unblocked the receiver via channel close. Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock a blocked receiver within 2s (shutdown leak)")
	}
}

// TestBus_Close_ConcurrentPublishNoPanic: racing Publish against Close must not
// produce a send-on-closed-channel panic. Close takes the write Lock; Publish
// takes the RLock — mutually exclusive, so the close(sub.ch) calls can never
// overlap a send. Run under -race; a panic here is the failure signal, and the
// post-Close Subscribe asserts Close took effect despite the concurrent flood.
func TestBus_Close_ConcurrentPublishNoPanic(t *testing.T) {
	b := New()
	_, _, unsub := b.Subscribe(8)
	defer unsub()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			b.Publish(DocumentEvent{Type: EventIngested, DocumentID: "d"})
		}
	}()

	b.Close()
	<-done // post-Close Publish is a no-op; the publisher drains harmlessly

	ch, _, lateUnsub := b.Subscribe(8)
	defer lateUnsub()
	if _, ok := <-ch; ok {
		t.Fatal("Subscribe after concurrent Close+Publish returned an open channel")
	}
}
