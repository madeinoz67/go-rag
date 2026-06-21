package engine

import (
	"fmt"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/vault"
)

// knownConfigKeys is the public, ordered set of config keys surfaced to
// consumers (mirrors the former mcp/cli config listing).
var knownConfigKeys = []string{
	"ollama_url", "embedding_model", "chunk_size", "chunk_overlap",
	"db_path", "poll_interval_secs",
}

// GetConfig returns config values as strings. If key is non-empty, only that
// key is returned (error if unknown); otherwise all known keys are returned.
func (e *Engine) GetConfig(key string) (map[string]string, error) {
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
func (e *Engine) SetConfig(key, val string) error {
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

// ListVaults lists every vault with its document count. It does not require the
// engine's own database to be open.
func (e *Engine) ListVaults() ([]VaultEntry, error) {
	names := vault.List()
	out := make([]VaultEntry, 0, len(names))
	for _, n := range names {
		docs := 0
		if _, db, err := Open(vault.Path(n)); err == nil {
			docs = countPrefix(db, storage.PrefixDocument)
			db.Close()
		}
		out = append(out, VaultEntry{Name: n, Documents: docs})
	}
	return out, nil
}
