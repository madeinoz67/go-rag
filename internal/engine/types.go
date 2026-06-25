// Package engine is go-rag's unified operation surface — the single layer every
// transport adapter (CLI, MCP, REST, gRPC) calls. It owns no new storage or
// indexing logic; it orchestrates the existing pipeline / index / storage /
// embed packages and returns structured results. Because every adapter invokes
// the same methods here, REST, gRPC, and MCP are guaranteed to agree
// (cross-transport parity, spec 003 FR-002/003).
package engine

import (
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/model"
)

// QueryRequest is the input to Engine.Query.
type QueryRequest struct {
	Query         string
	K             int
	Mode          string // "hybrid" (default) | "semantic" | "keyword"
	NoRerank      bool
	Threshold     float64       // minimum score; hits below are dropped
	RRFK          int           // H08/spec 009: per-query RRF constant override; 0 = use config EffectiveRRFK() (default 60)
	PoolSize      int           // H22/spec 024: per-query candidate-pool override; 0 = config EffectivePoolSize() (default 60), or classifier-derived when adaptive depth is on
	Filter        *index.Filter // H14/spec 014: optional metadata filter (source/type/tags); nil = no filter
	ContextWindow int           // H15/spec 015: N sibling chunks each side of a hit; 0 = off (default)
	NoCache       bool          // H06/spec 016: bypass serving from the result cache for this call (still stores on success)
	// IncludeQuarantined (H04/spec 019): when false (default) chunks whose poisoning
	// verdict is suspicious/quarantine are excluded from results (quarantine-by-
	// default, Q1=A). When true they are returned, each carrying its verdict so a
	// downstream LLM consumer can treat the text as untrusted (FR-005).
	IncludeQuarantined bool
	// Dedup (H20/spec 026): when true, collapse near-duplicate hits to one
	// representative per group (highest-scored). Default false (flag-only).
	Dedup bool
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
	Page       int            // Chunk.PageNumber; 0 when not paginated
	ChunkIndex int            // H21/spec 023: 0-based ordinal within the source document
	Preview    string         // convenience truncated preview for text renders
	Context    []ContextChunk // H15/spec 015: sibling chunks for reading context; nil when ContextWindow=0
	// Poisoning is the per-chunk injection-poisoning verdict (H04/spec 019),
	// surfaced on every hit so a downstream LLM consumer can treat retrieved
	// text as untrusted. nil on chunks ingested before the feature or with
	// detection disabled (treat as clean). Always populated when the chunk was
	// scored. Surfaced 1:1 by every transport (FR-005).
	Poisoning *model.PoisonVerdict
	// SectionContext is the ordered heading breadcrumb active at this chunk's
	// start position (top-level → governing heading), e.g. ["Operations",
	// "Backups", "Retention"]. nil/absent for chunks with no section context
	// (heading-less source, or a chunk ingested before H23/spec 025). Surfaced
	// 1:1 by every transport (FR-004).
	SectionContext []string
	// NearDup is this hit's near-duplicate context (audit H20 / spec 026):
	// sibling chunkIDs (pairwise) and closest similarity. nil/absent for chunks
	// with no near-dups or pre-feature. Surfaced 1:1 by every transport (FR-004);
	// consumed by opt-in collapse (FR-005).
	NearDup *model.NearDupInfo
	// Summary is the document's auto-generated one-line summary (spec 029),
	// surfaced on the hit. Empty when the doc is unenriched / off / pre-feature —
	// omitted, never an error. Surfaced 1:1 by every transport (FR-010).
	Summary string
	// EnrichmentStatus is the document's enrichment status (spec 029):
	// enriched|failed|nothing-to-enrich. Empty when unenriched. Surfaced 1:1 by
	// every transport (FR-010).
	EnrichmentStatus string
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

	// EffectiveK / EffectivePool / EffectiveMode (H22/spec 024, US3): the depth,
	// pool, and mode ACTUALLY used for this query — explicit | recommended |
	// default for K, per-query | classifier-derived | config for Pool, and the
	// parsed Mode (mode is never changed by H22). Surfaced so an operator can see
	// whether a per-query override or the classifier acted. Echoed 1:1 by every
	// transport; additive, so pre-H22 clients parsing the response are unaffected.
	EffectiveK    int
	EffectivePool int
	EffectiveMode string
}

