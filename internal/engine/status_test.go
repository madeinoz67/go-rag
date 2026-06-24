package engine

import (
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
)

// TestEngine_Status_AdaptiveKnobs_ReflectConfig (H22/spec 024, US3) proves the
// status surface echoes the configured pool ceiling and classifier posture, so
// an operator can read them (SC-004): AdaptiveDepthEnabled tracks the config,
// and PoolSize tracks the effective ceiling.
func TestEngine_Status_AdaptiveKnobs_ReflectConfig(t *testing.T) {
	eng, _ := newTestEngineCfg(t, func(c *config.Config) {
		c.AdaptiveDepthEnabled = true
		c.PoolSize = 90
	})
	st, err := eng.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.AdaptiveDepthEnabled {
		t.Errorf("AdaptiveDepthEnabled=false want true")
	}
	if st.PoolSize != 90 {
		t.Errorf("PoolSize=%d want 90", st.PoolSize)
	}

	// Default posture: classifier off, pool 60.
	eng2, _ := newTestEngineCfg(t, nil)
	st2, _ := eng2.Status()
	if st2.AdaptiveDepthEnabled {
		t.Errorf("default AdaptiveDepthEnabled=true want false")
	}
	if st2.PoolSize != 60 {
		t.Errorf("default PoolSize=%d want 60", st2.PoolSize)
	}
}


// pool-utilization signal is tracked across non-cached queries and surfaced in
// Status: Queries counts observed queries, AvgFetched is the mean effective pool,
// AvgKept the mean results returned, and Saturated counts queries that could not
// fill the requested depth (short corpus / under-covered topic). Initial state is
// zero (fresh process). PoolSize echoes the configured ceiling.
func TestEngine_Status_PoolUtilization(t *testing.T) {
	eng, _ := newTestEngineCfg(t, nil) // default PoolSize 60

	// Fresh engine: nothing observed yet.
	st, err := eng.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.PoolUtilization.Queries != 0 {
		t.Errorf("initial Queries=%d want 0", st.PoolUtilization.Queries)
	}
	if st.PoolSize != 60 {
		t.Errorf("PoolSize=%d want 60", st.PoolSize)
	}
	if st.PoolUtilization.AvgFetched != 0 || st.PoolUtilization.AvgKept != 0 {
		t.Errorf("initial averages nonzero: fetched=%v kept=%v", st.PoolUtilization.AvgFetched, st.PoolUtilization.AvgKept)
	}

	// Two non-cached queries with distinct pools (NoCache forces fresh computation
	// so both are observed). Empty corpus ⇒ 0 hits each, effK=5 ⇒ both saturated.
	if _, err := eng.Query(t.Context(), QueryRequest{Query: "a", Mode: "keyword", PoolSize: 40, NoCache: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Query(t.Context(), QueryRequest{Query: "b", Mode: "keyword", PoolSize: 60, NoCache: true}); err != nil {
		t.Fatal(err)
	}

	st, _ = eng.Status()
	if st.PoolUtilization.Queries != 2 {
		t.Errorf("Queries=%d want 2", st.PoolUtilization.Queries)
	}
	if want := 50.0; st.PoolUtilization.AvgFetched != want { // (40+60)/2
		t.Errorf("AvgFetched=%v want %v", st.PoolUtilization.AvgFetched, want)
	}
	if st.PoolUtilization.AvgKept != 0 { // empty corpus → 0 hits both
		t.Errorf("AvgKept=%v want 0", st.PoolUtilization.AvgKept)
	}
	if st.PoolUtilization.Saturated != 2 { // 0 hits < effK(5) both
		t.Errorf("Saturated=%d want 2", st.PoolUtilization.Saturated)
	}

	// A cache HIT must not double-count: repeating the first query without
	// NoCache serves from the result cache (same key) and leaves the counters.
	if _, err := eng.Query(t.Context(), QueryRequest{Query: "a", Mode: "keyword", PoolSize: 40}); err != nil {
		t.Fatal(err)
	}
	st, _ = eng.Status()
	if st.PoolUtilization.Queries != 2 {
		t.Errorf("cache hit double-counted: Queries=%d want 2", st.PoolUtilization.Queries)
	}
}
