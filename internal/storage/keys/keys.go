// Package keys builds Pebble keys for the unified multi-vault store (spec 052).
//
// # Key shape
//
// Every vault-scoped key is `kindByte(1) | wsPrefix(8) | payload`. Leading with
// the kind byte keeps each vault's data for one kind in a single contiguous
// Pebble range, so a per-vault-per-kind scan is the O(1) range read
// `[kind|ws, kind|wsPlus)` where wsPlus = IncrementWSPrefix(ws).
//
// Every global key is `kindByte(1) | payload` (no wsPrefix) — config, auth, and
// the vault registry itself are instance-wide.
//
// wsPrefix is SipHash-2-4 of the vault name (BigEndian, fixed keys). It is the
// vault's identity inside the key space; the human-readable name lives only in
// the registry indexes (PrefixVaultMeta / PrefixVaultNameIndex). The hash is a
// deterministic PRF — no coordination is required to compute a vault's prefix.
//
// This package is pure: it constructs byte slices and computes hashes. It does
// not touch Pebble. Every storage caller and every migration step goes through
// these builders so the key layout has one source of truth (data-model.md).
package keys

import (
	"encoding/binary"
	"fmt"

	"github.com/dchest/siphash"
)

// SipHash keys for vault-prefix computation. Project-local constants; any
// 128-bit value works (SipHash is a PRF), but fixing them makes wsPrefix stable
// across builds so a migrated vault keeps its prefix for the life of the store.
var (
	sipKey0 uint64 = 0x676f72616772656d // "goragrem"
	sipKey1 uint64 = 0x6d756c7469766175 // "multivau"
)

// VaultPrefix computes the 8-byte workspace prefix for a vault name.
// It is the SipHash-2-4 of the name under the fixed project keys, BigEndian.
// Pure and deterministic: the same name always yields the same prefix, in any
// process, with no coordination. The collision space is 2^64 (astronomically
// unlikely to collide at single-operator scale).
func VaultPrefix(vault string) [8]byte {
	h := siphash.Hash(sipKey0, sipKey1, []byte(vault))
	var ws [8]byte
	binary.BigEndian.PutUint64(ws[:], h)
	return ws
}

// VaultNameHash computes the 8-byte SipHash of a vault name used as the key
// payload for the VaultNameIndex (0x1B). It equals VaultPrefix(name) for a
// never-renamed vault; after a rename it still hashes the CURRENT name so the
// index resolves the name the operator typed, not the frozen creation prefix.
func VaultNameHash(name string) [8]byte {
	h := siphash.Hash(sipKey0, sipKey1, []byte(name))
	var out [8]byte
	binary.BigEndian.PutUint64(out[:], h)
	return out
}

// IncrementWSPrefix returns ws+1 (BigEndian, carry-forward) for use as the
// exclusive upper bound of a per-vault-per-kind Pebble range scan or range
// tombstone. An all-0xFF ws overflows and is rejected — at single-operator
// scale this is unreachable (2^64 vaults), but failing closed here beats a
// silent wrap that would scan/tombstone the wrong range.
func IncrementWSPrefix(ws [8]byte) ([8]byte, error) {
	result := ws
	for i := 7; i >= 0; i-- {
		result[i]++
		if result[i] != 0 {
			return result, nil
		}
	}
	return [8]byte{}, fmt.Errorf("workspace prefix overflow: %x", ws)
}

// kindWSPrefix returns the 9-byte head `kind | ws` shared by every key in the
// (kind, vault) range. Every vault-scoped builder starts from this head.
func kindWSPrefix(kind byte, ws [8]byte) []byte {
	k := make([]byte, 9)
	k[0] = kind
	copy(k[1:9], ws[:])
	return k
}

// vaultScoped returns `kind | ws | payload` — the shape of every vault-scoped
// key whose payload is a single opaque byte slice (the common case in go-rag:
// source/document/chunk/embedding IDs, content hashes, path hashes, etc.).
func vaultScoped(kind byte, ws [8]byte, payload []byte) []byte {
	k := make([]byte, 1+8+len(payload))
	k[0] = kind
	copy(k[1:9], ws[:])
	copy(k[9:], payload)
	return k
}

// vaultScopedString is the string-payload convenience form of vaultScoped.
func vaultScopedString(kind byte, ws [8]byte, id string) []byte {
	return vaultScoped(kind, ws, []byte(id))
}

