package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
