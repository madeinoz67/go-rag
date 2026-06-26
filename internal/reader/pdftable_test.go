package reader

import (
	"strings"
	"testing"
)

// tableStream builds a clean nRows x 3-col content stream using absolute Tm
// positioning (columns align precisely at baseX, baseX+100, baseX+200).
func tableStream(cells [][]string, baseY float64, rowGap float64) string {
	var b strings.Builder
	b.WriteString("BT /F1 10 Tf ")
	for r, row := range cells {
		y := baseY - float64(r)*rowGap
		for c, cell := range row {
			x := 72 + c*100
			b.WriteString("1 0 0 1 ")
			b.WriteString(itoa(x))
			b.WriteString(" ")
			b.WriteString(itoa(int(y)))
			b.WriteString(" Tm (")
			b.WriteString(cell)
			b.WriteString(") Tj ")
		}
	}
	b.WriteString("ET")
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestDetectTables_CleanGrid: a 3x3 table becomes a Markdown table with the
// first row as header (spec 031 T017, SC-003).
func TestDetectTables_CleanGrid(t *testing.T) {
	stream := tableStream([][]string{
		{"Name", "Age", "City"},
		{"Alice", "30", "Perth"},
		{"Bob", "25", "Sydney"},
	}, 700, 20)
	frags, flat, amb := parsePositionedText(stream)
	if amb {
		t.Fatal("unexpected ambiguous")
	}
	out := renderPageWithTables(flat, frags)
	for _, want := range []string{"| Name | Age | City |", "|---|---|---|", "| Alice | 30 | Perth |", "| Bob | 25 | Sydney |"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
	if flat == out {
		t.Error("expected the table to be spliced in (output unchanged)")
	}
}

// TestDetectTables_ProseUnchanged: a single-column paragraph (1 X cluster) is
// NOT a table -> content byte-identical (never worse than today).
func TestDetectTables_ProseUnchanged(t *testing.T) {
	stream := "BT /F1 10 Tf 72 700 Td (Line one of prose text here) Tj 0 -14 Td (Line two of prose text here) Tj 0 -14 Td (Line three of prose text here) Tj ET"
	frags, flat, amb := parsePositionedText(stream)
	if amb {
		t.Fatal("unexpected ambiguous")
	}
	out := renderPageWithTables(flat, frags)
	if out != flat {
		t.Errorf("prose must be unchanged; got:\n%s", out)
	}
	if strings.Contains(out, "|") {
		t.Errorf("prose must not contain table pipes; got:\n%s", out)
	}
}

// TestDetectTables_NumberedListBail: a 2-column numbered list (number | text) is
// NOT promoted to a table — the >=3-column gate rejects it.
func TestDetectTables_NumberedListBail(t *testing.T) {
	var b strings.Builder
	b.WriteString("BT /F1 10 Tf ")
	rows := [][]string{{"1.", "First item"}, {"2.", "Second item"}, {"3.", "Third item"}}
	for r, row := range rows {
		y := 700 - r*20
		b.WriteString("1 0 0 1 72 ")
		b.WriteString(itoa(y))
		b.WriteString(" Tm (")
		b.WriteString(row[0])
		b.WriteString(") Tj 1 0 0 1 100 ")
		b.WriteString(itoa(y))
		b.WriteString(" Tm (")
		b.WriteString(row[1])
		b.WriteString(") Tj ")
	}
	b.WriteString("ET")
	frags, flat, _ := parsePositionedText(b.String())
	out := renderPageWithTables(flat, frags)
	if strings.Contains(out, "|---|") {
		t.Errorf("2-column numbered list must NOT become a table; got:\n%s", out)
	}
}

// TestDetectTables_NonUniformPitch: rows with wildly varying gaps are not a table.
func TestDetectTables_NonUniformPitch(t *testing.T) {
	// 3 cols but row gaps 20, 80 (non-uniform) -> bail.
	stream := "BT /F1 10 Tf " +
		"1 0 0 1 72 700 Tm (A) Tj 1 0 0 1 172 700 Tm (B) Tj 1 0 0 1 272 700 Tm (C) Tj " +
		"1 0 0 1 72 680 Tm (D) Tj 1 0 0 1 172 680 Tm (E) Tj 1 0 0 1 272 680 Tm (F) Tj " +
		"1 0 0 1 72 600 Tm (G) Tj 1 0 0 1 172 600 Tm (H) Tj 1 0 0 1 272 600 Tm (I) Tj ET"
	frags, flat, _ := parsePositionedText(stream)
	out := renderPageWithTables(flat, frags)
	if strings.Contains(out, "|---|") {
		t.Errorf("non-uniform pitch must not be a table; got:\n%s", out)
	}
}
