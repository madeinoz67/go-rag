package events

import (
	"sync"
	"testing"
	"time"
)

// TestBus_PublishReachesSubscriber: a single Publish on a subscribed bus is
// received on the channel with Seq assigned + a timestamp. The load-bearing
// contract — Publish actually delivers (T007 / FR-001).
func TestBus_PublishReachesSubscriber(t *testing.T) {
	t.Parallel()
	b := New()
	ch, _, unsub := b.Subscribe(0)
	defer unsub()

	b.Publish(DocumentEvent{Type: EventIngested, DocumentID: "doc-1"})

	select {
	case ev := <-ch:
		if ev.DocumentID != "doc-1" {
			t.Fatalf("DocumentID = %q, want %q", ev.DocumentID, "doc-1")
		}
		if ev.Seq == 0 {
			t.Error("Seq not assigned by Publish (still 0)")
		}
		if ev.TimestampMs == 0 {
			t.Error("TimestampMs not stamped by Publish (still 0)")
		}
		if ev.Type != EventIngested {
			t.Errorf("Type = %d, want %d (EventIngested)", ev.Type, EventIngested)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish did not reach subscriber within 1s")
	}
}

// TestBus_FanOutTwoSubscribers: every published event reaches every subscriber
// (FR-010). Two subscribers, three events, both channels see all three in order.
func TestBus_FanOutTwoSubscribers(t *testing.T) {
	t.Parallel()
	b := New()
	ch1, _, unsub1 := b.Subscribe(0)
	defer unsub1()
	ch2, _, unsub2 := b.Subscribe(0)
	defer unsub2()

	for i := 0; i < 3; i++ {
		b.Publish(DocumentEvent{Type: EventIngested, DocumentID: "doc"})
	}

	drain := func(ch <-chan DocumentEvent) []DocumentEvent {
		var got []DocumentEvent
		for {
			select {
			case ev := <-ch:
				got = append(got, ev)
				if len(got) == 3 {
					return got
				}
			case <-time.After(time.Second):
				t.Fatalf("drained only %d events, want 3", len(got))
			}
		}
	}
	got1 := drain(ch1)
	got2 := drain(ch2)
	for i, ev := range got1 {
		if ev.Seq != got2[i].Seq {
			t.Errorf("subscriber Seq mismatch at %d: %d vs %d", i, ev.Seq, got2[i].Seq)
		}
	}
}

// TestBus_PublishNonBlockingUnderStuckSubscriber: the load-bearing invariant
// (FR-011, research.md R1/R2) — a subscriber that NEVER drains its channel
// cannot stall Publish. We publish far more events than the stuck buffer holds
// and assert every Publish returns within a tight per-call deadline; meanwhile
// a second subscriber, drained CONCURRENTLY, receives every event. The stuck
// subscriber's channel stays at cap (its events are dropped, not buffered).
//
// Note on the healthy subscriber's buffer: it is the default (64) too, so it
// would also drop-behind if not drained in real time. The contract under test
// is that the STUCK subscriber cannot harm the healthy one — so we drain the
// healthy channel concurrently with the publish loop.
func TestBus_PublishNonBlockingUnderStuckSubscriber(t *testing.T) {
	t.Parallel()
	b := New()
	stuckCh, _, _ := b.Subscribe(4)                // tiny buffer so it fills fast
	healthyCh, _, unsubHealthy := b.Subscribe(256) // buffer > total so the burst can't overflow it
	defer unsubHealthy()

	const total = 200 // >> 4, so the stuck buffer saturates early
	var healthyMu sync.Mutex
	healthy := make([]DocumentEvent, 0, total)
	var healthyWG sync.WaitGroup
	healthyWG.Add(1)
	go func() {
		defer healthyWG.Done()
		for ev := range healthyCh {
			healthyMu.Lock()
			healthy = append(healthy, ev)
			healthyMu.Unlock()
			if len(healthy) == total {
				return
			}
		}
	}()

	for i := 0; i < total; i++ {
		done := make(chan struct{})
		go func() {
			b.Publish(DocumentEvent{Type: EventIngested, DocumentID: "doc"})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Publish #%d blocked >100ms — non-blocking invariant violated", i)
		}
	}

	// The stuck subscriber's channel is full (cap reached, the rest dropped).
	if got, want := len(stuckCh), cap(stuckCh); got != want {
		t.Errorf("stuck channel len = %d, want %d (cap — drop-behind should hold it there)", got, want)
	}
	// Wait for the concurrent drainer to observe every event on the healthy channel.
	healthyDone := make(chan struct{})
	go func() { healthyWG.Wait(); close(healthyDone) }()
	select {
	case <-healthyDone:
	case <-time.After(3 * time.Second):
		t.Fatalf("healthy subscriber received only %d/%d events — fan-out broke under a stuck peer", len(healthy), total)
	}
}

// TestBus_SubscriberIsolation: one subscriber unsubscribes; the other keeps
// receiving. Closing one channel never affects another (FR-010 robustness). The
// healthy subscriber's buffer holds both pre- and post-unsub events; we drain in
// order and assert the post-unsub event is among them (the contract is "unsub1
// didn't break ch2's delivery", not "ch2 only sees post-unsub events").
func TestBus_SubscriberIsolation(t *testing.T) {
	t.Parallel()
	b := New()
	_, _, unsub1 := b.Subscribe(0)
	ch2, _, unsub2 := b.Subscribe(0)
	defer unsub2()

	b.Publish(DocumentEvent{Type: EventIngested, DocumentID: "a"})
	unsub1() // tear down subscriber 1
	b.Publish(DocumentEvent{Type: EventEmbedded, DocumentID: "b"})

	// ch2 should have buffered BOTH events (a before unsub1, b after). Drain both.
	var got []string
	deadline := time.After(time.Second)
	for len(got) < 2 {
		select {
		case ev := <-ch2:
			got = append(got, ev.DocumentID)
		case <-deadline:
			t.Fatalf("ch2 received only %d events, want 2: %v", len(got), got)
		}
	}
	if got[0] != "a" || got[1] != "b" {
		t.Errorf("ch2 order = %v, want [a b]", got)
	}
}

// TestBus_PublishAfterUnsubscribeNoPanic: publishing after one subscriber has
// unsubscribed must not panic (send-on-closed-channel is the classic bus bug).
func TestBus_PublishAfterUnsubscribeNoPanic(t *testing.T) {
	t.Parallel()
	b := New()
	_, _, unsub := b.Subscribe(0)
	unsub()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Publish after unsub panicked: %v", r)
		}
	}()
	b.Publish(DocumentEvent{Type: EventIngested})
}

