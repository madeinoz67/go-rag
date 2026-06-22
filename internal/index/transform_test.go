package index

import (
	"context"
	"testing"
)

// TestNormalizeQuery covers the default normalization (H05 US1): cosmetic
// equivalence (case/whitespace), Unicode safety, and the empty cases.
func TestNormalizeQuery(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Some Term", "some term"},
		{"  some   term ", "some term"},          // leading/trailing/internal whitespace
		{"SOME TERM", "some term"},                // case-fold
		{"already clean", "already clean"},        // no-op on clean input
		{"multiple\t\tspaces\n\nnewlines", "multiple spaces newlines"}, // tab/newline collapse
		{"Café Naïve", "café naïve"},              // accented letters lowercased, preserved
		{"数据 检索", "数据 检索"},                   // CJK preserved, single space kept
		{"数据  检索", "数据 检索"},                  // CJK double-space collapsed
		{"", ""},                                  // empty
		{"   ", ""},                               // whitespace-only → empty (FR-006)
	}
	for _, c := range cases {
		if got := normalizeQuery(c.in); got != c.want {
			t.Errorf("normalizeQuery(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeQuery_Idempotent (FR-007): normalize(normalize(q)) == normalize(q).
func TestNormalizeQuery_Idempotent(t *testing.T) {
	for _, q := range []string{"Some  Term", "  Café   naïve  ", "数据\t检索", "clean", ""} {
		once := normalizeQuery(q)
		twice := normalizeQuery(once)
		if once != twice {
			t.Errorf("not idempotent for %q: once=%q twice=%q", q, once, twice)
		}
	}
}

// TestNormalizingTransformer_Transform covers the default transformer's contract:
// returns a single normalized query; errors on empty-after-normalization (FR-006),
// never returning a slice containing an empty string.
func TestNormalizingTransformer_Transform(t *testing.T) {
	nt := NormalizingTransformer{}

	out, err := nt.Transform(context.Background(), "  Some   Term ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0] != "some term" {
		t.Errorf("Transform = %v, want [\"some term\"]", out)
	}

	// Whitespace-only → empty after normalization → error (FR-006).
	out, err = nt.Transform(context.Background(), "   ")
	if err == nil {
		t.Error("whitespace-only input must return an error (FR-006)")
	}
	if len(out) > 0 {
		t.Errorf("must not return a slice on empty-after-normalization; got %v", out)
	}
}
