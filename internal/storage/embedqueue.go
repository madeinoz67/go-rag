package storage

import "encoding/json"

// EmbedQueueItem is the pending-embed work-queue record (spec 030, prefix 0x14).
// Written atomically with the chunk (0x03) on ACK; removed when the embedding lands
// (0x04 written); marked status=failed on a permanent embed failure. The queue IS
// the crash-recovery signal: a record in 0x14 means "this chunk needs (re)embedding."
type EmbedQueueItem struct {
	Model    string `json:"model"`                       // embedding model to use
	Status   string `json:"status"`                      // pending | failed
	Attempts int    `json:"attempts,omitempty"`           // transient retry count
}

const (
	EmbedQueuePending = "pending"
	EmbedQueueFailed  = "failed"
)

// PutEmbedQueue enqueues a pending-embed record: key = chunkID, value = JSON item.
func (d *DB) PutEmbedQueue(chunkID string, item []byte) error {
	return d.SetWithPrefix(PrefixEmbedQueue, []byte(chunkID), item)
}

// PutEmbedQueueItem is a convenience that marshals the item before storing.
func (d *DB) PutEmbedQueueItem(chunkID, model string) error {
	rec, _ := json.Marshal(EmbedQueueItem{Model: model, Status: EmbedQueuePending})
	return d.PutEmbedQueue(chunkID, rec)
}

// GetEmbedQueue reads a pending-embed record. ok=false if absent (already embedded
// or never queued).
func (d *DB) GetEmbedQueue(chunkID string) (item EmbedQueueItem, ok bool, err error) {
	val, found, e := d.GetWithPrefix(PrefixEmbedQueue, []byte(chunkID))
	if !found || e != nil {
		return EmbedQueueItem{}, false, e
	}
	if json.Unmarshal(val, &item) != nil {
		return EmbedQueueItem{}, false, nil
	}
	return item, true, nil
}

// DeleteEmbedQueue removes a pending-embed record (called after the embedding lands).
func (d *DB) DeleteEmbedQueue(chunkID string) error {
	return d.DeleteWithPrefix(PrefixEmbedQueue, []byte(chunkID))
}

// ScanEmbedQueue iterates the pending-embed queue, invoking fn(chunkID, item) per
// entry. Iteration stops if fn returns false.
func (d *DB) ScanEmbedQueue(fn func(chunkID string, item EmbedQueueItem) bool) error {
	return d.PrefixScanByte(PrefixEmbedQueue, func(key, val []byte) bool {
		var item EmbedQueueItem
		if json.Unmarshal(val, &item) != nil {
			return true // skip unparseable
		}
		return fn(string(key[1:]), item) // strip prefix byte
	})
}

// CountEmbedQueue counts pending-embed records (the backlog — surfaced in status).
func (d *DB) CountEmbedQueue() int {
	n := 0
	_ = d.ScanEmbedQueue(func(_ string, _ EmbedQueueItem) bool { n++; return true })
	return n
}
