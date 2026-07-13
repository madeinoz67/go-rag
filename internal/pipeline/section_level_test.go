package pipeline

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// TestIngest_SectionLevel_Attached (spec 041 / BL-005): the chunk carries the
// governing heading's LEVEL (1-6) — section_depth — alongside the breadcrumb.
// Covers under-h1 → 1, under-h2 → 2, and a heading-less preamble → 0. The
// leaf-level resolution is proven in section_test.go (TestResolveBreadcrumb_*);
// this pins the pipeline threading (resolveBreadcrumb → model.Chunk.SectionLevel).
func TestIngest_SectionLevel_Attached(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want int
	}{
		{"h1", "# Title\nbody text under the top heading goes right here\n", 1},
		{"h2", "## Section Two\nbody text under a second level heading here\n", 2},
		{"preamble", "no headings just plain text in this document body\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "d.md"), c.md)
			p, cleanup := newTestPipeline(t, 0)
			defer cleanup()
			ws := wsOf(p)

			r, _ := p.Ingest(context.Background(), ws, dir, "*")
			if r.New != 1 {
				t.Fatalf("want 1 new doc, got %+v", r)
			}
			var lvl int
			scanVaultKind(t, p.db, storage.PrefixChunk, ws, func(_ []byte, v []byte) bool {
				var ch model.Chunk
				if json.Unmarshal(v, &ch) == nil {
					lvl = ch.SectionLevel
				}
				return true
			})
			if lvl != c.want {
				t.Errorf("SectionLevel = %d, want %d", lvl, c.want)
			}
		})
	}
}
