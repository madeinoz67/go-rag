package engine

import (
	"fmt"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// knownConfigKeys is the public, ordered set of config keys surfaced to
// consumers (mirrors the former mcp/cli config listing).
var knownConfigKeys = []string{
	"ollama_url", "embedding_model",
	"embedding_prefix", "embedding_query_prefix", "embedding_doc_prefix", // H07
	"chunk_size", "chunk_overlap",
	"db_path", "poll_interval_secs",
	"query_cache_enabled", "query_cache_results", "query_cache_embeddings", // H06/spec 016
	"pool_size", "adaptive_depth_enabled", // H22/spec 024
}

// GetConfig returns config values as strings. If key is non-empty, only that
// key is returned (error if unknown); otherwise all known keys are returned.
func (e *Engine) GetConfig(_, key string) (map[string]string, error) {
	if key != "" {
		v, ok := e.cfg.Get(key)
		if !ok {
			return nil, fmt.Errorf("unknown key %q: %w", key, ErrInvalid)
		}
		return map[string]string{key: v}, nil
	}
	out := map[string]string{}
	for _, k := range knownConfigKeys {
		if v, ok := e.cfg.Get(k); ok {
			out[k] = v
		}
	}
	return out, nil
}

// SetConfig updates one config value, validates it, persists it to the
// database's config.json, and updates the engine's in-memory config.
func (e *Engine) SetConfig(_, key, val string) error {
	if err := e.cfg.Set(key, val); err != nil {
		return err
	}
	if err := e.cfg.Validate(); err != nil {
		return err
	}
	path := filepath.Join(e.cfg.DBPath, "config.json")
	if err := config.Save(path, e.cfg); err != nil {
		return err
	}
	return nil
}

// ListVaults lists every vault the daemon serves with its document count. It
// reads the unified store's in-db registry (spec 052: VaultMeta 0x1A), NOT the
// filesystem — so it lists exactly the vaults the daemon can serve (including
// ones registered with no on-disk directory). The vault param carries the
// requesting caller's vault context; enumeration is global.
func (e *Engine) ListVaults(_ string) ([]VaultEntry, error) {
	names, err := e.db.ListVaultNames()
	if err != nil {
		return nil, fmt.Errorf("list vaults: %w", err)
	}
	out := make([]VaultEntry, 0, len(names))
	for _, n := range names {
		ws := e.db.ResolveVaultPrefix(n)
		out = append(out, VaultEntry{Name: n, Documents: countPrefix(e.db, ws, storage.PrefixDocument)})
	}
	return out, nil
}