// StatusInfo is the structured database health/count view.
type StatusInfo struct {
	Documents                int
	Chunks                   int
	Embeddings               int
	Dimensions               int    // stored majority dimensionality (0 if no embeddings)
	EmbeddingModel           string // stored majority model (falls back to configured when no embeddings)
	Reranker                 string // "disabled" when unset
	OllamaURL                string
	EmbeddingsComplete       bool
	EmbeddingDrift           bool           // true if >1 model, dimensionality, OR convention is stored (audit H03/H07)
	ModelCounts              map[string]int // per-model record counts (drift detail)
	DimCounts                map[int]int    // per-dimensionality record counts (drift detail)
	EmbeddingConvention      string         // stored majority instruction-prefix convention (audit H07)
	EmbeddingConventionDrift bool           // true if >1 prefix convention is stored (audit H07)
	ConventionCounts         map[string]int // per-convention record counts (H07 drift detail)
	ConfiguredPrefix         string         // active prefix mode resolved from config (auto|on|off)
	QueryPrefix              string         // resolved query-role prefix (empty when none in effect)
	DocPrefix                string         // resolved document-role prefix (empty when none in effect)
	ResultCache              CacheStats     // H06/spec 016: query result-cache stats (enabled/size/capacity/hits/misses)
	EmbeddingCache           CacheStats     // H06/spec 016: query-embedding-cache stats

	// H11/spec 017: embedding-drift monitoring. The corpus baseline (profile the
	// corpus was built under) vs live, plus the drift verdict. Orthogonal to the
	// H03/H07 intra-corpus drift fields above (those report mixed records within
	// the corpus; these report corpus-vs-config + Ollama-version drift).
	CorpusBaselineModel      string
	CorpusBaselineDim        int
	CorpusBaselineConvention string
	CorpusBaselineOllamaVer  string
	CorpusBaselineRecordedAt string // RFC3339
	LiveOllamaVersion        string // "" offline, "unknown" unreachable
	DriftVerdict             string // clean|hard-drift|version-warning|unknown|n/a
	HardDrift                bool   // true on model/dim/convention mismatch
	VersionDrift             bool   // true on Ollama-version change (soft)

	// H04/spec 019: retrieval-poisoning detection summary. Surfaced on `status`
	// so an operator sees whether detection is on, the thresholds, how many chunks
	// are currently flagged, and the threat-source/merged-list state.
	PoisoningEnabled   bool    // detection on (Q2=A default)
	PoisonThresholdSus float64 // effective suspicious threshold
	PoisonThresholdQua float64 // effective quarantine threshold
	PoisonFlagged      int     // chunks currently flagged (suspicious/quarantine)
	PoisonSources      int     // managed threat sources (excl. the built-in list)
	PoisonPhrases      int     // merged phrase-list size (built-in + enabled sources)

	// H22/spec 024: adaptive-retrieval observability. The configured pool ceiling
	// and the classifier posture, plus an aggregate pool-utilization signal so an
	// operator can size the pool. Effective per-query depth/pool/mode live on
	// QueryResult (US3); these are the system-level knobs (US3 scenario 1).
	PoolSize             int             // effective configured candidate-pool ceiling (cfg.EffectivePoolSize())
	AdaptiveDepthEnabled bool            // rule-based k-classifier posture (default false)
	PoolUtilization      PoolUtilization // aggregate, process-lifetime (not per-query)
	NearDupChunks        int             // H20/spec 026: chunks with near-dup siblings (eventually consistent)

	// spec 029: document enrichment observability (background tags + summary).
	EnrichmentEnabled bool // background enrichment on (opt-in, default false)
	EnrichedDocs      int  // documents with a non-nil Enrichment sidecar (eventually consistent)
}

// PoolUtilization is the aggregate candidate-pool consumption signal surfaced in
// Status (audit H22 / spec 024, FR-003). It is an AGGREGATE over the process
// lifetime (NOT attached to individual query responses — clarification Q3). It
// lets an operator size the pool: AvgFetched/AvgKept is the candidate-expansion
// ratio, and Saturated flags queries where the pool could not be filled (short
// corpus / reranker absent). In-memory only; cleared on restart.
type PoolUtilization struct {
	Queries    uint64  // observed (non-cached) queries since start (denominator; 0 ⇒ averages zero)
	AvgFetched float64 // mean effective pool actually fetched
	AvgKept    float64 // mean results returned (AvgFetched/AvgKept = expansion ratio)
	Saturated  uint64  // queries where the pool couldn't be filled (short corpus / reranker absent)
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
