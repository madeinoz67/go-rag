// Package index holds the two retrieval indexes (PRD §6.6): a field-weighted BM25
// full-text index and an HNSW vector index (chromem-go). This file implements the
// in-memory BM25 FTS.
package index

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Field weights (PRD §6.6): title 3x, headings 2x, body 1x.
const (
	weightTitle   = 3.0
	weightHeading = 2.0
	weightBody    = 1.0
)

// FTS is an in-memory field-weighted BM25 full-text index.
type FTS struct {
	// term -> chunkID -> weighted term frequency
	postings map[string]map[string]float64
	docLen   map[string]int // chunkID -> total weighted term count
	totalLen int
	N        int
}

// NewFTS returns an empty BM25 index.
func NewFTS() *FTS {
	return &FTS{
		postings: map[string]map[string]float64{},
		docLen:   map[string]int{},
	}
}

// fieldWeight returns the BM25 weight multiplier for a named field.
func fieldWeight(field string) float64 {
	switch field {
	case "title":
		return weightTitle
	case "heading", "headings":
		return weightHeading
	default:
		return weightBody
	}
}

// Index adds a chunk's fields to the index. fields maps field names (title/heading/
// body) to their text. Re-indexing an existing chunkID replaces its prior content.
func (f *FTS) Index(chunkID string, fields map[string]string) {
	if _, exists := f.docLen[chunkID]; !exists {
		f.N++
	} else {
		// Re-index: remove old postings for this chunk before re-adding.
		f.removeChunk(chunkID)
		f.N++
	}
	weighted := 0
	for field, text := range fields {
		w := fieldWeight(field)
		for _, term := range Tokenize(text) {
			if f.postings[term] == nil {
				f.postings[term] = map[string]float64{}
			}
			f.postings[term][chunkID] += w
			weighted++
		}
	}
	f.docLen[chunkID] = weighted
	f.totalLen += weighted
}

// removeChunk drops a chunk's postings (used on re-index / delete).
func (f *FTS) removeChunk(chunkID string) {
	old := f.docLen[chunkID]
	for term, posts := range f.postings {
		if _, ok := posts[chunkID]; ok {
			delete(posts, chunkID)
			if len(posts) == 0 {
				delete(f.postings, term)
			}
		}
	}
	f.totalLen -= old
	delete(f.docLen, chunkID)
	f.N--
}

// Delete removes a chunk from the index.
func (f *FTS) Delete(chunkID string) {
	if _, ok := f.docLen[chunkID]; !ok {
		return
	}
	f.removeChunk(chunkID)
}

// Hit is a ranked search result.
type Hit struct {
	ChunkID string
	Score   float64
}

// Search ranks chunks by BM25 relevance to the query, returning the top k.
func (f *FTS) Search(query string, k int) []Hit {
	terms := Tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	avgDL := 0.0
	if f.N > 0 {
		avgDL = float64(f.totalLen) / float64(f.N)
	}
	const k1, b = 1.2, 0.75

	scores := map[string]float64{}
	for _, term := range terms {
		posts := f.postings[term]
		if len(posts) == 0 {
			// Short-term fallback: match indexed terms that start with the query
			// term (a lighter stand-in for trigram fallback).
			if len(term) < 4 {
				for indexed, iposts := range f.postings {
					if strings.HasPrefix(indexed, term) {
						posts = mergePostings(posts, iposts)
					}
				}
			}
			if len(posts) == 0 {
				continue
			}
		}
		df := len(posts)
		idf := math.Log(1 + (float64(f.N)-float64(df)+0.5)/(float64(df)+0.5))
		for chunkID, tf := range posts {
			dl := float64(f.docLen[chunkID])
			denom := tf + k1*(1-b+b*dl/avgDL)
			scores[chunkID] += idf * (tf * (k1 + 1)) / denom
		}
	}

	hits := make([]Hit, 0, len(scores))
	for id, sc := range scores {
		hits = append(hits, Hit{ChunkID: id, Score: sc})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ChunkID < hits[j].ChunkID
	})
	if k > 0 && k < len(hits) {
		hits = hits[:k]
	}
	return hits
}

func mergePostings(a, b map[string]float64) map[string]float64 {
	if a == nil {
		a = map[string]float64{}
	}
	for k, v := range b {
		a[k] += v
	}
	return a
}

// Tokenize lowercases, splits on non-alphanumerics, and drops stopwords. Short
// terms are retained (the Search short-term fallback handles fuzzy matching).
func Tokenize(s string) []string {
	s = strings.ToLower(s)
	out := make([]string, 0, 16)
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			w := b.String()
			if !isStopword(w) {
				out = append(out, w)
			}
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func isStopword(w string) bool {
	switch w {
	case "the", "a", "an", "and", "or", "but", "of", "to", "in", "on", "for",
		"is", "are", "was", "were", "be", "been", "with", "as", "at", "by",
		"this", "that", "it", "from":
		return true
	}
	return false
}
