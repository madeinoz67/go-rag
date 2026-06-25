// Package config loads and saves the persisted go-rag configuration
// (PRD §5.7), stored as JSON in .go-rag/config.json.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/madeinoz67/go-rag/internal/embed"
)

// Config is the set of configurable options (PRD §5.7 table).
type Config struct {
	OllamaURL            string   `json:"ollama_url"`
	EmbeddingModel       string   `json:"embedding_model,omitempty"`
	EmbeddingPrefix      string   `json:"embedding_prefix,omitempty"`       // H07: auto|on|off ("" = auto)
	EmbeddingQueryPrefix string   `json:"embedding_query_prefix,omitempty"` // H07: explicit query-prefix override
	EmbeddingDocPrefix   string   `json:"embedding_doc_prefix,omitempty"`   // H07: explicit document-prefix override
	WatchDirs            []string `json:"watch_dirs"`
	ChunkSize            int      `json:"chunk_size"`
	ChunkOverlap         int      `json:"chunk_overlap"`
	DBPath               string   `json:"db_path"`
	FileGlob             string   `json:"file_glob"`
	PollIntervalSec      int      `json:"poll_interval_secs"`
	MCPAddr              string   `json:"mcp_addr"`
	MCPToken             string   `json:"mcp_token,omitempty"`
	RerankModel          string   `json:"rerank_model,omitempty"`
	RerankCandidates     int      `json:"rerank_candidates,omitempty"`
	RerankRetryOnFailure bool     `json:"rerank_retry_on_failure,omitempty"`
	RRFK                 int      `json:"rrf_k,omitempty"`                  // H08/spec 009: RRF smoothing constant; 0 = default (60); <0 invalid
	QueryCacheEnabled    bool     `json:"query_cache_enabled,omitempty"`    // H06/spec 016: global cache kill-switch; default true (omitted → true at runtime via EffectiveQueryCache*); false disables both caches
	QueryCacheResults    int      `json:"query_cache_results,omitempty"`    // H06/spec 016: result-cache capacity (entries); 0 = result cache off; <0 invalid
	QueryCacheEmbeddings int      `json:"query_cache_embeddings,omitempty"` // H06/spec 016: query-embedding-cache capacity (entries); 0 = embedding cache off; <0 invalid

	// H22/spec 024: adaptive retrieval. pool_size is the configured candidate-pool
	// ceiling (FTS/vector fetch + rerank candidate budget) — promoted from the
	// previously-hardcoded 60. 0 (or absent) ⇒ DefaultPoolSize (60) via
	// EffectivePoolSize(); per-query overrides + classifier-driven shrinking clamp
	// against it. adaptive_depth_enabled (default false) gates the rule-based query
	// classifier that recommends retrieval depth k when the caller has not set it
	// (mode is never recommended). Default-off ⇒ byte-identical pre-H22 behavior.
	PoolSize             int  `json:"pool_size,omitempty"`              // H22/spec 024: candidate-pool ceiling; 0 = default 60; <0 invalid
	AdaptiveDepthEnabled bool `json:"adaptive_depth_enabled,omitempty"` // H22/spec 024: rule-based k-classifier on/off (default off)

	// spec 029: background document enrichment (local model; opt-in, default off).
	EnrichmentEnabled bool   `json:"enrichment_enabled,omitempty"` // default false; true enables background doc tag+summary
	EnrichmentModel  string `json:"enrichment_model,omitempty"`   // the local generation model for tags+summary

	// H04/spec 019: retrieval-poisoning (indirect prompt injection) detection.
	// Detection scores every chunk at ingest and quarantines flagged chunks out
	// of default query results (Q1=A). Detection is default-on (Q2=A) — the blind
	// spot is closed out of the box — and configurable off.
	PoisoningEnabled             bool    `json:"poisoning_enabled,omitempty"`              // default true (Q2=A); false disables detection
	PoisoningThresholdSuspicious float64 `json:"poisoning_threshold_suspicious,omitempty"` // 0 = default 0.40
	PoisoningThresholdQuarantine float64 `json:"poisoning_threshold_quarantine,omitempty"` // 0 = default 0.70
	PoisoningPhraseList          string  `json:"poisoning_phrase_list,omitempty"`          // path to an override instruction-phrase source (D9/D12)

	// H17/spec 020: observability (OpenTelemetry). Metrics expose a scraped /metrics
	// endpoint (loopback, default on); traces export to a LOCAL sink by default, with
	// OTLP remote export opt-in only (Constitution I — zero telemetry egress unless set).
	MetricsEnabled bool   `json:"metrics_enabled,omitempty"` // default true; false disables the /metrics endpoint
	OTelExport     string `json:"otel_export,omitempty"`     // none|stdout (default)|otlp
	OTelEndpoint   string `json:"otel_endpoint,omitempty"`   // OTLP endpoint; used only when otel_export=otlp

	// H18/spec 021: structured audit log (local, append-only JSONL). Default-on
	// (security); query text is hashed, never plaintext; rotation bounds growth.
	AuditLogEnabled  bool   `json:"audit_log_enabled,omitempty"`   // default true; false disables auditing
	AuditLogMaxBytes int    `json:"audit_log_max_bytes,omitempty"` // 0 = default (~16 MiB)
	AuditPath        string `json:"audit_path,omitempty"`          // optional override of the audit log path

	// H19/spec 022: opt-in regex secret/PII redaction at ingest. Default off;
	// when enabled, secrets are replaced with placeholders before indexing.
	PIIRedactEnabled bool   `json:"pii_redact_enabled,omitempty"` // default false (opt-in)
	PIIPatterns      string `json:"pii_patterns,omitempty"`       // path to a custom patterns file

	// H20/spec 026: near-duplicate chunk detection. near_dup_hamming is the SimHash
	// Hamming-distance threshold (bits); two chunks within this distance are
	// near-duplicates. 0 (or absent) ⇒ DefaultNearDupHamming (3). Detection is
	// always on (pure-Go, ACK-path); collapse is opt-in per query (dedup flag).
	NearDupHamming int `json:"near_dup_hamming,omitempty"` // H20/spec 026: SimHash Hamming threshold; 0 = default 3; <0 invalid
}

