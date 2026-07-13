package storage

// poison.go provides the quarantine-index primitives for retrieval-poisoning
// (spec 019 / audit H04). The verdict itself rides the Chunk record (prefix 0x03,
// free batch write); this 0x11 index is a SECONDARY index of flagged chunks for
// O(flagged) listing (US2 ListPoisoned). Helpers are byte-generic to keep the
// storage layer model-agnostic (callers marshal model.PoisonVerdict). All access
// is vault-scoped (spec 052).

import "github.com/madeinoz67/go-rag/internal/storage/keys"

// PutQuarantine indexes a flagged chunk for one vault:
// key = keys.QuarantineKey(ws, chunkID) (`0x11|ws|chunkID`), value = marshalled verdict.
// Idempotent (overwrites). Called when a chunk scores suspicious/quarantine.
func (d *DB) PutQuarantine(ws [8]byte, chunkID string, verdict []byte) error {
	return d.Set(keys.QuarantineKey(ws, chunkID), verdict)
}

// DeleteQuarantine removes a chunk from one vault's quarantine index (on
// release/reset, or when a re-score downgrades it to clean).
func (d *DB) DeleteQuarantine(ws [8]byte, chunkID string) error {
	return d.Delete(keys.QuarantineKey(ws, chunkID))
}

// ScanQuarantine iterates ONE vault's quarantine index, invoking fn(chunkID,
// verdictBytes) per entry. Iteration stops if fn returns false.
func (d *DB) ScanQuarantine(ws [8]byte, fn func(chunkID string, verdict []byte) bool) error {
	lower, upper, err := keys.VaultKindRange(PrefixPoisonQuar, ws)
	if err != nil {
		return err
	}
	return d.RangeScan(lower, upper, func(key, val []byte) bool {
		return fn(string(key[9:]), val) // strip `kind|ws`
	})
}

// ScanThreatSources iterates ONE vault's threat-source store (0x12), invoking
// fn(id, bytes) per entry. Iteration stops if fn returns false.
func (d *DB) ScanThreatSources(ws [8]byte, fn func(id string, val []byte) bool) error {
	lower, upper, err := keys.VaultKindRange(PrefixThreatSrc, ws)
	if err != nil {
		return err
	}
	return d.RangeScan(lower, upper, func(key, val []byte) bool {
		return fn(string(key[9:]), val) // strip `kind|ws`
	})
}
