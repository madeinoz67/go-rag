package index

import "testing"

// TestRuleBasedClassifier_ShallowFactoidVsDefault (H22/spec 024, US2) proves the
// rule-based classifier recommends a shallow k only for obvious short factoid
// lookups and K:0 (no recommendation ⇒ default depth) for everything else
// (comparative/broad queries, standard-length queries, empty input) — so a
// misclassification can never reduce quality below the baseline.
func TestRuleBasedClassifier_ShallowFactoidVsDefault(t *testing.T) {
	c := RuleBasedClassifier{}
	cases := []struct {
		name  string
		query string
		wantK int
	}{
		{"empty", "", 0},
		{"single token", "timeout", 3},
		{"short factoid 3 tokens", "max batch size", 3},
		{"comparative compare", "compare caching and drift approaches", 0},
		{"comparative differences", "differences between caching strategies", 0},
		{"standard 6 tokens", "how does the retry logic work", 0},
	}
	for _, tc := range cases {
		got := c.Classify(t.Context(), tc.query)
		if got.K != tc.wantK {
			t.Errorf("%s: Classify(%q).K=%d want %d (rationale=%q)", tc.name, tc.query, got.K, tc.wantK, got.Rationale)
		}
	}
	// Determinism: same query ⇒ same classification (the result-cache key folds k).
	a := c.Classify(t.Context(), "max batch size")
	b := c.Classify(t.Context(), "max batch size")
	if a != b {
		t.Errorf("non-deterministic: %v vs %v", a, b)
	}
}

// TestEffectivePoolFor_FloorsAndCeilings (H22/spec 024, US2/FR-011) proves the
// classifier-derived pool is k+slack clamped to [floor, ceiling]: below the floor
// it floors up, above the ceiling it caps down, and in range it passes through.
func TestEffectivePoolFor_FloorsAndCeilings(t *testing.T) {
	cases := []struct {
		k, slack, floor, ceiling, want int
	}{
		{3, 10, 20, 60, 20},  // 13 < floor 20 → 20
		{30, 10, 20, 60, 40}, // 40, in range → 40
		{80, 10, 20, 60, 60}, // 90 > ceiling 60 → 60
		{3, 10, 5, 60, 13},   // 13, above floor 5 → 13
	}
	for _, tc := range cases {
		got := EffectivePoolFor(tc.k, tc.slack, tc.floor, tc.ceiling)
		if got != tc.want {
			t.Errorf("EffectivePoolFor(k=%d,slack=%d,floor=%d,ceil=%d)=%d want %d",
				tc.k, tc.slack, tc.floor, tc.ceiling, got, tc.want)
		}
	}
}