// Default returns the configuration used by `go-rag init` when no overrides apply.
func Default() Config {
	return Config{
		OllamaURL:                    "http://localhost:11434",
		EmbeddingPrefix:              "auto", // H07: derive instruction prefixes from the model by default
		WatchDirs:                    []string{"."},
		ChunkSize:                    512,
		ChunkOverlap:                 50,
		DBPath:                       "./.go-rag",
		FileGlob:                     "*",
		PollIntervalSec:              60,
		MCPAddr:                      "127.0.0.1:7878", // loopback by default (spec 007, audit H13); never all-interfaces
		RerankCandidates:             20,
		RRFK:                         60,              // H08/spec 009: standard single-k RRF default (retrieval book §6.6)
		PoolSize:                     DefaultPoolSize, // H22/spec 024: today's hardcoded candidate pool (60) as the configured ceiling
		QueryCacheEnabled:            true,            // H06/spec 016: caching on by default (transparent; escape hatches exist)
		QueryCacheResults:            DefaultQueryCacheResults,
		QueryCacheEmbeddings:         DefaultQueryCacheEmbeddings,
		PoisoningEnabled:             true, // Q2=A: detection default-on (closes the P0 blind spot out of the box)
		PoisoningThresholdSuspicious: DefaultPoisonThresholdSuspicious,
		PoisoningThresholdQuarantine: DefaultPoisonThresholdQuarantine,
		MetricsEnabled:               true,                    // H17: /metrics on by default (loopback, scraped)
		OTelExport:                   DefaultOTelExport,       // H17: local stdout trace exporter by default
		AuditLogEnabled:              true,                    // H18: audit on by default
		AuditLogMaxBytes:             DefaultAuditLogMaxBytes, // H18: ~16 MiB rotation cap

		NearDupHamming: DefaultNearDupHamming, // H20/spec 026: SimHash Hamming threshold
	}
}

