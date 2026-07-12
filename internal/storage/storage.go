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
	PrefixNearDup      byte = 0x13 // H20/spec 026: near-dup SimHash fingerprint (chunkID → uint64) for sibling scan
	PrefixEmbedQueue   byte = 0x14 // spec 030: durable pending-embed work queue (chunkID → {model,status,attempts})
	PrefixImageCaption byte = 0x15 // spec 031: cross-doc image-caption cache (image SHA-256 → {caption,model})

	// Auth (spec 045). 0x16 is reserved for BL-011 (webhook registry, planned)
	// per the bridge backlog — auth takes the bytes above it to avoid a future collision.
	PrefixAuthAPIKey  byte = 0x17 // spec 045: API-key record (SHA-256(secret)[:16] → APIKey JSON)
	PrefixAuthAdmin   byte = 0x18 // spec 045: admin-user record (username → AdminUser JSON)
	PrefixAuthSession byte = 0x19 // spec 045: session record (SHA-256(token)[:16] → Session JSON)

	// Vault registry (spec 052 / v2.0 storage model). These are GLOBAL — the
	// registry records vaults and MUST NOT be scoped by the very prefix it maps.
	// VaultMeta is keyed by ws[8] (scan to list); VaultNameIndex is keyed by
	// siphash(name)[8] (point-get to resolve). See internal/storage/keys/keys.go
	// and internal/storage/vault.go.
	PrefixVaultMeta      byte = 0x1A // spec 052: ws[8] → vault name (list index)
	PrefixVaultNameIndex byte = 0x1B // spec 052: siphash(name)[8] → ws[8] (resolve index)
)

// VaultScopedKinds is the fixed set of key-prefix kinds that widen with an
// 8-byte wsPrefix under the v2.0 unified store (spec 052 / data-model.md).
// Every key in these families has shape `kind | wsPrefix(8) | payload`. The
// v3→v4 migration mechanically prepends ws to each; ClearVault tombstones each
// of these ranges for one vault.
//
// The global kinds NOT in this list (Config 0x09, AuthAPIKey 0x17, AuthAdmin
// 0x18, AuthSession 0x19, VaultMeta 0x1A, VaultNameIndex 0x1B) keep the flat
// `kind | payload` shape — they are instance-wide, not per-vault.
var VaultScopedKinds = []byte{
	0x01, // Source
	0x02, // Document
	0x03, // Chunk
	0x04, // Embedding
	0x05, // FTSPosting
	0x07, // FTSIndexed
	0x08, // FTSGlobalSt
	0x0A, // SourceDocs
	0x0B, // DocChunks
	0x0C, // PathDoc
	0x0D, // ContentHash
	0x0E, // ChangeDetect
	0x0F, // Idempotency
	0x10, // CorpusMeta
	0x11, // PoisonQuar
	0x12, // ThreatSrc
	0x13, // NearDup
	0x14, // EmbedQueue
	0x15, // ImageCaption
}
