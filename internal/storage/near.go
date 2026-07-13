package storage

// near.go provides the near-duplicate fingerprint index primitives (audit H20 /
// spec 026). The resolved sibling relationships ride the Chunk record (prefix
// 0x03); this 0x13 index maps chunkID → 64-bit SimHash fingerprint for the
// ingest-time sibling scan. Helpers are byte-generic (callers encode uint64),
// mirroring poison.go's quarantine index. All access is vault-scoped (spec 052).

import (
	"encoding/binary"

	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// PutNearDup indexes a chunk's SimHash fingerprint for one vault:
// key = keys.NearDupKey(ws, chunkID) (`0x13|ws|chunkID`), value = 8-byte BE uint64.
// Idempotent (overwrites). Called on the ACK path when a chunk is fingerprinted.
func (d *DB) PutNearDup(ws [8]byte, chunkID string, fp uint64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], fp)
	return d.Set(keys.NearDupKey(ws, chunkID), buf[:])
}

// GetNearDup reads a chunk's SimHash fingerprint for one vault, if present.
func (d *DB) GetNearDup(ws [8]byte, chunkID string) (uint64, bool) {
	val, ok, _ := d.Get(keys.NearDupKey(ws, chunkID))
	if !ok || len(val) < 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(val[:8]), true
}

// DeleteNearDup removes a chunk's fingerprint (on chunk delete).
func (d *DB) DeleteNearDup(ws [8]byte, chunkID string) error {
	return d.Delete(keys.NearDupKey(ws, chunkID))
}

// ScanNearDup iterates ONE vault's fingerprint index, invoking fn(chunkID, fp)
// per entry. Iteration stops if fn returns false. The vault-kind range bounds keep
// the scan inside one vault.
func (d *DB) ScanNearDup(ws [8]byte, fn func(chunkID string, fp uint64) bool) error {
	lower, upper, err := keys.VaultKindRange(PrefixNearDup, ws)
	if err != nil {
		return err
	}
	return d.RangeScan(lower, upper, func(key, val []byte) bool {
		if len(val) < 8 {
			return true // skip malformed
		}
		return fn(string(key[9:]), binary.BigEndian.Uint64(val[:8])) // strip `kind|ws`
	})
}
