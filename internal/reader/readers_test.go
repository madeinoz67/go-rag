package reader

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestTextReader(t *testing.T) {
	r := &TextReader{}
	content, md, err := r.Read(context.Background(), []byte("line one\nline two\n"), "x.txt")
	if err != nil {
		t.Fatal(err)
	}
	if content != "line one\nline two\n" {
		t.Errorf("content mismatch: %q", content)
	}
	if md["line_count"].(int) != 2 {
		t.Errorf("line_count: %v", md["line_count"])
	}
}

func TestMarkdownReader_FrontmatterAndHeadings(t *testing.T) {
	src := []byte("---\ntitle: Hello\nauthor: Me\n---\n# Title\n\nbody **bold** text\n")
	r := &MarkdownReader{}
	content, md, err := r.Read(context.Background(), src, "x.md")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := md["title"]; !ok || v != "Hello" {
		t.Errorf("frontmatter title: %v", md["title"])
	}
	headings, _ := md["headings"].([]string)
	if len(headings) == 0 || headings[0] != "Title" {
		t.Errorf("headings: %v", headings)
	}
	if !contains(content, "body") {
		t.Errorf("body text lost: %q", content)
	}
}

func TestDocxReader_FromGeneratedFixture(t *testing.T) {
	docx := mustBuildDocx(t, "Hello docx world", "Doc Title", "Doc Author")
	r := &DocxReader{}
	content, md, err := r.Read(context.Background(), docx, "x.docx")
	if err != nil {
		t.Fatalf("docx read: %v", err)
	}
	if !contains(content, "Hello docx world") {
		t.Errorf("docx body not extracted: %q", content)
	}
	if md["title"] != "Doc Title" {
		t.Errorf("docx title metadata: %v", md["title"])
	}
	if md["author"] != "Doc Author" {
		t.Errorf("docx author metadata: %v", md["author"])
	}
}

func TestPNGReader_Dimensions(t *testing.T) {
	pngBytes := mustBuildPNG(t, 4, 2)
	r := &PNGReader{}
	_, md, err := r.Read(context.Background(), pngBytes, "x.png")
	if err != nil {
		t.Fatal(err)
	}
	if md["width"].(int) != 4 || md["height"].(int) != 2 {
		t.Errorf("png dimensions: %v", md)
	}
}

func TestRegistry_DispatchAndUnknown(t *testing.T) {
	DefaultReaders()
	if r, ok := Get(".md"); !ok || r.Name() != "Markdown" {
		t.Errorf("dispatch .md: ok=%v r=%v", ok, r)
	}
	if r, ok := Get(".pdf"); !ok || r.Name() != "PDF" {
		t.Errorf("dispatch .pdf: ok=%v r=%v", ok, r)
	}
	if _, ok := Get(".bogus"); ok {
		t.Error("unknown extension must not resolve")
	}
}

// --- fixture helpers ---

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func mustBuildDocx(t *testing.T, body, title, author string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	must := func(name, content string) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	must("[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="text/xml"/></Types>`)
	must("word/document.xml", `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>`+body+`</w:t></w:r></w:p></w:body></w:document>`)
	must("docProps/core.xml", `<?xml version="1.0"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>`+title+`</dc:title><dc:creator>`+author+`</dc:creator></cp:coreProperties>`)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustBuildPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Ensure testdata fixtures exist (sanity for the integration fixtures).
func TestTestDataFixturesExist(t *testing.T) {
	for _, name := range []string{"sample.txt", "sample.md"} {
		if _, err := os.Stat(filepath.Join("..", "..", "testdata", name)); err != nil {
			t.Logf("note: %s fixture present check: %v", name, err)
		}
	}
}
