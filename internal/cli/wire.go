package cli

import (
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// openDB loads the config and opens the Pebble store under <base>/data. Delegates
// to engine.Open (the single implementation) — the inline version that used to
// live here was an exact duplicate.
func openDB(base string) (config.Config, *storage.DB, error) {
	return engine.Open(base)
}
