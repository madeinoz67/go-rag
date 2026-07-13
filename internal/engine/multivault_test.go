package engine

// multivault_test.go (T016 / spec 052 US1) proves the unified-store multi-vault
// guarantees end-to-end through the public Engine API:
//
//  1. Two-vault isolation — documents added to vault A are retrievable from A and
//     invisible to a query against vault B (and vice-versa). No cross-leak.
//  2. Self-registration — writing to a previously-unseen vault name implicitly
//     registers it; it appears in ListVaultNames with no explicit Create call.
//  3. Default vault — the legacy "default" vault name keeps working (it is just
//     another vault in the unified store) and stays isolated from other vaults.
//  4. Per-vault index seeding — each vault's shared *FTS/*Vector is seeded lazily
//     on first access; the first query pays the LoadIndex cost and every later
//     query for that vault reuses the SAME pointers (distinct vaults get distinct
//     pointers — per-vault registries, not a single shared pair).
//
// It reuses the hermetic fake embedder (cacheFakeEmb / newCacheEngine / hitsEqual)
// defined in index_cache_test.go so no Ollama is required.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// addDocToVault writes content to a temp file, ingests it into the named vault,
// and waits for that vault's async embeddings + BM25 indexing to drain so a
// keyword query can observe it. Returns the ingested file path.
func addDocToVault(t *testing.T, e *Engine, vault, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if _, err := e.Add(context.Background(), vault, path, "*"); err != nil {
		t.Fatalf("Add(%q): %v", vault, err)
	}
	waitEmbeddedVault(t, e, vault)
	return path
}

