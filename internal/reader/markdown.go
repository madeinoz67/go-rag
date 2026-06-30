package reader

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
)

// HeadingSpan is one in-body Markdown heading with its position in the text the
// chunker receives. Offset is a byte index into the STRIPPED text returned by
// the reader; the pipeline translates it into redacted-text space (research R3)
// before resolving the per-chunk breadcrumb. Produced by stripMarkdownSpans —
// the unified code-fence-aware scan (research R1/R4). Transient: the pipeline
// consumes it during chunking and removes it from document metadata before
// identity/store, so it is never persisted.
type HeadingSpan struct {
	Level  int    // 1..6 (#..######)
	Text   string // heading text, emphasis-stripped and trimmed
	Offset int    // byte offset into the reader's returned (stripped) text
}

// WikilinkSpan is one Obsidian [[wikilink]] in the stripped text the chunker
// receives (spec 036 / BL-004). Target is canonicalised by linkTarget (alias
// and anchor stripped; path preserved). Offset is the byte index of the link's
// opening "[" into the reader's returned (stripped) text — the same coordinate
// space the chunker and HeadingSpan use; the pipeline translates it through
// redaction before resolving the per-chunk set. Transient: produced by the
// reader, consumed by the pipeline to assign per-chunk Wikilinks, and removed
// from document metadata before identity/store (the HeadingSpan precedent), so
// it is never persisted.
type WikilinkSpan struct {
	Target string
	Offset int
}

// MarkdownReader extracts text from Markdown, parsing YAML frontmatter,
// collecting headings into metadata, and normalizing Obsidian syntax:
//
//   - ![[file.ext]]  image/file embed  -> the filename kept as a searchable token
//     (brackets dropped; the binary is indexed by its own reader, not inlined)
//   - ![[Note]]      note transclusion  -> the target name kept as a token AND
//     recorded in metadata["transcludes"] (relationship captured, not inlined)
//   - [[Note]]       wikilink           -> the link's display text (alias/heading aware)
type MarkdownReader struct{}

func (r *MarkdownReader) Name() string                  { return "Markdown" }
func (r *MarkdownReader) SupportedExtensions() []string { return []string{".md", ".markdown"} }
func (r *MarkdownReader) SupportedMimeTypes() []string  { return []string{"text/markdown"} }

func (r *MarkdownReader) Read(_ context.Context, data []byte, _ string) (string, map[string]any, error) {
	src := string(data)
	md := map[string]any{}
	body := src

	if fm, rest, ok := extractFrontmatter(src); ok {
		body = rest
		for k, v := range fm {
			md[k] = v
		}
	}

	var headings []string
	body, transcludes := normalizeObsidian(body)
	if len(transcludes) > 0 {
		md["transcludes"] = transcludes
	}
	md["format"] = "markdown"

	// Unified code-fence-aware scan (audit H23 / spec 025, research R1/R4):
	// produces the plain stripped text (unchanged from legacy stripMarkdown) AND
	// a positional heading-span table the pipeline threads into per-chunk section
	// context. Code-fence state is tracked once, so a `# comment` or `#!/bin/sh`
	// inside a fenced block is neither collected as a heading nor mis-offset
	// (FR-009) — the legacy flat-heading loop and stripMarkdown disagreed about
	// code fences; this unifies them. Heading offsets index into the returned
	// text, so they share the chunker's coordinate space (the pipeline translates
	// them through redaction — research R3).
	stripped, hspans, wspans := stripMarkdownSpans(body)
	if len(hspans) > 0 {
		headings = make([]string, len(hspans))
		for i, sp := range hspans {
			headings[i] = sp.Text
		}
		md["headings"] = headings    // backward-compatible flat list
		md["heading_spans"] = hspans // positional; consumed + dropped by the pipeline
	}
	if len(wspans) > 0 {
		md["wikilink_spans"] = wspans // spec 036 / BL-004: positional; consumed + dropped by the pipeline
	}
	return stripped, md, nil
}

var (
	reObsidianEmbed = regexp.MustCompile(`!\[\[([^\]]+)\]\]`)
	reObsidianLink  = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
)

