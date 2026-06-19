package index

import (
	"testing"
)

func TestFTS_RanksRelevantChunkFirst(t *testing.T) {
	f := NewFTS()
	f.Index("c1", map[string]string{"body": "the authentication system uses jwt tokens"})
	f.Index("c2", map[string]string{"body": "recipes for chocolate cake and cookies"})

	hits := f.Search("authentication tokens", 5)
	if len(hits) == 0 || hits[0].ChunkID != "c1" {
		t.Fatalf("expected c1 first, got %v", hits)
	}
}

func TestFTS_TitleFieldOutranksBody(t *testing.T) {
	f := NewFTS()
	// "auth" appears in a body chunk and in a title chunk; title must rank higher.
	f.Index("bodyChunk", map[string]string{"body": "auth middleware handles requests"})
	f.Index("titleChunk", map[string]string{"title": "Auth Overview", "body": "intro material"})

	hits := f.Search("auth", 5)
	if len(hits) < 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].ChunkID != "titleChunk" {
		t.Fatalf("title-weighted chunk must rank first, got %s", hits[0].ChunkID)
	}
}

func TestFTS_CaseFoldingAndStopwords(t *testing.T) {
	f := NewFTS()
	f.Index("c1", map[string]string{"body": "The Quick Brown Fox"})
	// Uppercase query; stopword "the" ignored.
	hits := f.Search("THE quick", 5)
	if len(hits) != 1 || hits[0].ChunkID != "c1" {
		t.Fatalf("case-folded non-stopword match expected, got %v", hits)
	}
}

func TestFTS_ShortTermFallback(t *testing.T) {
	f := NewFTS()
	f.Index("c1", map[string]string{"body": "category catalog"})
	// "cat" (len 3 < 4) has no exact posting but should match via prefix fallback.
	hits := f.Search("cat", 5)
	if len(hits) == 0 {
		t.Fatalf("short-term fallback should match, got %v", hits)
	}
}

func TestFTS_Delete(t *testing.T) {
	f := NewFTS()
	f.Index("c1", map[string]string{"body": "solo uniqueterm here"})
	f.Delete("c1")
	if hits := f.Search("uniqueterm", 5); len(hits) != 0 {
		t.Fatalf("deleted chunk should not match, got %v", hits)
	}
}
