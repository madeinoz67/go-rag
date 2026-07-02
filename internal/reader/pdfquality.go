package reader

// pdfquality.go derives a per-document extraction-method + quality score from
// the PDF reader's per-page text coverage (spec 042 / BL-006). Pure + deterministic
// so it can be unit-tested without PDF fixtures.
//
// IMPORTANT: this is a COVERAGE-CONFIDENCE signal, NOT an OCR-quality verdict.
// go-rag is pure-Go and does not OCR (PRD §2.2 out-of-scope), so a scanned PDF
// that happens to ship a machine text layer reads as "native". The bridge's
// actual gate — "don't promote sparse / image-only extraction as authoritative"
// — is satisfied; true OCR-quality detection waits for an OCR story.

// nativeCharsPerPage is the average extracted chars/page at/above which a
// text-bearing PDF is classified "native" (high coverage confidence). Below it
// the text is considered sparse (→ "mixed"). Heuristic + tunable; see
// specs/042-chunk-extraction-quality/spec.md.
const nativeCharsPerPage = 200.0

// classifyExtraction maps per-page text coverage to (method, quality).
//
// pages        = total PDF page count.
// pagesWithText = pages that yielded at least one byte of text.
// totalChars   = sum of extracted text length across pages.
//
// Returns method ∈ {"native","mixed","image"} + quality ∈ [0.0, 1.0]:
//   - no pages              → image, 0.0  (degenerate; no chunks produced)
//   - no page has text       → image, 0.1  (all-image; no text layer → no chunks)
//   - some pages text-less   → mixed, 0.3 + 0.5·(pagesWithText/pages)  (0.3–0.8)
//   - all pages have text, dense (≥ nativeCharsPerPage/page) → native, 0.95
//   - all pages have text, sparse                           → mixed, 0.5 + 0.4·(avg/200)  (0.5–0.9)
func classifyExtraction(pages, pagesWithText int, totalChars int) (method string, quality float64) {
	switch {
	case pages == 0:
		return "image", 0.0
	case pagesWithText == 0:
		return "image", 0.1
	case pagesWithText < pages:
		return "mixed", 0.3 + 0.5*float64(pagesWithText)/float64(pages)
	default:
		avgPerPage := float64(totalChars) / float64(pages)
		if avgPerPage >= nativeCharsPerPage {
			return "native", 0.95
		}
		return "mixed", 0.5 + 0.4*(avgPerPage/nativeCharsPerPage)
	}
}