// normalizeObsidian resolves Obsidian embeds and wikilinks into plain tokens and
// collects note-transclusion targets. Embeds are handled before wikilinks so that
// "![[x]]" is not double-processed as "[[x]]".
// normalizeObsidian resolves Obsidian note/file embeds (![[…]]) into plain tokens
// and collects note-transclusion targets. Plain [[wikilink]] syntax is NOT
// substituted here: it is resolved later by stripMarkdownSpans, which must
// record each link's offset in stripped-text space (spec 036 / BL-004) so the
// pipeline can attribute links to chunks through the same redaction translation
// HeadingSpan uses. Embeds are handled first so "![[x]]" is not later read as a
// plain "[[x]]" wikilink.
func normalizeObsidian(s string) (string, []string) {
	var transcludes []string

	s = reObsidianEmbed.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSpace(m[3 : len(m)-2]) // strip "![[ " and "]]"
		if isMediaEmbed(inner) {
			return inner // file embed: keep filename as a token, drop syntax
		}
		transcludes = append(transcludes, linkTarget(inner)) // note transclusion
		return linkDisplay(inner)
	})

	return s, transcludes
}

// linkDisplay returns the human-readable text of a wikilink inner: the alias if
// present ([[target|Display]]), else the target; a heading ref ([[target#Sec]])
// yields the target name.
func linkDisplay(inner string) string {
	inner = strings.TrimSpace(inner)
	if i := strings.Index(inner, "|"); i >= 0 {
		return strings.TrimSpace(inner[i+1:])
	}
	if i := strings.Index(inner, "#"); i >= 0 {
		return strings.TrimSpace(inner[:i])
	}
	return inner
}

// linkTarget returns the canonical target of a link inner (strips alias and heading).
func linkTarget(inner string) string {
	inner = strings.TrimSpace(inner)
	if i := strings.Index(inner, "|"); i >= 0 {
		inner = inner[:i]
	}
	if i := strings.Index(inner, "#"); i >= 0 {
		inner = inner[:i]
	}
	return strings.TrimSpace(inner)
}

// isMediaEmbed reports whether an embed target is a binary file (image/pdf) rather
// than a note transclusion. Note extensions (.md/.txt) are treated as transclusions.
func isMediaEmbed(inner string) bool {
	switch strings.ToLower(filepath.Ext(inner)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".svg", ".webp", ".pdf":
		return true
	}
	return false
}

