package index

import (
	"encoding/json"
	"math"
	"os"
	"sort"
)

// Vector is a pure-Go in-memory vector store with cosine-similarity nearest-
// neighbour search and optional JSON persistence. The interface mirrors a chromem-go
// (HNSW) backend that can be swapped in later (Principle V; research Q4 deferred
// chromem-go disk persistence to keep the MVP CGo-free and testable).
type Vector struct {
	chunks map[string][]float32
	dims   int
}

// NewVector returns an empty vector store.
func NewVector() *Vector {
	return &Vector{chunks: map[string][]float32{}}
}

// Add stores (or replaces) the vector for a chunk.
func (v *Vector) Add(id string, vec []float32) {
	if v.dims == 0 && len(vec) > 0 {
		v.dims = len(vec)
	}
	v.chunks[id] = vec
}

// Delete removes a chunk's vector.
func (v *Vector) Delete(id string) {
	delete(v.chunks, id)
}

// Query returns the top-k chunks by cosine similarity to vec (Score = similarity).
func (v *Vector) Query(vec []float32, k int) []Hit {
	type scored struct {
		id string
		s  float64
	}
	all := make([]scored, 0, len(v.chunks))
	for id, cv := range v.chunks {
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
