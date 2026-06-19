package reader

import (
	"context"
	"strings"
)

// TextReader extracts plain text from .txt/.log/.csv files.
type TextReader struct{}

func (r *TextReader) Name() string                  { return "Text" }
func (r *TextReader) SupportedExtensions() []string { return []string{".txt", ".log", ".csv"} }
func (r *TextReader) SupportedMimeTypes() []string  { return []string{"text/plain"} }

func (r *TextReader) Read(_ context.Context, data []byte, _ string) (string, map[string]any, error) {
	s := string(data)
	md := map[string]any{
		"encoding":   "utf-8",
		"line_count": countLines(s),
		"char_count": len(s),
	}
	return s, md, nil
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
