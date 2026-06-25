package reader

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
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
	body, spans := extractDocxBody(text)

	md := map[string]any{"format": "docx"}
	if core, err := readZipFile(zr, "docProps/core.xml"); err == nil {
		if title := extractXMLElementText(core, "title"); strings.TrimSpace(title) != "" {
			md["title"] = strings.TrimSpace(title)
		}
		if creator := extractXMLElementText(core, "creator"); strings.TrimSpace(creator) != "" {
			md["author"] = strings.TrimSpace(creator)
		}
	}
	if len(spans) > 0 {
		md["heading_spans"] = spans // spec 031 US2 — non-identity sidecar (pipeline strips before identity)
	}
	return body, md, nil
}

// extractDocxBody (spec 031 US2, research R3) walks document.xml paragraph by
// paragraph, producing the extracted text (paragraphs joined by newline) AND a
// positional heading-span table. A paragraph's <w:pStyle w:val="HeadingN"/>
// (or Title/Subtitle) marks it as a heading; the span Offset is the byte position
// where the heading text lands in the returned content. Paragraph-aware (unlike
// the old flat <w:t> concatenation) so headings carry their section boundaries.
func extractDocxBody(xmlBytes []byte) (string, []HeadingSpan) {
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	var content, paraText strings.Builder
	var spans []HeadingSpan
	inPara := false
	inT := false
	headingLevel := 0
	flush := func() {
		text := strings.TrimSpace(paraText.String())
		if text == "" {
			paraText.Reset()
			headingLevel = 0
			return
		}
		if content.Len() > 0 {
			content.WriteByte('\n')
		}
		if headingLevel > 0 {
			spans = append(spans, HeadingSpan{Level: headingLevel, Text: text, Offset: content.Len()})
		}
		content.WriteString(text)
		paraText.Reset()
		headingLevel = 0
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inPara = true
				headingLevel = 0
				paraText.Reset()
			case "pStyle":
				if inPara {
					for _, a := range t.Attr {
						if a.Name.Local == "val" {
							headingLevel = docxHeadingLevel(a.Value)
						}
					}
				}
			case "t":
				inT = true
			case "br", "tab":
				paraText.WriteByte(' ')
			}
		case xml.CharData:
			if inT {
				paraText.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "p":
				if inPara {
					flush()
				}
				inPara = false
			}
		}
	}
	return strings.TrimSpace(content.String()), spans
}

// docxHeadingLevel maps a Word pStyle val to a 1-based heading level (0 = not a
// heading). Recognises Heading1..9, Title (->1) and Subtitle (->2); anything else
// (body text, custom styles) yields 0.
func docxHeadingLevel(val string) int {
	val = strings.TrimSpace(val)
	if val == "" {
		return 0
	}
	low := strings.ToLower(val)
	switch low {
	case "title":
		return 1
	case "subtitle":
		return 2
	}
	const p = "heading"
	if strings.HasPrefix(low, p) {
		if n, err := strconv.Atoi(strings.TrimSpace(low[len(p):])); err == nil && n >= 1 && n <= 9 {
			return n
		}
	}
	return 0
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
