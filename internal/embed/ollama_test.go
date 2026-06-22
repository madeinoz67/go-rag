package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEmbed_FakeServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := embedResponse{Model: req.Model}
		for range req.Input {
			resp.Embeddings = append(resp.Embeddings, []float32{0.1, 0.2, 0.3})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "test-model")
	vecs, err := o.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("want 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) != 3 {
		t.Fatalf("want dim 3, got %d", len(vecs[0]))
	}
	if o.Model() != "test-model" {
		t.Errorf("model: %q", o.Model())
	}
	if o.Dimensions() != 3 {
		t.Errorf("dimensions: %d", o.Dimensions())
	}
}

func TestEmbed_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{{0.5}}})
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m")
	vecs, err := o.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("should succeed after retry: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("want 1 vector, got %d", len(vecs))
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("should have retried (calls=%d)", calls)
	}
}

func TestEmbed_EmptyInput(t *testing.T) {
	o := NewOllama("http://x", "m")
	vecs, err := o.Embed(context.Background(), nil)
	if err != nil || vecs != nil {
		t.Fatalf("empty input -> nil,nil; got %v %v", vecs, err)
	}
}

// Compile-time check: *Ollama satisfies Embedder.
var _ Embedder = (*Ollama)(nil)

// --- H12/spec 010: bounded batching tests ---

