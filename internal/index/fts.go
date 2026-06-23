// Package index holds the two retrieval indexes (PRD §6.6): a field-weighted BM25
// full-text index and an HNSW vector index (chromem-go). This file implements
// the Pebble-backed BM25 FTS (audit H16/spec 018, pivoted from an in-memory map).
// Both FTS and Vector are goroutine-safe because the ingest pipeline's background
// workers mutate them concurrently.
package index

import (
	"encoding/binary"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/cockroachdb/pebble"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// Field weights (PRD §6.6): title 3x, headings 2x, body 1x.
const (
	weightTitle   = 3.0
	weightHeading = 2.0
	weightBody    = 1.0
)

// BM25 constants (unchanged from the in-memory FTS — transparency, FR-008).
const (
	k1BM25 = 1.2
	bBM25  = 0.75
)

// FTS is a Pebble-backed field-weighted BM25 full-text index (audit H16/spec
// 018). Postings live as Pebble keys under PrefixFTSPosting (0x05), queried via
// per-term prefix scans. The only in-memory state is a lazy IDF cache. Safe for
// concurrent use.
type FTS struct {
	db       *pebble.DB
	mu       sync.RWMutex
	idfCache map[string]float64 // term → idf (invalidated on Index/Delete)
}

// NewFTS returns a Pebble-backed BM25 index over db. O(1) — no postings to load
// (they're on disk). The db must remain open for the FTS's lifetime.
func NewFTS(db *pebble.DB) *FTS {
	return &FTS{db: db, idfCache: make(map[string]float64, 1024)}
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

// postingKey builds a Pebble key for a (term, chunkID) posting.
// Layout: 0x05 | term | 0x00 | chunkID
func postingKey(term, chunkID string) []byte {
	k := make([]byte, 0, 1+len(term)+1+len(chunkID))
	k = append(k, storage.PrefixFTSPosting)
	k = append(k, term...)
	k = append(k, 0x00) // separator (terms + chunkIDs are alphanumeric — no 0x00)
	k = append(k, chunkID...)
	return k
}

// encodePosting encodes tf (weighted) + docLen into 6 bytes.
func encodePosting(tf float32, docLen int) []byte {
	buf := make([]byte, 6)
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(tf))
	binary.LittleEndian.PutUint16(buf[4:6], uint16(docLen))
	return buf
}

// decodePosting decodes 6 bytes into tf + docLen.
func decodePosting(buf []byte) (tf float32, docLen int) {
	if len(buf) < 6 {
		return 0, 0
	}
	tf = math.Float32frombits(binary.LittleEndian.Uint32(buf[0:4]))
	docLen = int(binary.LittleEndian.Uint16(buf[4:6]))
	return
}

// chunkIDFromKey extracts the chunkID from a posting key (after the 0x00 sep).
func chunkIDFromKey(key []byte) string {
	// key = 0x05 | term | 0x00 | chunkID. Find the first 0x00 after the prefix.
	for i := 1; i < len(key); i++ {
		if key[i] == 0x00 {
			return string(key[i+1:])
		}
	}
	return ""
}

// globalStats holds the vault-level BM25 statistics (stored under PrefixFTSGlobalSt).
type globalStats struct {
	N        uint64 // number of indexed chunks
	TotalLen uint64 // sum of all chunk docLens
}

func encodeStats(s globalStats) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint64(buf[0:8], s.N)
	binary.LittleEndian.PutUint64(buf[8:16], s.TotalLen)
	return buf
}

func decodeStats(buf []byte) globalStats {
	if len(buf) < 16 {
		return globalStats{}
	}
	return globalStats{
		N:        binary.LittleEndian.Uint64(buf[0:8]),
		TotalLen: binary.LittleEndian.Uint64(buf[8:16]),
	}
}

func statsKey() []byte {
	return []byte{storage.PrefixFTSGlobalSt, 's', 't', 'a', 't', 's'}
}

// readStats reads the global BM25 stats (N, TotalLen) from Pebble.
func (f *FTS) readStats() globalStats {
	val, closer, err := f.db.Get(statsKey())
	if err != nil || closer == nil {
		return globalStats{}
	}
	defer closer.Close()
	return decodeStats(val)
}

// writeStats writes the global BM25 stats (NoSync — called from a batch commit).
func (f *FTS) writeStats(b *pebble.Batch, s globalStats) {
	b.Set(statsKey(), encodeStats(s), nil)
}