// DefaultRRFK is the RRF smoothing constant used when rrf_k is unset (spec 009).
const DefaultRRFK = 60

// DefaultPoolSize is the reranker candidate-pool ceiling used when pool_size is
// unset (spec 024 / audit H22). It is today's hardcoded value (60) so the
// default-off posture is byte-identical to pre-H22.
const DefaultPoolSize = 60

// Default query-cache capacities (spec 016 / audit H06). Kept modest so the
// combined worst-case footprint stays well inside the memory budget (≈3–4 MB).
const (
	DefaultQueryCacheResults    = 256
	DefaultQueryCacheEmbeddings = 512
)

// Default poisoning thresholds (spec 019 / audit H04, research D8). Starting
// points, tuned against a payload fixture via SC-001 (≥95% recall) and SC-002
// (≤5% false positive on a clean + security-writeup corpus).
const (
	DefaultPoisonThresholdSuspicious = 0.40
	DefaultPoisonThresholdQuarantine = 0.70

	// DefaultNearDupHamming: SimHash Hamming threshold for near-dup detection
	// (spec 026 / audit H20); conservative (3 of 64 bits ≈ high similarity) to
	// protect precision (FR-009).
	DefaultNearDupHamming = 3
)

// DefaultOTelExport is the trace exporter used when otel_export is unset (spec 020):
// "stdout" — a LOCAL sink. OTLP ("otlp", remote) is opt-in; "none" disables traces.
const DefaultOTelExport = "stdout"

// EffectiveMetricsEnabled reports whether the /metrics endpoint is exposed (spec 020).
// Defaults to true: an absent metrics_enabled key is treated as on via Load(); an
// explicit false disables the endpoint.
func (c Config) EffectiveMetricsEnabled() bool { return c.MetricsEnabled }

// EffectiveOTelExport returns the configured trace exporter, or the default ("stdout",
// local) when unset. Air-gap posture: "stdout"/"none" are local; "otlp" is the only
// remote (opt-in) path.
func (c Config) EffectiveOTelExport() string {
	if c.OTelExport == "" {
		return DefaultOTelExport
	}
	return c.OTelExport
}

// DefaultAuditLogMaxBytes is the audit-log rotation cap when audit_log_max_bytes is
// unset (spec 021): ~16 MiB. Keep-last-3 archives ⇒ ~64 MiB worst case.
const DefaultAuditLogMaxBytes = 16 << 20

// EffectiveAuditLogEnabled reports whether auditing is on (spec 021). Default true
// (absent key ⇒ on via Load); explicit false disables.
func (c Config) EffectiveAuditLogEnabled() bool { return c.AuditLogEnabled }

// EffectiveAuditLogMaxBytes returns the rotation cap, or the default (~16 MiB) when
// unset/non-positive.
func (c Config) EffectiveAuditLogMaxBytes() int {
	if c.AuditLogMaxBytes > 0 {
		return c.AuditLogMaxBytes
	}
	return DefaultAuditLogMaxBytes
}

// EffectiveRRFK returns the effective RRF smoothing constant: the configured
// rrf_k when positive, else DefaultRRFK (60). This is the single resolution site
// for the "absent key = default" rule, so an existing config that omits rrf_k
// (which unmarshals to 0) keeps working.
func (c Config) EffectiveRRFK() int {
	if c.RRFK > 0 {
		return c.RRFK
	}
	return DefaultRRFK
}

// EffectivePoolSize returns the configured candidate-pool ceiling: the configured
// pool_size when positive, else DefaultPoolSize (60). This is the single
// resolution site for the "absent/zero key = default 60" rule, so an existing
// config that omits pool_size (which unmarshals to 0) keeps today's behavior.
func (c Config) EffectivePoolSize() int {
	if c.PoolSize > 0 {
		return c.PoolSize
	}
	return DefaultPoolSize
}

