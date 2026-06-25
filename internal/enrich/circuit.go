package enrich

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the enrichment circuit breaker is open (the
// model has failed consecutively). Transient — the caller leaves the document's
// sidecar nil for a later retry (it is NOT a permanent failure).
var ErrCircuitOpen = errors.New("enrich: circuit breaker open")

// breaker is a simple three-state circuit breaker for the enrichment model call
// (spec 029, research R5; modelled on the source-verified MuninnDB breaker — 5
// consecutive failures → 30 s open, then a half-open probe). Safe for concurrent
// use. It stops a down/misbehaving model from stalling the ingest worker under a
// flood of failing calls.
type breaker struct {
	mu           sync.Mutex
	state        int // stClosed | stOpen | stHalfOpen
	fails        int
	lastFail     time.Time
	maxFails     int
	resetAfter   time.Duration
	halfOpenUsed bool
}

const (
	stClosed   = 0
	stOpen     = 1
	stHalfOpen = 2
)

// newBreaker returns a breaker with the MuninnDB-verified defaults (5 fails, 30s).
func newBreaker() *breaker { return &breaker{maxFails: 5, resetAfter: 30 * time.Second} }

// allow returns nil if a call may proceed, or ErrCircuitOpen if the breaker is
// open (fast-fail). After resetAfter, one half-open probe is allowed.
func (b *breaker) allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stClosed:
		return nil
	case stOpen:
		if time.Since(b.lastFail) >= b.resetAfter {
			b.state = stHalfOpen
			b.halfOpenUsed = true
			return nil // one probe
		}
		return ErrCircuitOpen
	case stHalfOpen:
		if b.halfOpenUsed {
			return ErrCircuitOpen
		}
		b.halfOpenUsed = true
		return nil
	}
	return nil
}

// ok records a successful call and closes the breaker.
func (b *breaker) ok() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails = 0
	b.state = stClosed
	b.halfOpenUsed = false
}

// fail records a failed call and opens the breaker once failures reach maxFails
// (or immediately if a half-open probe fails). Resets the fail counter on open
// so the next cycle starts clean.
func (b *breaker) fail() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails++
	b.lastFail = time.Now()
	if b.state == stHalfOpen || b.fails >= b.maxFails {
		b.fails = 0
		b.state = stOpen
		b.halfOpenUsed = false
	}
}
