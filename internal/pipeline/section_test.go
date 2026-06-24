package pipeline

// section_test.go covers per-chunk section context (audit H23 / spec 025): the
// resolveBreadcrumb resolver (research R5) and its pipeline wiring (R2/R3/R8).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/reader"
	"github.com/madeinoz67/go-rag/internal/redact"
	"github.com/madeinoz67/go-rag/internal/storage"
)

func streq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestResolveBreadcrumb_Nesting: the breadcrumb is the full ancestor path
// (top-level → governing heading) for a chunk under each depth.
func TestResolveBreadcrumb_Nesting(t *testing.T) {
	spans := []reader.HeadingSpan{
		{Level: 1, Text: "Ops", Offset: 0},
		{Level: 2, Text: "Backups", Offset: 10},
		{Level: 3, Text: "Retention", Offset: 20},
	}
	cases := []struct {
		start int
		want  []string
	}{
		{0, []string{"Ops"}},
		{5, []string{"Ops"}}, // between H1 and H2, still under Ops
		{10, []string{"Ops", "Backups"}},
		{20, []string{"Ops", "Backups", "Retention"}},
	}
	for _, c := range cases {
		if got := resolveBreadcrumb(spans, c.start, nil); !streq(got, c.want) {
			t.Errorf("start=%d: got %v want %v", c.start, got, c.want)
		}
	}
}

// TestResolveBreadcrumb_SiblingReset: a new top-level heading resets the stack.
func TestResolveBreadcrumb_SiblingReset(t *testing.T) {
	spans := []reader.HeadingSpan{
		{Level: 1, Text: "A", Offset: 0},
		{Level: 2, Text: "B", Offset: 10},
		{Level: 1, Text: "C", Offset: 20},
	}
	if got := resolveBreadcrumb(spans, 25, nil); !streq(got, []string{"C"}) {
		t.Errorf("after sibling top-level: got %v want [C]", got)
	}
}

// TestResolveBreadcrumb_StraddleStartRule (FR-007): a chunk straddling a heading
// boundary carries the heading active at its START, not a later one it runs into.
func TestResolveBreadcrumb_StraddleStartRule(t *testing.T) {
	spans := []reader.HeadingSpan{
		{Level: 1, Text: "A", Offset: 0},
		{Level: 2, Text: "B", Offset: 50},
	}
	if got := resolveBreadcrumb(spans, 10, nil); !streq(got, []string{"A"}) {
		t.Errorf("straddling chunk should carry start heading A: got %v", got)
	}
}

// TestResolveBreadcrumb_PreambleAndEmpty: a chunk before the first heading, and
// the no-spans case, both yield nil (absent section context — FR-006).
func TestResolveBreadcrumb_PreambleAndEmpty(t *testing.T) {
	spans := []reader.HeadingSpan{{Level: 1, Text: "A", Offset: 20}}
	if got := resolveBreadcrumb(spans, 5, nil); got != nil {
		t.Errorf("preamble chunk should have nil context: got %v", got)
	}
	if got := resolveBreadcrumb(nil, 0, nil); got != nil {
		t.Errorf("no spans should give nil: got %v", got)
	}
}

// TestResolveBreadcrumb_RedactionOffsetAlignment (R3): a secret redacted before a
// heading shifts the heading's redacted-space offset; the resolver translates spans
// via the redaction edits so the heading still governs the right chunk starts.
func TestResolveBreadcrumb_RedactionOffsetAlignment(t *testing.T) {
	spans := []reader.HeadingSpan{{Level: 1, Text: "A", Offset: 10}}
	// 5-byte secret at offset 0 replaced by a 15-byte placeholder (net delta +10).
	edits := []redact.Edit{{Pos: 0, RemovedLen: 5, InsertedLen: 15}}

	// Without edits, heading A (offset 10) governs startIdx >= 10.
	if got := resolveBreadcrumb(spans, 10, nil); !streq(got, []string{"A"}) {
		t.Errorf("no edits, start=10: got %v want [A]", got)
	}
	// With edits, the heading's effective offset becomes 20 — startIdx 10 is now
	// preamble (the secret's expansion pushed the heading past it).
	if got := resolveBreadcrumb(spans, 10, edits); got != nil {
		t.Errorf("with edits, start=10 should be preamble (nil): got %v", got)
	}
	if got := resolveBreadcrumb(spans, 20, edits); !streq(got, []string{"A"}) {
		t.Errorf("with edits, start=20: got %v want [A]", got)
	}
}