// --- Vault-scoped record keys (0x01–0x15) ----------------------------------
//
// Each builder mirrors the pre-multi-vault payload exactly (a single string ID,
// or the structured FTS posting) with ws inserted after the kind byte. The
// v3→v4 migration is a mechanical prepend, so preserving the payload shape is
// what makes migrated data reachable by the widened code paths.

// SourceKey: 0x01 | ws | sourceID.
func SourceKey(ws [8]byte, sourceID string) []byte { return vaultScopedString(0x01, ws, sourceID) }

// DocumentKey: 0x02 | ws | docID.
func DocumentKey(ws [8]byte, docID string) []byte { return vaultScopedString(0x02, ws, docID) }

// ChunkKey: 0x03 | ws | chunkID.
func ChunkKey(ws [8]byte, chunkID string) []byte { return vaultScopedString(0x03, ws, chunkID) }

// EmbeddingKey: 0x04 | ws | chunkID.
func EmbeddingKey(ws [8]byte, chunkID string) []byte { return vaultScopedString(0x04, ws, chunkID) }

// FTSPostingKey: 0x05 | ws | term | 0x00 | chunkID.
// The 0x00 separator terminates the variable-length term so a scan over
// `[0x05|ws|term|0x00, 0x05|ws|term|0x01)` covers every posting for that term
// in the vault. Matches the pre-multi-vault FTS layout (internal/index/fts.go)
// with ws inserted after the kind byte.
func FTSPostingKey(ws [8]byte, term, chunkID string) []byte {
	tb := []byte(term)
	k := make([]byte, 1+8+len(tb)+1+len(chunkID))
	k[0] = 0x05
	copy(k[1:9], ws[:])
	off := 9
	copy(k[off:off+len(tb)], tb)
	off += len(tb)
	k[off] = 0x00 // term terminator
	off++
	copy(k[off:], chunkID)
	return k
}

// FTSPostingTermPrefix returns `0x05 | ws | term` (no terminator) for use as
// the lower bound of a per-term posting scan. Callers pair it with the
// term-terminator upper bound; see FTSPostingRange.
func FTSPostingTermPrefix(ws [8]byte, term string) []byte {
	tb := []byte(term)
	k := make([]byte, 1+8+len(tb))
	k[0] = 0x05
	copy(k[1:9], ws[:])
	copy(k[9:], tb)
	return k
}

// FTSIndexedKey: 0x07 | ws | chunkID (the indexed-chunk idempotency guard).
func FTSIndexedKey(ws [8]byte, chunkID string) []byte { return vaultScopedString(0x07, ws, chunkID) }

// FTSGlobalStatsKey: 0x08 | ws | "stats". The literal suffix is preserved from
// the pre-multi-vault BM25 stats key so a migrated store stays readable.
func FTSGlobalStatsKey(ws [8]byte) []byte {
	k := make([]byte, 1+8+5)
	k[0] = 0x08
	copy(k[1:9], ws[:])
	copy(k[9:], []byte("stats"))
	return k
}

// SourceDocsKey: 0x0A | ws | sourceID | docID (source→document secondary index).
// Two-string compound payload with no separator — the sourceID length is known
// by the caller. Provided per data-model.md for the secondary index; the
// mechanical migration preserves whatever legacy payload existed.
func SourceDocsKey(ws [8]byte, sourceID, docID string) []byte {
	k := make([]byte, 1+8+len(sourceID)+len(docID))
	k[0] = 0x0A
	copy(k[1:9], ws[:])
	copy(k[9:9+len(sourceID)], sourceID)
	copy(k[9+len(sourceID):], docID)
	return k
}

// DocChunksKey: 0x0B | ws | docID | ordinalBE32 (document→chunks ordered index).
func DocChunksKey(ws [8]byte, docID string, ordinal uint32) []byte {
	k := make([]byte, 1+8+len(docID)+4)
	k[0] = 0x0B
	copy(k[1:9], ws[:])
	copy(k[9:9+len(docID)], docID)
	binary.BigEndian.PutUint32(k[9+len(docID):], ordinal)
	return k
}

// PathDocKey: 0x0C | ws | filePath. Value is the docID. Matches the
// pre-multi-vault PathDoc payload (a single path string) with ws inserted.
func PathDocKey(ws [8]byte, filePath string) []byte { return vaultScopedString(0x0C, ws, filePath) }

