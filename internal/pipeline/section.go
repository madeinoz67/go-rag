package pipeline

// section.go threads per-chunk section context (audit H23 / spec 025). It is the
// single correctness function: given the reader's positional heading spans and a
// chunk's start position (both in the chunker's coordinate space — spans are
// translated through redaction first), it returns the ordered heading breadcrumb
// active at that position. Research R5.

import (
	"sort"

	"github.com/madeinoz67/go-rag/internal/reader"
	"github.com/madeinoz67/go-rag/internal/redact"
)

// resolveBreadcrumb returns the ordered heading path (top-level → governing
// heading) active at startIdx, or nil when there are no spans or no heading
// governs the position (e.g. a chunk in the document preamble before the first
// heading).
//
// spans carry offsets in the reader's STRIPPED-text space; edits translate them
// into the REDACTED-text space the chunker indexes (research R3 — identity when
// redaction is off, the common case). startIdx is a Segment.StartCharIdx, i.e. an
// offset into that same redacted text.
//
// The breadcrumb is the heading-stack state at the last span whose (translated)
// offset ≤ startIdx: standard ancestor handling (push/pop by level), and a chunk
// that straddles a heading boundary carries the heading active at its START
// position (FR-007) — deterministic, not configurable per chunk.
func resolveBreadcrumb(spans []reader.HeadingSpan, startIdx int, edits []redact.Edit) ([]string, int) {
	if len(spans) == 0 {
		return nil, 0
	}
	type mark struct {
		off   int
		level int
		text  string
	}
	marks := make([]mark, 0, len(spans))
	for _, sp := range spans {
		marks = append(marks, mark{
			off:   redact.TranslateOffset(sp.Offset, edits),
			level: sp.Level,
			text:  sp.Text,
		})
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i].off < marks[j].off })

	// Replay headings up to the chunk's start position, maintaining the ancestor stack.
	var stack []mark
	for _, m := range marks {
		if m.off > startIdx {
			break
		}
		// Pop same-or-deeper levels (a new H2 replaces a prior H2/H3…), then push.
		for len(stack) > 0 && stack[len(stack)-1].level >= m.level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, m)
	}
	if len(stack) == 0 {
		return nil, 0
	}
	out := make([]string, len(stack))
	for i, m := range stack {
		out[i] = m.text
	}
	// Return the breadcrumb + the governing (leaf) heading's level (1-6), so
	// the chunk carries section_depth (spec 041 / BL-005). 0 when no heading
	// governs the position. NOTE: len(out) is NOT the level — a breadcrumb
	// [h1, h3] has length 2 but the governing heading is level 3.
	return out, stack[len(stack)-1].level
}

// resolveWikilinks returns the de-duplicated, first-occurrence-ordered canonical
// wikilink targets whose (redaction-translated) offset falls within the chunk's
// [startIdx, endIdx) range — i.e. the links physically located in this chunk's
// text (spec 036 / BL-004, research R3). wspans carry offsets in the reader's
// STRIPPED-text space, in document order; edits translate them into the
// REDACTED-text space the chunker indexes (identity when redaction is off, the
// common case) — the same translation resolveBreadcrumb applies to heading
// spans. Uses offset containment (a wikilink is a point in the text) rather than
// the heading stack; targets seen earlier in the chunk are dropped (de-dup,
// first-occurrence order). nil when no span falls in the chunk.
func resolveWikilinks(wspans []reader.WikilinkSpan, startIdx, endIdx int, edits []redact.Edit) []string {
	if len(wspans) == 0 {
		return nil
	}
	var out []string
	seen := make(map[string]struct{})
	for _, sp := range wspans {
		off := redact.TranslateOffset(sp.Offset, edits)
		if off < startIdx || off >= endIdx {
			continue
		}
		if _, dup := seen[sp.Target]; dup {
			continue
		}
		seen[sp.Target] = struct{}{}
		out = append(out, sp.Target)
	}
	return out
}
