package audit

// audit.go is the append-only JSONL appender (spec 021 / audit H18). Callers send
// Events via Log (a non-blocking channel send — dropped if the buffer is full, so the
// caller's path is never blocked; Constitution IV). A single writer goroutine drains
// the channel, serializing appends + rotation. Race-free (single writer).
//
// AIR-GAP (Constitution I): the log is a LOCAL vault file; nothing is forwarded.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// DefaultPath returns the default audit-log path for a vault: <dbPath>/audit/audit.log.
func DefaultPath(dbPath string) string {
	return filepath.Join(dbPath, "audit", "audit.log")
}

// Global appender — the daemon sets it once at boot (SetGlobal); call sites
// (engine/transports) use the package Log func, which is a no-op when unset (one-shot
// CLI, or audit disabled). Keeps the Appender handle out of the engine/transport types.
var global atomic.Pointer[Appender]

// SetGlobal installs the process-wide audit appender (called once at daemon boot).
func SetGlobal(a *Appender) { global.Store(a) }

// Log emits an event to the global appender. No-op when no appender is set.
func Log(e Event) {
	if a := global.Load(); a != nil {
		a.Log(e)
	}
}

// Appender is the append-only JSONL logger. Construct with Init; call Log for each
// event; Close at shutdown to drain + release the file.
type Appender struct {
	path     string
	maxBytes int
	ch       chan Event
	closed   atomic.Bool
	wg       sync.WaitGroup
	mu       sync.Mutex // guards the file handle (rotation)
	f        *os.File
}

// Init opens the audit log at path (creating the directory), defaulting maxBytes when
// ≤0, and starts the single writer goroutine.
func Init(path string, maxBytes int) (*Appender, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultAuditMaxBytes
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	a := &Appender{path: path, maxBytes: maxBytes, ch: make(chan Event, 1024), f: f}
	a.wg.Add(1)
	go a.run()
	return a, nil
}

// DefaultAuditMaxBytes mirrors config.DefaultAuditLogMaxBytes (kept here so the package
// is self-contained for tests that pass maxBytes ≤ 0).
const DefaultAuditMaxBytes = 16 << 20 // ~16 MiB

// Log sends an event. Non-blocking: drops the event if the buffer is full rather than
// block the caller (Constitution IV — the audit path never gates query/ingest ACK).
// No-op on a nil/closed appender.
func (a *Appender) Log(e Event) {
	if a == nil || a.closed.Load() {
		return
	}
	select {
	case a.ch <- e:
	default: // buffer full — drop (best-effort; an audit log must never stall the daemon)
	}
}

// Path returns the active log file path.
func (a *Appender) Path() string { return a.path }

func (a *Appender) run() {
	defer a.wg.Done()
	for e := range a.ch {
		a.appendLocked(e)
	}
}

func (a *Appender) appendLocked(e Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	line, err := e.Marshal()
	if err != nil {
		return
	}
	if a.needsRotate(len(line)) {
		a.rotateLocked()
	}
	if a.f != nil {
		_, _ = a.f.Write(line) // best-effort; an audit write failure is never fatal
	}
}

// Close drains pending events (the writer goroutine flushes them), then closes the
// file. Call at daemon shutdown (after Log traffic has ceased). Context cancels the
// drain wait. Idempotent.
func (a *Appender) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if !a.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(a.ch) // signal the writer to drain + exit
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f != nil {
		_ = a.f.Close()
		a.f = nil
	}
	return nil
}
