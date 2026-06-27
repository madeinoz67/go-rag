package embed

import (
	"context"
	"strings"
	"testing"
)

// TestHugotEmbedder_AbsentModelDoesNotFetch is the US2 guarantee (spec 032): when the
// bundled model is absent, Embed returns an actionable error and MUST NOT fetch — the
// query/ingest path never initiates a network download (Constitution Principle I).
// This runs in the normal (offline, fast) suite: it forces the model absent via HOME
// and asserts Embed errors, proving ensure() short-circuits before any Download.
func TestHugotEmbedder_AbsentModelDoesNotFetch(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // model resolves under a tmp HOME → absent

	e := NewHugot()
	_, err := e.Embed(context.Background(), []string{"anything"})
	if err == nil {
		t.Fatal("Embed must error when the model is absent — it must never fetch on the query/ingest path")
	}
	if !strings.Contains(err.Error(), "model install") {
		t.Fatalf("error should point the user at `go-rag model install`; got: %v", err)
	}
}
