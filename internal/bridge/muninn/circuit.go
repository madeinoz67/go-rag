package muninn

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned/used when the MuninnDB circuit breaker is open (the
// server has failed consecutively). Transient — the dropped promotion is not lost
// under content-addressed UPSERT: the next backfill re-walk re-promotes it as a
// no-op. Modelled on internal/enrich/circuit.go (the source-verified MuninnDB
// breaker: 5 consecutive fails → 30s open, then one half-open probe).
var ErrCircuitOpen = errors.New("bridge: circuit breaker open")

// breaker is a three-state circuit breaker for the MuninnDB egress. It stops a
// down/unreachable MuninnDB from stalling the bridge worker pool under a flood of
// failing RPCs. Safe for concurrent use.
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
func newBreaker() *breaker {
	return &breaker{maxFails: 5, resetAfter: 30 * time.Second}
}

// allow returns nil if a call may proceed, or ErrCircuitOpen if the breaker is
// open. After resetAfter elapses, one half-open probe is allowed; the probe either
// closes the breaker (on success) or re-opens it (on failure).
func (b *breaker) allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stClosed:
		return nil
	case stOpen:
		if time.Since(b.lastFail) >= b.resetAfter {
			// This call IS the single half-open probe — mark it consumed so a
			// concurrent caller fast-fails until ok()/fail() resolves the probe.
			b.state = stHalfOpen
			b.halfOpenUsed = true
			return nil
		}
		return ErrCircuitOpen
	case stHalfOpen:
		// Only one probe in flight at a time; further concurrent callers fast-fail
		// until the probe resolves (ok/fail flips the state).
		if !b.halfOpenUsed {
			b.halfOpenUsed = true
			return nil
		}
		return ErrCircuitOpen
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
// (or immediately if a half-open probe fails). Resets the fail counter on open.
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
