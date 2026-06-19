package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new RAG database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ollamaURL, _ := cmd.Flags().GetString("ollama-url")
			model, _ := cmd.Flags().GetString("model")
			chunkSize, _ := cmd.Flags().GetInt("chunk-size")
			chunkOverlap, _ := cmd.Flags().GetInt("chunk-overlap")
			watchDir, _ := cmd.Flags().GetString("watch-dir")

			cfg := config.Default()
			cfg.DBPath = dbPath
			cfg.WatchDirs = []string{watchDir}
			if ollamaURL != "" {
				cfg.OllamaURL = ollamaURL
			}
			if model != "" {
				cfg.OllamaModel = model
			}
			if cfg.OllamaModel == "" {
				cfg.OllamaModel = "nomic-embed-text"
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
			fmt.Printf("Embedding model: %s (%s)\n", cfg.OllamaModel, cfg.OllamaURL)
			fmt.Println("Next: go-rag add <path>")
			return nil
		},
	}
	cmd.Flags().String("ollama-url", "http://localhost:11434", "Ollama server URL")
	cmd.Flags().String("model", "", "embedding model name (default: nomic-embed-text)")
	cmd.Flags().String("watch-dir", ".", "directory to watch")
	cmd.Flags().Int("chunk-size", 512, "chunk size in tokens")
	cmd.Flags().Int("chunk-overlap", 50, "chunk overlap in tokens")
	return cmd
}
