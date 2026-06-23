// Package storage wraps the embedded Pebble KV store (PRD §6.7, §4.2).
//
// All data — documents, chunks, embeddings, indexes — lives in one Pebble
// database. Logical data types are separated by single-byte key prefixes,
// enabling efficient prefix scans and independent index rebuilds. The DB type
// and Pebble wiring live in db.go.
package storage

// Key-space prefixes (PRD §6.7). Single byte, prefix-partitioned.
const (
	PrefixSource    byte = 0x01 // Source records
	PrefixDocument  byte = 0x02 // Document records
	PrefixChunk     byte = 0x03 // Chunk records
	PrefixEmbedding byte = 0x04 // Embedding metadata
	// 0x05–0x08 reserved for the BM25 FTS inverted index.
	PrefixFTSPosting   byte = 0x05 // H16/spec 018: FTS postings (term → chunkID → tf+docLen)
	PrefixFTSIndexed   byte = 0x07 // H16/spec 018: indexed-chunk set (chunkID → docLen; idempotency guard)
	PrefixFTSGlobalSt  byte = 0x08 // H16/spec 018: global BM25 stats (N + totalLen)
	PrefixConfig       byte = 0x09 // Config key/value store
	PrefixSourceDocs   byte = 0x0A // Source -> Document secondary index
	PrefixDocChunks    byte = 0x0B // Document -> Chunks ordered index
	PrefixPathDoc      byte = 0x0C // File path -> Document ID lookup
	PrefixContentHash  byte = 0x0D // Content hash index (dedup)
	PrefixChangeDetect byte = 0x0E // Change detection state
	PrefixIdempotency  byte = 0x0F // Idempotency receipts
	PrefixCorpusMeta   byte = 0x10 // H11/spec 017: corpus baseline metadata (embedding drift) — single record
	PrefixPoisonQuar   byte = 0x11 // H04/spec 019: quarantine index (chunkID → verdict) for O(flagged) listing
	PrefixThreatSrc    byte = 0x12 // H04/spec 019: threat-source store (FR-012/013, D12)
)
