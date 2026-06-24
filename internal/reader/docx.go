package reader

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// DocxReader extracts text and doc-props metadata from Word .docx files (a ZIP of
// XML parts). Text is gathered from every <w:t> run; metadata from core.xml.
type DocxReader struct{}

func (r *DocxReader) Name() string                  { return "Word" }
func (r *DocxReader) SupportedExtensions() []string { return []string{".docx"} }
func (r *DocxReader) SupportedMimeTypes() []string {
	return []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
}

func (r *DocxReader) Read(_ context.Context, data []byte, _ string) (string, map[string]any, error) {
	zr, err := zipReader(data)
	if err != nil {
		return "", nil, fmt.Errorf("open docx: %w", err)
	}
	text, err := readZipFile(zr, "word/document.xml")
	if err != nil {
		return "", nil, fmt.Errorf("read document.xml: %w", err)
	}
	body := extractXMLElementText(text, "t")

	md := map[string]any{"format": "docx"}
	if core, err := readZipFile(zr, "docProps/core.xml"); err == nil {
		if title := extractXMLElementText(core, "title"); strings.TrimSpace(title) != "" {
			md["title"] = strings.TrimSpace(title)
		}
		if creator := extractXMLElementText(core, "creator"); strings.TrimSpace(creator) != "" {
			md["author"] = strings.TrimSpace(creator)
		}
	}
	return strings.TrimSpace(body), md, nil
}

// extractXMLElementText concatenates the character data inside every element whose
// local name matches (namespace-agnostic, robust across OOXML namespaces).
func extractXMLElementText(xmlBytes []byte, local string) string {
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	var b strings.Builder
	in := false
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return strings.TrimSpace(b.String())
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == local {
				in = true
			}
		case xml.CharData:
			if in {
				b.Write(t)
				b.WriteRune(' ')
			}
		case xml.EndElement:
			if t.Name.Local == local {
				in = false
			}
		}
	}
	return strings.TrimSpace(strings.TrimFunc(b.String(), unicode.IsSpace))
}
