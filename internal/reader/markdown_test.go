package reader

import (
	"context"
	"strings"
	"testing"
)

func TestMarkdownReader_ObsidianEmbeds(t *testing.T) {
	src := []byte("See the diagram ![[Honeypot Deception 20220819112648.png]] for detail.\n")
	r := &MarkdownReader{}
	content, _, err := r.Read(context.Background(), src, "x.md")
	if err != nil {
		t.Fatal(err)
	}
	// brackets dropped, filename kept as a token
	if !contains(content, "Honeypot Deception 20220819112648.png") {
		t.Errorf("image filename should be kept as a token: %q", content)
	}
	if contains(content, "![[") || contains(content, "]]") {
		t.Errorf("embed syntax should be stripped: %q", content)
	}
}

func TestMarkdownReader_ObsidianWikilinks(t *testing.T) {
	cases := map[string]string{
		"plain link [[Intrusion Detection Honeypots]] end": "Intrusion Detection Honeypots",
		"alias [[IDH|Honeypots book]] end":                 "Honeypots book",
		"heading [[Notes#Architecture]] end":               "Notes",
	}
	r := &MarkdownReader{}
	for in, want := range cases {
		content, _, err := r.Read(context.Background(), []byte(in), "x.md")
		if err != nil {
			t.Fatal(err)
		}
		if !contains(content, want) {
			t.Errorf("wikilink text mismatch: want %q in %q", want, content)
		}
		if contains(content, "[[") || contains(content, "]]") {
			t.Errorf("wikilink brackets should be stripped: %q", content)
		}
	}
}

func TestMarkdownReader_ObsidianTransclusion(t *testing.T) {
	// A note embed (![[Note]] with no media extension) is a transclusion: the
	// target is recorded in metadata["transcludes"], not inlined.
	src := []byte("Intro.\n![[Threat Models]]\nMore text.\n![[diagram.png]]\n")
	r := &MarkdownReader{}
	content, md, err := r.Read(context.Background(), src, "x.md")
	if err != nil {
		t.Fatal(err)
	}
	transcludes, _ := md["transcludes"].([]string)
	found := false
	for _, tr := range transcludes {
		if tr == "Threat Models" {
			found = true
		}
	}
	if !found {
		t.Errorf("note transclusion should be recorded; got transcludes=%v", transcludes)
	}
	// image embed is NOT a transclusion
	for _, tr := range transcludes {
		if tr == "diagram.png" {
			t.Errorf("image embed must not be a transclusion: %v", transcludes)
		}
	}
	// the transclusion target name is still kept as a searchable token
	if !contains(content, "Threat Models") {
		t.Errorf("transclusion target should remain as a token: %q", content)
	}
}

// TestStripMarkdownSpans_HeadingsAndOffsets checks the unified scan records each
// heading's level, emphasis-stripped text, and an offset that points at where the
// heading text lands in the stripped output (spec 025/H23, research R1/R5).
func TestStripMarkdownSpans_HeadingsAndOffsets(t *testing.T) {
	in := "# Top\nintro\n## Mid\ntext"
	out, spans := stripMarkdownSpans(in)
	if !strings.Contains(out, "Top") || !strings.Contains(out, "Mid") {
		t.Fatalf("stripped text missing heading text: %q", out)
	}
	if len(spans) != 2 {
		t.Fatalf("want 2 spans, got %d: %+v", len(spans), spans)
	}
	if spans[0].Level != 1 || spans[0].Text != "Top" {
		t.Errorf("span0 = %+v, want Level=1 Text=Top", spans[0])
	}
	if spans[1].Level != 2 || spans[1].Text != "Mid" {
		t.Errorf("span1 = %+v, want Level=2 Text=Mid", spans[1])
	}
	// Offset must point at the heading text in the stripped output the chunker sees.
	if spans[0].Offset != strings.Index(out, "Top") {
		t.Errorf("span0 Offset=%d want %d", spans[0].Offset, strings.Index(out, "Top"))
	}
	if spans[1].Offset != strings.Index(out, "Mid") {
		t.Errorf("span1 Offset=%d want %d", spans[1].Offset, strings.Index(out, "Mid"))
	}
}

