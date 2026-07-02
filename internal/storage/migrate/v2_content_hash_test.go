package migrate

import (
	"encoding/json"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// TestRunV2BackfillsContentHash (spec 043 / BL-010): a v1-era chunk record (no
// ContentHash field) is backfilled on the v0→v2 run, and re-running v2 is
// idempotent (same SHA-256 written). Verifies the RedTeam caveat: a chunk whose
// sidecars were nil round-trips cleanly (no zero-value drift).
func TestRunV2BackfillsContentHash(t *testing.T) {
	db, cleanup := newDB(t)
	defer cleanup()

	// Seed a v1-era chunk record under PrefixChunk — no ContentHash (pre-v2 shape).
	pre := model.Chunk{ID: "chunk-1", DocumentID: "doc-1", Content: "the quick brown fox"}
	raw, _ := json.Marshal(pre)
	key := append([]byte{byte(storage.PrefixChunk)}, []byte(pre.ID)...)
	if err := db.Set(key, raw, pebble.Sync); err != nil {
		t.Fatal(err)
	}

	// Run v0→v2 (v1 bootstrap + v2 ContentHash backfill).
	applied, err := Run(db, 2, defaultMigrations)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if applied != 2 {
		t.Errorf("applied = %d, want 2 (bootstrap + contenthash)", applied)
	}

	// The chunk now carries ContentHash = SHA-256 of its content.
	got, closer, err := db.Get(key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer closer.Close()
	var c model.Chunk
	if err := json.Unmarshal(got, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := model.ContentHash([]byte("the quick brown fox"))
	if c.ContentHash != want {
		t.Errorf("ContentHash = %q, want %q", c.ContentHash, want)
	}

	// Idempotent: re-running v2 writes the same value (no drift).
	if err := v2ContentHash(db); err != nil {
		t.Fatalf("re-run v2ContentHash: %v", err)
	}
	got2, closer2, _ := db.Get(key)
	defer closer2.Close()
	var c2 model.Chunk
	if json.Unmarshal(got2, &c2) != nil {
		t.Fatal("re-unmarshal")
	}
	if c2.ContentHash != want {
		t.Errorf("after re-run ContentHash = %q, want %q (idempotency broke)", c2.ContentHash, want)
	}
}
