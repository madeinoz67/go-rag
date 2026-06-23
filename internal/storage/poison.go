package storage

// poison.go provides the quarantine-index primitives for retrieval-poisoning
// (spec 019 / audit H04). The verdict itself rides the Chunk record (prefix 0x03,
// free batch write); this 0x11 index is a SECONDARY index of flagged chunks for
// O(flagged) listing (US2 ListPoisoned). Helpers are byte-generic to keep the
// storage layer model-agnostic (callers marshal model.PoisonVerdict).

// PutQuarantine indexes a flagged chunk: key = chunkID, value = marshalled verdict.
// Idempotent (overwrites). Called when a chunk scores suspicious/quarantine.
func (d *DB) PutQuarantine(chunkID string, verdict []byte) error {
	return d.SetWithPrefix(PrefixPoisonQuar, []byte(chunkID), verdict)
}

// DeleteQuarantine removes a chunk from the quarantine index (on release/reset,
// or when a re-score downgrades it to clean).
func (d *DB) DeleteQuarantine(chunkID string) error {
	return d.DeleteWithPrefix(PrefixPoisonQuar, []byte(chunkID))
}

// ScanQuarantine iterates the quarantine index, invoking fn(chunkID, verdictBytes)
// per entry. Iteration stops if fn returns false. (PrefixScan includes the prefix
// byte in the key; stripped here so callers get the bare chunkID.)
func (d *DB) ScanQuarantine(fn func(chunkID string, verdict []byte) bool) error {
	return d.PrefixScanByte(PrefixPoisonQuar, func(key, val []byte) bool {
		return fn(string(key[1:]), val)
	})
}

// ScanThreatSources iterates the threat-source store (0x12), invoking fn(id, bytes)
// per entry. (PrefixScan includes the prefix byte; stripped here.)
func (d *DB) ScanThreatSources(fn func(id string, val []byte) bool) error {
	return d.PrefixScanByte(PrefixThreatSrc, func(key, val []byte) bool {
		return fn(string(key[1:]), val)
	})
}
