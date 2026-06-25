package embedproc

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the embedder's circuit breaker is open (the
// embedding backend has failed consecutively). Transient — the queue records stay
// pending for a later retry.
var ErrCircuitOpen = errors.New("embedproc: circuit breaker open")

// breaker is a simple three-state circuit breaker for the embedding call (spec 030,
// research R4; modelled on the spec-029 / MuninnDB breaker — 5 consecutive failures
// → 30 s open, then a half-open probe). Safe for concurrent use.
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

func newBreaker() *breaker { return &breaker{maxFails: 5, resetAfter: 30 * time.Second} }

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
			return nil
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

func (b *breaker) ok() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails = 0
	b.state = stClosed
	b.halfOpenUsed = false
}

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
