// Package engine is go-rag's unified operation surface — the single layer every
// transport adapter (CLI, MCP, REST, gRPC) calls. It owns no new storage or
// indexing logic; it orchestrates the existing pipeline / index / storage /
// embed packages and returns structured results. Because every adapter invokes
// the same methods here, REST, gRPC, and MCP are guaranteed to agree
// (cross-transport parity, spec 003 FR-002/003).
package engine

import "github.com/madeinoz67/go-rag/internal/index"

// QueryRequest is the input to Engine.Query.
type QueryRequest struct {
	Query     string
	K         int
	Mode      string // "hybrid" (default) | "semantic" | "keyword"
	NoRerank  bool
	Threshold float64 // minimum score; hits below are dropped
	RRFK      int     // H08/spec 009: per-query RRF constant override; 0 = use config EffectiveRRFK() (default 60)
	Filter        *index.Filter // H14/spec 014: optional metadata filter (source/type/tags); nil = no filter
	ContextWindow int           // H15/spec 015: N sibling chunks each side of a hit; 0 = off (default)
	NoCache       bool          // H06/spec 016: bypass serving from the result cache for this call (still stores on success)
}

// NewFilter constructs a metadata Filter for a query (H14/spec 014). Returns nil
// when no dimension is set (no filtering). Transports use this to avoid importing
// the index package directly.
func NewFilter(source, ftype string, tags []string) *index.Filter {
	f := index.Filter{Source: source, Type: ftype, Tags: tags}
	if f.Empty() {
		return nil
	}
	return &f
}

// ContextChunk is a sibling chunk included for reading context around a hit
// (H15/spec 015). It is NOT a ranked result — it provides the surrounding text.
type ContextChunk struct {
	ChunkID   string
	Content   string
	Direction string // "previous" | "next" relative to the hit
}

// QueryHit is one ranked result. Adapters serialize this verbatim.
type QueryHit struct {
	ChunkID    string
	DocumentID string
	Score      float64
	Content    string // full chunk text
	FilePath   string
	Page       int    // Chunk.PageNumber; 0 when not paginated
	Preview    string // convenience truncated preview for text renders
	Context    []ContextChunk // H15/spec 015: sibling chunks for reading context; nil when ContextWindow=0
}

// QueryResult wraps the ranked hits returned by Engine.Query.
type QueryResult struct {
	Hits []QueryHit

	// RerankFailed (H09) is true iff reranking was attempted for this query and
	// failed (rerank error or score-count mismatch). When true, Hits are in
	// fallback (RRF) order, not reranked order, and a failure was logged. False
	// covers rerank-succeeded, reranking-not-configured, and empty-pool — those
	// non-failure cases are not distinguished on the response. Surfaced 1:1 by
	// every transport (FR-002/004).
	RerankFailed bool
}

// StatusInfo is the structured database health/count view.
type StatusInfo struct {
	Documents          int
	Chunks             int
	Embeddings         int
	Dimensions         int    // stored majority dimensionality (0 if no embeddings)
	EmbeddingModel     string // stored majority model (falls back to configured when no embeddings)
	Reranker           string // "disabled" when unset
	OllamaURL          string
	EmbeddingsComplete bool
	EmbeddingDrift     bool           // true if >1 model, dimensionality, OR convention is stored (audit H03/H07)
	ModelCounts        map[string]int // per-model record counts (drift detail)
	DimCounts          map[int]int    // per-dimensionality record counts (drift detail)
	EmbeddingConvention     string         // stored majority instruction-prefix convention (audit H07)
	EmbeddingConventionDrift bool         // true if >1 prefix convention is stored (audit H07)
	ConventionCounts   map[string]int // per-convention record counts (H07 drift detail)
	ConfiguredPrefix   string         // active prefix mode resolved from config (auto|on|off)
	QueryPrefix        string         // resolved query-role prefix (empty when none in effect)
	DocPrefix          string         // resolved document-role prefix (empty when none in effect)
	ResultCache    CacheStats // H06/spec 016: query result-cache stats (enabled/size/capacity/hits/misses)
	EmbeddingCache CacheStats // H06/spec 016: query-embedding-cache stats
}

// IngestSummary describes one ingest/scan/reprocess/migrate run. Modified and
// Deleted are populated only by Scan (filesystem change detection); they are 0
// for Add/Reprocess/Migrate.
type IngestSummary struct {
	New      int
	Skipped  int
	Modified int
	Deleted  int
	Errors   int
}

// FileEntry is one ingested file, for Files().
type FileEntry struct {
	FilePath   string
	FileType   string
	Status     string
	ChunkCount int
}

// DirEntry aggregates per-directory counts, for Dirs().
type DirEntry struct {
	Dir    string
	Files  int
	Chunks int
}

// VaultEntry is one vault with its document count, for ListVaults().
type VaultEntry struct {
	Name      string
	Documents int
}
