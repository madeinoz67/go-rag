package engine

// crossvault_test.go (T018 / US2) proves the cross-vault query path end-to-end
// through the public Engine API. It reuses the hermetic fake-embedder harness
// (newCacheEngine / addDocToVault) from the sibling test files so no Ollama is
// required. Keyword mode is used throughout for deterministic BM25 scoring.

import (
	"context"
	"strings"
	"testing"
)

// TestCrossVault_Query_FansOutAcrossVaults (T018 / US2): three vaults with
// distinct documents. A cross-vault query [A,B,C] for a shared term returns
// hits from all three vaults; each per-vault query returns only that vault's
// document; the cross-vault result subsumes each individual vault's top hit.
func TestCrossVault_Query_FansOutAcrossVaults(t *testing.T) {
	e := newCacheEngine(t)

	// Each vault holds one document containing a shared queryable token plus a
	// unique distinguishing token so hits from different vaults are identifiable.
	addDocToVault(t, e, "alpha", "alpha vault report sharedterm alphatoken content")
	addDocToVault(t, e, "beta", "beta vault report sharedterm betatoken content")
	addDocToVault(t, e, "gamma", "gamma vault report sharedterm gammatoken content")

	// --- Per-vault queries return only that vault's document (no cross-leak). ---
	for _, vault := range []string{"alpha", "beta", "gamma"} {
		res, err := e.Query(context.Background(), vault, QueryRequest{
			Query: "sharedterm", Mode: "keyword", K: 5, NoCache: true,
		})
		if err != nil {
			t.Fatalf("query %q: %v", vault, err)
		}
		if len(res.Hits) != 1 {
			t.Errorf("vault %q: expected exactly 1 hit, got %d", vault, len(res.Hits))
			continue
		}
		if !strings.Contains(res.Hits[0].Content, vault+"token") {
			t.Errorf("vault %q: hit content %q does not contain %q",
				vault, res.Hits[0].Content, vault+"token")
		}
	}

	// --- Cross-vault query fans out to all three and returns hits from each. ---
	crossRes, err := e.Query(context.Background(), "ignored", QueryRequest{
		Query:   "sharedterm",
		Mode:    "keyword",
		K:       10,
		NoCache: true,
		Vaults:  []string{"alpha", "beta", "gamma"},
	})
	if err != nil {
		t.Fatalf("cross-vault query: %v", err)
	}
	if len(crossRes.Hits) < 3 {
		t.Fatalf("cross-vault query: expected ≥3 hits (one per vault), got %d", len(crossRes.Hits))
	}

	// Every vault must be represented — each vault's unique token appears in at
	// least one hit's content.
	seen := map[string]bool{"alpha": false, "beta": false, "gamma": false}
	for _, h := range crossRes.Hits {
		for vault := range seen {
			if strings.Contains(h.Content, vault+"token") {
				seen[vault] = true
			}
		}
	}
	for vault, found := range seen {
		if !found {
			t.Errorf("cross-vault query: vault %q not represented in %d hits", vault, len(crossRes.Hits))
		}
	}

	// --- Subsumption: each vault's individual top hit must appear in the
	// cross-vault result set (by ChunkID). ---
	crossIDs := make(map[string]bool, len(crossRes.Hits))
	for _, h := range crossRes.Hits {
		crossIDs[h.ChunkID] = true
	}
	for _, vault := range []string{"alpha", "beta", "gamma"} {
		single, err := e.Query(context.Background(), vault, QueryRequest{
			Query: "sharedterm", Mode: "keyword", K: 5, NoCache: true,
		})
		if err != nil {
			t.Fatalf("subsumption query %q: %v", vault, err)
		}
		if len(single.Hits) == 0 {
			t.Fatalf("subsumption: vault %q returned no hits", vault)
		}
		if !crossIDs[single.Hits[0].ChunkID] {
			t.Errorf("subsumption: vault %q top hit %q not in cross-vault result set",
				vault, single.Hits[0].ChunkID)
		}
	}
}

