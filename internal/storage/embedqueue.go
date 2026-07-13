package storage

import (
	"encoding/json"

	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// EmbedQueueItem is the pending-embed work-queue record (spec 030, prefix 0x14).
// Written atomically with the chunk (0x03) on ACK; removed when the embedding lands
// (0x04 written); marked status=failed on a permanent embed failure. The queue IS
// the crash-recovery signal: a record in 0x14 means "this chunk needs (re)embedding."
type EmbedQueueItem struct {
	Model    string `json:"model"`              // embedding model to use
	Status   string `json:"status"`             // pending | failed
	Attempts int    `json:"attempts,omitempty"` // transient retry count
}

const (
	EmbedQueuePending = "pending"
	EmbedQueueFailed  = "failed"
)

// PutEmbedQueue enqueues a pending-embed record for one vault:
// key = keys.EmbedQueueKey(ws, chunkID) (`0x14|ws|chunkID`), value = JSON item.
func (d *DB) PutEmbedQueue(ws [8]byte, chunkID string, item []byte) error {
	return d.Set(keys.EmbedQueueKey(ws, chunkID), item)
}

// PutEmbedQueueItem is a convenience that marshals the item before storing.
func (d *DB) PutEmbedQueueItem(ws [8]byte, chunkID, model string) error {
	rec, _ := json.Marshal(EmbedQueueItem{Model: model, Status: EmbedQueuePending})
	return d.PutEmbedQueue(ws, chunkID, rec)
}

// GetEmbedQueue reads a pending-embed record. ok=false if absent (already embedded
// or never queued).
func (d *DB) GetEmbedQueue(ws [8]byte, chunkID string) (item EmbedQueueItem, ok bool, err error) {
	val, found, e := d.Get(keys.EmbedQueueKey(ws, chunkID))
	if !found || e != nil {
		return EmbedQueueItem{}, false, e
	}
	if json.Unmarshal(val, &item) != nil {
		return EmbedQueueItem{}, false, nil
	}
	return item, true, nil
}

// DeleteEmbedQueue removes a pending-embed record (called after the embedding lands).
func (d *DB) DeleteEmbedQueue(ws [8]byte, chunkID string) error {
	return d.Delete(keys.EmbedQueueKey(ws, chunkID))
}

// ScanEmbedQueue iterates ONE vault's pending-embed queue, invoking fn(chunkID, item)
// per entry. Iteration stops if fn returns false. Bounds come from
// keys.VaultKindRange so the scan never crosses into another vault's queue.
func (d *DB) ScanEmbedQueue(ws [8]byte, fn func(chunkID string, item EmbedQueueItem) bool) error {
	lower, upper, err := keys.VaultKindRange(PrefixEmbedQueue, ws)
	if err != nil {
		return err
	}
	return d.RangeScan(lower, upper, func(key, val []byte) bool {
		var item EmbedQueueItem
		if json.Unmarshal(val, &item) != nil {
			return true // skip unparseable
		}
		return fn(string(key[9:]), item) // strip `kind|ws` (1+8 bytes)
	})
}

// CountEmbedQueue counts ONE vault's pending-embed records (the backlog — surfaced
// in status).
func (d *DB) CountEmbedQueue(ws [8]byte) int {
	n := 0
	_ = d.ScanEmbedQueue(ws, func(_ string, _ EmbedQueueItem) bool { n++; return true })
	return n
}
