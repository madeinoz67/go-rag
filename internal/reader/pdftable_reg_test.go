package reader

import (
	"fmt"
	"strings"
	"testing"
)

// TestDetectTables_NewsletterColumns (spec 031 US3 regression, design-verdict
// HIGH fix): three widely-spaced columns of body-text sentences (a newsletter /
// multi-column layout) must NOT become a table — its cells are long sentences
// with uniform length across columns, which the asymmetry check (column-vs-column)
// misses. The mean-cell-length cap rejects it. Splicing it row-major would
// destroy the column-major reading order (the cardinal sin).
func TestDetectTables_NewsletterColumns(t *testing.T) {
	cols := []float64{72, 250, 430}
	sentences := []string{
		"The quarterly results exceeded all analyst expectations this period.",
		"Revenue growth accelerated across every single regional segment.",
		"Profit margins expanded due to improved operational efficiency here.",
		"Management expects the positive trend to continue into next year.",
	}
	var b strings.Builder
	b.WriteString("BT /F1 10 Tf ")
	for r, sent := range sentences {
		y := 700 - r*20
		for _, x := range cols {
			fmt.Fprintf(&b, "1 0 0 1 %g %g Tm (%s) Tj ", x, float64(y), sent)
		}
	}
	b.WriteString("ET")
	frags, flat, _ := parsePositionedText(b.String())
	out := renderPageWithTables(flat, frags)
	if strings.Contains(out, "|---|") {
		t.Errorf("multi-column newsletter body must NOT become a table; got:\n%s", out)
	}
}

// TestDetectTables_CodeBlock (spec 031 US3 regression, design-verdict HIGH fix):
// an indented code block has tight X gaps (indentation stops), which the
// column-span gate (< 3*medFS apart) rejects. Common in the technical-spec PDFs
// go-rag targets — a code block rendered as a table would be severe garbling.
func TestDetectTables_CodeBlock(t *testing.T) {
	code := []struct {
		x    float64
		text string
	}{
		{72, "func main() {"},
		{84, "if err != nil {"},
		{96, "return err"},
		{84, "}"},
		{72, "}"},
	}
	var b strings.Builder
	b.WriteString("BT /F1 10 Tf ")
	for r, line := range code {
		fmt.Fprintf(&b, "1 0 0 1 %g %g Tm (%s) Tj ", line.x, float64(700-r*20), line.text)
	}
	b.WriteString("ET")
	frags, flat, _ := parsePositionedText(b.String())
	out := renderPageWithTables(flat, frags)
	if strings.Contains(out, "|---|") {
		t.Errorf("indented code block must NOT become a table; got:\n%s", out)
	}
}
