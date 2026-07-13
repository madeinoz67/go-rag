// audit_read.go (spec 049 Slice 3, R3) is a thin read-only wrapper exposing the
// existing audit.Read (spec 021 / audit H18) to in-process transports. The
// engine owns its config + paths, so the UI transport — the 4th adapter, a peer
// to REST/gRPC/MCP — can serve a bounded recent-activity feed without learning
// the filesystem, exactly as it calls Status / ListDocuments / Query.
//
// This is NOT a new operation: audit.Read already backs the `go-rag audit` CLI.
// Full audit cross-transport parity (REST/gRPC/MCP endpoints) is a pre-existing
// spec 021 gap, intentionally deferred (R9) — 049 adds only the UI consumer.

package engine

import "github.com/madeinoz67/go-rag/internal/audit"

// AuditRead returns filtered audit events by delegating to audit.Read after
// resolving the log path the same way the `go-rag audit` CLI does: the
// configured AuditPath when set, otherwise audit.DefaultPath(DBPath)
// (<dbPath>/audit/audit.log). Read-only — it opens no DB write and mutates
// nothing. A missing or disabled log returns an empty slice with no error
// (healthy empty, mirroring audit.Read and the CLI).
func (e *Engine) AuditRead(_ string, opts audit.ReadOptions) ([]audit.Event, error) {
	path := e.cfg.AuditPath
	if path == "" {
		path = audit.DefaultPath(e.cfg.DBPath)
	}
	return audit.Read(path, opts)
}