// TestBus_UnsubscribeIdempotent: calling unsub twice does not panic (double-
// close on the channel is the classic bus bug). The bus's sync.Once guard is
// the contract.
func TestBus_UnsubscribeIdempotent(t *testing.T) {
	t.Parallel()
	b := New()
	_, _, unsub := b.Subscribe(0)
	unsub()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("second unsub panicked: %v", r)
		}
	}()
	unsub() // second call — must be a no-op
}

// TestBus_NextSeqMonotonic: NextSeq increases by exactly one per Publish and is
// never reused. Drives the cursor-resume contract (FR-005).
func TestBus_NextSeqMonotonic(t *testing.T) {
	t.Parallel()
	b := New()
	first := b.NextSeq()
	if first != 1 {
		t.Fatalf("initial NextSeq = %d, want 1", first)
	}
	b.Publish(DocumentEvent{Type: EventIngested})
	if got, want := b.NextSeq(), uint64(2); got != want {
		t.Errorf("NextSeq after 1 Publish = %d, want %d", got, want)
	}
	b.Publish(DocumentEvent{Type: EventIngested})
	if got, want := b.NextSeq(), uint64(3); got != want {
		t.Errorf("NextSeq after 2 Publish = %d, want %d", got, want)
	}
}

// TestCursor_RoundTrip: EncodeCursor → DecodeCursor is a bijection on the uint64
// space, including the boundary values 0, 1, max. (T008 / FR-005.)
func TestCursor_RoundTrip(t *testing.T) {
	t.Parallel()
	cases := []uint64{0, 1, 2, 63, 64, 255, 256, 1<<63 - 1, ^uint64(0)}
	for _, seq := range cases {
		c := EncodeCursor(seq)
		if c == "" {
			t.Errorf("EncodeCursor(%d) = empty", seq)
			continue
		}
		got, ok := DecodeCursor(c)
		if !ok {
			t.Errorf("DecodeCursor(%q) = !ok, want ok", c)
			continue
		}
		if got != seq {
			t.Errorf("DecodeCursor(EncodeCursor(%d)) = %d, round-trip broke", seq, got)
		}
	}
}

// TestCursor_Malformed: garbage input is rejected (!ok), never panics, never
// returns a misleading seq. (T008 / FR-008 — unrecognized → from-now is the
// handler's job; the codec's job is to say !ok.)
func TestCursor_Malformed(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",             // empty — the from-now sentinel, not a cursor
		"not-a-cursor", // non-base64
		"!!!!",         // base64-illegal chars
		"AAAAAAA",      // wrong length (7 bytes — must be 8)
		"AAAAAAAAAA",   // wrong length (10 bytes)
		"AQID",         // valid base64 but 3 bytes, not 8
	}
	for _, c := range cases {
		if _, ok := DecodeCursor(c); ok {
			t.Errorf("DecodeCursor(%q) = ok, want !ok (malformed accepted)", c)
		}
	}
}

// TestCursor_EncodeDistinct: distinct seqs encode to distinct cursors (so a
// client comparing cursors for equality is safe). Collisions would break resume.
func TestCursor_EncodeDistinct(t *testing.T) {
	t.Parallel()
	seen := make(map[string]uint64)
	for seq := uint64(0); seq < 1000; seq++ {
		c := EncodeCursor(seq)
		if prev, dup := seen[c]; dup {
			t.Fatalf("EncodeCursor collision: seq %d and %d both → %q", prev, seq, c)
		}
		seen[c] = seq
	}
}

// TestBus_ConcurrentPublishDrain: -race exercise of the publish fan-out under
// concurrent publishers + concurrent drainers. The RLock-vs-Lock split must
// hold: no send races a close. (T009 concurrency leg.) Tolerates drop-behind
// (slow drainers may lose events) — the contract under test is just "no race,
// no panic, no deadlock", not lossless delivery.
func TestBus_ConcurrentPublishDrain(t *testing.T) {
	t.Parallel()
	b := New()
	const subs = 4
	const pubs = 4
	const perPub = 250
	total := pubs * perPub

	var wg sync.WaitGroup
	for s := 0; s < subs; s++ {
		ch, _, unsub := b.Subscribe(64)
		defer unsub()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < total; i++ {
				select {
				case <-ch:
				case <-time.After(time.Second):
					return // stall-tolerant: drop-behind may lose events
				}
			}
		}()
	}
	var pwg sync.WaitGroup
	for p := 0; p < pubs; p++ {
		pwg.Add(1)
		go func() {
			defer pwg.Done()
			for i := 0; i < perPub; i++ {
				b.Publish(DocumentEvent{Type: EventIngested, DocumentID: "doc"})
			}
		}()
	}
	pwg.Wait()
	wg.Wait()
}