// EffectiveAdaptiveDepthEnabled reports whether the rule-based query classifier
// recommends retrieval depth (spec 024 / audit H22). Default false (off): an
// absent adaptive_depth_enabled key is treated as off via the bool zero value, so
// existing configs keep pre-H22 behavior.
func (c Config) EffectiveAdaptiveDepthEnabled() bool { return c.AdaptiveDepthEnabled }

// EffectivePoisoningEnabled reports whether detection runs. Defaults to true
// (Q2=A): an absent poisoning_enabled key is treated as on via the Load()
// backward-compat path; an explicit false disables detection (chunks are
// ingested/queried as clean, no verdict computed).
func (c Config) EffectivePoisoningEnabled() bool { return c.PoisoningEnabled }

// EffectiveEnrichmentEnabled reports whether background document enrichment
// (spec 029) runs. Defaults to false (opt-in): enrichment consumes local model
// resources and needs a tagging model pulled, so it is off until an operator
// enables it. When false the system makes zero enrichment model calls and is
// byte-identical to today.
func (c Config) EffectiveEnrichmentEnabled() bool { return c.EnrichmentEnabled }

// EffectivePoisonThresholdSuspicious returns the configured suspicious threshold
// when positive, else the default (0.40).
func (c Config) EffectivePoisonThresholdSuspicious() float64 {
	if c.PoisoningThresholdSuspicious > 0 {
		return c.PoisoningThresholdSuspicious
	}
	return DefaultPoisonThresholdSuspicious
}

// EffectivePoisonThresholdQuarantine returns the configured quarantine threshold
// when positive, else the default (0.70).
func (c Config) EffectivePoisonThresholdQuarantine() float64 {
	if c.PoisoningThresholdQuarantine > 0 {
		return c.PoisoningThresholdQuarantine
	}
	return DefaultPoisonThresholdQuarantine
}

// EffectiveNearDupHamming returns the SimHash Hamming-distance threshold for
// near-duplicate detection (spec 026 / audit H20): the configured value when
// positive, else DefaultNearDupHamming (3).
func (c Config) EffectiveNearDupHamming() int {
	if c.NearDupHamming > 0 {
		return c.NearDupHamming
	}
	return DefaultNearDupHamming
}

// Validate returns an error if the config has invalid values.
func (c Config) Validate() error {
	u, err := url.Parse(c.OllamaURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid ollama_url: %q", c.OllamaURL)
	}
	if c.ChunkSize <= 0 {
		return fmt.Errorf("chunk_size must be positive")
	}
	if c.ChunkOverlap < 0 {
		return fmt.Errorf("chunk_overlap must be non-negative")
	}
	if c.PollIntervalSec <= 0 {
		return fmt.Errorf("poll_interval_secs must be positive")
	}
	if c.RRFK < 0 {
		return fmt.Errorf("rrf_k must be non-negative (0 = default %d)", DefaultRRFK)
	}
	if c.PoolSize < 0 {
		return fmt.Errorf("pool_size must be non-negative (0 = default %d)", DefaultPoolSize)
	}
	if c.QueryCacheResults < 0 {
		return fmt.Errorf("query_cache_results must be non-negative (0 = result cache disabled)")
	}
	if c.QueryCacheEmbeddings < 0 {
		return fmt.Errorf("query_cache_embeddings must be non-negative (0 = embedding cache disabled)")
	}
	if c.PoisoningThresholdSuspicious < 0 || c.PoisoningThresholdSuspicious > 1 {
		return fmt.Errorf("poisoning_threshold_suspicious must be in [0,1] (0 = default)")
	}
	if c.PoisoningThresholdQuarantine < 0 || c.PoisoningThresholdQuarantine > 1 {
		return fmt.Errorf("poisoning_threshold_quarantine must be in [0,1] (0 = default)")
	}
	if c.NearDupHamming < 0 {
		return fmt.Errorf("near_dup_hamming must be non-negative (0 = default %d)", DefaultNearDupHamming)
	}

	if c.EffectivePoisonThresholdSuspicious() > c.EffectivePoisonThresholdQuarantine() {
		return fmt.Errorf("poisoning_threshold_suspicious (%.2f) must be <= poisoning_threshold_quarantine (%.2f)",
			c.EffectivePoisonThresholdSuspicious(), c.EffectivePoisonThresholdQuarantine())
	}
	// H17/spec 020: otel_export must be a known mode; otlp requires an endpoint.
	switch c.EffectiveOTelExport() {
	case "none", "stdout", "otlp":
	default:
		return fmt.Errorf("invalid otel_export: %q (want none|stdout|otlp)", c.OTelExport)
	}
	if c.EffectiveOTelExport() == "otlp" && c.OTelEndpoint == "" {
		return fmt.Errorf("otel_endpoint is required when otel_export=otlp")
	}
	if c.AuditLogMaxBytes < 0 {
		return fmt.Errorf("audit_log_max_bytes must be non-negative (0 = default)")
	}
	if c.MCPAddr != "" {
		if _, _, err := net.SplitHostPort(c.MCPAddr); err != nil {
			return fmt.Errorf("invalid mcp_addr: %q", c.MCPAddr)
		}
	}
	return nil
}