// extractFrontmatter parses a leading "---\n...\n---\n" block into key/value pairs.
func extractFrontmatter(src string) (map[string]any, string, bool) {
	if !strings.HasPrefix(src, "---\n") {
		return nil, src, false
	}
	end := strings.Index(src[4:], "\n---\n")
	if end < 0 {
		return nil, src, false
	}
	block := src[4 : 4+end]
	rest := src[4+end+len("\n---\n"):]
	fm := map[string]any{}
	for _, line := range strings.Split(block, "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		fm[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return fm, rest, true
}

// stripInlineEmphasis removes inline markdown emphasis/code markers from a line
// fragment. Order matters: `**`/`__` before the single-char forms, and `_`
// becomes a space (matching the legacy stripMarkdown behaviour exactly).
func stripInlineEmphasis(t string) string {
	t = strings.ReplaceAll(t, "**", "")
	t = strings.ReplaceAll(t, "__", "")
	t = strings.ReplaceAll(t, "`", "")
	t = strings.ReplaceAll(t, "*", "")
	t = strings.ReplaceAll(t, "_", " ")
	return t
}

// stripMarkdownSpans is the unified, code-fence-aware scan (audit H23 / spec
// 025, research R1/R4). It produces the plain stripped text — byte-identical to
// legacy stripMarkdown — AND a positional table of in-body Markdown headings,
// each with its byte Offset into the returned stripped text. Fenced-code state
// is tracked once so a `# comment` inside a code block is not mistaken for a
// heading (FR-009) and heading offsets align with the chunker's coordinate
// space. The span Text is the emphasis-stripped heading text (what the
// breadcrumb shows); the offset points at where that text is written.
// stripMarkdownSpans is the unified, code-fence-aware scan (audit H23 / spec
// 025, research R1/R4). It produces the plain stripped text — byte-identical to
// legacy stripMarkdown — a positional table of in-body Markdown headings, and a
// positional table of Obsidian [[wikilink]] spans (spec 036 / BL-004), each with
// its byte Offset into the returned stripped text. Fenced-code state is tracked
// once so a `# comment` inside a code block is not mistaken for a heading
// (FR-009) and heading/wikilink offsets align with the chunker's coordinate
// space. Wikilinks inside fenced code are substituted to display text (for
// byte-identity) but NOT recorded — code is literal, not graph edges (FR-014);
// wikilinks in body/heading text are recorded with targets canonicalised by
// linkTarget (alias/anchor stripped, path preserved).
func stripMarkdownSpans(s string) (string, []HeadingSpan, []WikilinkSpan) {
	var b strings.Builder
	var hspans []HeadingSpan
	var wspans []WikilinkSpan
	inCode := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			// Fenced code: substitute wikilinks to display (byte-identity with the
			// pre-feature body-wide normalisation) but do NOT record spans (FR-014).
			b.WriteString(substituteWikilinks(line))
			b.WriteByte('\n')
			continue
		}
		// Heading: starts with '#', outside any code fence, with non-empty text.
		if strings.HasPrefix(t, "#") {
			level := 0
			for level < len(t) && t[level] == '#' {
				level++
			}
			if rest := strings.TrimSpace(t[level:]); rest != "" {
				stripped, w := stripLineSpans(rest, b.Len())
				hspans = append(hspans, HeadingSpan{
					Level:  level,
					Text:   stripped,
					Offset: b.Len(), // where the heading text lands in the output
				})
				wspans = append(wspans, w...)
				b.WriteString(stripped)
				b.WriteByte('\n')
				continue
			}
		}
		// Non-heading line (lone '#', blockquote, or body): legacy transform +
		// wikilink substitution. Trim leading markers first (matches legacy).
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") {
			t = strings.TrimLeft(t, "#> ")
		}
		stripped, w := stripLineSpans(t, b.Len())
		wspans = append(wspans, w...)
		b.WriteString(stripped)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String()), hspans, wspans
}

// substituteWikilinks replaces every [[wikilink]] in s with its display text
// (linkDisplay), matching the pre-feature reader's body-wide normalisation. Used
// for fenced-code lines, where links are substituted for byte-identity but NOT
// recorded as spans (code is literal, not graph edges — FR-014).
func substituteWikilinks(s string) string {
	return reObsidianLink.ReplaceAllStringFunc(s, func(m string) string {
		return linkDisplay(m[2 : len(m)-2]) // strip "[[" and "]]"
	})
}

// stripLineSpans processes one non-code line: it drops inline emphasis
// (identical to stripInlineEmphasis) and substitutes [[wikilink]] syntax with
// its (emphasis-stripped) display text, recording each link's canonical target
// and the byte offset of its display text in the returned stripped line.
// baseOffset is the byte offset in the document's stripped output where this
// line begins; WikilinkSpan.Offset is in that output space — the same coordinate
// space HeadingSpan uses, which the pipeline translates through redaction.
// Targets are extracted from the raw inner via linkTarget (before emphasis
// stripping) to match the reader's canonicaliser; the display text is
// emphasis-stripped so the stripped output stays byte-identical to the
// pre-feature reader. Targets that canonicalise to empty (e.g. [[]], [[|a]]) are
// not recorded. De-duplication is deferred to the pipeline (per-chunk).
func stripLineSpans(s string, baseOffset int) (string, []WikilinkSpan) {
	var b strings.Builder
	var spans []WikilinkSpan
	last := 0
	for _, m := range reObsidianLink.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[0], m[1]
		innerStart, innerEnd := m[2], m[3]
		// Emphasis on the text before this link (piece-wise == whole-line).
		b.WriteString(stripInlineEmphasis(s[last:start]))
		inner := s[innerStart:innerEnd]
		if target := linkTarget(inner); target != "" {
			spans = append(spans, WikilinkSpan{Target: target, Offset: baseOffset + b.Len()})
		}
		// Emphasis-strip the display text too (byte-identity with pre-feature,
		// which emphasis-stripped the line after substituting the link).
		b.WriteString(stripInlineEmphasis(linkDisplay(inner)))
		last = end
	}
	b.WriteString(stripInlineEmphasis(s[last:]))
	return b.String(), spans
}
