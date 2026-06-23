package cli

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/vault"
	"github.com/spf13/cobra"
)

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage document vaults (create, list, delete, clear)",
	}
	cmd.AddCommand(newVaultCreateCmd(), newVaultListCmd(), newVaultDeleteCmd(), newVaultClearCmd(),
		newVaultCloneCmd(), newVaultExportCmd(), newVaultImportCmd())
	return cmd
}

func newVaultCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new vault with default config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg := config.Default()
			if m, _ := cmd.Flags().GetString("embedding_model"); m != "" {
				cfg.EmbeddingModel = m
			}
			if u, _ := cmd.Flags().GetString("ollama-url"); u != "" {
				cfg.OllamaURL = u
			}
			if a, _ := cmd.Flags().GetString("mcp-addr"); a != "" {
				cfg.MCPAddr = a
			}
			if err := vault.Create(name, cfg); err != nil {
				return err
			}
			fmt.Printf("Created vault %q at %s\n", name, vault.Path(name))
			fmt.Printf("  Embedding model: %s\n", cfg.EmbeddingModel)
			fmt.Printf("  Next: go-rag --vault %s add <path>\n", name)
			return nil
		},
	}
	cmd.Flags().String("embedding_model", "", "embedding model for this vault")
	cmd.Flags().String("ollama-url", "", "Ollama server URL")
	cmd.Flags().String("mcp-addr", "", "MCP listen address for this vault's daemon")
	return cmd
}

func newVaultListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all vaults with document counts and status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			vault.EnsureDefault()
			names := vault.List()
			type info struct {
				Name    string `json:"name"`
				Docs    int    `json:"docs"`
				Model   string `json:"model"`
				Daemon  string `json:"daemon"`
				Storage int64  `json:"storage"`
			}
			var entries []info
			for _, n := range names {
				vp := vault.Path(n)
				cfg, _ := config.Load(vp + "/config.json")
				docs := 0
				var storage int64
				// Try to open the vault's DB for doc count
				if _, db, err := openDB(vp); err == nil {
					docs = countPrefix(db, 0x02) // PrefixDocument
					storage = dirSize(vp + "/data")
					db.Close()
				}
				running, _, _ := daemon.Status(vp)
				st := "stopped"
				if running {
					st = "running"
				}
				entries = append(entries, info{n, docs, cfg.EmbeddingModel, st, storage})
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(entries)
			}
			if len(entries) == 0 {
				fmt.Println("No vaults. Run 'go-rag vault create <name>' to create one.")
				return nil
			}
			fmt.Printf("%-16s %6s  %-20s %-8s %s\n", "VAULT", "DOCS", "MODEL", "DAEMON", "STORAGE")
			for _, e := range entries {
				fmt.Printf("%-16s %6d  %-20s %-8s %s\n", e.Name, e.Docs, e.Model, e.Daemon, humanBytes(e.Storage))
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output as JSON")
	return cmd
}

func newVaultDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a vault and all its data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			force, _ := cmd.Flags().GetBool("force")
			if !force {
				fmt.Printf("Delete vault %q and ALL its data? (y/N) ", name)
				var resp string
				fmt.Scanln(&resp)
				if resp != "y" && resp != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}
			// Check daemon not running
			if daemon.IsRunning(vault.Path(name)) {
				return fmt.Errorf("daemon is running for vault %q — stop it first ('go-rag --vault %s stop')", name, name)
			}
			if err := vault.Delete(name); err != nil {
				return err
			}
			fmt.Printf("Deleted vault %q\n", name)
			return nil
		},
	}
	cmd.Flags().Bool("force", false, "skip confirmation")
	return cmd
}

func newVaultClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear <name>",
		Short: "Remove all data from a vault (preserves config)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if daemon.IsRunning(vault.Path(name)) {
				return fmt.Errorf("daemon is running for vault %q — stop it first", name)
			}
			if err := vault.Clear(name); err != nil {
				return err
			}
			fmt.Printf("Cleared vault %q (config preserved)\n", name)
			return nil
		},
	}
}

func newVaultCloneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clone <src> <dst>",
		Short: "Clone a vault (copy config + data to a new vault)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			src, dst := args[0], args[1]
			if !vault.Exists(src) {
				return fmt.Errorf("source vault %q not found", src)
			}
			srcCfg, _ := config.Load(filepath.Join(vault.Path(src), "config.json"))
			if err := vault.Create(dst, srcCfg); err != nil {
				return err
			}
			// Recursively copy data/
			srcData := filepath.Join(vault.Path(src), "data")
			dstData := filepath.Join(vault.Path(dst), "data")
			err := filepath.Walk(srcData, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(srcData, path)
				target := filepath.Join(dstData, rel)
				if info.IsDir() {
					return os.MkdirAll(target, info.Mode())
				}
				data, rErr := os.ReadFile(path)
				if rErr != nil {
					return rErr
				}
				return os.WriteFile(target, data, info.Mode())
			})
			if err != nil {
				return fmt.Errorf("clone data: %w", err)
			}
			fmt.Printf("Cloned vault %q → %q\n", src, dst)
			return nil
		},
	}
}

func newVaultExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a vault as a tar archive (stdout or --output)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !vault.Exists(name) {
				return fmt.Errorf("vault %q not found", name)
			}
			output, _ := cmd.Flags().GetString("output")
			var w io.Writer = os.Stdout
			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return err
				}
				defer f.Close()
				w = f
			}
			tw := tar.NewWriter(w)
			defer tw.Close()
			src := vault.Path(name)
			return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(src, path)
				hdr := &tar.Header{Name: rel, Size: info.Size(), Mode: int64(info.Mode())}
				if err := tw.WriteHeader(hdr); err != nil {
					return err
				}
				data, rErr := os.ReadFile(path)
				if rErr != nil {
					return rErr
				}
				_, err = tw.Write(data)
				return err
			})
		},
	}
	cmd.Flags().StringP("output", "o", "", "output file (default: stdout)")
	return cmd
}

func newVaultImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <name> --from <path>",
		Short: "Import an existing database directory as a vault (no re-ingestion)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			from, _ := cmd.Flags().GetString("from")
			if from == "" {
				return fmt.Errorf("--from <path> is required (path to existing .go-rag directory)")
			}
			// Read source config
			srcCfg, err := config.Load(filepath.Join(from, "config.json"))
			if err != nil {
				return fmt.Errorf("read config from %s: %w", from, err)
			}
			// Create the vault with the source's config
			if err := vault.Create(name, srcCfg); err != nil {
				return err
			}
			// Recursively copy data/
			srcData := filepath.Join(from, "data")
			dstData := filepath.Join(vault.Path(name), "data")
			err = filepath.Walk(srcData, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(srcData, path)
				target := filepath.Join(dstData, rel)
				if info.IsDir() {
					return os.MkdirAll(target, info.Mode())
				}
				data, rErr := os.ReadFile(path)
				if rErr != nil {
					return rErr
				}
				return os.WriteFile(target, data, info.Mode())
			})
			if err != nil {
				return fmt.Errorf("import data: %w", err)
			}
			fmt.Printf("Imported %q → vault %q at %s\n", from, name, vault.Path(name))
			return nil
		},
	}
	cmd.Flags().String("from", "", "path to existing database directory (e.g., ./.go-rag or ~/Documents/ObsidianVault/.go-rag)")
	return cmd
}
