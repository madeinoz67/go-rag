package muninn

import (
	"fmt"
	"strings"

	"github.com/madeinoz67/go-rag/internal/model"
)

// concept derives the engram concept label via a rule-based cascade (no LLM —
// research.md R4; LLM enrichment is deferred to MuninnDB's own enrich plugin).
// Priority: governing section heading → document title (metadata) → filename
// stem → first ~60 chars of content. A "[i/n]" position suffix is appended when
// the chunk is one of many (total_chunks > 1); single-chunk documents get none.
//
// The cascade reads fields go-rag already populates at ingest: SectionContext
// (spec 025), document metadata title (PDF/frontmatter), FileName, Content.
func concept(c model.Chunk, doc model.Document) string {
	if h := leafHeading(c); h != "" {
		return withPosition(h, c)
	}
	if t := docTitle(doc); t != "" {
		return withPosition(t, c)
	}
	if f := fileStem(doc.FileName); f != "" {
		return withPosition(f, c)
	}
	return withPosition(snippet(c.Content, 60), c)
}

// withPosition appends the "[i+1/n]" suffix for multi-chunk documents.
func withPosition(label string, c model.Chunk) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "untitled"
	}
	if c.TotalChunks <= 1 {
		return label
	}
	return fmt.Sprintf("%s [%d/%d]", label, c.ChunkIndex+1, c.TotalChunks)
}

// leafHeading returns the governing (nearest) section heading, cleaned of leading
// markdown "#" markers and whitespace. Empty when the chunk has no SectionContext.
func leafHeading(c model.Chunk) string {
	if len(c.SectionContext) == 0 {
		return ""
	}
	return cleanHeading(c.SectionContext[len(c.SectionContext)-1])
}

// cleanHeading strips leading "#" markers and surrounding whitespace.
func cleanHeading(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "#")
	return strings.TrimSpace(s)
}

// docTitle returns the document title from metadata (PDF info / frontmatter),
// or "" if absent or filename-like. A title that looks like a filename (has a dot
// extension and no spaces) is rejected — readers sometimes fall back to the path.
func docTitle(doc model.Document) string {
	v, ok := doc.Metadata["title"]
	if !ok {
		return ""
	}
	t, _ := v.(string)
	t = strings.TrimSpace(t)
	if t == "" || looksLikeFilename(t) {
		return ""
	}
	return t
}

// fileStem turns a filename into a readable label: strips the extension and
// replaces "-" / "_" with spaces. "auth-design.md" → "auth design".
func fileStem(name string) string {
	if name == "" {
		return ""
	}
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return name
}

// looksLikeFilename reports whether s has a dot extension and no spaces — a
// heuristic for "this 'title' is really a path/filename".
func looksLikeFilename(s string) bool {
	return strings.Contains(s, ".") && !strings.ContainsAny(s, " \t")
}

// snippet returns the first ~n chars of s trimmed to a word boundary with an
// ellipsis. Used as the concept fallback when no heading/title/filename applies.
func snippet(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := strings.LastIndexAny(s[:n], " \t\n")
	if cut <= 0 {
		cut = n
	}
	return s[:cut] + "…"
}