// Load reads config from a JSON file.
func Load(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse config %q: %w", path, err)
	}
	// Backward compat: old configs used "ollama_model" instead of "embedding_model".
	if c.EmbeddingModel == "" {
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		if v, ok := raw["ollama_model"]; ok {
			c.EmbeddingModel = fmt.Sprintf("%v", v)
		}
	}
	// H06/spec 016 backward compat: the query-cache keys default to ON with
	// non-zero capacities, but a bool/int's zero value after unmarshal can't
	// distinguish "absent" from "explicitly false/0". An old config (pre-016)
	// omits all three, which would otherwise silently disable caching on
	// upgrade. Treat an ABSENT key as the default; a PRESENT key (including an
	// explicit 0 to disable one cache) is honored verbatim.
	var raw map[string]any
	if json.Unmarshal(data, &raw) == nil {
		if _, ok := raw["query_cache_enabled"]; !ok {
			c.QueryCacheEnabled = true
		}
		if _, ok := raw["query_cache_results"]; !ok {
			c.QueryCacheResults = DefaultQueryCacheResults
		}
		if _, ok := raw["query_cache_embeddings"]; !ok {
			c.QueryCacheEmbeddings = DefaultQueryCacheEmbeddings
		}
		// H04/spec 019 backward compat: poisoning_enabled defaults to true (Q2=A).
		// A pre-019 config omits it; an absent key stays on, an explicit false disables.
		if _, ok := raw["poisoning_enabled"]; !ok {
			c.PoisoningEnabled = true
		}
		// H17/spec 020 backward compat: metrics_enabled defaults to true (/metrics on).
		if _, ok := raw["metrics_enabled"]; !ok {
			c.MetricsEnabled = true
		}
		// H18/spec 021 backward compat: audit_log_enabled defaults to true.
		if _, ok := raw["audit_log_enabled"]; !ok {
			c.AuditLogEnabled = true
		}
	}
	return c, nil
}

