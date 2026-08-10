package muninn

import (
	"testing"
	"time"
)

// TestBreaker_StateMachine pins the 3-state logic the breaker shares with
// internal/enrich/circuit.go (5 fails → open → half-open probe → close on ok).
func TestBreaker_StateMachine(t *testing.T) {
	b := newBreaker() // maxFails=5, resetAfter=30s

	// Closed: allow always passes.
	for i := 0; i < 4; i++ {
		if err := b.allow(); err != nil {
			t.Fatalf("closed allow %d: %v", i, err)
		}
		b.fail()
	}
	// 4 fails — still closed (threshold is 5).
	if err := b.allow(); err != nil {
		t.Fatalf("still-closed allow: %v", err)
	}

	// 5th fail opens the breaker.
	b.fail()
	if err := b.allow(); err != ErrCircuitOpen {
		t.Fatalf("after 5 fails: want ErrCircuitOpen, got %v", err)
	}

	// resetAfter hasn't elapsed → still open.
	if err := b.allow(); err != ErrCircuitOpen {
		t.Fatalf("before resetAfter: want ErrCircuitOpen, got %v", err)
	}

	// Simulate resetAfter elapsing by rewinding lastFail.
	b.mu.Lock()
	b.lastFail = time.Now().Add(-b.resetAfter - time.Second)
	b.mu.Unlock()

	// One half-open probe is allowed.
	if err := b.allow(); err != nil {
		t.Fatalf("half-open probe: want nil, got %v", err)
	}
	// A concurrent caller during the probe fast-fails (only one probe at a time).
	if err := b.allow(); err != ErrCircuitOpen {
		t.Fatalf("second probe during half-open: want ErrCircuitOpen, got %v", err)
	}

	// Probe succeeds → closes.
	b.ok()
	if err := b.allow(); err != nil {
		t.Fatalf("after ok: want nil (closed), got %v", err)
	}

	// A half-open probe that fails re-opens immediately. Re-arm: force open with
	// resetAfter elapsed, allow() does the half-open transition + returns the probe,
	// then fail() (state is half-open) must flip back to open.
	b.mu.Lock()
	b.state = stOpen
	b.lastFail = time.Now().Add(-b.resetAfter - time.Second)
	b.mu.Unlock()
	if err := b.allow(); err != nil {
		t.Fatalf("re-armed half-open probe: want nil, got %v", err)
	}
	b.fail()
	if err := b.allow(); err != ErrCircuitOpen {
		t.Fatalf("half-open fail should re-open: want ErrCircuitOpen, got %v", err)
	}
}
