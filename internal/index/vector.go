package index

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"sync"
)

// Vector is a pure-Go in-memory vector store with cosine-similarity nearest-
// neighbour search and optional JSON persistence. Goroutine-safe (mutated by the
// pipeline's concurrent background workers). The interface mirrors a chromem-go
// (HNSW) backend that can be swapped in later (Principle V; research Q4).
type Vector struct {
	mu     sync.Mutex
	chunks map[string][]float32
	dims   int
}

// NewVector returns an empty vector store.
func NewVector() *Vector {
	return &Vector{chunks: map[string][]float32{}}
}

// Add stores (or replaces) the vector for a chunk.
func (v *Vector) Add(id string, vec []float32) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dims == 0 && len(vec) > 0 {
		v.dims = len(vec)
	}
	v.chunks[id] = vec
}

// Delete removes a chunk's vector.
func (v *Vector) Delete(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.chunks, id)
}

// Query returns the top-k chunks by cosine similarity to vec (Score = similarity).
//
// Audit H03 guard: a stored vector whose length differs from the query vector's
// length is SKIPPED, never scored. Without this, cosine() would silently score
// over min(len(a), len(b)) dimensions and return a plausible-but-wrong similarity
// for a model/dimensionality mismatch — the silent-corruption failure mode. (On
// the happy path the corpus is single-dimensionality, so nothing is skipped; the
// guard only bites a mixed or mismatched corpus.)
func (v *Vector) Query(vec []float32, k int) []Hit {
	v.mu.Lock()
	defer v.mu.Unlock()

	type scored struct {
		id string
		s  float64
	}
	all := make([]scored, 0, len(v.chunks))
	for id, cv := range v.chunks {
		if len(cv) != len(vec) {
			continue // length mismatch: skip rather than garbage-score (audit H03)
		}
		all = append(all, scored{id, cosine(vec, cv)})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].s != all[j].s {
			return all[i].s > all[j].s
		}
		return all[i].id < all[j].id
	})
	if k > 0 && k < len(all) {
		all = all[:k]
	}
	out := make([]Hit, len(all))
	for i, a := range all {
		out[i] = Hit{ChunkID: a.id, Score: a.s}
	}
	return out
}

// Save persists the vectors to a JSON file.
func (v *Vector) Save(path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	data, err := json.Marshal(v.chunks)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads vectors from a JSON file, replacing current contents.
func (v *Vector) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.chunks = map[string][]float32{}
	if err := json.Unmarshal(data, &v.chunks); err != nil {
		return err
	}
	v.dims = 0
	for _, vec := range v.chunks {
		v.dims = len(vec)
		break
	}
	return nil
}

// cosine returns the cosine similarity between two vectors.
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
