package storage

import (
	"bytes"
	"fmt"
	"os"
	"sync"

	"github.com/cockroachdb/pebble"
)

// DB wraps the embedded Pebble store. Pebble holds a file lock on the data
// directory, so only one process may Open a given path at a time — enforcing the
// single-writer invariant (Principle IV, research Q6).
type DB struct {
	db   *pebble.DB
	path string

	// vaultPrefixCache memoizes vault-name → wsPrefix resolutions (spec 052).
	// It is the hot path of ResolveVaultPrefix and is seeded from the persisted
	// VaultNameIndex (0x1B) at startup so the SipHash fallback stays cold-only.
	// Bounded (10k); single-operator scale never approaches the bound.
	vaultPrefixCache *vaultCache

	// vaultNameWritten records wsPrefixes whose VaultMeta/VaultNameIndex keys
	// have already been persisted this session, so WriteVaultName's existence
	// check fires at most once per vault per process (the per-write fast path).
	vaultNameWritten sync.Map
}

// quietLogger suppresses Pebble's chatty Info-level logs (WAL replay notices,
// compaction events) that would otherwise interrupt CLI output. Fatalf still
// prints so real crashes are visible.
type quietLogger struct{}

func (quietLogger) Infof(_ string, _ ...interface{}) {} // suppress
func (quietLogger) Fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "pebble: "+format+"\n", args...)
}

// Open creates or opens the Pebble database at path. A second Open on the same
// path (even from the same process) fails with a lock error — single-writer.
func Open(path string) (*DB, error) {
	db, err := pebble.Open(path, &pebble.Options{Logger: quietLogger{}})
	if err != nil {
		return nil, fmt.Errorf("open pebble %q: %w", path, err)
	}
	return &DB{db: db, path: path, vaultPrefixCache: newVaultCache(vaultCacheCapacity)}, nil
}

// Close closes the database.
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// Pebble returns the underlying *pebble.DB handle for low-level operations
// (iterators, batches) that the prefix-partitioned helpers don't expose.
// Callers MUST use the correct key prefixes (see storage.go) — this bypasses
// the prefix discipline. Used by the Pebble-backed FTS (audit H16/spec 018).
func (d *DB) Pebble() *pebble.DB {
	if d == nil {
		return nil
	}
	return d.db
}

// Set stores key->value with a durable (Sync) write.
func (d *DB) Set(key, value []byte) error {
	return d.db.Set(key, value, pebble.Sync)
}

// SetWithPrefix stores a single-byte-prefixed key (use the Prefix* constants).
func (d *DB) SetWithPrefix(prefix byte, key, value []byte) error {
	return d.db.Set(append([]byte{prefix}, key...), value, pebble.Sync)
}

// Get returns the value for key (no prefix). ok is false if the key is absent.
func (d *DB) Get(key []byte) (value []byte, ok bool, err error) {
	v, closer, err := d.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer closer.Close()
	out := make([]byte, len(v))
	copy(out, v)
	return out, true, nil
}

// GetWithPrefix looks up a single-byte-prefixed key.
func (d *DB) GetWithPrefix(prefix byte, key []byte) ([]byte, bool, error) {
	return d.Get(append([]byte{prefix}, key...))
}

// Delete removes a key (no prefix).
func (d *DB) Delete(key []byte) error {
	return d.db.Delete(key, pebble.Sync)
}

// DeleteWithPrefix removes a single-byte-prefixed key.
func (d *DB) DeleteWithPrefix(prefix byte, key []byte) error {
	return d.db.Delete(append([]byte{prefix}, key...), pebble.Sync)
}

// PrefixScan iterates over all keys beginning with prefix, invoking fn for each
// (key and value include the prefix byte). Iteration stops if fn returns false.
func (d *DB) PrefixScan(prefix []byte, fn func(key, value []byte) bool) error {
	iter, err := d.db.NewIter(&pebble.IterOptions{LowerBound: prefix})
	if err != nil {
		return err
	}
	defer iter.Close()
	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}
		if !fn(iter.Key(), iter.Value()) {
			break
		}
	}
	return iter.Error()
}

// PrefixScanByte is a convenience wrapper for a single-byte prefix.
//
// NOTE (spec 052): this and PrefixScan above are retained ONLY for GLOBAL
// prefixes (Config 0x09, Auth 0x17/0x18/0x19, VaultMeta 0x1A, VaultNameIndex
// 0x1B). Vault-scoped access (0x01–0x15) MUST go through the keys-package
// builders + bare Set/Get/Delete + RangeScan so wsPrefix is prepended. Calling
// PrefixScanByte with a vault-scoped kind scans ALL vaults' data for that kind
// — a cross-vault leak. Callers that need one vault's data use RangeScan with
// bounds from keys.VaultKindRange.
func (d *DB) PrefixScanByte(prefix byte, fn func(key, value []byte) bool) error {
	return d.PrefixScan([]byte{prefix}, fn)
}

// RangeScan iterates over the inclusive-lower / exclusive-upper key range,
// invoking fn for each key/value pair. It is the vault-scoped scan primitive:
// callers pass bounds from keys.VaultKindRange(kind, ws) so the scan covers
// exactly one vault's data for one kind (`kind|ws` ≤ key < `kind|wsPlus`).
// Iteration stops if fn returns false.
func (d *DB) RangeScan(lower, upper []byte, fn func(key, value []byte) bool) error {
	iter, err := d.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		if !fn(iter.Key(), iter.Value()) {
			break
		}
	}
	return iter.Error()
}
