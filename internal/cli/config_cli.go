package cli

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or change configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load(filepath.Join(dbPath, "config.json"))
			if err != nil {
				return fmt.Errorf("no database here — run `go-rag init` first: %w", err)
			}
			printConfig(cfg)
			return nil
		},
	}
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [key]",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load(filepath.Join(dbPath, "config.json"))
			if err != nil {
				return err
			}
			v, ok := cfg.Get(args[0])
			if !ok {
				return fmt.Errorf("unknown key: %s", args[0])
			}
			fmt.Println(v)
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			path := filepath.Join(dbPath, "config.json")
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			// Set + Validate; on failure the previous value is retained (not saved).
			if err := cfg.Set(args[0], args[1]); err != nil {
				return fmt.Errorf("invalid value for %s: %w (previous retained)", args[0], err)
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("validation failed: %w (previous retained)", err)
			}
			return config.Save(path, cfg)
		},
	}
}

func printConfig(cfg config.Config) {
	keys := []string{
		"ollama_url", "embedding_model", "watch_dirs", "chunk_size",
		"chunk_overlap", "db_path", "file_glob", "poll_interval_secs",
		"mcp_addr", "mcp_token", "rerank_model", "rerank_candidates",
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v, ok := cfg.Get(k); ok {
			fmt.Printf("%s = %s\n", k, v)
		}
	}
}
