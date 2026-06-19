// Package storage wraps the embedded Pebble KV store (PRD §6.7, §4.2).
//
// All data — documents, chunks, embeddings, indexes — lives in one Pebble
// database. Logical data types are separated by single-byte key prefixes,
// enabling efficient prefix scans and independent index rebuilds.
//
// TODO(later): import cockroachdb/pebble and implement the DB type. The key-space
// contract is fixed here so index/pipeline code can be written against stable
// prefixes before the store is wired.
package storage

// Key-space prefixes (PRD §6.7). Single byte, prefix-partitioned.
const (
	PrefixSource       byte = 0x01 // Source records
	PrefixDocument     byte = 0x02 // Document records
	PrefixChunk        byte = 0x03 // Chunk records
	PrefixEmbedding    byte = 0x04 // Embedding metadata
	// 0x05–0x08 reserved for the BM25 FTS inverted index.
	PrefixConfig       byte = 0x09 // Config key/value store
	PrefixSourceDocs   byte = 0x0A // Source -> Document secondary index
	PrefixDocChunks    byte = 0x0B // Document -> Chunks ordered index
	PrefixPathDoc      byte = 0x0C // File path -> Document ID lookup
	PrefixContentHash  byte = 0x0D // Content hash index (dedup)
	PrefixChangeDetect byte = 0x0E // Change detection state
	PrefixIdempotency  byte = 0x0F // Idempotency receipts
)

// DB wraps the embedded Pebble store. The pebble handle is added when the
// storage task is implemented; for now this is the stable type skeleton.
type DB struct {
	// pebble *pebble.DB
	path string
}

// Open opens (or creates) the database at path. Stub.
func Open(path string) (*DB, error) {
	return &DB{path: path}, nil
}

// Close closes the database. Stub.
func (d *DB) Close() error { return nil }