// Save writes config to a JSON file (creating parent dirs).
func Save(path string, c Config) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Get returns the string value of a config key, or ok=false if unknown.
func (c Config) Get(key string) (string, bool) {
	switch key {
	case "ollama_url":
		return c.OllamaURL, true
	case "embedding_model":
		return c.EmbeddingModel, true
	case "embedding_prefix":
		if c.EmbeddingPrefix == "" {
			return "auto", true // "" resolves to auto
		}
		return c.EmbeddingPrefix, true
	case "embedding_query_prefix":
		return c.EmbeddingQueryPrefix, true
	case "embedding_doc_prefix":
		return c.EmbeddingDocPrefix, true
	case "db_path":
		return c.DBPath, true
	case "file_glob":
		return c.FileGlob, true
	case "chunk_size":
		return strconv.Itoa(c.ChunkSize), true
	case "chunk_overlap":
		return strconv.Itoa(c.ChunkOverlap), true
	case "poll_interval_secs":
		return strconv.Itoa(c.PollIntervalSec), true
	case "mcp_addr":
		return c.MCPAddr, true
	case "mcp_token":
		return c.MCPToken, true
	case "rerank_model":
		return c.RerankModel, true
	case "rerank_candidates":
		return strconv.Itoa(c.RerankCandidates), true
	case "rerank_retry_on_failure":
		return strconv.FormatBool(c.RerankRetryOnFailure), true
	case "rrf_k":
		return strconv.Itoa(c.EffectiveRRFK()), true
	case "pool_size":
		return strconv.Itoa(c.EffectivePoolSize()), true
	case "adaptive_depth_enabled":
		return strconv.FormatBool(c.EffectiveAdaptiveDepthEnabled()), true
	case "query_cache_enabled":
		return strconv.FormatBool(c.QueryCacheEnabled), true
	case "query_cache_results":
		return strconv.Itoa(c.QueryCacheResults), true
	case "query_cache_embeddings":
		return strconv.Itoa(c.QueryCacheEmbeddings), true
	case "poisoning_enabled":
		return strconv.FormatBool(c.EffectivePoisoningEnabled()), true
	case "poisoning_threshold_suspicious":
		return strconv.FormatFloat(c.EffectivePoisonThresholdSuspicious(), 'f', -1, 64), true
	case "poisoning_threshold_quarantine":
		return strconv.FormatFloat(c.EffectivePoisonThresholdQuarantine(), 'f', -1, 64), true
	case "poisoning_phrase_list":
		return c.PoisoningPhraseList, true
	case "metrics_enabled":
		return strconv.FormatBool(c.EffectiveMetricsEnabled()), true
	case "otel_export":
		return c.EffectiveOTelExport(), true
	case "otel_endpoint":
		return c.OTelEndpoint, true
	case "audit_log_enabled":
		return strconv.FormatBool(c.EffectiveAuditLogEnabled()), true
	case "audit_log_max_bytes":
		return strconv.Itoa(c.EffectiveAuditLogMaxBytes()), true
	case "audit_path":
		return c.AuditPath, true
	case "pii_redact_enabled":
		return strconv.FormatBool(c.PIIRedactEnabled), true
	case "pii_patterns":
		return c.PIIPatterns, true
	}
	return "", false
}

