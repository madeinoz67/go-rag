package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// GoldenQuery is one row of the committed golden evaluation dataset: a
// natural-language query plus the chunk_ids a human judged relevant for it
// (data-model.md). The relevant chunk_ids are content-addressed (Principle II),
// so they are stable across any vault built from the same corpus with the same
// chunker — labels are portable (research.md D5).
type GoldenQuery struct {
	ID       string   `json:"id"`
	Query    string   `json:"query"`
	Relevant []string `json:"relevant"`
	Notes    string   `json:"notes,omitempty"`
}

// LoadGolden parses a JSONL golden dataset (one GoldenQuery per line). Blank
// lines are ignored. Every record MUST have a unique non-empty id, a non-empty
// query, and a (possibly empty) relevant list of non-empty chunk_ids. A record
// whose relevant list is empty is allowed but will be skipped during scoring
// (FR-008). Returns an error naming the offending line on any violation.
func LoadGolden(path string) ([]GoldenQuery, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open golden %q: %w", path, err)
	}
	defer f.Close()

	var out []GoldenQuery
	seen := make(map[string]bool)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // allow long queries
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		// Skip blank lines (and line-level JSONL comments starting with '#').
		trimmed := trimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		var gq GoldenQuery
		if err := json.Unmarshal(line, &gq); err != nil {
			return nil, fmt.Errorf("golden %q line %d: %w", path, lineNo, err)
		}
		if gq.ID == "" {
			return nil, fmt.Errorf("golden %q line %d: empty id", path, lineNo)
		}
		if seen[gq.ID] {
			return nil, fmt.Errorf("golden %q line %d: duplicate id %q", path, lineNo, gq.ID)
		}
		if gq.Query == "" {
			return nil, fmt.Errorf("golden %q line %d: empty query for %q", path, lineNo, gq.ID)
		}
		for i, r := range gq.Relevant {
			if r == "" {
				return nil, fmt.Errorf("golden %q line %d: empty relevant chunk_id at index %d for %q", path, lineNo, i, gq.ID)
			}
		}
		seen[gq.ID] = true
		out = append(out, gq)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read golden %q: %w", path, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("golden %q: no records", path)
	}
	return out, nil
}

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}
