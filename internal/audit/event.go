// Package audit is go-rag's append-only JSONL audit logger (spec 021 / audit H18,
// book §11.4). It records every query (by SHA-256 hash — never plaintext), every
// ingest, and every failed authentication to a LOCAL vault file. The log is
// append-only, size-capped (rotated), and never transmitted off-host (Constitution I).
//
// The Appender is the only audit-importing package; engine + transports call Log (a
// non-blocking channel send), keeping the append off the caller's path (Constitution IV).
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Event type discriminators.
const (
	TypeQuery    = "query"
	TypeIngest   = "ingest"
	TypeAuthFail = "auth-fail"
)

// Event is one JSONL audit record. `Type` selects which fields are populated; omitted
// fields are dropped from the JSON (omitempty). No query plaintext, no document
// content, and no credentials appear on any record (privacy, book §11.4).
type Event struct {
	TS   time.Time `json:"ts"`
	Type string    `json:"type"`

	// query
	QueryHash string `json:"query_hash,omitempty"` // SHA-256 hex — NEVER the plaintext
	Mode      string `json:"mode,omitempty"`
	K         int    `json:"k,omitempty"`
	Hits      int    `json:"hits,omitempty"`

	// query + ingest
	Status string `json:"status,omitempty"` // ok | error

	// ingest
	Op      string `json:"op,omitempty"`
	Path    string `json:"path,omitempty"`
	New     int    `json:"new,omitempty"`
	Skipped int    `json:"skipped,omitempty"`
	Errors  int    `json:"errors,omitempty"`

	// auth-fail
	Transport string `json:"transport,omitempty"` // rest | grpc | mcp
	Detail    string `json:"detail,omitempty"`    // short reason; NEVER the rejected token
}

// Marshal encodes the event as one JSONL line (with trailing newline).
func (e Event) Marshal() ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// QueryHash returns the SHA-256 hex hash of the query text — the only form in which a
// query is allowed on an audit record (never the plaintext).
func QueryHash(query string) string {
	h := sha256.Sum256([]byte(query))
	return hex.EncodeToString(h[:])
}

// QueryEvent builds a query audit event. status derives from err.
func QueryEvent(query, mode string, k, hits int, err error) Event {
	return Event{TS: time.Now().UTC(), Type: TypeQuery, QueryHash: QueryHash(query), Mode: mode, K: k, Hits: hits, Status: statusOf(err)}
}

// IngestEvent builds an ingest audit event (counts only — no content).
func IngestEvent(op, path string, new, skipped, errors int, err error) Event {
	return Event{TS: time.Now().UTC(), Type: TypeIngest, Op: op, Path: path, New: new, Skipped: skipped, Errors: errors, Status: statusOf(err)}
}

// AuthFailEvent builds an auth-fail audit event. detail is a short reason (never the
// rejected credential).
func AuthFailEvent(transport, detail string) Event {
	return Event{TS: time.Now().UTC(), Type: TypeAuthFail, Transport: transport, Detail: detail}
}

func statusOf(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}
