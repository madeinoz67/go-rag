package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReranker_Score(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "8, 3, 9, 1"})
	}))
	defer srv.Close()

	rr := New(srv.URL, "test")
	scores, err := rr.Score(context.Background(), "query", []string{"a", "b", "c", "d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 4 {
		t.Fatalf("want 4 scores, got %d", len(scores))
	}
	// Candidate 3 (index 2) scored 9 → max → 1.0 (normalised by max).
	if scores[2] < 0.99 {
		t.Errorf("scores[2] should be 1.0 (max-normalised), got %f", scores[2])
	}
	// Candidate 4 (index 3) scored 1 → 1/9 ≈ 0.11.
	if scores[3] > 0.15 {
		t.Errorf("scores[3] should be ~0.11 (1/9 normalised), got %f", scores[3])
	}
}

func TestParseScores_Fallback(t *testing.T) {
	scores := parseScores("garbage no numbers here", 3)
	for i, s := range scores {
		if s != 0.5 {
			t.Errorf("fallback[%d] = %f, want 0.5", i, s)
		}
	}
}

func TestParseScores_MaxNormalisation(t *testing.T) {
	// phi3 sometimes uses a 0-20 scale instead of 0-9; max-normalisation
	// should adapt so the best candidate is always 1.0.
	scores := parseScores("18, 9, 0", 3)
	if scores[0] != 1.0 {
		t.Errorf("max score should normalise to 1.0, got %f", scores[0])
	}
	if scores[1] < 0.49 || scores[1] > 0.51 {
		t.Errorf("9/18 should be 0.5, got %f", scores[1])
	}
	if scores[2] != 0.0 {
		t.Errorf("0/18 should be 0.0, got %f", scores[2])
	}
}

func TestReranker_EmptyInput(t *testing.T) {
	rr := New("http://x", "m")
	scores, err := rr.Score(context.Background(), "q", nil)
	if err != nil || scores != nil {
		t.Fatalf("empty input → nil,nil; got %v %v", scores, err)
	}
}
