package storage

// near.go provides the near-duplicate fingerprint index primitives (audit H20 /
// spec 026). The resolved sibling relationships ride the Chunk record (prefix
// 0x03); this 0x13 index maps chunkID → 64-bit SimHash fingerprint for the
// ingest-time sibling scan. Helpers are byte-generic (callers encode uint64),
// mirroring poison.go's quarantine index.

import "encoding/binary"

// PutNearDup indexes a chunk's SimHash fingerprint: key = chunkID, value = 8-byte
// big-endian uint64. Idempotent (overwrites). Called on the ACK path when a chunk
// is fingerprinted.
func (d *DB) PutNearDup(chunkID string, fp uint64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], fp)
	return d.SetWithPrefix(PrefixNearDup, []byte(chunkID), buf[:])
}

// GetNearDup reads a chunk's SimHash fingerprint, if present.
func (d *DB) GetNearDup(chunkID string) (uint64, bool) {
	val, ok, _ := d.GetWithPrefix(PrefixNearDup, []byte(chunkID))
	if !ok || len(val) < 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(val[:8]), true
}

// DeleteNearDup removes a chunk's fingerprint (on chunk delete).
func (d *DB) DeleteNearDup(chunkID string) error {
	return d.DeleteWithPrefix(PrefixNearDup, []byte(chunkID))
}

// ScanNearDup iterates the fingerprint index, invoking fn(chunkID, fingerprint)
// per entry. Iteration stops if fn returns false. (PrefixScan includes the prefix
// byte in the key; stripped here so callers get the bare chunkID.)
func (d *DB) ScanNearDup(fn func(chunkID string, fp uint64) bool) error {
	return d.PrefixScanByte(PrefixNearDup, func(key, val []byte) bool {
		if len(val) < 8 {
			return true // skip malformed
		}
		return fn(string(key[1:]), binary.BigEndian.Uint64(val[:8]))
	})
}
