package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed/modelbundle"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new RAG database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ollamaURL, _ := cmd.Flags().GetString("ollama-url")
			provider, _ := cmd.Flags().GetString("embedding-provider")
			model, _ := cmd.Flags().GetString("model")
			chunkSize, _ := cmd.Flags().GetInt("chunk-size")
			chunkOverlap, _ := cmd.Flags().GetInt("chunk-overlap")
			watchDir, _ := cmd.Flags().GetString("watch-dir")

			cfg := config.Default()
			cfg.DBPath = dbPath
			cfg.WatchDirs = []string{watchDir}
			cfg.EmbeddingProvider = provider // spec 032: "native" (bundled pure-Go) is the default
			if ollamaURL != "" {
				cfg.OllamaURL = ollamaURL
			}
			if model != "" {
				cfg.EmbeddingModel = model
			}
			if cfg.EmbeddingModel == "" {
				cfg.EmbeddingModel = "nomic-embed-text"
			}
			cfg.ChunkSize = chunkSize
			cfg.ChunkOverlap = chunkOverlap

			if err := cfg.Validate(); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(cfg.DBPath, "data"), 0o755); err != nil {
				return err
			}
			if err := config.Save(filepath.Join(cfg.DBPath, "config.json"), cfg); err != nil {
				return err
			}

			fmt.Printf("Initialized go-rag database at %s\n", cfg.DBPath)
			// Spec 032: when the bundled pure-Go provider is selected, fetch the model
			// now (one-time, hash-gated) so add/query work with no external service.
			// Best-effort — a fetch failure (e.g. offline) does not abort init; the
			// user can run `go-rag model install` later.
			if cfg.EmbeddingProvider == "native" {
				switch {
				case testing.Testing():
					// Tests: never fetch (slow/flaky network). Tests that embed opt into
					// provider "ollama"; a pre-seeded model covers any native-path test.
					fmt.Println("  (test mode: skipping model fetch)")
				default:
					fmt.Printf("Fetching bundled embedding model %s...\n", modelbundle.ModelID)
					if _, err := modelbundle.EnsureModel(context.Background()); err != nil {
						fmt.Printf("  could not fetch the model now: %v\n", err)
						fmt.Println("  run `go-rag model install` when online to enable embeddings.")
					} else {
						fmt.Println("  model ready (pure-Go, no Ollama required).")
					}
				}
			} else {
				fmt.Printf("Embedding provider: %s\n", cfg.EmbeddingProvider)
				fmt.Printf("Embedding model: %s (%s)\n", cfg.EmbeddingModel, cfg.OllamaURL)
			}
			fmt.Println("Next: go-rag add <path>")
			return nil
		},
	}
	cmd.Flags().String("ollama-url", "http://localhost:11434", "Ollama server URL (used when provider=ollama)")
	cmd.Flags().String("embedding-provider", "native", `embedding provider: "native" (bundled pure-Go, default, spec 032) or "ollama"`)
	cmd.Flags().String("model", "", "embedding model name (provider=ollama; default: nomic-embed-text)")
	cmd.Flags().String("watch-dir", ".", "directory to watch")
	cmd.Flags().Int("chunk-size", 512, "chunk size in tokens")
	cmd.Flags().Int("chunk-overlap", 50, "chunk overlap in tokens")
	return cmd
}