// countEmbedServer serves a trivial valid embedding for each input and counts
// how many requests it received (via *calls). Embed is sequential per call, so
// the counter needs no extra synchronization beyond atomic.
func countEmbedServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := embedResponse{}
		for range req.Input {
			resp.Embeddings = append(resp.Embeddings, []float32{0.1})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// detLenServer returns, for each input text, a 1-D vector encoding the text's
// length. It is deterministic and position-independent — so batching cannot
// change the result, which is what TestEmbed_OrderPreserved_AcrossBatches proves.
func detLenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := embedResponse{}
		for _, txt := range req.Input {
			resp.Embeddings = append(resp.Embeddings, []float32{float32(len(txt))})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEmbed_LargeInput_BoundedRequests (H12 US1, SC-001/SC-002): N≫cap texts
// embed fully and EVERY request carries ≤ embedBatchSize texts — no oversized
// request. Embed is sequential, so the recorded sizes need no lock.
func TestEmbed_LargeInput_BoundedRequests(t *testing.T) {
	const n = 500
	var got []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		got = append(got, len(req.Input))
		resp := embedResponse{Model: req.Model}
		for range req.Input {
			resp.Embeddings = append(resp.Embeddings, []float32{0.1})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	texts := make([]string, n)
	for i := range texts {
		texts[i] = "x"
	}
	o := NewOllama(srv.URL, "m")
	vecs, err := o.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("embed 500: %v", err)
	}
	if len(vecs) != n {
		t.Fatalf("want %d vectors, got %d", n, len(vecs))
	}
	for _, sz := range got {
		if sz > embedBatchSize {
			t.Errorf("a request carried %d texts; cap is %d", sz, embedBatchSize)
		}
	}
	if want := (n + embedBatchSize - 1) / embedBatchSize; len(got) != want {
		t.Errorf("want %d requests, got %d", want, len(got))
	}
}

// TestEmbed_TransientBatchFailure_Retried (H12 US2): a transient 500 on one
// batch's first attempt is retried and the whole call still succeeds.
func TestEmbed_TransientBatchFailure_Retried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The 2nd request overall is the 2nd batch's first attempt (batch 1
		// succeeded on attempt 1). Fail it once; the retry then succeeds.
		if atomic.AddInt32(&calls, 1) == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := embedResponse{}
		for range req.Input {
			resp.Embeddings = append(resp.Embeddings, []float32{0.7})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	const batches = 3
	texts := make([]string, embedBatchSize*batches) // forces ≥2 batches
	for i := range texts {
		texts[i] = "y"
	}
	o := NewOllama(srv.URL, "m")
	vecs, err := o.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("transient batch failure should be retried: %v", err)
	}
	if len(vecs) != embedBatchSize*batches {
		t.Fatalf("want %d vectors, got %d", embedBatchSize*batches, len(vecs))
	}
	if atomic.LoadInt32(&calls) < 4 {
		t.Errorf("expected a retry (≥4 requests for 3 batches); got %d", calls)
	}
}

// TestEmbed_PermanentBatchFailure_NoPartial (H12 US2, FR-006): a batch that
// fails on every retry fails the whole call with NO partial vector set. The
// failing batch is identified by content ("FAIL"); it 500s persistently.
func TestEmbed_PermanentBatchFailure_NoPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, txt := range req.Input {
			if strings.Contains(txt, "FAIL") {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		resp := embedResponse{}
		for range req.Input {
			resp.Embeddings = append(resp.Embeddings, []float32{0.7})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	texts := make([]string, embedBatchSize*3)
	for i := range texts {
		if i >= embedBatchSize && i < 2*embedBatchSize {
			texts[i] = "FAIL" // the whole 2nd batch fails persistently
		} else {
			texts[i] = "ok"
		}
	}
	o := NewOllama(srv.URL, "m")
	vecs, err := o.Embed(context.Background(), texts)
	if err == nil {
		t.Fatal("permanent batch failure must surface an error")
	}
	if len(vecs) != 0 {
		t.Errorf("no partial result allowed; got %d vectors", len(vecs))
	}
}

// TestEmbed_OrderPreserved_AcrossBatches (H12 US3, SC-003): the returned vectors
// are identical in value and order regardless of batching. The stand-in is purely
// a function of each text (its length), so grouping cannot change the result;
// the order check (out[i] encodes len(texts[i])) proves concatenation is ordered.
func TestEmbed_OrderPreserved_AcrossBatches(t *testing.T) {
	srv := detLenServer(t)
	o := NewOllama(srv.URL, "m")
	// 50 texts of distinct lengths 1..50 — > embedBatchSize (32), so 2 batches.
	texts := make([]string, 50)
	for i := range texts {
		texts[i] = strings.Repeat("a", i+1)
	}
	vecs, err := o.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 50 {
		t.Fatalf("want 50 vectors, got %d", len(vecs))
	}
	for i, v := range vecs {
		want := float32(i + 1)
		if len(v) != 1 || v[0] != want {
			t.Errorf("out[%d] = %v, want [%v] (text length %d) — order/value not preserved",
				i, v, want, i+1)
		}
	}
}

// TestEmbed_PerBatchCountMismatch_Rejected (H12 US3, FR-005): a response whose
// vector count ≠ its text count is rejected — never a short/padded result that
// would silently misalign later batches.
func TestEmbed_PerBatchCountMismatch_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := embedResponse{}
		// Return one fewer embedding than inputs → integrity guard trips.
		for i := 0; i < len(req.Input)-1; i++ {
			resp.Embeddings = append(resp.Embeddings, []float32{0.1})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m")
	// 3 texts (< cap → one batch); stand-in returns 2 → mismatch → error.
	if _, err := o.Embed(context.Background(), []string{"a", "b", "c"}); err == nil {
		t.Fatal("count mismatch must be rejected, not silently accepted")
	}
}

// TestEmbed_BatchEdgeCases (H12 US3, FR-007): empty input is a no-op (zero
// requests); sub-cap input is exactly one request; a non-multiple-of-cap input
// produces ceil(N/cap) requests and all N vectors in order.
func TestEmbed_BatchEdgeCases(t *testing.T) {
	// (a) empty input: no request, (nil, nil).
	srv0, calls0 := countEmbedServer(t)
	o0 := NewOllama(srv0.URL, "m")
	if vecs, err := o0.Embed(context.Background(), nil); err != nil || vecs != nil {
		t.Fatalf("empty: want nil,nil; got %v %v", vecs, err)
	}
	if c := atomic.LoadInt32(calls0); c != 0 {
		t.Errorf("empty: want 0 requests, got %d", c)
	}

	// (b) sub-cap (5): exactly one request.
	srv1, calls1 := countEmbedServer(t)
	o1 := NewOllama(srv1.URL, "m")
	vecs5, err := o1.Embed(context.Background(), []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatalf("sub-cap: %v", err)
	}
	if len(vecs5) != 5 {
		t.Fatalf("sub-cap: want 5 vecs, got %d", len(vecs5))
	}
	if c := atomic.LoadInt32(calls1); c != 1 {
		t.Errorf("sub-cap: want 1 request, got %d", c)
	}

	// (c) non-multiple of cap (70): ceil(70/32)=3 requests, 70 vecs.
	srv2, calls2 := countEmbedServer(t)
	o2 := NewOllama(srv2.URL, "m")
	texts70 := make([]string, 70)
	for i := range texts70 {
		texts70[i] = "x"
	}
	vecs70, err := o2.Embed(context.Background(), texts70)
	if err != nil {
		t.Fatalf("70: %v", err)
	}
	if len(vecs70) != 70 {
		t.Fatalf("70: want 70 vecs, got %d", len(vecs70))
	}
	wantReqs := int32((70 + embedBatchSize - 1) / embedBatchSize) // ceil(70/32)=3
	if c := atomic.LoadInt32(calls2); c != wantReqs {
		t.Errorf("70: want %d requests, got %d", wantReqs, c)
	}
}
