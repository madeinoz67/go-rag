package index

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
)

// staticEmbed returns a fixed vector regardless of input, so query embedding is
// deterministic and close to a chosen chunk's vector.
func staticEmbed(vec []float32) EmbedFunc {
	return func(_ context.Context, _ []string) ([][]float32, error) {
		return [][]float32{vec}, nil
	}
}

func TestRetrieval_Hybrid_BothListsRankAboveOneList(t *testing.T) {
	fts := NewFTS()
	vec := NewVector()

	// c1 matches FTS ("alpha") and is near the query vector.
	fts.Index("c1", map[string]string{"body": "alpha keyword document"})
	vec.Add("c1", []float32{0.99, 0.0})
	// c3 matches FTS only; its vector is orthogonal to the query.
	fts.Index("c3", map[string]string{"body": "alpha other note"})
	vec.Add("c3", []float32{0.0, 1.0})

	r := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	hits, err := r.Search(context.Background(), "alpha", 5, ModeHybrid, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].ChunkID != "c1" {
		t.Fatalf("c1 (in both lists) must rank first, got %v", hits)
	}
}

func TestRetrieval_CollapseSameDocument(t *testing.T) {
	fts := NewFTS()
	vec := NewVector()
	fts.Index("c1", map[string]string{"body": "alpha beta"})
	fts.Index("c1b", map[string]string{"body": "alpha gamma"})
	fts.Index("c2", map[string]string{"body": "alpha delta"})
	vec.Add("c1", []float32{1.0, 0.0})
	vec.Add("c1b", []float32{0.9, 0.1})
	vec.Add("c2", []float32{0.1, 0.9})

	docOf := func(id string) string {
		if id == "c1" || id == "c1b" {
			return "docA"
		}
		return "docB"
	}
	r := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	hits, err := r.Search(context.Background(), "alpha", 5, ModeHybrid, docOf)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]bool{}
	for _, h := range hits {
		docs[docOf(h.ChunkID)] = true
	}
	if docs["docA"] && docs["docB"] && len(hits) == 2 {
		return // collapsed to one per doc
	}
	if len(hits) != 2 {
		t.Fatalf("same-document hits must collapse to top-1 per doc; got %d hits: %v", len(hits), hits)
	}
}

func TestRetrieval_ModeSelection(t *testing.T) {
	fts := NewFTS()
	vec := NewVector()
	// cFTS: only in FTS. cVEC: only in vector.
	fts.Index("cFTS", map[string]string{"body": "unique keyword term"})
	vec.Add("cVEC", []float32{1.0, 0.0})

	r := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))

	// Keyword mode must NOT surface the vector-only chunk.
	kw, _ := r.Search(context.Background(), "unique keyword", 5, ModeKeyword, nil)
	for _, h := range kw {
		if h.ChunkID == "cVEC" {
			t.Fatal("keyword mode must not use vector index")
		}
	}
	// Semantic mode must NOT surface the FTS-only chunk.
	sem, _ := r.Search(context.Background(), "unique keyword", 5, ModeSemantic, nil)
	for _, h := range sem {
		if h.ChunkID == "cFTS" {
			t.Fatal("semantic mode must not use FTS index")
		}
	}
}

// --- H09: rerank-failure surfacing (SearchWithRerank) ---

// captureLog redirects the stdlib log to a buffer for the test and restores it
// afterward, so FR-003 (a single diagnostic log line, no query text) is assertable.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// rerankFixture builds a retrieval over three FTS+vector chunks and returns it
// with a chunkText lookup, ready for SearchWithRerank tests.
func rerankFixture() (*Retrieval, func(string) string) {
	fts := NewFTS()
	vec := NewVector()
	chunks := []struct {
		id, body string
		v        []float32
	}{
		{"c1", "alpha keyword document", []float32{0.99, 0.0}},
		{"c2", "alpha secondary note", []float32{0.80, 0.20}},
		{"c3", "beta unrelated content", []float32{0.10, 0.90}},
	}
	text := map[string]string{}
	for _, c := range chunks {
		fts.Index(c.id, map[string]string{"body": c.body})
		vec.Add(c.id, c.v)
		text[c.id] = c.body
	}
	r := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	return r, func(id string) string { return text[id] }
}

