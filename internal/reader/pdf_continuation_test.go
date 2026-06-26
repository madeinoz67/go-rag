package reader

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// buildPDFGridPages builds an N-page PDF where each page is a table grid rendered
// at the given column X anchors (spec 031 T018 fixtures). Each pageGrid is rows of
// cells. Used to exercise cross-page table continuation.
func buildPDFGridPages(pageGrids [][][]string, colX []float64) []byte {
	n := len(pageGrids)
	fontObj := 3 + n
	var kids []string
	for i := 0; i < n; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i))
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n),
	}
	for i := 0; i < n; i++ {
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontObj, 4+n+i))
	}
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for _, grid := range pageGrids {
		var s strings.Builder
		s.WriteString("BT /F1 10 Tf ")
		for r, row := range grid {
			y := 700 - r*20
			for c, cell := range row {
				fmt.Fprintf(&s, "1 0 0 1 %g %g Tm (%s) Tj ", colX[c], float64(y), cell)
			}
		}
		s.WriteString("ET")
		stream := s.String()
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	return assemblePDF(objs, "")
}

// TestPDFReader_MultiPageTableContinued (spec 031 T018): a table spanning two
// pages (same columns + anchors) is marked with continuation markers on both pages.
func TestPDFReader_MultiPageTableContinued(t *testing.T) {
	colX := []float64{72, 172, 272}
	pdf := buildPDFGridPages([][][]string{
		{{"Year", "Revenue", "Profit"}, {"2023", "4.2M", "1.1M"}, {"2024", "5.8M", "1.6M"}},
		{{"2025", "7.1M", "2.0M"}, {"2026", "8.3M", "2.5M"}, {"2027", "9.0M", "2.9M"}},
	}, colX)
	r := &PDFReader{}
	content, _, err := r.Read(context.Background(), pdf, "span.pdf")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(content, "[table continues on next page]") {
		t.Errorf("expected page-1 continuation marker; got:\n%s", content)
	}
	if !strings.Contains(content, "[table continued from previous page]") {
		t.Errorf("expected page-2 continuation marker; got:\n%s", content)
	}
}

// TestPDFReader_UnrelatedTablesNotMarked (spec 031 T018, the FALSE-MERGE GUARD):
// two per-page tables with DIFFERENT column X anchors are NOT marked as continued.
// This is the load-bearing test for the cardinal sin — a false merge would garble
// the connection between unrelated tables.
func TestPDFReader_UnrelatedTablesNotMarked(t *testing.T) {
	// Page 1 columns at 72/172/272; page 2 columns at 100/250/400 (different origin).
	pdf := buildPDFGridPages([][][]string{
		{{"A", "B", "C"}, {"1", "2", "3"}, {"4", "5", "6"}},
		{{"X", "Y", "Z"}, {"7", "8", "9"}, {"10", "11", "12"}},
	}, []float64{72, 172, 272})
	// Override: rebuild page-2 grid at different anchors via a separate build is awkward;
	// instead build two single-page PDFs is not the test. Build page 2 at shifted anchors
	// by constructing the pages with per-page colX. Simpler: assert the default (same anchors)
	// DOES mark (covered above), and here use different anchors by hand-building.
	_ = pdf // (the helper uses one colX for all pages; see the hand-built variant below)
	twoPage := buildTwoPageGridDifferentAnchors()
	r := &PDFReader{}
	content, _, err := r.Read(context.Background(), twoPage, "unrelated.pdf")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(content, "[table continues on next page]") || strings.Contains(content, "[table continued from previous page]") {
		t.Errorf("unrelated tables (different column anchors) must NOT be marked continued; got:\n%s", content)
	}
	// both tables still render as standalone tables:
	if !strings.Contains(content, "| A | B | C |") || !strings.Contains(content, "| X | Y | Z |") {
		t.Errorf("both unrelated tables should still render; got:\n%s", content)
	}
}

// buildTwoPageGridDifferentAnchors: page 1 cols at 72/172/272, page 2 cols at 100/250/400.
func buildTwoPageGridDifferentAnchors() []byte {
	mkStream := func(grid [][]string, colX []float64) string {
		var s strings.Builder
		s.WriteString("BT /F1 10 Tf ")
		for r, row := range grid {
			y := 700 - r*20
			for c, cell := range row {
				fmt.Fprintf(&s, "1 0 0 1 %g %g Tm (%s) Tj ", colX[c], float64(y), cell)
			}
		}
		s.WriteString("ET")
		return s.String()
	}
	s1 := mkStream([][]string{{"A", "B", "C"}, {"1", "2", "3"}, {"4", "5", "6"}}, []float64{72, 172, 272})
	s2 := mkStream([][]string{{"X", "Y", "Z"}, {"7", "8", "9"}, {"10", "11", "12"}}, []float64{100, 250, 400})
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s1), s1),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s2), s2),
	}
	return assemblePDF(objs, "")
}

// TestPDFReader_ThreePageTableChain (spec 031 T018): a table spanning three pages
// gets markers at BOTH boundaries (iterative chaining, not pairwise).
func TestPDFReader_ThreePageTableChain(t *testing.T) {
	colX := []float64{72, 172, 272}
	page := func() [][]string { return [][]string{{"a", "b", "c"}, {"1", "2", "3"}, {"4", "5", "6"}} }
	pdf := buildPDFGridPages([][][]string{page(), page(), page()}, colX)
	r := &PDFReader{}
	content, _, err := r.Read(context.Background(), pdf, "chain.pdf")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// page1 -> page2 marker, and page2 -> page3 marker (two "continues" + two "continued").
	if c := strings.Count(content, "[table continues on next page]"); c != 2 {
		t.Errorf("expected 2 'continues' markers (one per boundary), got %d; content:\n%s", c, content)
	}
	if c := strings.Count(content, "[table continued from previous page]"); c != 2 {
		t.Errorf("expected 2 'continued' markers, got %d; content:\n%s", c, content)
	}
}

// TestDetectTablesStructured_ProveEqual (spec 031 T018 no-drift guard):
// detectTablesStructured must find tables on exactly the frags where detectTables
// does (one gate code path). Any divergence is a drift bug.
func TestDetectTablesStructured_ProveEqual(t *testing.T) {
	stream := tableStream([][]string{{"Name", "Age", "City"}, {"Alice", "30", "Perth"}, {"Bob", "25", "Sydney"}}, 700, 20)
	frags, _, _ := parsePositionedText(stream)
	splices := detectTables(frags)
	cands := detectTablesStructured(frags)
	if len(splices) != len(cands) {
		t.Fatalf("detectTables=%d splices, detectTablesStructured=%d candidates (drift)", len(splices), len(cands))
	}
	for i := range splices {
		if splices[i].ByteStart != cands[i].ByteStart || splices[i].ByteEnd != cands[i].ByteEnd {
			t.Errorf("candidate %d byte range diverges: splice=%d:%d cand=%d:%d", i, splices[i].ByteStart, splices[i].ByteEnd, cands[i].ByteStart, cands[i].ByteEnd)
		}
	}
	// prose should yield none from both.
	prose := "BT /F1 10 Tf 72 700 Td (just body prose no table structure) Tj 0 -14 Td (more body prose) Tj 0 -14 Td (third line) Tj ET"
	pfrags, _, _ := parsePositionedText(prose)
	if detectTables(pfrags) != nil || detectTablesStructured(pfrags) != nil {
		t.Error("prose should yield no tables from either detector")
	}
	_ = bytes.NewReader // keep bytes referenced
}
