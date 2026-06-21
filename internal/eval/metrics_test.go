package eval

import (
	"math"
	"testing"
)

func approxEqual(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("got %.6f, want %.6f (diff %.6f)", got, want, math.Abs(got-want))
	}
}

// Scenario: relevant = {A,B,C}; retrieved = [X,A,Y,B,Z] (A rank 2, B rank 4, C
// never retrieved). Hand-computed expectations below.
func TestMetrics_HandComputed(t *testing.T) {
	retrieved := []string{"X", "A", "Y", "B", "Z"}
	relevant := map[string]bool{"A": true, "B": true, "C": true}

	// Top-5 has A,B → 2 of 3 relevant.
	approxEqual(t, RecallAt(retrieved, relevant, 5), 2.0/3.0)
	// Top-10 is the same 5 items (list shorter than 10).
	approxEqual(t, RecallAt(retrieved, relevant, 10), 2.0/3.0)
	// 2 relevant in top-5 / 5 slots.
	approxEqual(t, PrecisionAt(retrieved, relevant, 5), 2.0/5.0)
	// First relevant (A) at rank 2 → 1/2.
	approxEqual(t, MRR(retrieved, relevant), 0.5)
	// DCG = 1/log2(3) + 1/log2(5); IDCG (3 ideal) = 1/log2(2)+1/log2(3)+1/log2(4).
	dcg := 1.0/math.Log2(3) + 1.0/math.Log2(5)
	idcg := 1.0/math.Log2(2) + 1.0/math.Log2(3) + 1.0/math.Log2(4)
	approxEqual(t, NDCGAt(retrieved, relevant, 10), dcg/idcg)
}

func TestMetrics_FirstRelevantAtRank1(t *testing.T) {
	retrieved := []string{"A", "X", "Y"}
	relevant := map[string]bool{"A": true}
	approxEqual(t, MRR(retrieved, relevant), 1.0) // rank 1
	approxEqual(t, RecallAt(retrieved, relevant, 5), 1.0)
	approxEqual(t, NDCGAt(retrieved, relevant, 10), 1.0) // single relevant at rank 1 → perfect
}

func TestMetrics_NoRelevantRetrieved(t *testing.T) {
	retrieved := []string{"X", "Y", "Z"}
	relevant := map[string]bool{"A": true}
	approxEqual(t, RecallAt(retrieved, relevant, 5), 0.0)
	approxEqual(t, PrecisionAt(retrieved, relevant, 5), 0.0)
	approxEqual(t, MRR(retrieved, relevant), 0.0)
	approxEqual(t, NDCGAt(retrieved, relevant, 10), 0.0)
}

func TestMetrics_EmptyRelevantIsZero(t *testing.T) {
	// Callers skip empty-relevant queries, but the metric itself must not panic
	// or divide by zero; it returns 0.
	retrieved := []string{"X", "Y"}
	approxEqual(t, RecallAt(retrieved, map[string]bool{}, 5), 0.0)
	approxEqual(t, NDCGAt(retrieved, map[string]bool{}, 10), 0.0)
}

func TestMetrics_KClampsToRetrievedLength(t *testing.T) {
	// k larger than retrieved: precision divides by k, recall by total relevant.
	retrieved := []string{"A"}
	relevant := map[string]bool{"A": true, "B": true}
	approxEqual(t, RecallAt(retrieved, relevant, 10), 1.0/2.0)
	approxEqual(t, PrecisionAt(retrieved, relevant, 10), 1.0/10.0)
}
