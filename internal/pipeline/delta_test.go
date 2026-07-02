package pipeline

import (
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
