package cli

import (
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// openDB loads the config and opens the Pebble store under <base>/data. Delegates
// to engine.Open (the single implementation) — the inline version that used to
// live here was an exact duplicate. It also normalises the package-global
// vaultName so an unspecified --vault defaults to "default" even when a command's
// RunE is invoked directly (e.g. from tests) and the root PersistentPreRunE has
// not fired.
func openDB(base string) (config.Config, *storage.DB, error) {
	if vaultName == "" {
		vaultName = "default"
	}
	return engine.Open(base)
}
