package pipeline

import (
	"fmt"
	"testing"

	"github.com/madeinoz67/go-rag/internal/events"
	"github.com/madeinoz67/go-rag/internal/model"
)

// countChanges tallies deltas by change type.
func countChanges(deltas []events.ChunkDelta) map[events.ChunkChange]int {
	m := map[events.ChunkChange]int{}
	for _, d := range deltas {
		m[d.Change]++
	}
	return m
}

// TestDiffChunks_LocalizedEdit: 2 unchanged + 1 removed (C) + 1 added (X) +
// the old→new remap for the unchanged chunks.
func TestDiffChunks_LocalizedEdit(t *testing.T) {
	old := []model.Chunk{{ID: "o1", ContentHash: "A"}, {ID: "o2", ContentHash: "B"}, {ID: "o3", ContentHash: "C"}}
	newChunks := []model.Chunk{{ID: "n1", ContentHash: "A"}, {ID: "n2", ContentHash: "B"}, {ID: "n3", ContentHash: "X"}}
	deltas, remap := diffChunks(old, newChunks)
	c := countChanges(deltas)
	if c[events.ChangeUnchanged] != 2 || c[events.ChangeRemoved] != 1 || c[events.ChangeAdded] != 1 {
		t.Errorf("counts = %+v, want 2 UNCHANGED, 1 REMOVED, 1 ADDED", c)
	}
	if remap["o1"] != "n1" || remap["o2"] != "n2" {
		t.Errorf("remap = %v, want o1->n1, o2->n2", remap)
	}
}

// TestDiffChunks_RepeatedText: a paragraph repeated 3×→2× yields 2 UNCHANGED +
// 1 REMOVED (multiset, not set).
func TestDiffChunks_RepeatedText(t *testing.T) {
	old := []model.Chunk{{ID: "o1", ContentHash: "R"}, {ID: "o2", ContentHash: "R"}, {ID: "o3", ContentHash: "R"}}
	newChunks := []model.Chunk{{ID: "n1", ContentHash: "R"}, {ID: "n2", ContentHash: "R"}}
	deltas, _ := diffChunks(old, newChunks)
	c := countChanges(deltas)
	if c[events.ChangeUnchanged] != 2 || c[events.ChangeRemoved] != 1 || c[events.ChangeAdded] != 0 {
		t.Errorf("repeated 3->2: counts = %+v, want 2 UNCHANGED, 1 REMOVED, 0 ADDED", c)
	}
}

// TestDiffChunks_MovedParagraph: same text at a different position is UNCHANGED
// (content identity, not position).
func TestDiffChunks_MovedParagraph(t *testing.T) {
	old := []model.Chunk{{ID: "o1", ContentHash: "A"}, {ID: "o2", ContentHash: "B"}}
	newChunks := []model.Chunk{{ID: "n1", ContentHash: "B"}, {ID: "n2", ContentHash: "A"}}
	deltas, _ := diffChunks(old, newChunks)
	c := countChanges(deltas)
	if c[events.ChangeUnchanged] != 2 || c[events.ChangeAdded] != 0 || c[events.ChangeRemoved] != 0 {
		t.Errorf("moved: counts = %+v, want 2 UNCHANGED", c)
	}
}

// TestDiffChunks_EmptyHashIsAlwaysChanged: pre-v2 chunks (no ContentHash) never
// match — the safe degradation (always ADDED/REMOVED, no false UNCHANGED).
func TestDiffChunks_EmptyHashIsAlwaysChanged(t *testing.T) {
	old := []model.Chunk{{ID: "o1", ContentHash: ""}, {ID: "o2", ContentHash: ""}}
	newChunks := []model.Chunk{{ID: "n1", ContentHash: ""}, {ID: "n2", ContentHash: ""}}
	deltas, remap := diffChunks(old, newChunks)
	c := countChanges(deltas)
	if c[events.ChangeUnchanged] != 0 || c[events.ChangeAdded] != 2 || c[events.ChangeRemoved] != 2 {
		t.Errorf("empty-hash: counts = %+v, want 0 UNCHANGED, 2 ADDED, 2 REMOVED", c)
	}
	if len(remap) != 0 {
		t.Errorf("empty-hash remap = %v, want empty", remap)
	}
}

// BenchmarkDiffChunks (spec 043 / BL-010, T019): the multiset diff is the new
// ACK-path overhead. A 50-chunk doc with 10% changed should be sub-millisecond
// (well within the <10ms Constitution Principle IV budget). The PrefixEmbedding
// copy (preserveEmbeds) is N Pebble SetWithPrefix calls (~µs each) — also
// bounded; this benchmark isolates the pure-function diff (the CPU-bound part).
func BenchmarkDiffChunks(b *testing.B) {
	old := make([]model.Chunk, 50)
	newChunks := make([]model.Chunk, 50)
	for i := 0; i < 50; i++ {
		h := fmt.Sprintf("hash-%d", i)
		old[i] = model.Chunk{ID: fmt.Sprintf("o-%d", i), ContentHash: h}
		newChunks[i] = model.Chunk{ID: fmt.Sprintf("n-%d", i), ContentHash: h}
	}
	// Simulate a 10% edit: change 5 of 50 hashes.
	for i := 45; i < 50; i++ {
		newChunks[i].ContentHash = fmt.Sprintf("changed-%d", i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		diffChunks(old, newChunks)
	}
}