// fakeReranker is a controllable index.Reranker. err forces a rerank error; short
// forces a score-count mismatch (one fewer score than candidates).
type fakeReranker struct {
	err   error
	short bool
	model string
	calls int
}

func (f *fakeReranker) Score(_ context.Context, _ string, candidates []string) ([]float64, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	n := len(candidates)
	if f.short && n > 0 {
		n-- // length mismatch
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(n-i) / float64(n+1)
	}
	return out, nil
}
func (f *fakeReranker) Model() string { return f.model }

// eventuallyOKReranker fails the first `failsLeft` Score calls then succeeds — for
// retry-recovery tests.
type eventuallyOKReranker struct {
	failsLeft int
	model     string
	calls     int
}

func (f *eventuallyOKReranker) Score(_ context.Context, _ string, candidates []string) ([]float64, error) {
	f.calls++
	if f.failsLeft > 0 {
		f.failsLeft--
		return nil, errors.New("transient")
	}
	n := len(candidates)
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(n-i) / float64(n+1)
	}
	return out, nil
}
func (f *eventuallyOKReranker) Model() string { return f.model }

// T006: a rerank error degrades to fallback-ordered hits + a flag, with one
// diagnostic log line (and never an error return, never the query text logged).
func TestSearchWithRerank_RerankError_DegradesWithFlagAndLog(t *testing.T) {
	logbuf := captureLog(t)
	r, chunkText := rerankFixture()
	rr := &fakeReranker{err: errors.New("ollama down"), model: "bge-reranker"}

	hits, failed, err := r.SearchWithRerank(context.Background(), "alpha", 5, ModeHybrid, nil, rr, chunkText)
	if err != nil {
		t.Fatalf("rerank error must NOT propagate as a query error; got %v", err)
	}
	if !failed {
		t.Fatal("rerank error must set rerankFailed=true (graceful degradation, FR-001/002)")
	}
	if len(hits) == 0 {
		t.Fatal("rerank failure must still return fallback-ordered hits (FR-007)")
	}
	if rr.calls != 1 {
		t.Errorf("reranker called %d times, want 1 (no retry by default)", rr.calls)
	}
	line := logbuf.String()
	if !strings.Contains(line, "rerank failed") {
		t.Errorf("FR-003: expected a rerank-failed log line; got %q", line)
	}
	if !strings.Contains(line, "bge-reranker") {
		t.Errorf("FR-003: log line should name the model; got %q", line)
	}
	if strings.Contains(line, "alpha") {
		t.Errorf("FR-003: log line must NOT contain the query text; got %q", line)
	}
}

// T007: a score-count mismatch is treated identically to a rerank error.
func TestSearchWithRerank_LengthMismatch_DegradesWithFlag(t *testing.T) {
	logbuf := captureLog(t)
	r, chunkText := rerankFixture()
	rr := &fakeReranker{short: true, model: "bge-reranker"}

	hits, failed, err := r.SearchWithRerank(context.Background(), "alpha", 5, ModeHybrid, nil, rr, chunkText)
	if err != nil {
		t.Fatalf("length mismatch must NOT propagate as a query error; got %v", err)
	}
	if !failed {
		t.Fatal("length mismatch must set rerankFailed=true")
	}
	if len(hits) == 0 {
		t.Fatal("length mismatch must still return fallback-ordered hits")
	}
	out := logbuf.String()
	if !strings.Contains(out, "candidates=") || !strings.Contains(out, "scores=") {
		t.Errorf("FR-003: log line should report candidates/scores for diagnosis; got %q", out)
	}
}

