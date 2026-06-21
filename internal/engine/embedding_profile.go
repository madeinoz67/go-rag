package engine

import (
	"encoding/json"
	"sort"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// EmbeddingProfile summarizes the embedding provenance stored in a vault: the
// majority model + dimensionality, the per-model/per-dim distribution that
// reveals drift, and whether the corpus is consistent (one model, one dim). It
// is the basis for the H03 mismatch guard (query refusal) and the status drift
// view. Derived from stored records — no new storage, no schema change.
type EmbeddingProfile struct {
	MajorityModel string         // the model name on the plurality of records ("" if none)
	MajorityDim   int            // len(Vector) of the plurality (0 if none)
	ModelCounts   map[string]int // per-model record counts
	DimCounts     map[int]int    // per-dimensionality record counts
	Total         int            // number of embedding records scanned
	Consistent    bool           // true iff at most one model and one dim are present
}

// storedEmbed mirrors the persisted pipeline embedding shape {model, vector} at
// prefix 0x04 (see internal/pipeline/workers.go) without importing the pipeline
// package. Dimensionality is len(Vector).
type storedEmbed struct {
	Model  string    `json:"model,omitempty"`
	Vector []float32 `json:"vector"`
}

// CorpusProfile derives the embedding profile from a vault's stored embedding
// records (prefix 0x04). Read-only and pure (no network). An empty corpus
// returns a zero profile with Consistent=true (vacuously — no drift). Ties in
// the majority are broken deterministically (lexicographically for models,
// numerically for dims) so the reported "expected" value is stable.
func CorpusProfile(db *storage.DB) EmbeddingProfile {
	p := EmbeddingProfile{
		ModelCounts: map[string]int{},
		DimCounts:   map[int]int{},
		Consistent:  true,
	}
	if db == nil {
		return p
	}
	_ = db.PrefixScanByte(storage.PrefixEmbedding, func(_, val []byte) bool {
		var se storedEmbed
		if json.Unmarshal(val, &se) != nil {
			return true
		}
		p.Total++
		p.ModelCounts[se.Model]++
		p.DimCounts[len(se.Vector)]++
		return true
	})
	if p.Total == 0 {
		return p
	}
	p.MajorityModel = majorityString(p.ModelCounts)
	p.MajorityDim = majorityInt(p.DimCounts)
	p.Consistent = len(p.ModelCounts) <= 1 && len(p.DimCounts) <= 1
	return p
}

// majorityString returns the key with the highest count, breaking ties by
// lexicographic order for determinism.
func majorityString(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best := ""
	bestN := -1
	for _, k := range keys {
		if m[k] > bestN {
			best, bestN = k, m[k]
		}
	}
	return best
}

// majorityInt returns the key with the highest count, breaking ties by ascending
// numeric order for determinism.
func majorityInt(m map[int]int) int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	best, bestN := 0, -1
	for _, k := range keys {
		if m[k] > bestN {
			best, bestN = k, m[k]
		}
	}
	return best
}
