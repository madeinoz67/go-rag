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
	EmbeddingPrefix      string   `json:"embedding_prefix,omitempty"`     // H07: auto|on|off ("" = auto)
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
}

// Default returns the configuration used by `go-rag init` when no overrides apply.
func Default() Config {
	return Config{
		OllamaURL:        "http://localhost:11434",
		EmbeddingPrefix:  "auto", // H07: derive instruction prefixes from the model by default
		WatchDirs:        []string{"."},
		ChunkSize:        512,
		ChunkOverlap:     50,
		DBPath:           "./.go-rag",
		FileGlob:         "*",
		PollIntervalSec:  60,
		MCPAddr:          "127.0.0.1:7878", // loopback by default (spec 007, audit H13); never all-interfaces
		RerankCandidates: 20,
	}
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
