package index

// filter.go defines the metadata filter for retrieval scoping (audit H14 / spec 014).
//
// Filter scopes a query to a document subset by source path (glob or prefix), file
// type (exact, case-insensitive), and/or tags (conjunction). It is an optional
// dimension of QueryRequest; an empty Filter = no constraint (today's behavior).
//
// The filter is applied at the Retrieval layer (pre-fusion): FTS and Vector
// candidate lists are filtered BEFORE RRF fusion, collapse-by-doc, and rerank — so
// non-matching chunks never reach scoring/ranking. The engine builds a
// keep(chunkID) closure from Filter + the docOf/lookupDoc resolvers; Retrieval
// calls keep(chunkID) per candidate.

import (
	"path"
	"strings"
)

// Filter scopes a query to documents matching all set dimensions (conjunction).
type Filter struct {
	Source string   // path glob (* or ?) or prefix (no wildcards) against Document.FilePath; "" = no constraint
	Type   string   // exact case-insensitive match against Document.FileType; "" = no constraint
	Tags   []string // conjunction: document must carry ALL specified tags; nil/empty = no constraint
}

// Empty reports whether no dimension is set (no filtering — today's behavior).
func (f Filter) Empty() bool {
	return f.Source == "" && f.Type == "" && len(f.Tags) == 0
}

// Matches reports whether a document with the given attributes passes the filter.
// All set dimensions must match (conjunction). Unset dimensions are ignored.
func (f Filter) Matches(filePath, fileType string, docTags []string) bool {
	if f.Source != "" {
		if !sourceMatches(f.Source, filePath) {
			return false
		}
	}
	if f.Type != "" {
		if !strings.EqualFold(f.Type, fileType) {
			return false
		}
	}
	if len(f.Tags) > 0 {
		set := make(map[string]bool, len(docTags))
		for _, t := range docTags {
			set[t] = true
		}
		for _, want := range f.Tags {
			if !set[want] {
				return false
			}
		}
	}
	return true
}

// sourceMatches checks a file path against a source pattern: prefix match (no
// wildcards) or glob match (path.Match, single-segment * / ?). The type filter
// handles file-type scoping (e.g. ".md"); the source filter is for directory /
// path scoping.
func sourceMatches(pattern, filePath string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		matched, err := path.Match(pattern, filePath)
		return err == nil && matched
	}
	return strings.HasPrefix(filePath, pattern)
}
