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
	fts := newTestFTS(t)
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

// closef is a tolerance helper that avoids importing math just for Abs.
func closef(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }

// TestRRF_FormulaPin (H08/spec 009, FR-006) pins the exact fusion formula so any
// future drift is caught immediately: score(d) = Σ 1/(k + rank), rank 1-based
// (the 0-based loop adds +1). Tested directly on the fuser, independent of
// FTS/Vector ordering:
//   - "both"  ranks #1 (index 0) in each list → 1/(k+1) + 1/(k+1) = 2/(k+1)
//   - "vonly" ranks #2 (index 1) in vector only → 1/(k+2)
//   - "fonly" ranks #2 (index 1) in FTS only    → 1/(k+2)
func TestRRF_FormulaPin(t *testing.T) {
	vecHits := []Hit{{ChunkID: "both"}, {ChunkID: "vonly"}}
	ftsHits := []Hit{{ChunkID: "both"}, {ChunkID: "fonly"}}
	fused := reciprocalRankFusion(vecHits, ftsHits, 60)

	byID := map[string]float64{}
	for _, h := range fused {
		byID[h.ChunkID] = h.Score
	}
	if !closef(byID["both"], 2.0/61.0) {
		t.Errorf("both-lists score = %v, want 2/61 (%v)", byID["both"], 2.0/61.0)
	}
	if !closef(byID["vonly"], 1.0/62.0) {
		t.Errorf("vector-only score = %v, want 1/62 (%v)", byID["vonly"], 1.0/62.0)
	}
	if !closef(byID["fonly"], 1.0/62.0) {
		t.Errorf("fts-only score = %v, want 1/62 (%v)", byID["fonly"], 1.0/62.0)
	}
	// The both-lists chunk must rank first; the two single-list chunks tie and are
	// broken by ChunkID ("fonly" < "vonly").
	if len(fused) == 0 || fused[0].ChunkID != "both" {
		t.Errorf("both-lists chunk must rank first, got %v", fused)
	}
	if len(fused) != 3 || fused[1].ChunkID != "fonly" || fused[2].ChunkID != "vonly" {
		t.Errorf("tie-break order wrong: got %v", fused)
	}
}

// TestRetrieval_SetRRFK_ChangesFusionScore (H08/spec 009, US1) proves the RRF
// constant is honored end-to-end through Search: a chunk that ranks #1 in BOTH
// lists scores 2/(k+1), so the default (60), SetRRFK(30), and SetRRFK(200) yield
// three distinct, predictable scores. The fixture makes c1 unambiguously rank 0
// in both lists — the sole FTS match for "alpha" and an exact vector match.
func TestRetrieval_SetRRFK_ChangesFusionScore(t *testing.T) {
	mk := func() *Retrieval {
		fts := newTestFTS(t)
		vec := NewVector()
		fts.Index("c1", map[string]string{"body": "alpha"})
		vec.Add("c1", []float32{1.0, 0.0})
		fts.Index("c2", map[string]string{"body": "beta"}) // no "alpha" → not in FTS hits
		vec.Add("c2", []float32{0.5, 0.5})                 // cosine 0.707 → vector rank 1
		return NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	}
	scoreOf := func(r *Retrieval) float64 {
		hits, err := r.Search(context.Background(), "alpha", 5, ModeHybrid, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 || hits[0].ChunkID != "c1" {
			t.Fatalf("c1 must be the top hit, got %v", hits)
		}
		return hits[0].Score
	}

	def := scoreOf(mk()) // default k=60 → 2/(60+1)
	if !closef(def, 2.0/61.0) {
		t.Errorf("default k=60 score = %v, want 2/61 (%v)", def, 2.0/61.0)
	}
	r30 := mk()
	r30.SetRRFK(30)
	if s := scoreOf(r30); !closef(s, 2.0/31.0) {
		t.Errorf("k=30 score = %v, want 2/31 (%v)", s, 2.0/31.0)
	}
	r200 := mk()
	r200.SetRRFK(200)
	if s := scoreOf(r200); !closef(s, 2.0/201.0) {
		t.Errorf("k=200 score = %v, want 2/201 (%v)", s, 2.0/201.0)
	}
	// SetRRFK(0) is a no-op → default stays in effect.
	r0 := mk()
	r0.SetRRFK(0)
	if !closef(scoreOf(r0), def) {
		t.Error("SetRRFK(0) must leave the default in effect")
	}
}

// TestRetrieval_RRFK_NoOpInSingleListModes (H08/spec 009 edge case) confirms the
// RRF constant is inert in keyword and semantic modes (single list, no fusion):
// SetRRFK does not error and does not change single-list results.
func TestRetrieval_RRFK_NoOpInSingleListModes(t *testing.T) {
	fts := newTestFTS(t)
	vec := NewVector()
	fts.Index("c1", map[string]string{"body": "alpha keyword"})
	vec.Add("c1", []float32{1.0, 0.0})

	base := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	kwBase, err := base.Search(context.Background(), "alpha", 5, ModeKeyword, nil)
	if err != nil {
		t.Fatal(err)
	}

	tweaked := NewRetrieval(fts, vec, staticEmbed([]float32{1.0, 0.0}))
	tweaked.SetRRFK(999) // would be nonsensical for fusion; must be harmless here
	kwTweaked, err := tweaked.Search(context.Background(), "alpha", 5, ModeKeyword, nil)
	if err != nil {
		t.Fatalf("keyword search with SetRRFK must not error: %v", err)
	}
	if len(kwTweaked) != len(kwBase) || (len(kwTweaked) > 0 && kwTweaked[0].ChunkID != kwBase[0].ChunkID) {
		t.Errorf("keyword results must be unaffected by rrf_k: base=%v tweaked=%v", kwBase, kwTweaked)
	}

	semTweaked, err := tweaked.Search(context.Background(), "alpha", 5, ModeSemantic, nil)
	if err != nil {
		t.Fatalf("semantic search with SetRRFK must not error: %v", err)
	}
	if len(semTweaked) == 0 || semTweaked[0].ChunkID != "c1" {
		t.Errorf("semantic results must be unaffected by rrf_k: got %v", semTweaked)
	}
}

func TestRetrieval_CollapseSameDocument(t *testing.T) {
	fts := newTestFTS(t)
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
	fts := newTestFTS(t)
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
func rerankFixture(t testing.TB) (*Retrieval, func(string) string) {
	fts := newTestFTS(t)
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
	r, chunkText := rerankFixture(t)
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
	r, chunkText := rerankFixture(t)
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
	fts := newTestFTS(t)
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
	r, chunkText := rerankFixture(t)
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
	r, chunkText := rerankFixture(t)
	rr := &fakeReranker{err: errors.New("down"), model: "bge"}
	_, failed, err := r.SearchWithRerank(context.Background(), "alpha", 5, ModeHybrid, nil, rr, chunkText)
	if err != nil || !failed || rr.calls != 1 {
		t.Fatalf("retry off: err=%v failed=%v calls=%d (want nil,true,1)", err, failed, rr.calls)
	}
}

func TestSearchWithRerank_Retry_AlwaysFails(t *testing.T) {
	captureLog(t)
	r, chunkText := rerankFixture(t)
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
	r, chunkText := rerankFixture(t)
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