// waitEmbeddedVault polls Status until the named vault's async embedders have
// drained (vault-aware variant of waitEmbedded, which is hardcoded to "default").
func waitEmbeddedVault(t *testing.T, e *Engine, vault string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := e.Status(vault)
		if st != nil && st.Embeddings > 0 && st.EmbeddingsComplete {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("embeddings for vault %q did not drain within 5s", vault)
}

// vaultNames returns every vault name registered in the unified store's VaultMeta
// registry (the db.ListVaultNames scan over the 0x1A prefix).
func vaultNames(t *testing.T, e *Engine) []string {
	t.Helper()
	names, err := e.db.ListVaultNames()
	if err != nil {
		t.Fatalf("ListVaultNames: %v", err)
	}
	return names
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// --- T016 / spec 052 US1 -----------------------------------------------------

// TestMultiVault_Isolation_NoCrossLeak (FR-003/FR-004, SC-001): two vaults in
// one unified store are isolated by their 8-byte wsPrefix. A document added to
// vault A is retrievable from A and invisible to a keyword query against vault B,
// and vice-versa. No cross-leak in either direction.
func TestMultiVault_Isolation_NoCrossLeak(t *testing.T) {
	e := newCacheEngine(t)
	// Distinct, unambiguous tokens so a leak is unmistakable.
	addDocToVault(t, e, "alpha", "alpha vault only content zebratwo pizza")
	addDocToVault(t, e, "beta", "beta vault only content unicorneight pizza")

	// Query alpha for alpha's token → hit, and the hit is alpha's content.
	aRes, err := e.Query(context.Background(), "alpha", QueryRequest{Query: "zebratwo", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("query alpha: %v", err)
	}
	if len(aRes.Hits) == 0 {
		t.Fatal("alpha query returned no hits")
	}

	// Query beta for beta's token → hit, and the hit is beta's content.
	bRes, err := e.Query(context.Background(), "beta", QueryRequest{Query: "unicorneight", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("query beta: %v", err)
	}
	if len(bRes.Hits) == 0 {
		t.Fatal("beta query returned no hits")
	}

	// Negative isolation: querying alpha for beta's token finds nothing.
	miss, _ := e.Query(context.Background(), "alpha", QueryRequest{Query: "unicorneight", Mode: "keyword", K: 5})
	if len(miss.Hits) != 0 {
		t.Errorf("alpha query leaked beta's document: %d hits", len(miss.Hits))
	}
	// And querying beta for alpha's token finds nothing.
	miss2, _ := e.Query(context.Background(), "beta", QueryRequest{Query: "zebratwo", Mode: "keyword", K: 5})
	if len(miss2.Hits) != 0 {
		t.Errorf("beta query leaked alpha's document: %d hits", len(miss2.Hits))
	}
}

// TestMultiVault_SelfRegistration (T016): writing to a previously-unseen vault
// name implicitly registers it — it appears in ListVaultNames without an explicit
// Create call (research R5 — vault creation is implicit on first write).
func TestMultiVault_SelfRegistration(t *testing.T) {
	e := newCacheEngine(t)

	// Precondition: the "test" vault is not yet registered.
	if containsStr(vaultNames(t, e), "test") {
		t.Fatal("precondition: 'test' vault already registered before any write")
	}

	addDocToVault(t, e, "test", "self-registering vault content plumb")

	// After the write, "test" must be present in the VaultMeta registry.
	if !containsStr(vaultNames(t, e), "test") {
		t.Errorf("vault 'test' not in ListVaultNames after write; got %v", vaultNames(t, e))
	}
}

// TestMultiVault_DefaultVault_BackwardCompat (T016): operations that target the
// "default" vault (the pre-multi-vault single-vault name) keep working — "default"
// is just another vault in the unified store, so legacy callers passing "default"
// are backward-compatible. It is registered on write like any other vault and is
// isolated from other vaults.
func TestMultiVault_DefaultVault_BackwardCompat(t *testing.T) {
	e := newCacheEngine(t)
	// The search token "fluffseven" appears ONLY in the "default" vault's doc,
	// so a query against "other" for it is a true isolation probe (not a
	// self-hit).
	addDocToVault(t, e, "default", "default vault backward compat content fluffseven")
	addDocToVault(t, e, "other", "other vault content nothing relevant here")

	// "default" resolves and returns its own documents.
	res, err := e.Query(context.Background(), "default", QueryRequest{Query: "fluffseven", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("query default: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("default vault query returned no hits")
	}

	// "default" is registered on write (same self-registration path as any vault).
	if !containsStr(vaultNames(t, e), "default") {
		t.Error("default vault not registered in ListVaultNames after write")
	}

	// Isolation: "other" must not surface "default"'s document.
	leak, _ := e.Query(context.Background(), "other", QueryRequest{Query: "fluffseven", Mode: "keyword", K: 5})
	if len(leak.Hits) != 0 {
		t.Errorf("default vault content leaked into 'other': %d hits", len(leak.Hits))
	}
}

// TestMultiVault_PerVaultIndexSeeding (T016 / T006): each vault's shared index is
// seeded lazily on first access. The first indexes() call for a vault pays the
// LoadIndex cost; every subsequent call for that vault reuses the SAME *FTS /
// *Vector pointers (no per-query rebuild). Distinct vaults get distinct pointers
// (per-vault registries). Timing is flaky, so the proof is pointer identity across
// calls + distinctness across vaults — the same approach TestQuery_ReusesSharedIndex
// and TestIndexes_SeedsOnce_NoThunderingHerd use.
func TestMultiVault_PerVaultIndexSeeding(t *testing.T) {
	e := newCacheEngine(t)
	addDocToVault(t, e, "seedA", "seedable content for vault a kangaroo")
	addDocToVault(t, e, "seedB", "seedable content for vault b wombat")

	wsA := e.db.ResolveVaultPrefix("seedA")
	wsB := e.db.ResolveVaultPrefix("seedB")
	if wsA == wsB {
		t.Fatal("two distinct vault names must resolve to distinct wsPrefixes")
	}

	// First access seeds vault A (pays LoadIndex).
	ftsA1, vecA1, err := e.indexes(wsA)
	if err != nil {
		t.Fatalf("indexes(A) first: %v", err)
	}
	if ftsA1 == nil || vecA1 == nil {
		t.Fatal("seeded FTS/Vector for vault A must be non-nil")
	}

	// Second access reuses the SAME pointers — no re-LoadIndex.
	ftsA2, vecA2, err := e.indexes(wsA)
	if err != nil {
		t.Fatalf("indexes(A) second: %v", err)
	}
	if ftsA2 != ftsA1 || vecA2 != vecA1 {
		t.Error("second indexes(A) must reuse the same *FTS/*Vector pointers (no per-query rebuild)")
	}

	// Distinct vault → distinct pointers (per-vault registry, not a shared pair).
	ftsB, vecB, err := e.indexes(wsB)
	if err != nil {
		t.Fatalf("indexes(B): %v", err)
	}
	if ftsB == ftsA1 || vecB == vecA1 {
		t.Error("indexes(B) must not share pointers with vault A (per-vault isolation)")
	}

	// The registry maps hold exactly the seeded entries, keyed per ws.
	e.idxMu.Lock()
	if e.idxFts[wsA] != ftsA1 {
		t.Error("idxFts registry entry for wsA does not match the seeded pointer")
	}
	if e.idxFts[wsB] != ftsB {
		t.Error("idxFts registry entry for wsB does not match the seeded pointer")
	}
	e.idxMu.Unlock()

	// Behavioral confirmation through the public Query path: two queries against
	// the same vault reuse the seeded index and return identical results.
	q1, err := e.Query(context.Background(), "seedA", QueryRequest{Query: "kangaroo", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("query seedA: %v", err)
	}
	if len(q1.Hits) == 0 {
		t.Fatal("expected hits for 'kangaroo' in vault seedA")
	}
	q2, err := e.Query(context.Background(), "seedA", QueryRequest{Query: "kangaroo", Mode: "keyword", K: 5})
	if err != nil {
		t.Fatalf("query seedA second: %v", err)
	}
	if !hitsEqual(q1.Hits, q2.Hits) {
		t.Errorf("two queries on the same vault must return identical results: %v vs %v", q1.Hits, q2.Hits)
	}
}
