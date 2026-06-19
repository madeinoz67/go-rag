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
	// Candidate 3 (index 2) scored 9 → highest → ~1.0.
	if scores[2] < 0.99 {
		t.Errorf("scores[2] should be ~1.0 (9/9), got %f", scores[2])
	}
	// Candidate 4 (index 3) scored 1 → lowest → ~0.11.
	if scores[3] > 0.2 {
		t.Errorf("scores[3] should be ~0.11 (1/9), got %f", scores[3])
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

func TestParseScores_PartialParse(t *testing.T) {
	// "7, junk, 2" → parses 7 and 2; missing 3rd → fallback 0.5.
	scores := parseScores("7, junk, 2", 3)
	if scores[0] < 0.77 || scores[0] > 0.78 {
		t.Errorf("scores[0] = %f, want ~0.78 (7/9)", scores[0])
	}
	if scores[1] != 0.5 {
		t.Errorf("scores[1] = %f, want 0.5 (unparseable)", scores[1])
	}
	if scores[2] < 0.22 || scores[2] > 0.23 {
		t.Errorf("scores[2] = %f, want ~0.22 (2/9)", scores[2])
	}
}

func TestReranker_EmptyInput(t *testing.T) {
	rr := New("http://x", "m")
	scores, err := rr.Score(context.Background(), "q", nil)
	if err != nil || scores != nil {
		t.Fatalf("empty input → nil,nil; got %v %v", scores, err)
	}
}
