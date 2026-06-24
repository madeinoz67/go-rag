package engine

// audit_test.go (package engine, T010) proves US1 end-to-end: with the global audit
// appender set, a query + an ingest each produce a correctly-typed JSONL record, and
// the query plaintext never appears (only its hash). Mirrors the daemon wiring
// (audit.Init + audit.SetGlobal).

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/audit"
)

func TestAudit_QueryIngestEvents(t *testing.T) {
	// Init the global audit appender to an isolated path.
	dir := t.TempDir()
	ap := audit.DefaultPath(dir)
	aud, err := audit.Init(ap, 0)
	if err != nil {
		t.Fatal(err)
	}
	audit.SetGlobal(aud)
	defer func() {
		audit.SetGlobal(nil)
		_ = aud.Close(context.Background())
	}()

	e := newCacheEngine(t)
	const q = "audit-sentinel-query-98765"
	addDoc(t, e, "audit test document about local retrieval and search ranking")

	// A query (→ query event) — the ingest above already produced an ingest event.
	if _, err := e.Query(context.Background(), QueryRequest{Query: q, Mode: "keyword", K: 5}); err != nil {
		t.Fatal(err)
	}

	// Drain the async audit writer before reading. Log is non-blocking (buffered
	// channel drained by a writer goroutine), so the query event may still be
	// pending when we read the file — reading first races and can miss it. Close
	// drains + flushes (idempotent; the deferred Close below is then a no-op),
	// making the read deterministic. os.ReadFile reopens the path independently of
	// the appender's (now-closed) handle.
	_ = aud.Close(context.Background())

	data, err := os.ReadFile(ap)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"type":"query"`) {
		t.Error("audit log: missing query event")
	}
	if !strings.Contains(s, `"type":"ingest"`) {
		t.Error("audit log: missing ingest event")
	}
	// Privacy (FR-002/SC-002): the query plaintext must NOT appear (only its hash).
	if strings.Contains(s, q) {
		t.Error("audit log leaked query plaintext (must be hashed only)")
	}
}
