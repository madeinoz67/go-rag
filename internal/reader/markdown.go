package reader

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
)

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
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimLeft(line, "#")
		if trim != line && strings.TrimSpace(trim) != "" {
			headings = append(headings, strings.TrimSpace(trim))
		}
	}
	if len(headings) > 0 {
		md["headings"] = headings
	}

	body, transcludes := normalizeObsidian(body)
	if len(transcludes) > 0 {
		md["transcludes"] = transcludes
	}
	md["format"] = "markdown"

	return stripMarkdown(body), md, nil
}

var (
	reObsidianEmbed = regexp.MustCompile(`!\[\[([^\]]+)\]\]`)
	reObsidianLink  = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
)

// normalizeObsidian resolves Obsidian embeds and wikilinks into plain tokens and
// collects note-transclusion targets. Embeds are handled before wikilinks so that
// "![[x]]" is not double-processed as "[[x]]".
func normalizeObsidian(s string) (string, []string) {
	var transcludes []string

	s = reObsidianEmbed.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSpace(m[3 : len(m)-2]) // strip "![[" and "]]"
		if isMediaEmbed(inner) {
			return inner // file embed: keep filename as a token, drop syntax
		}
		transcludes = append(transcludes, linkTarget(inner)) // note transclusion
		return linkDisplay(inner)
	})

	s = reObsidianLink.ReplaceAllStringFunc(s, func(m string) string {
		return linkDisplay(m[2 : len(m)-2]) // strip "[[" and "]]"
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

// stripMarkdown removes common markdown markers, leaving plain readable text.
func stripMarkdown(s string) string {
	var b strings.Builder
	inCode := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") {
			t = strings.TrimLeft(t, "#> ")
		}
		t = strings.ReplaceAll(t, "**", "")
		t = strings.ReplaceAll(t, "__", "")
		t = strings.ReplaceAll(t, "`", "")
		t = strings.ReplaceAll(t, "*", "")
		t = strings.ReplaceAll(t, "_", " ")
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}
