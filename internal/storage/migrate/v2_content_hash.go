package migrate

import (
	"encoding/json"

	"github.com/cockroachdb/pebble"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// v2ContentHash (spec 043 / BL-010) backfills the per-chunk ContentHash sidecar
// (SHA-256 of the chunk's text) on every chunk record ingested before this
// feature. ContentHash rides in the existing PrefixChunk (0x03) JSON value — no
// new prefix, no key change; only the marshaled value gains a field.
//
// Idempotent + crash-safe: re-running recomputes the same SHA-256 and writes the
// same bytes (a no-op effect), so a crash before writeVersion advances is
// replayed safely. The Runner calls Up only when Version (2) > current.
//
// Iterates raw pebble (the migration Up receives *pebble.DB, not *storage.DB):
// the [PrefixChunk, PrefixChunk+1) key range captures exactly the chunk records.
// A single batched Commit with pebble.Sync makes the backfill atomic.
func v2ContentHash(db *pebble.DB) error {
	lo := []byte{byte(storage.PrefixChunk)}
	hi := []byte{byte(storage.PrefixChunk) + 1}
	iter, err := db.NewIter(&pebble.IterOptions{LowerBound: lo, UpperBound: hi})
	if err != nil {
		return err
	}
	defer iter.Close()

	batch := db.NewBatch()
	defer batch.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		// The iterator key slice is invalidated on the next Next() — copy it so
		// the batched Set references a stable key.
		key := append([]byte(nil), iter.Key()...)
		var c model.Chunk
		if json.Unmarshal(iter.Value(), &c) != nil {
			continue // skip unparseable records (defensive — shouldn't happen)
		}
		c.ContentHash = model.ContentHash([]byte(c.Content))
		newVal, err := json.Marshal(c)
		if err != nil {
			continue
		}
		if err := batch.Set(key, newVal, nil); err != nil {
			return err
		}
	}
	if err := iter.Error(); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}
