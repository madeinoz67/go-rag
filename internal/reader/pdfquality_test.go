package reader

import "testing"

// TestClassifyExtraction (spec 042 / BL-006): the per-document coverage →
// (method, quality) classifier, pure-function so it can be tested without PDF
// fixtures. Covers all five branches (no pages / all-image / mixed pages /
// dense native / sparse native).
func TestClassifyExtraction(t *testing.T) {
	cases := []struct {
		name          string
		pages         int
		pagesWithText int
		totalChars    int
		wantMethod    string
		wantQuality   float64
	}{
		{"no pages", 0, 0, 0, "image", 0.0},
		{"all image", 3, 0, 0, "image", 0.1},
		{"mixed pages", 4, 2, 800, "mixed", 0.3 + 0.5*2.0/4.0},        // 0.55
		{"native dense", 2, 2, 600, "native", 0.95},                   // avg=300 ≥ 200
		{"native sparse", 2, 2, 100, "mixed", 0.5 + 0.4*(50.0/200.0)}, // avg=50 → 0.6
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, q := classifyExtraction(c.pages, c.pagesWithText, c.totalChars)
			if m != c.wantMethod {
				t.Errorf("method = %q, want %q", m, c.wantMethod)
			}
			if q != c.wantQuality {
				t.Errorf("quality = %v, want %v", q, c.wantQuality)
			}
		})
	}
}
