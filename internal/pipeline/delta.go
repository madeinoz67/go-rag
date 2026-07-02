package pipeline

import (
	"github.com/madeinoz67/go-rag/internal/events"
	"github.com/madeinoz67/go-rag/internal/model"
)

// diffChunks computes the per-chunk delta between an old and a new chunk set for
// the same source document, keyed on ContentHash (content identity, not position).
// Returns the deltas + a map from old chunk ID -> new chunk ID for UNCHANGED
// chunks (the remap a consumer uses to preserve stored references).
//
// Multiset semantics (spec 043 / BL-010; research R2): repeated text — the same
// ContentHash appearing N times — yields min(N_old, N_new) UNCHANGED, with the
// surplus counted REMOVED (old surplus) or ADDED (new surplus). A paragraph that
// moved (same text, new position) is UNCHANGED. A chunk with an empty
// ContentHash (pre-v2, pre-migration) is treated as unique — always changed —
// the safe degradation (research R5).
func diffChunks(old, newChunks []model.Chunk) ([]events.ChunkDelta, map[string]string) {
	// Bucket old + new chunk IDs by ContentHash, preserving within-bucket order
	// so the UNCHANGED pairing (old[i] -> new[i]) is stable.
	oldBuckets := bucketByHash(old)
	newBuckets := bucketByHash(newChunks)

	var deltas []events.ChunkDelta
	remap := map[string]string{}

	// For each old hash: pair min(old,new) as UNCHANGED, surplus-old as REMOVED;
	// consume the matching new bucket (what remains is ADDED).
	for h, ob := range oldBuckets {
		nb := newBuckets[h]
		u := len(ob)
		if len(nb) < u {
			u = len(nb)
		}
		for i := 0; i < u; i++ {
			deltas = append(deltas, events.ChunkDelta{Change: events.ChangeUnchanged, NewChunkID: nb[i], PrevChunkID: ob[i]})
			remap[ob[i]] = nb[i]
		}
		for i := u; i < len(ob); i++ {
			deltas = append(deltas, events.ChunkDelta{Change: events.ChangeRemoved, PrevChunkID: ob[i]})
		}
		delete(newBuckets, h) // consumed
	}
	// Remaining new buckets are ADDED.
	for _, nb := range newBuckets {
		for _, id := range nb {
			deltas = append(deltas, events.ChunkDelta{Change: events.ChangeAdded, NewChunkID: id})
		}
	}
	return deltas, remap
}

// bucketByHash groups chunk IDs by ContentHash, preserving order. An empty
// ContentHash (pre-v2) is made unique per-chunk so it never matches (always
// changed) — the safe degradation.
func bucketByHash(chunks []model.Chunk) map[string][]string {
	buckets := map[string][]string{}
	for _, c := range chunks {
		h := c.ContentHash
		if h == "" {
			h = "\x00" + c.ID // unique per pre-v2 chunk
		}
		buckets[h] = append(buckets[h], c.ID)
	}
	return buckets
}
