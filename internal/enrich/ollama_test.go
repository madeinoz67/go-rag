package enrich

import "testing"

// TestNormalizeTags (spec 029, US1 / T007): tags are lowercased, punctuation-
// trimmed, space→hyphen, deduped, and capped at 5 — keeping the filter set small
// and stable.
func TestNormalizeTags(t *testing.T) {
	// lowercased, space→hyphen, leading/trailing punctuation trimmed, deduped,
	// empties dropped, capped at 5.
	got := normalizeTags([]string{"Security", "security", "Back Ups", " Data-Base ", "", "x", "y", "z", "w", "v", "u"})
	want := []string{"security", "back-ups", "data-base", "x", "y"}
	if len(got) != len(want) {
		t.Fatalf("got %d tags %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tag[%d] = %q, want %q (full got=%v)", i, got[i], want[i], got)
		}
	}
}

// TestParseEnrichJSON (spec 029, US1 / T007): the parser extracts {tags,summary}
// from clean JSON and from JSON embedded in prose (the model may add surrounding
// text despite the format=json instruction).
func TestParseEnrichJSON(t *testing.T) {
	tags, summary, err := parseEnrichJSON(`{"tags":["a","b"],"summary":"a short doc"}`)
	if err != nil {
		t.Fatalf("clean JSON: %v", err)
	}
	if summary != "a short doc" || len(tags) != 2 || tags[0] != "a" {
		t.Errorf("clean JSON parsed wrong: tags=%v summary=%q", tags, summary)
	}

	// JSON embedded in prose.
	tags2, summary2, err := parseEnrichJSON(`Here you go: {"tags":["x"],"summary":"hi"} thanks`)
	if err != nil {
		t.Fatalf("embedded JSON: %v", err)
	}
	if summary2 != "hi" || len(tags2) != 1 || tags2[0] != "x" {
		t.Errorf("embedded JSON parsed wrong: tags=%v summary=%q", tags2, summary2)
	}

	if _, _, err := parseEnrichJSON(""); err == nil {
		t.Error("empty response must error")
	}
	if _, _, err := parseEnrichJSON("no json here at all"); err == nil {
		t.Error("non-JSON response must error")
	}
}
