package reader

import (
	"context"
	"strings"
)

// MarkdownReader extracts text from Markdown, parsing YAML frontmatter and
// collecting headings into metadata.
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
	md["format"] = "markdown"

	return stripMarkdown(body), md, nil
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
