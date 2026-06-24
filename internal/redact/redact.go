package redact

// redact.go implements the Scanner + Apply transform (spec 022 / audit H19). Apply
// replaces each secret match with a typed placeholder [REDACTED:<type>] and returns
// per-type Finding counts. Credit cards are LUHN-validated before redaction (cuts false
// positives). Deterministic for a fixed pattern set.

import (
	"fmt"
	"sort"
	"strings"
)

// Finding is a per-type aggregate of what a redaction pass detected + replaced. Carries
// the type + count — never the matched text (privacy: findings don't re-expose secrets).
type Finding struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// Scanner applies a set of patterns to text. Construct with NewScanner; call Apply per
// document. Immutable after construction; safe for concurrent use.
type Scanner struct {
	patterns []Pattern
}

// NewScanner builds a scanner from the given patterns (typically DefaultPatterns(custom)).
func NewScanner(patterns []Pattern) *Scanner {
	return &Scanner{patterns: patterns}
}

// Edit records one redaction substitution in the ORIGINAL text's coordinate
// space: the match began at Pos, spanned RemovedLen bytes, and was replaced by a
// placeholder of InsertedLen bytes. Edits are returned sorted by Pos; TranslateOffset
// composes them to map a pre-redaction offset into the redacted text (research R3).
type Edit struct {
	Pos         int
	RemovedLen  int
	InsertedLen int
}

// ApplyWithEdits is the offset-aware redaction pass (audit H23 / spec 025, R3). It
// produces the SAME redacted text and per-type Finding counts as Apply, in one
// left-to-right pass over the original text, and additionally returns one Edit per
// substitution in original-text coordinates. TranslateOffset consumes the edits to map
// heading offsets (stripped-text space) into the redacted-text space the chunker indexes.
// Credit-card matches are LUHN-validated; non-LUHN matches are left un-redacted
// (false-positive guard) and emit no edit. When nothing matches, edits is nil and
// TranslateOffset is the identity — the common case (redaction off / clean text).
func (s *Scanner) ApplyWithEdits(text string) (string, []Finding, []Edit) {
	if s == nil || len(s.patterns) == 0 {
		return text, nil, nil
	}
	// Collect every match of every pattern in original-text coordinates.
	type cand struct {
		start, end int
		typ        string
	}
	var cands []cand
	for _, p := range s.patterns {
		for _, m := range p.re.FindAllStringSubmatchIndex(text, -1) {
			lo, hi := m[0], m[1]
			if p.credit && !luhnValid(text[lo:hi]) {
				continue // non-LUHN card match — leave un-redacted (false-positive guard)
			}
			cands = append(cands, cand{start: lo, end: hi, typ: p.Type})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].start != cands[j].start {
			return cands[i].start < cands[j].start
		}
		return cands[i].end < cands[j].end
	})

	var b strings.Builder
	counts := make(map[string]int)
	var edits []Edit
	prev := 0
	for _, c := range cands {
		if c.start < prev {
			continue // overlaps an already-redacted span — first match wins (patterns are disjoint in practice)
		}
		b.WriteString(text[prev:c.start])
		ph := placeholder(c.typ)
		b.WriteString(ph)
		counts[c.typ]++
		edits = append(edits, Edit{Pos: c.start, RemovedLen: c.end - c.start, InsertedLen: len(ph)})
		prev = c.end
	}
	b.WriteString(text[prev:])
	return b.String(), toFindings(counts), edits
}

// Apply scans text, replaces each match with [REDACTED:<type>], and returns the redacted
// text + per-type Finding counts. Thin wrapper over the single canonical redaction pass
// (ApplyWithEdits) so there is exactly one redaction code path. Credit-card matches are
// LUHN-validated; non-LUHN matches are left un-redacted (false-positive guard). Findings
// sorted by Type.
func (s *Scanner) Apply(text string) (string, []Finding) {
	red, findings, _ := s.ApplyWithEdits(text)
	return red, findings
}

// TranslateOffset maps a byte offset in the ORIGINAL (pre-redaction) text to its position
// in the REDACTED text, given the redaction edits (original coordinates, sorted by Pos).
// For every substitution strictly before the offset (Pos < offset), the offset shifts by
// (InsertedLen - RemovedLen). An offset at a substitution's start maps to the replacement's
// start. Identity when edits is empty (redaction disabled or no matches) — the common case.
// (Heading offsets are never coincident with a secret, so the strict `<` covers every real
// resolution; research R3.)
func TranslateOffset(offset int, edits []Edit) int {
	for _, e := range edits {
		if e.Pos < offset {
			offset += e.InsertedLen - e.RemovedLen
		} else {
			break
		}
	}
	return offset
}

func placeholder(typ string) string {
	return fmt.Sprintf("[REDACTED:%s]", typ)
}

// luhnValid checks the LUHN checksum of a string containing digits (ignores spaces/dashes).
func luhnValid(s string) bool {
	digits := make([]int, 0, len(s))
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum, parity := 0, len(digits)%2
	for i, d := range digits {
		if i%2 == parity {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

func toFindings(counts map[string]int) []Finding {
	if len(counts) == 0 {
		return nil
	}
	out := make([]Finding, 0, len(counts))
	for typ, n := range counts {
		if n > 0 {
			out = append(out, Finding{Type: typ, Count: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// RenderFindings renders findings as a compact "type=N, type=N" string for the ingest summary.
func RenderFindings(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	parts := make([]string, len(findings))
	for i, f := range findings {
		parts[i] = fmt.Sprintf("%s=%d", f.Type, f.Count)
	}
	return strings.Join(parts, ", ")
}
