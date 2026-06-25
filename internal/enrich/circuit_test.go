package enrich

import (
	"errors"
	"testing"
	"time"
)

// TestBreaker_OpensAfterMaxFails (spec 029, US3 / R5): the breaker stays closed
// through maxFails-1 failures and opens on the maxFails-th (MuninnDB-verified 5).
func TestBreaker_OpensAfterMaxFails(t *testing.T) {
	b := newBreaker() // maxFails=5
	for i := 0; i < 4; i++ {
		if err := b.allow(); err != nil {
			t.Fatalf("allow %d (closed): %v", i, err)
		}
		b.fail()
	}
	if err := b.allow(); err != nil {
		t.Fatalf("5th allow still closed until the fail: %v", err)
	}
	b.fail()
	if err := b.allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("after 5 fails the breaker must be open, got %v", err)
	}
}

// TestBreaker_ProbeAfterReset (spec 029, US3 / R5): after resetAfter the breaker
// allows exactly one half-open probe; a concurrent caller is rejected, and a
// successful probe closes the breaker.
func TestBreaker_ProbeAfterReset(t *testing.T) {
	b := &breaker{maxFails: 2, resetAfter: 10 * time.Millisecond}
	b.fail()
	b.fail() // open
	if err := b.allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("expected open immediately after 2 fails")
	}
	time.Sleep(15 * time.Millisecond)
	if err := b.allow(); err != nil {
		t.Fatalf("after resetAfter one probe must be allowed, got %v", err)
	}
	if err := b.allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("a second caller while half-open must be rejected")
	}
	b.ok()
	if err := b.allow(); err != nil {
		t.Fatalf("after a successful probe the breaker must close, got %v", err)
	}
}
