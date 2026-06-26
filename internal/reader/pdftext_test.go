package reader

import (
	"math"
	"testing"
)

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 0.5 }

// TestParsePositionedText_Simple: one Tj glyph run yields one frag at the text
// origin with the Tf font size and name (spec 031 T004).
func TestParsePositionedText_Simple(t *testing.T) {
	frags, flat, amb := parsePositionedText("BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	if amb {
		t.Fatal("unexpected ambiguous")
	}
	if len(frags) != 1 {
		t.Fatalf("frags: got %d, want 1 (%+v)", len(frags), frags)
	}
	f := frags[0]
	if f.Text != "Hello" || !approxEqual(f.X, 72) || !approxEqual(f.Y, 700) ||
		!approxEqual(f.FontSize, 12) || f.Font != "F1" {
		t.Errorf("frag: got %+v, want Hello@72,700 size12 F1", f)
	}
	if flat != "Hello " {
		t.Errorf("flat: got %q, want %q", flat, "Hello ")
	}
	if f.ByteStart != 0 || f.ByteEnd != 5 {
		t.Errorf("byte range: got %d:%d, want 0:5", f.ByteStart, f.ByteEnd)
	}
}

// TestParsePositionedText_TJConcat: a TJ array concatenates its string elements
// with NO separator (the correctness fix — the old reader space-separated and
// would mangle [(1)10(,)-5(000)] -> "1 , 000").
func TestParsePositionedText_TJConcat(t *testing.T) {
	frags, _, amb := parsePositionedText("BT /F1 12 Tf 72 700 Td [(1)10(,)-5(000)] TJ ET")
	if amb {
		t.Fatal("unexpected ambiguous")
	}
	if len(frags) != 1 || frags[0].Text != "1,000" {
		t.Fatalf("TJ concat: got %+v, want single frag Text=\"1,000\"", frags)
	}
}

// TestParsePositionedText_TJDisplacementNoFontSize: TJ numeric displacement must
// NOT scale by font size (ISO 32000-1 §9.4.4; design-verdict critical fix #2).
// [(A)-1000(B)] TJ then (C) Tj: with the correct formula, C lands at X≈13
// (displacement +1, then ~12 for "AB" width); the buggy fontSize formula would
// put it at ≈24.
func TestParsePositionedText_TJDisplacementNoFontSize(t *testing.T) {
	frags, _, _ := parsePositionedText("BT /F1 12 Tf 0 0 Td [(A)-1000(B)] TJ (C) Tj ET")
	if len(frags) != 2 {
		t.Fatalf("frags: got %d, want 2 (%+v)", len(frags), frags)
	}
	if frags[0].Text != "AB" {
		t.Errorf("first frag Text: got %q, want AB", frags[0].Text)
	}
	if frags[1].Text != "C" {
		t.Errorf("second frag Text: got %q, want C", frags[1].Text)
	}
	// Correct (no fontSize): TJ moved pen by +1, "AB" width ≈12 -> C at ~13.
	// Buggy (fontSize×): would move by +12 -> C at ~24.
	if frags[1].X > 18 {
		t.Errorf("C.X = %v suggests fontSize-scaled displacement (the bug); want <=18 (no fontSize factor)", frags[1].X)
	}
}

// TestParsePositionedText_Grid: a clean 3-col x 2-row table with absolute Tm
// positioning yields 6 frags at 2 Y-bands and 3 X-columns.
func TestParsePositionedText_Grid(t *testing.T) {
	stream := "BT /F1 10 Tf " +
		"1 0 0 1 72 700 Tm (Name) Tj 1 0 0 1 172 700 Tm (Age) Tj 1 0 0 1 272 700 Tm (City) Tj " +
		"1 0 0 1 72 680 Tm (Alice) Tj 1 0 0 1 172 680 Tm (30) Tj 1 0 0 1 272 680 Tm (Perth) Tj ET"
	frags, _, amb := parsePositionedText(stream)
	if amb {
		t.Fatal("unexpected ambiguous")
	}
	if len(frags) != 6 {
		t.Fatalf("frags: got %d, want 6", len(frags))
	}
	want := map[string][2]float64{
		"Name": {72, 700}, "Age": {172, 700}, "City": {272, 700},
		"Alice": {72, 680}, "30": {172, 680}, "Perth": {272, 680},
	}
	for _, f := range frags {
		w, ok := want[f.Text]
		if !ok {
			t.Errorf("unexpected frag %q", f.Text)
			continue
		}
		if !approxEqual(f.X, w[0]) || !approxEqual(f.Y, w[1]) {
			t.Errorf("frag %q: got (%v,%v), want (%v,%v)", f.Text, f.X, f.Y, w[0], w[1])
		}
	}
}

// TestParsePositionedText_Malformed: a truncated Tm (3 operands, not 6) sets
// ambiguous=true; the parser never panics and returns frags-so-far.
func TestParsePositionedText_Malformed(t *testing.T) {
	frags, _, amb := parsePositionedText("BT /F1 12 Tf 72 700 Td (Before) Tj 1 2 3 Tm (After) Tj")
	if !amb {
		t.Error("expected ambiguous=true for truncated Tm")
	}
	if len(frags) == 0 {
		t.Error("expected at least the pre-Tm fragment")
	}
	// Must not panic on garbage either.
	_, _, _ = parsePositionedText("BT ((( \\ (( ]] << >> EI Tj Tj Tj")
}

// TestParsePositionedText_InlineImage: a BI...EI inline image is skipped wholesale
// — its bytes (which contain fake operators and parens) do not poison the page,
// and text after EI extracts normally.
func TestParsePositionedText_InlineImage(t *testing.T) {
	stream := "BI /W 1 /H 1 /BPC 8 ID \x00\x00\x00 (fake) Tj ) EI BT /F1 12 Tf 72 700 Td (After) Tj ET"
	frags, _, _ := parsePositionedText(stream)
	if len(frags) != 1 || frags[0].Text != "After" {
		t.Fatalf("expected single frag \"After\"; got %+v (inline image not skipped)", frags)
	}
}
