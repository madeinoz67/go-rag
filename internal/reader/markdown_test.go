package reader

import (
	"context"
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