// ContentHashKey: 0x0D | ws | contentHash. Value is the docID.
func ContentHashKey(ws [8]byte, contentHash string) []byte {
	return vaultScopedString(0x0D, ws, contentHash)
}

// ChangeDetectKey: 0x0E | ws | pathHash.
func ChangeDetectKey(ws [8]byte, pathHash string) []byte {
	return vaultScopedString(0x0E, ws, pathHash)
}

// IdempotencyKey: 0x0F | ws | opKey.
func IdempotencyKey(ws [8]byte, opKey string) []byte { return vaultScopedString(0x0F, ws, opKey) }

// CorpusMetaKey: 0x10 | ws | key. The single corpus-baseline record per vault;
// `key` is the caller-defined sub-key (the baseline liveness check uses a fixed
// constant). ws makes the baseline per-vault.
func CorpusMetaKey(ws [8]byte, key string) []byte { return vaultScopedString(0x10, ws, key) }

// QuarantineKey: 0x11 | ws | chunkID (poison-chunk secondary index).
func QuarantineKey(ws [8]byte, chunkID string) []byte { return vaultScopedString(0x11, ws, chunkID) }

// ThreatSourceKey: 0x12 | ws | sourceID.
func ThreatSourceKey(ws [8]byte, sourceID string) []byte {
	return vaultScopedString(0x12, ws, sourceID)
}

// NearDupKey: 0x13 | ws | chunkID (SimHash fingerprint index).
func NearDupKey(ws [8]byte, chunkID string) []byte { return vaultScopedString(0x13, ws, chunkID) }

// EmbedQueueKey: 0x14 | ws | chunkID (durable pending-embed work queue).
func EmbedQueueKey(ws [8]byte, chunkID string) []byte { return vaultScopedString(0x14, ws, chunkID) }

// ImageCaptionKey: 0x15 | ws | imageHash (cross-doc image-caption cache).
func ImageCaptionKey(ws [8]byte, imageHash string) []byte {
	return vaultScopedString(0x15, ws, imageHash)
}

// --- Vault registry (GLOBAL: 0x1A / 0x1B, no ws in the key head) -----------
//
// These two prefixes ARE global — the registry must not be scoped by the very
// prefix it records. VaultMeta is keyed by ws (so a scan lists every vault);
// VaultNameIndex is keyed by SipHash(name) (so resolution is a point-get on the
// name the operator typed, even after a rename).

// VaultMetaKey: 0x1A | ws. Value is the human-readable vault name. Used by
// ListVaultNames via the scan [0x1A, 0x1B).
func VaultMetaKey(ws [8]byte) []byte {
	k := make([]byte, 9)
	k[0] = 0x1A
	copy(k[1:9], ws[:])
	return k
}

// VaultNameIndexKey: 0x1B | siphash(name). Value is the ws[8] the name resolves
// to. Keyed by the hash of the CURRENT name (not by ws) — this is the
// indirection that makes rename a two-key metadata operation instead of a full
// rehash of every data key. For a never-renamed vault siphash(name) == ws.
func VaultNameIndexKey(name string) []byte {
	h := VaultNameHash(name)
	k := make([]byte, 9)
	k[0] = 0x1B
	copy(k[1:9], h[:])
	return k
}

// --- Range helpers --------------------------------------------------------

// VaultKindRange returns the `[lower, upper)` byte slices covering every key of
// one kind in one vault: lower = `kind | ws`, upper = `kind | wsPlus`. Use for
// per-vault-per-kind scans and for the ClearVault range tombstones. The vault-
// scoped kinds are enumerated in internal/storage/storage.go (VaultScopedKinds).
func VaultKindRange(kind byte, ws [8]byte) (lower, upper []byte, err error) {
	wsPlus, err := IncrementWSPrefix(ws)
	if err != nil {
		return nil, nil, err
	}
	return kindWSPrefix(kind, ws), kindWSPrefix(kind, wsPlus), nil
}

// VaultKindLower returns `kind | ws` — the inclusive lower bound of a
// per-vault-per-kind range. Use when the caller already holds wsPlus (or builds
// the upper bound separately).
func VaultKindLower(kind byte, ws [8]byte) []byte { return kindWSPrefix(kind, ws) }