// TestIngest_SectionContext_Attached (SC-001 / US2): a heading-bearing Markdown doc
// gets the governing breadcrumb on its chunk(s). This short doc fits one chunk that
// starts at offset 0 (under the top-level heading) per the FR-007 start-position rule.
func TestIngest_SectionContext_Attached(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ops.md"),
		"# Operations\n## Backups\nRetention keeps 30 days of incremental backups nightly.\n")
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	r, _ := p.Ingest(context.Background(), dir, "*")
	if r.New != 1 {
		t.Fatalf("want 1 new doc, got %+v", r)
	}

	var chunks []model.Chunk
	_ = p.db.PrefixScanByte(storage.PrefixChunk, func(_, v []byte) bool {
		var c model.Chunk
		if json.Unmarshal(v, &c) == nil {
			chunks = append(chunks, c)
		}
		return true
	})
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	if !streq(chunks[0].SectionContext, []string{"Operations"}) {
		t.Errorf("SectionContext=%v want [Operations]", chunks[0].SectionContext)
	}
	if chunks[0].TokenCount <= 0 { // FR-008 sanity: geometry still populated
		t.Errorf("token count not populated: %d", chunks[0].TokenCount)
	}
}

// TestIngest_SectionContext_IdempotentReAdd (FR-003 / US3-scenario-3): re-adding an
// unchanged heading-bearing doc is a no-op — the span table is removed before identity
// (docID stable) and content-hash dedup short-circuits unchanged files.
func TestIngest_SectionContext_IdempotentReAdd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ops.md"),
		"# Operations\n## Backups\nRetention keeps 30 days of incremental backups nightly.\n")
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	r1, _ := p.Ingest(context.Background(), dir, "*")
	if r1.New != 1 {
		t.Fatalf("first ingest: %+v", r1)
	}
	r2, _ := p.Ingest(context.Background(), dir, "*")
	if r2.New != 0 || r2.Skipped != 1 {
		t.Fatalf("re-add must be a no-op (skipped): got %+v", r2)
	}
	if n := p.CountDocuments(); n != 1 {
		t.Errorf("want 1 document after re-add, got %d", n)
	}
}

// TestIngest_SectionContext_HeadinglessAbsent (FR-006 / US3-1): heading-less
// documents ingest/query fine and their chunks carry NO section context (absent,
// never an error). The code-only Markdown also exercises FR-009 — a `#` inside the
// fence is not a heading, so no breadcrumb is synthesized.
func TestIngest_SectionContext_HeadinglessAbsent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plain.txt"),
		"a clean document about retrieval and ranking with enough words to chunk\n")
	writeFile(t, filepath.Join(dir, "code.md"), "```sh\necho hello world\n# not a heading inside a fence\n```\n")
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	r, _ := p.Ingest(context.Background(), dir, "*")
	if r.New != 2 {
		t.Fatalf("want 2 new docs, got %+v", r)
	}
	var saw int
	_ = p.db.PrefixScanByte(storage.PrefixChunk, func(_, v []byte) bool {
		var c model.Chunk
		if json.Unmarshal(v, &c) == nil {
			saw++
			if c.SectionContext != nil {
				t.Errorf("heading-less chunk %s has section context %v (want nil)", c.ID, c.SectionContext)
			}
		}
		return true
	})
	if saw == 0 {
		t.Fatal("no chunks stored for heading-less docs")
	}
}

// TestIngest_SectionContext_ReprocessBackfill (US3 / research R7): Reprocess
// re-reads the source and re-derives section context — there is no cheap rescan
// (the raw heading structure is not persisted), so back-fill goes through re-ingest.
func TestIngest_SectionContext_ReprocessBackfill(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ops.md"),
		"# Operations\n## Backups\nRetention keeps thirty days of incremental backups nightly.\n")
	p, cleanup := newTestPipeline(t, 0)
	defer cleanup()

	if _, err := p.Ingest(context.Background(), dir, "*"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Reprocess(context.Background(), dir, "*"); err != nil {
		t.Fatalf("reprocess: %v", err)
	}
	var got []string
	var count int
	_ = p.db.PrefixScanByte(storage.PrefixChunk, func(_, v []byte) bool {
		var c model.Chunk
		if json.Unmarshal(v, &c) == nil {
			count++
			got = c.SectionContext
		}
		return true
	})
	if count == 0 {
		t.Fatal("no chunks after reprocess")
	}
	if len(got) == 0 || got[0] != "Operations" {
		t.Errorf("reprocessed chunk breadcrumb = %v, want first element Operations", got)
	}
}
