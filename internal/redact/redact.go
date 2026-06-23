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

// Apply scans text, replaces each match with [REDACTED:<type>], and returns the redacted
// text + per-type Finding counts. Credit-card matches are LUHN-validated; non-LUHN matches
// are left un-redacted (false-positive guard). Findings sorted by Type.
func (s *Scanner) Apply(text string) (string, []Finding) {
	if s == nil || len(s.patterns) == 0 {
		return text, nil
	}
	counts := make(map[string]int)
	result := text
	for _, p := range s.patterns {
		if p.credit {
			var n int
			result, n = applyLuhn(result, p)
			if n > 0 {
				counts[p.Type] += n
			}
		} else {
			n := 0
			result = p.re.ReplaceAllStringFunc(result, func(m string) string {
				n++
				return placeholder(p.Type)
			})
			if n > 0 {
				counts[p.Type] += n
			}
		}
	}
	return result, toFindings(counts)
}

func placeholder(typ string) string {
	return fmt.Sprintf("[REDACTED:%s]", typ)
}

// applyLuhn replaces only LUHN-valid credit-card matches.
func applyLuhn(text string, p Pattern) (string, int) {
	count := 0
	result := p.re.ReplaceAllStringFunc(text, func(m string) string {
		if luhnValid(m) {
			count++
			return placeholder(p.Type)
		}
		return m // not a valid card number — leave it
	})
	return result, count
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