// Index adds a chunk's fields to the Pebble-backed index. fields maps field names
// (title/heading/body) to their text. Re-indexing an existing chunkID is a no-op
// (idempotency guard via PrefixFTSIndexed). The tf stored per posting is the
// field-weighted term frequency SUMMED across all fields (same as the in-memory
// FTS — transparency, FR-008). BM25 math is unchanged.
func (f *FTS) Index(chunkID string, fields map[string]string) {
	// Idempotency guard: if this chunkID is already indexed, skip.
	idxKey := append([]byte{storage.PrefixFTSIndexed}, []byte(chunkID)...)
	if val, closer, err := f.db.Get(idxKey); err == nil {
		closer.Close()
		_ = val // already indexed — no-op (processJob may call twice; H01 idempotent)
		return
	}

	// Tokenize each field, accumulate weighted tf per term.
	termCounts := make(map[string]float64) // term → summed weighted tf
	docLen := 0
	for field, text := range fields {
		w := fieldWeight(field)
		for _, term := range Tokenize(text) {
			termCounts[term] += w
			docLen++
		}
	}
	if len(termCounts) == 0 {
		return // nothing to index (empty/all-stopword content)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Build one atomic batch: postings + indexed-set + stats.
	batch := f.db.NewBatch()
	for term, tf := range termCounts {
		batch.Set(postingKey(term, chunkID), encodePosting(float32(tf), docLen), nil)
	}
	batch.Set(idxKey, encodePosting(0, docLen)[4:6], nil) // store docLen (2 bytes) for delete

	// Update global stats.
	s := f.readStats()
	s.N++
	s.TotalLen += uint64(docLen)
	f.writeStats(batch, s)

	// Invalidate the IDF cache for all touched terms.
	for term := range termCounts {
		delete(f.idfCache, term)
	}

	_ = batch.Commit(pebble.NoSync)
}

// Delete removes a chunk from the index. content is the chunk's text (needed to
// recover the terms for key construction — the posting key shape puts chunkID at
// the key's end, so a chunkID-prefix scan is not possible). Idempotent (deleting
// absent keys is a Pebble no-op).
func (f *FTS) Delete(chunkID, content string) {
	idxKey := append([]byte{storage.PrefixFTSIndexed}, []byte(chunkID)...)

	// Read the stored docLen for stats update.
	val, closer, err := f.db.Get(idxKey)
	docLen := 0
	if err == nil && closer != nil {
		if len(val) >= 2 {
			docLen = int(binary.LittleEndian.Uint16(val[0:2]))
		}
		closer.Close()
	} else {
		return // not indexed — nothing to delete
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Collect unique terms from the content.
	termSet := make(map[string]struct{})
	for _, term := range Tokenize(content) {
		termSet[term] = struct{}{}
	}

	batch := f.db.NewBatch()
	for term := range termSet {
		batch.Delete(postingKey(term, chunkID), nil)
		delete(f.idfCache, term)
	}
	batch.Delete(idxKey, nil)

	// Update global stats.
	s := f.readStats()
	if s.N > 0 {
		s.N--
	}
	if s.TotalLen >= uint64(docLen) {
		s.TotalLen -= uint64(docLen)
	}
	f.writeStats(batch, s)

	_ = batch.Commit(pebble.NoSync)
}

// Hit is a ranked search result.
type Hit struct {
	ChunkID string
	Score   float64
}

// Search ranks chunks by BM25 relevance to the query, returning the top k.
// The BM25 math is IDENTICAL to the pre-pivot in-memory FTS (transparency, FR-008):
// k1=1.2, b=0.75, field-weighted tf, prefix expansion for short terms (<4 chars).
func (f *FTS) Search(query string, k int) []Hit {
	terms := Tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	st := f.readStats()
	n := float64(st.N)
	avgDL := 0.0
	if st.N > 0 {
		avgDL = float64(st.TotalLen) / float64(st.N)
	}

	scores := map[string]float64{}
	for _, term := range terms {
		// Scan postings for this term (exact match).
		posts := f.scanTerm(term)
		if len(posts) == 0 && len(term) < 4 {
			// Prefix expansion: scan all terms starting with this prefix.
			posts = f.scanPrefix(term)
		}
		if len(posts) == 0 {
			continue
		}
		df := float64(len(posts))
		idf := math.Log(1 + (n-df+0.5)/(df+0.5))
		for chunkID, tf := range posts {
			dl := f.docLenOf(chunkID)
			if dl == 0 {
				dl = avgDL
			}
			denom := tf + k1BM25*(1-bBM25+bBM25*dl/avgDL)
			scores[chunkID] += idf * (tf * (k1BM25 + 1)) / denom
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

// scanTerm scans postings for an exact term and returns chunkID → tf.
func (f *FTS) scanTerm(term string) map[string]float64 {
	lower := postingKey(term, "")
	// UpperBound: 0x05 | term | 0x01 (covers all 0x00 | chunkID suffixes)
	upper := make([]byte, len(lower))
	copy(upper, lower)
	upper[len(lower)-1] = 0x01 // bump the 0x00 separator to 0x01

	return f.scanRange(lower[:1+len(term)], upper, term)
}

// scanPrefix scans postings for ALL terms starting with prefix (the prefix
// expansion for short queries). Returns chunkID → summed tf (merged across terms).
func (f *FTS) scanPrefix(prefix string) map[string]float64 {
	if prefix == "" {
		return nil
	}
	lower := append([]byte{storage.PrefixFTSPosting}, []byte(prefix)...)
	// UpperBound: increment the last byte of the prefix.
	upper := make([]byte, len(lower))
	copy(upper, lower)
	upper[len(upper)-1]++
	if upper[len(upper)-1] == 0 {
		return nil // overflow (prefix ends with 0xFF) — skip
	}
	return f.scanRange(lower, upper, "")
}

// scanRange scans a Pebble key range, decoding postings and returning chunkID → tf.
// knownTerm, when non-empty, allows direct chunkID extraction (offset is fixed).
// When empty (prefix expansion), the chunkID is extracted via the 0x00 separator.
func (f *FTS) scanRange(lower, upper []byte, knownTerm string) map[string]float64 {
	posts := map[string]float64{}
	iter, err := f.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return posts
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		var chunkID string
		if knownTerm != "" {
			// Exact term: chunkID starts after prefix(1) + term(n) + sep(1).
			off := 1 + len(knownTerm) + 1
			if len(key) > off {
				chunkID = string(key[off:])
			}
		} else {
			chunkID = chunkIDFromKey(key)
		}
		if chunkID == "" {
			continue
		}
		tf, _ := decodePosting(iter.Value())
		posts[chunkID] += float64(tf) // merge (prefix expansion may hit same chunkID via different terms)
	}
	return posts
}

// docLenOf reads a chunk's docLen from its indexed-set entry.
func (f *FTS) docLenOf(chunkID string) float64 {
	idxKey := append([]byte{storage.PrefixFTSIndexed}, []byte(chunkID)...)
	val, closer, err := f.db.Get(idxKey)
	if err != nil || closer == nil {
		return 0
	}
	defer closer.Close()
	if len(val) >= 2 {
		return float64(binary.LittleEndian.Uint16(val[0:2]))
	}
	return 0
}

// Tokenize lowercases, splits on non-alphanumerics, and drops stopwords.
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

// migrateFromChunks builds the Pebble-backed FTS postings from existing chunks
// (one-time migration for pre-pivot vaults). Called by LoadIndex when the global
// stats key is absent. Uses a Pebble batch for efficiency.
func MigrateFromChunks(db *pebble.DB, chunks func(yield func(chunkID, content string) bool)) error {
	batch := db.NewBatch()
	var n, totalLen uint64
	flush := func() {
		_ = batch.Commit(pebble.NoSync)
		batch = db.NewBatch()
	}
	count := 0
	chunks(func(chunkID, content string) bool {
		terms := Tokenize(content)
		if len(terms) == 0 {
			return true // skip empty/all-stopword
		}
		termCounts := map[string]float64{}
		for _, t := range terms {
			termCounts[t] += weightBody
		}
		docLen := len(terms)
		for term, tf := range termCounts {
			batch.Set(postingKey(term, chunkID), encodePosting(float32(tf), docLen), nil)
		}
		idxKey := append([]byte{storage.PrefixFTSIndexed}, []byte(chunkID)...)
		batch.Set(idxKey, encodePosting(0, docLen)[4:6], nil)
		n++
		totalLen += uint64(docLen)
		count++
		if count >= 256 {
			flush()
		}
		return true // continue
	})
	batch.Set(statsKey(), encodeStats(globalStats{N: n, TotalLen: totalLen}), nil)
	_ = batch.Commit(pebble.NoSync)
	return nil
}

// HasPostings reports whether the Pebble-backed FTS has been initialized (the
// global stats key exists). Used by LoadIndex to decide whether to run the
// one-time migration.
func HasPostings(db *pebble.DB) bool {
	val, closer, err := db.Get(statsKey())
	if err != nil || closer == nil {
		return false
	}
	closer.Close()
	return len(val) >= 16
}