// TestStripMarkdownSpans_CodeFenceHashExcluded enforces FR-009: `#` lines inside
// a fenced code block (comments, shebangs) are NOT treated as headings. The legacy
// flat-heading loop ignored code fences; the unified scan tracks them.
func TestStripMarkdownSpans_CodeFenceHashExcluded(t *testing.T) {
	in := "# Real Heading\n\n```sh\n#!/bin/sh\n# a comment\necho hi\n```\n\n## After\n"
	_, spans := stripMarkdownSpans(in)
	if len(spans) != 2 {
		t.Fatalf("want exactly 2 headings, got %d: %+v", len(spans), spans)
	}
	for _, sp := range spans {
		if sp.Text == "bin/sh" || sp.Text == "#/bin/sh" || sp.Text == "a comment" || sp.Text == "!/bin/sh" {
			t.Errorf("code-fence line mistaken for heading: %+v", sp)
		}
	}
	if spans[0].Text != "Real Heading" || spans[1].Text != "After" {
		t.Errorf("headings = %+v, want Real Heading then After", spans)
	}
}

// TestStripMarkdownSpans_Nesting verifies H1..H3 levels are captured in order.
func TestStripMarkdownSpans_Nesting(t *testing.T) {
	in := "# A\n## B\n### C\nbody"
	_, spans := stripMarkdownSpans(in)
	want := []struct {
		level int
		text  string
	}{{1, "A"}, {2, "B"}, {3, "C"}}
	if len(spans) != len(want) {
		t.Fatalf("want %d spans, got %d: %+v", len(want), len(spans), spans)
	}
	for i, w := range want {
		if spans[i].Level != w.level || spans[i].Text != w.text {
			t.Errorf("span%d = %+v, want level=%d text=%q", i, spans[i], w.level, w.text)
		}
	}
}

// TestStripMarkdownSpans_NoHeadings: a heading-less body yields no spans and is
// returned unchanged (FR-006 graceful — section context will be absent).
func TestStripMarkdownSpans_NoHeadings(t *testing.T) {
	in := "just some plain text\nno headings here"
	out, spans := stripMarkdownSpans(in)
	if len(spans) != 0 {
		t.Errorf("want no spans for heading-less body, got %+v", spans)
	}
	if out != in {
		t.Errorf("plain text changed: got %q want %q", out, in)
	}
}

// TestMarkdownReader_FrontmatterTitleNotHeading: a YAML front-matter `title` is
// document metadata, not a section heading — only in-body Markdown headings
// contribute to section context (spec edge case).
func TestMarkdownReader_FrontmatterTitleNotHeading(t *testing.T) {
	r := &MarkdownReader{}
	src := []byte("---\ntitle: My Doc\n---\n# Real\nbody\n")
	_, md, err := r.Read(context.Background(), src, "x.md")
	if err != nil {
		t.Fatal(err)
	}
	spans, _ := md["heading_spans"].([]HeadingSpan)
	if len(spans) != 1 || spans[0].Text != "Real" {
		t.Fatalf("want one heading span (Real), got %+v", spans)
	}
	if title, ok := md["title"]; !ok || title != "My Doc" {
		t.Errorf("front-matter title not captured as metadata: %+v", md)
	}
}

// TestMarkdownReader_HeadingSpansMetadata: the reader emits the positional span
// table under metadata["heading_spans"] (consumed by the pipeline) and keeps the
// backward-compatible flat list under metadata["headings"].
func TestMarkdownReader_HeadingSpansMetadata(t *testing.T) {
	r := &MarkdownReader{}
	src := []byte("# A\n## B\nbody\n")
	_, md, err := r.Read(context.Background(), src, "x.md")
	if err != nil {
		t.Fatal(err)
	}
	spans, ok := md["heading_spans"].([]HeadingSpan)
	if !ok || len(spans) != 2 {
		t.Fatalf("want 2 heading_spans, got %+v", md["heading_spans"])
	}
	flat, ok := md["headings"].([]string)
	if !ok || len(flat) != 2 || flat[0] != "A" || flat[1] != "B" {
		t.Errorf("flat headings list = %+v, want [A B]", md["headings"])
	}
}