// T008: a retrieval-stage failure on the rerank path propagates as a query error
// (FR-009/SC-006) — never silent empty results, never a rerank-failed flag.
func TestSearchWithRerank_RetrievalError_Propagates(t *testing.T) {
	logbuf := captureLog(t)
	fts := NewFTS()
	vec := NewVector()
	fts.Index("c1", map[string]string{"body": "alpha document"})
	vec.Add("c1", []float32{1.0, 0.0})
	// An embedder that always errors → hybrid retrieval fails at the semantic step.
	r := NewRetrieval(fts, vec, func(_ context.Context, _ []string) ([][]float32, error) {
		return nil, errors.New("embed unreachable")
	})
	rr := &fakeReranker{model: "bge-reranker"}

	hits, failed, err := r.SearchWithRerank(context.Background(), "alpha", 5, ModeHybrid, nil, rr, func(string) string { return "" })
	if err == nil {
		t.Fatal("FR-009: retrieval-stage failure must propagate as an error, not degrade silently")
	}
	if failed {
		t.Error("FR-008: retrieval-stage failure must NOT set rerankFailed (distinct from a rerank failure)")
	}
	if hits != nil {
		t.Errorf("FR-009: retrieval failure must return nil hits; got %d", len(hits))
	}
	if logbuf.String() != "" {
		t.Errorf("no rerank-failed log line on a retrieval error; got %q", logbuf.String())
	}
	if rr.calls != 0 {
		t.Errorf("reranker must not be called when retrieval fails; called %d times", rr.calls)
	}
}

// Happy path: a successful rerank returns reranked hits, no flag, no log.
func TestSearchWithRerank_Success_ReranksAndNoFlag(t *testing.T) {
	logbuf := captureLog(t)
	r, chunkText := rerankFixture()
	rr := &fakeReranker{model: "bge-reranker"}

	hits, failed, err := r.SearchWithRerank(context.Background(), "alpha", 3, ModeHybrid, nil, rr, chunkText)
	if err != nil || failed {
		t.Fatalf("success: err=%v failed=%v", err, failed)
	}
	if rr.calls != 1 {
		t.Errorf("reranker called %d times, want 1", rr.calls)
	}
	if len(hits) == 0 {
		t.Fatal("expected reranked hits")
	}
	if logbuf.String() != "" {
		t.Errorf("no log line on a successful rerank; got %q", logbuf.String())
	}
}

// T019: retry is off by default (one attempt); when enabled it retries once.
func TestSearchWithRerank_Retry_DisabledByDefault(t *testing.T) {
	captureLog(t) // swallow the expected log line
	r, chunkText := rerankFixture()
	rr := &fakeReranker{err: errors.New("down"), model: "bge"}
	_, failed, err := r.SearchWithRerank(context.Background(), "alpha", 5, ModeHybrid, nil, rr, chunkText)
	if err != nil || !failed || rr.calls != 1 {
		t.Fatalf("retry off: err=%v failed=%v calls=%d (want nil,true,1)", err, failed, rr.calls)
	}
}

func TestSearchWithRerank_Retry_AlwaysFails(t *testing.T) {
	captureLog(t)
	r, chunkText := rerankFixture()
	r.EnableRerankRetry()
	rr := &fakeReranker{err: errors.New("down"), model: "bge"}
	hits, failed, err := r.SearchWithRerank(context.Background(), "alpha", 5, ModeHybrid, nil, rr, chunkText)
	if err != nil || !failed {
		t.Fatalf("retry then fail: err=%v failed=%v (want nil,true)", err, failed)
	}
	if rr.calls != 2 {
		t.Errorf("retry enabled + persistent failure: reranker called %d times, want 2", rr.calls)
	}
	if len(hits) == 0 {
		t.Error("should still return fallback hits after retry exhaustion")
	}
}

func TestSearchWithRerank_Retry_Recovers(t *testing.T) {
	logbuf := captureLog(t)
	r, chunkText := rerankFixture()
	r.EnableRerankRetry()
	rr := &eventuallyOKReranker{failsLeft: 1, model: "bge"}

	hits, failed, err := r.SearchWithRerank(context.Background(), "alpha", 5, ModeHybrid, nil, rr, chunkText)
	if err != nil {
		t.Fatalf("recover: err=%v", err)
	}
	if failed {
		t.Error("after a successful retry, rerankFailed must be false")
	}
	if rr.calls != 2 {
		t.Errorf("retry recover: reranker called %d times, want 2", rr.calls)
	}
	if len(hits) == 0 {
		t.Error("recovered rerank must return reranked hits")
	}
	if logbuf.String() != "" {
		t.Errorf("no log line should be emitted when retry recovers; got %q", logbuf.String())
	}
}
