package caption

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the captioning circuit breaker is open (the
// vision model has failed consecutively). Transient — the caller leaves the
// caption chunk unwritten for a later retry (it is NOT a permanent failure).
var ErrCircuitOpen = errors.New("caption: circuit breaker open")

// breaker is a simple three-state circuit breaker for the captioning vision-model
// call (spec 031 US4, mirroring the spec 029/030 enrichment breaker — 5
// consecutive failures → 30s open, then a half-open probe). Safe for concurrent
// use. It stops a down/misbehaving model from stalling the ingest worker under a
// flood of failing calls. (One breaker per package is the verified convention;
// the DRY-smell of triplicating this across enrich/embedproc/caption is a noted
// follow-up refactor, intentionally out of scope for US4.)
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
// (or immediately if a half-open probe fails).
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