// TestCrossVault_EmptyVaultsList_UsesSingleVaultPath: when Vaults is nil/empty,
// the branch is not taken and the single-vault path runs unchanged. Guards
// against the branch condition regressing and swallowing the legacy path.
func TestCrossVault_EmptyVaultsList_UsesSingleVaultPath(t *testing.T) {
	e := newCacheEngine(t)
	addDocToVault(t, e, "solo", "solo vault content uniquetoken report")

	res, err := e.Query(context.Background(), "solo", QueryRequest{
		Query: "uniquetoken", Mode: "keyword", K: 5, NoCache: true,
	})
	if err != nil {
		t.Fatalf("single-vault query: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("single-vault query returned no hits (branch may have regressed)")
	}
}

// TestCrossVault_CacheKey_DistinctFromSingleVault (US2/T017 cache-key
// requirement): the cross-vault result-cache key folds in the sorted vault set,
// so a cross-vault query and a single-vault query for the same text cannot
// share a cache entry. After warming the cache with a cross-vault query, a
// single-vault query against one of those vaults must NOT serve the cross-vault
// result (which would leak the other vault's content).
func TestCrossVault_CacheKey_DistinctFromSingleVault(t *testing.T) {
	e := newCacheEngine(t)
	addDocToVault(t, e, "alpha", "alpha cachekey sharedterm alphatoken content")
	addDocToVault(t, e, "beta", "beta cachekey sharedterm betatoken content")

	// Warm the cache with a cross-vault query (NoCache=false so it stores).
	cross, err := e.Query(context.Background(), "ignored", QueryRequest{
		Query:  "sharedterm",
		Mode:   "keyword",
		K:      5,
		Vaults: []string{"alpha", "beta"},
	})
	if err != nil {
		t.Fatalf("cross-vault warm: %v", err)
	}
	// Sanity: the cross-vault result includes beta's content.
	crossHasBeta := false
	for _, h := range cross.Hits {
		if strings.Contains(h.Content, "betatoken") {
			crossHasBeta = true
		}
	}
	if !crossHasBeta {
		t.Fatal("cross-vault warm: expected beta content in cross-vault result")
	}

	// A single-vault query against "alpha" (cache enabled) must NOT serve the
	// cross-vault cached entry — it should return only alpha's hit(s).
	single, err := e.Query(context.Background(), "alpha", QueryRequest{
		Query: "sharedterm", Mode: "keyword", K: 5,
	})
	if err != nil {
		t.Fatalf("single-vault query: %v", err)
	}
	for _, h := range single.Hits {
		if strings.Contains(h.Content, "betatoken") {
			t.Fatal("single-vault query served cross-vault cache: beta content leaked into alpha-only query")
		}
	}
}

// TestCrossVault_TwoVaultOrderIrrelevant: the cross-vault result is the same
// regardless of the order vaults are listed (RRF is rank-based, not positional,
// and the cache key sorts the vault set). Tests both orders return hits from
// both vaults.
func TestCrossVault_TwoVaultOrderIrrelevant(t *testing.T) {
	e := newCacheEngine(t)
	addDocToVault(t, e, "va", "vault a report ordertoken vacontent")
	addDocToVault(t, e, "vb", "vault b report ordertoken vbcontent")

	r1, err := e.Query(context.Background(), "ignored", QueryRequest{
		Query: "ordertoken", Mode: "keyword", K: 5, NoCache: true,
		Vaults: []string{"va", "vb"},
	})
	if err != nil {
		t.Fatalf("query [va,vb]: %v", err)
	}
	r2, err := e.Query(context.Background(), "ignored", QueryRequest{
		Query: "ordertoken", Mode: "keyword", K: 5, NoCache: true,
		Vaults: []string{"vb", "va"},
	})
	if err != nil {
		t.Fatalf("query [vb,va]: %v", err)
	}

	// Both orders must return hits from both vaults.
	for name, res := range map[string]*QueryResult{"[va,vb]": r1, "[vb,va]": r2} {
		hasA, hasB := false, false
		for _, h := range res.Hits {
			if strings.Contains(h.Content, "vacontent") {
				hasA = true
			}
			if strings.Contains(h.Content, "vbcontent") {
				hasB = true
			}
		}
		if !hasA || !hasB {
			t.Errorf("order %s: expected hits from both vaults (hasA=%v hasB=%v, %d hits)",
				name, hasA, hasB, len(res.Hits))
		}
	}
}