// Set assigns a string value to a config key, returning an error on an unknown
// key or an invalid value (non-numeric / non-positive where required).
func (c *Config) Set(key, val string) error {
	switch key {
	case "ollama_url":
		c.OllamaURL = val
	case "embedding_model":
		c.EmbeddingModel = val
	case "embedding_prefix":
		if val != "" {
			switch val {
			case "auto", "on", "off":
			default:
				return fmt.Errorf("invalid embedding_prefix: %q (want auto|on|off)", val)
			}
		}
		c.EmbeddingPrefix = val
	case "embedding_query_prefix":
		if err := validatePrefixString(val); err != nil {
			return err
		}
		c.EmbeddingQueryPrefix = val
	case "embedding_doc_prefix":
		if err := validatePrefixString(val); err != nil {
			return err
		}
		c.EmbeddingDocPrefix = val
	case "db_path":
		c.DBPath = val
	case "file_glob":
		c.FileGlob = val
	case "chunk_size":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid chunk_size: %q", val)
		}
		c.ChunkSize = n
	case "chunk_overlap":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid chunk_overlap: %q", val)
		}
		c.ChunkOverlap = n
	case "poll_interval_secs":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid poll_interval_secs: %q", val)
		}
		c.PollIntervalSec = n
	case "mcp_addr":
		c.MCPAddr = val
	case "mcp_token":
		c.MCPToken = val
	case "rerank_model":
		c.RerankModel = val
	case "rerank_candidates":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid rerank_candidates: %q", val)
		}
		c.RerankCandidates = n
	case "rerank_retry_on_failure":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid rerank_retry_on_failure: %q", val)
		}
		c.RerankRetryOnFailure = b
	case "rrf_k":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid rrf_k: %q (want a non-negative integer; 0 = default %d)", val, DefaultRRFK)
		}
		c.RRFK = n
	case "pool_size":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid pool_size: %q (want a non-negative integer; 0 = default %d)", val, DefaultPoolSize)
		}
		c.PoolSize = n
	case "adaptive_depth_enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid adaptive_depth_enabled: %q", val)
		}
		c.AdaptiveDepthEnabled = b
	case "query_cache_enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid query_cache_enabled: %q", val)
		}
		c.QueryCacheEnabled = b
	case "query_cache_results":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid query_cache_results: %q (want a non-negative integer; 0 = disabled)", val)
		}
		c.QueryCacheResults = n
	case "query_cache_embeddings":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid query_cache_embeddings: %q (want a non-negative integer; 0 = disabled)", val)
		}
		c.QueryCacheEmbeddings = n
	case "poisoning_enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid poisoning_enabled: %q", val)
		}
		c.PoisoningEnabled = b
	case "poisoning_threshold_suspicious":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil || f < 0 || f > 1 {
			return fmt.Errorf("invalid poisoning_threshold_suspicious: %q (want a float in [0,1]; 0 = default)", val)
		}
		c.PoisoningThresholdSuspicious = f
	case "poisoning_threshold_quarantine":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil || f < 0 || f > 1 {
			return fmt.Errorf("invalid poisoning_threshold_quarantine: %q (want a float in [0,1]; 0 = default)", val)
		}
		c.PoisoningThresholdQuarantine = f
	case "poisoning_phrase_list":
		c.PoisoningPhraseList = val
	case "metrics_enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid metrics_enabled: %q", val)
		}
		c.MetricsEnabled = b
	case "otel_export":
		switch val {
		case "", "none", "stdout", "otlp":
		default:
			return fmt.Errorf("invalid otel_export: %q (want none|stdout|otlp)", val)
		}
		c.OTelExport = val
	case "otel_endpoint":
		c.OTelEndpoint = val
	case "audit_log_enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid audit_log_enabled: %q", val)
		}
		c.AuditLogEnabled = b
	case "audit_log_max_bytes":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid audit_log_max_bytes: %q (want a non-negative integer; 0 = default)", val)
		}
		c.AuditLogMaxBytes = n
	case "audit_path":
		c.AuditPath = val
	case "pii_redact_enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid pii_redact_enabled: %q", val)
		}
		c.PIIRedactEnabled = b
	case "pii_patterns":
		c.PIIPatterns = val
	default:
		return fmt.Errorf("unknown config key: %q", key)
	}
	return nil
}

// validatePrefixString rejects instruction-prefix strings that would embed
// degenerate input — control characters and newlines (audit H07 edge case). An
// empty string is valid (it clears the override / means "derive").
func validatePrefixString(s string) error {
	for _, r := range s {
		if r == '\n' || r == '\r' {
			return fmt.Errorf("invalid prefix (contains newline): %q", s)
		}
		if r < 0x20 {
			return fmt.Errorf("invalid prefix (contains control character): %q", s)
		}
	}
	return nil
}

// Prefixer builds the instruction-prefix resolver from the configured embedding
// settings (audit H07). Centralized here so the ingest pipeline and the query
// path build identical prefixers — a document ingested by any transport gets the
// same document prefix, and a query gets the same query prefix (cross-transport
// parity, FR-009). A defensively-invalid mode falls back to auto.
func (c Config) Prefixer() *embed.Prefixer {
	mode, err := embed.ParseMode(c.EmbeddingPrefix)
	if err != nil {
		mode = embed.ModeAuto
	}
	return embed.NewPrefixer(c.EmbeddingModel, mode, c.EmbeddingQueryPrefix, c.EmbeddingDocPrefix)
}
