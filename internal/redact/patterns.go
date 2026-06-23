// Package redact is go-rag's opt-in regex secret/PII scanner (spec 022 / audit H19,
// book §11.2). When enabled, it detects + redacts secrets (API keys, tokens, private
// keys, credit cards, SSNs, emails) from ingested text BEFORE indexing, so indexed
// text never contains live credentials. Pure stdlib `regexp` (Constitution III).
//
// The pipeline calls Apply BETWEEN identity computation (docID over original content)
// and chunking — so document identity is stable regardless of redaction (Constitution II).
package redact

import (
	"bufio"
	"os"
	"regexp"
	"sort"
)

// Pattern is one detection rule: a compiled regex + its type label + placeholder.
type Pattern struct {
	Type   string
	re     *regexp.Regexp
	credit bool // LUHN-validate before redacting (credit cards only)
}

// Built-in curated regex set (contracts/patterns.md). ASCII-centric; Unicode PII not
// covered by default — supply custom patterns via a patterns file.
var builtinPatterns = []Pattern{
	{"aws-key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`), false},
	{"github-token", regexp.MustCompile(`gh[opsu]_[A-Za-z0-9]{36}`), false},
	{"private-key", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), false},
	{"ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), false},
	{"email", regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`), false},
	{"credit-card", regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`), true}, // LUHN-validated in Apply
}

// LoadCustom reads a patterns file ("<type>\t<regex>" per line, "#" comments) and
// returns compiled Patterns. Bad regexes return an error (fail at boot, never mid-ingest).
func LoadCustom(path string) ([]Pattern, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Pattern
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		tab := indexByte(line, '\t')
		if tab < 0 {
			continue
		}
		typ, regexStr := line[:tab], line[tab+1:]
		re, err := regexp.Compile(regexStr)
		if err != nil {
			return nil, &patternError{typ: typ, err: err}
		}
		out = append(out, Pattern{Type: typ, re: re})
	}
	return out, sc.Err()
}

// DefaultPatterns returns a copy of the built-in set + any custom patterns merged in.
func DefaultPatterns(custom []Pattern) []Pattern {
	out := make([]Pattern, 0, len(builtinPatterns)+len(custom))
	out = append(out, builtinPatterns...)
	out = append(out, custom...)
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

type patternError struct {
	typ string
	err error
}

func (e *patternError) Error() string { return "redact: bad pattern " + e.typ + ": " + e.err.Error() }
