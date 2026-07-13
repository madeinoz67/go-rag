package index

import (
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

func TestFTS_RanksRelevantChunkFirst(t *testing.T) {
	f := newTestFTS(t)
	ws := keys.VaultPrefix("default")
	f.Index(ws, "c1", map[string]string{"body": "the authentication system uses jwt tokens"})
	f.Index(ws, "c2", map[string]string{"body": "recipes for chocolate cake and cookies"})

	hits := f.Search(ws, "authentication tokens", 5)
	if len(hits) == 0 || hits[0].ChunkID != "c1" {
		t.Fatalf("expected c1 first, got %v", hits)
	}
}

func TestFTS_TitleFieldOutranksBody(t *testing.T) {
	f := newTestFTS(t)
	ws := keys.VaultPrefix("default")
	// "auth" appears in a body chunk and in a title chunk; title must rank higher.
	f.Index(ws, "bodyChunk", map[string]string{"body": "auth middleware handles requests"})
	f.Index(ws, "titleChunk", map[string]string{"title": "Auth Overview", "body": "intro material"})

	hits := f.Search(ws, "auth", 5)
	if len(hits) < 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].ChunkID != "titleChunk" {
		t.Fatalf("title-weighted chunk must rank first, got %s", hits[0].ChunkID)
	}
}

func TestFTS_CaseFoldingAndStopwords(t *testing.T) {
	f := newTestFTS(t)
	ws := keys.VaultPrefix("default")
	f.Index(ws, "c1", map[string]string{"body": "The Quick Brown Fox"})
	// Uppercase query; stopword "the" ignored.
	hits := f.Search(ws, "THE quick", 5)
	if len(hits) != 1 || hits[0].ChunkID != "c1" {
		t.Fatalf("case-folded non-stopword match expected, got %v", hits)
	}
}

func TestFTS_ShortTermFallback(t *testing.T) {
	f := newTestFTS(t)
	ws := keys.VaultPrefix("default")
	f.Index(ws, "c1", map[string]string{"body": "category catalog"})
	// "cat" (len 3 < 4) has no exact posting but should match via prefix fallback.
	hits := f.Search(ws, "cat", 5)
	if len(hits) == 0 {
		t.Fatalf("short-term fallback should match, got %v", hits)
	}
}

func TestFTS_Delete(t *testing.T) {
	f := newTestFTS(t)
	ws := keys.VaultPrefix("default")
	f.Index(ws, "c1", map[string]string{"body": "solo uniqueterm here"})
	f.Delete(ws, "c1", "solo uniqueterm here")
	if hits := f.Search(ws, "uniqueterm", 5); len(hits) != 0 {
		t.Fatalf("deleted chunk should not match, got %v", hits)
	}
}

func TestFTS_VaultIsolation(t *testing.T) {
	f := newTestFTS(t)
	defaultWS := keys.VaultPrefix("default")
	otherWS := keys.VaultPrefix("other")

	f.Index(defaultWS, "c1", map[string]string{"body": "alpha"})
	f.Index(otherWS, "c2", map[string]string{"body": "alpha"})

	defaultHits := f.Search(defaultWS, "alpha", 10)
	if len(defaultHits) != 1 || defaultHits[0].ChunkID != "c1" {
		t.Fatalf("default vault hits = %v, want only c1", defaultHits)
	}
	otherHits := f.Search(otherWS, "alpha", 10)
	if len(otherHits) != 1 || otherHits[0].ChunkID != "c2" {
		t.Fatalf("other vault hits = %v, want only c2", otherHits)
	}
}

func TestHasPostingsAndMigrateFromChunksAreVaultScoped(t *testing.T) {
	db, err := pebble.Open(t.TempDir(), &pebble.Options{})
	if err != nil {
		t.Fatalf("open pebble: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	defaultWS := keys.VaultPrefix("default")
	otherWS := keys.VaultPrefix("other")
	if HasPostings(db, defaultWS) || HasPostings(db, otherWS) {
		t.Fatal("fresh db must not report postings for either vault")
	}
	if err := MigrateFromChunks(db, defaultWS, func(yield func(chunkID, content string) bool) {
		yield("c1", "alpha beta")
	}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !HasPostings(db, defaultWS) {
		t.Fatal("default vault should report postings after migration")
	}
	if HasPostings(db, otherWS) {
		t.Fatal("other vault must remain uninitialized")
	}
}
