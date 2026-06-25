package engine

import (
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
)

// TestTagsForDoc_Bridge (spec 029, US1 / SC-001, T010): the filter bridge merges
// manual Metadata["tags"] with auto-generated Enrichment.Tags, so --tags
// consumes auto-tags with no new query field. Both sources are optional.
func TestTagsForDoc_Bridge(t *testing.T) {
	manual := map[string]any{"tags": []string{"alpha"}}
	auto := &model.EnrichInfo{Tags: []string{"beta"}}

	// Both present → merged.
	got := tagsForDoc(manual, auto)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("merge: got %v, want [alpha beta]", got)
	}
	// Auto only.
	got = tagsForDoc(nil, auto)
	if len(got) != 1 || got[0] != "beta" {
		t.Errorf("auto-only: got %v, want [beta]", got)
	}
	// Manual only (nil enrichment — pre-feature / off doc).
	got = tagsForDoc(manual, nil)
	if len(got) != 1 || got[0] != "alpha" {
		t.Errorf("manual-only: got %v, want [alpha]", got)
	}
	// Neither.
	if got := tagsForDoc(nil, nil); len(got) != 0 {
		t.Errorf("neither: got %v, want []", got)
	}
}
