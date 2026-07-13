package engine

// audit_read_test.go (package engine, spec 049 T003) pins the thin read-only
// AuditRead wrapper: path resolution (configured vs default), Tail + Type
// pass-through parity with a direct audit.Read, the healthy-empty posture on a
// missing log, and the read-only invariant (no DB mutation).

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// newAuditReadEngine builds a minimal engine on a temp DB. The wrapper touches
// only e.cfg (path resolution) and the filesystem (audit.Read), so no pipeline
// or ingestion is wired — the engine is a read-only vehicle. Returns the engine
// and the vault dir (whose <dir>/audit/audit.log is the default audit path).
func newAuditReadEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	cfg := config.Default()
	cfg.DBPath = dir
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eng := NewWithDB(cfg, db)
	t.Cleanup(eng.Close)
	return eng, dir
}

// writeAuditEvents appends JSONL events to path (creating the parent dir),
// using Event.Marshal so the bytes are byte-identical to what the appender
// writes in production.
func writeAuditEvents(t *testing.T, path string, events ...audit.Event) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir audit: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	defer f.Close()
	for _, e := range events {
		line, err := e.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := f.Write(line); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

// snapCounts captures the mutable Status() counts as a string for a
// before/after equality check (the read-only invariant).
func snapCounts(t *testing.T, eng *Engine) string {
	t.Helper()
	s, err := eng.Status("default")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	return fmt.Sprintf("docs=%d chunks=%d emb=%d pend=%d fail=%d",
		s.Documents, s.Chunks, s.Embeddings, s.EmbedPending, s.EmbedFailed)
}

// TestAuditRead_DefaultPathResolution — empty AuditPath resolves to
// <dbPath>/audit/audit.log (the same path `go-rag audit` reads), and the events
// come back oldest→newest, matching audit.Read.
func TestAuditRead_DefaultPathResolution(t *testing.T) {
	eng, dir := newAuditReadEngine(t)
	want := []audit.Event{
		audit.IngestEvent("add", "docs/a.md", 3, 0, 0, nil),
		audit.IngestEvent("add", "docs/b.md", 5, 1, 0, nil),
	}
	writeAuditEvents(t, audit.DefaultPath(dir), want...)

	got, err := eng.AuditRead("default", audit.ReadOptions{All: true})
	if err != nil {
		t.Fatalf("AuditRead: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i, e := range got {
		if e.Type != want[i].Type || e.Path != want[i].Path || e.New != want[i].New {
			t.Fatalf("event %d: got %+v want %+v", i, e, want[i])
		}
	}
}

// TestAuditRead_ConfiguredPath — a non-empty AuditPath overrides the default,
// so the wrapper reads exactly where the operator pointed the log.
func TestAuditRead_ConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	custom := filepath.Join(dir, "custom-audit.log")
	cfg := config.Default()
	cfg.DBPath = dir
	cfg.AuditPath = custom
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eng := NewWithDB(cfg, db)
	t.Cleanup(eng.Close)

	writeAuditEvents(t, custom, audit.IngestEvent("add", "x.md", 1, 0, 0, nil))
	// The default path must not exist — the wrapper should not fall back to it.
	if _, err := os.Stat(audit.DefaultPath(dir)); err == nil {
		t.Fatal("default audit path should not exist when AuditPath is set")
	}

	got, err := eng.AuditRead("default", audit.ReadOptions{})
	if err != nil {
		t.Fatalf("AuditRead: %v", err)
	}
	if len(got) != 1 || got[0].Path != "x.md" {
		t.Fatalf("got %+v", got)
	}
}

// TestAuditRead_TailAndType_PassThrough — Tail + Type pass straight through to
// audit.Read, so the wrapper is byte-identical to a direct call on the resolved
// path with the same options (parity with the CLI's underlying read).
func TestAuditRead_TailAndType_PassThrough(t *testing.T) {
	eng, dir := newAuditReadEngine(t)
	path := audit.DefaultPath(dir)
	writeAuditEvents(t, path,
		audit.IngestEvent("add", "i1.md", 1, 0, 0, nil),
		audit.QueryEvent("q1", "hybrid", 5, 3, nil),
		audit.IngestEvent("add", "i2.md", 2, 0, 0, nil),
		audit.QueryEvent("q2", "keyword", 3, 1, nil),
		audit.IngestEvent("add", "i3.md", 4, 0, 1, nil),
	)

	opts := audit.ReadOptions{Type: audit.TypeIngest, Tail: 2}
	got, err := eng.AuditRead("default", opts)
	if err != nil {
		t.Fatalf("AuditRead: %v", err)
	}
	// audit.Read returns oldest→newest; the last 2 ingest events are i2, i3.
	if len(got) != 2 {
		t.Fatalf("len: got %d want 2", len(got))
	}
	if got[0].Path != "i2.md" || got[1].Path != "i3.md" {
		t.Fatalf("order/content: got %s, %s", got[0].Path, got[1].Path)
	}
	// Parity: identical to a direct audit.Read on the resolved path.
	direct, err := audit.Read(path, opts)
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(direct) != len(got) {
		t.Fatalf("parity len: wrapper %d direct %d", len(got), len(direct))
	}
	for i := range got {
		if got[i].Path != direct[i].Path || got[i].New != direct[i].New {
			t.Fatalf("parity event %d: wrapper %+v direct %+v", i, got[i], direct[i])
		}
	}
}

// TestAuditRead_MissingLogIsEmpty — no audit file present → empty slice, no
// error. This is the healthy-empty posture the UI activity feed relies on (a
// quiet or audit-disabled vault is not an error state).
func TestAuditRead_MissingLogIsEmpty(t *testing.T) {
	eng, _ := newAuditReadEngine(t)
	got, err := eng.AuditRead("default", audit.ReadOptions{Type: audit.TypeIngest, Tail: 20})
	if err != nil {
		t.Fatalf("AuditRead on missing log: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing log: got %d events, want 0", len(got))
	}
}

// TestAuditRead_IsReadOnly — a read does not mutate the DB's stored counts
// (the read-only invariant that the Bridge Ops no-write test re-derives at the
// transport level).
func TestAuditRead_IsReadOnly(t *testing.T) {
	eng, dir := newAuditReadEngine(t)
	writeAuditEvents(t, audit.DefaultPath(dir),
		audit.IngestEvent("add", "a.md", 2, 0, 0, nil),
		audit.QueryEvent("q", "hybrid", 5, 1, nil),
	)
	before := snapCounts(t, eng)
	if _, err := eng.AuditRead("default", audit.ReadOptions{All: true, Type: audit.TypeIngest, Tail: 5}); err != nil {
		t.Fatalf("AuditRead: %v", err)
	}
	after := snapCounts(t, eng)
	if before != after {
		t.Fatalf("AuditRead mutated DB: before=%s after=%s", before, after)
	}
}
