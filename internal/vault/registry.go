// Package vault manages document vault directories. Each vault is a physically
// isolated directory containing its own config.json + Pebble data directory.
// Vault root defaults to ~/.go-rag/vaults/ (override via GO_RAG_VAULT_ROOT).
package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/config"
)

// Root returns the vault root directory (GO_RAG_VAULT_ROOT or ~/.go-rag/vaults/).
func Root() string {
	if r := os.Getenv("GO_RAG_VAULT_ROOT"); r != "" {
		return r
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".go-rag", "vaults")
}

// Path returns the absolute path for a named vault.
func Path(name string) string {
	return filepath.Join(Root(), name)
}

// ValidateName checks that a vault name is lowercase alphanumeric + hyphens, 1–64 chars.
func ValidateName(name string) error {
	if len(name) < 1 || len(name) > 64 {
		return fmt.Errorf("vault name must be 1–64 characters")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("vault name must be lowercase alphanumeric + hyphens only")
		}
	}
	return nil
}

// Exists reports whether a vault directory with a config.json exists.
func Exists(name string) bool {
	info, err := os.Stat(Path(name))
	return err == nil && info.IsDir()
}

// List returns the names of all vaults (directories with config.json under Root).
func List() []string {
	entries, err := os.ReadDir(Root())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(Root(), e.Name(), "config.json")); err == nil {
			names = append(names, e.Name())
		}
	}
	return names
}

// Create makes a new vault directory with config + empty data dir.
func Create(name string, cfg config.Config) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if Exists(name) {
		return fmt.Errorf("vault %q already exists", name)
	}
	dir := Path(name)
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		return err
	}
	cfg.DBPath = dir
	return config.Save(filepath.Join(dir, "config.json"), cfg)
}

// Delete removes an entire vault directory.
func Delete(name string) error {
	if !Exists(name) {
		return fmt.Errorf("vault %q not found", name)
	}
	return os.RemoveAll(Path(name))
}

// Clear removes a vault's data directory but preserves its config.
func Clear(name string) error {
	if !Exists(name) {
		return fmt.Errorf("vault %q not found", name)
	}
	return os.RemoveAll(filepath.Join(Path(name), "data"))
}

// EnsureDefault creates a "default" vault if no vaults exist. Idempotent — mirrors
// muninndb's bootstrap which creates a "default" vault on first run so the user
// has a starting point.
func EnsureDefault() {
	if len(List()) > 0 {
		return
	}
	_ = Create("default", config.Default())
}
