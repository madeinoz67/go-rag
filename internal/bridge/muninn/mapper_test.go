package muninn

import (
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
)

func doc(fileType, fileName string, title string) model.Document {
	d := model.Document{FileName: fileName, FileType: fileType, Metadata: map[string]any{}}
	if title != "" {
		d.Metadata["title"] = title
	}
	return d
}

// TestMapper_Invariants pins the maintainer write invariants (research.md R4) that
// NFR-002 and MuninnDB's vector search both rest on. If any drifts the bridge
// silently breaks memory hygiene or retrieval.
func TestMapper_Invariants(t *testing.T) {
	m := Mapper{SourceVault: "default", TargetVault: "go-rag"}
	c := model.Chunk{ID: "abc123", Content: "tokens expire after 15m", ChunkIndex: 0, TotalChunks: 1}
	p := m.Map(c, doc("markdown", "auth.md", ""))

	if p.Embedding != nil {
		t.Errorf("Embedding must be nil (MuninnDB re-embeds); got %v", p.Embedding)
	}
	if p.Stability != 30.0 {
		t.Errorf("Stability = %v, want 30.0", p.Stability)
	}
	if p.IdempotentID != "chunk:abc123" {
		t.Errorf("IdempotentID = %q, want \"chunk:abc123\"", p.IdempotentID)
	}
	if !p.UpsertMode {
		t.Error("UpsertMode must be true")
	}
	if p.Vault != "go-rag" {
		t.Errorf("Vault = %q, want go-rag (target)", p.Vault)
	}
	if p.TypeLabel != "go-rag-chunk" {
		t.Errorf("TypeLabel = %q", p.TypeLabel)
	}
	wantTags := []string{"go-rag", "default", "markdown"}
	if len(p.Tags) != len(wantTags) || p.Tags[0] != wantTags[0] || p.Tags[1] != wantTags[1] || p.Tags[2] != wantTags[2] {
		t.Errorf("Tags = %v, want %v (no low-quality; extraction_quality unset)", p.Tags, wantTags)
	}
}

// TestMapper_LowQualityTag confirms the spec-042 marker fires for sparse PDF
// extraction (quality < 0.5) so MuninnDB-side filtering can down-rank it.
func TestMapper_LowQualityTag(t *testing.T) {
	m := Mapper{SourceVault: "v", TargetVault: "go-rag"}
	c := model.Chunk{ID: "x", Content: "c", ExtractionQuality: 0.2}
	p := m.Map(c, doc("pdf", "scan.pdf", ""))
	found := false
	for _, tg := range p.Tags {
		if tg == "low-quality" {
			found = true
		}
	}
	if !found {
		t.Fatalf("low-quality tag missing for quality=0.2; tags=%v", p.Tags)
	}

	// quality >= 0.5 (or unset) does NOT add the marker.
	c2 := model.Chunk{ID: "y", Content: "c", ExtractionQuality: 0.9}
	if hasLow(m.Map(c2, doc("pdf", "ok.pdf", ""))) {
		t.Fatal("low-quality tag should NOT fire for quality=0.9")
	}
}

func hasLow(p WriteParams) bool {
	for _, tg := range p.Tags {
		if tg == "low-quality" {
			return true
		}
	}
	return false
}

// TestConcept_Cascade covers the four-priority label cascade + position suffix.
func TestConcept_Cascade(t *testing.T) {
	// Priority 1: section heading wins; single-chunk ⇒ no suffix.
	c := model.Chunk{Content: "body", SectionContext: []string{"Ops", "Backups", "Retention"}, TotalChunks: 1}
	if got := concept(c, doc("markdown", "x.md", "")); got != "Retention" {
		t.Errorf("heading cascade: got %q, want Retention", got)
	}

	// Priority 1, multi-chunk: heading + [i/n] suffix.
	c.TotalChunks = 4
	c.ChunkIndex = 2
	if got := concept(c, doc("markdown", "x.md", "")); got != "Retention [3/4]" {
		t.Errorf("heading+suffix: got %q, want \"Retention [3/4]\"", got)
	}

	// Priority 2: document title (metadata), when no heading.
	c2 := model.Chunk{Content: "body", TotalChunks: 1}
	if got := concept(c2, doc("pdf", "x.pdf", "JWT Architecture")); got != "JWT Architecture" {
		t.Errorf("title cascade: got %q", got)
	}
	// A filename-like "title" is rejected → falls through.
	if got := concept(c2, doc("pdf", "real.pdf", "auth-design.md")); got != "real" {
		t.Errorf("filename-like title should fall through to fileStem; got %q, want \"real\"", got)
	}

	// Priority 3: filename stem (no heading, no title).
	c3 := model.Chunk{Content: "body", ChunkIndex: 0, TotalChunks: 2}
	if got := concept(c3, doc("markdown", "auth-design.md", "")); got != "auth design [1/2]" {
		t.Errorf("fileStem cascade: got %q, want \"auth design [1/2]\"", got)
	}

	// Priority 4: content snippet fallback (no heading, no title, no filename).
	c4 := model.Chunk{Content: "Authentication tokens expire after fifteen minutes of inactivity generally", TotalChunks: 1}
	if got := concept(c4, doc("text", "", "")); got != "Authentication tokens expire after fifteen minutes of…" {
		t.Errorf("snippet cascade: got %q", got)
	}
}
