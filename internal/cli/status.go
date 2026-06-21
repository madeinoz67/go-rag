package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/spf13/cobra"
)

type statusInfo struct {
	Sources        int            `json:"sources"`
	Documents      int            `json:"documents"`
	Chunks         int            `json:"chunks"`
	EmbeddedPct    int            `json:"embedded_pct"`
	StorageBytes   int64          `json:"storage_bytes"`
	EmbeddingModel string         `json:"embedding_model"`
	Provider       string         `json:"provider"`
	Dimensions     int            `json:"dimensions"`
	Health         string         `json:"health"`
	LastActivity   string         `json:"last_activity"`
	EmbeddingDrift bool           `json:"embedding_drift"`
	ModelCounts    map[string]int `json:"model_counts,omitempty"`
	DimCounts      map[int]int    `json:"dim_counts,omitempty"`
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon and database status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			running, pid, addrs := daemon.Status(dbPath)

			if running {
				counts, _ := daemon.CallTool(addrs.MCPAddr, daemon.ReadToken(dbPath), "go_rag_status", nil)
				if asJSON {
					return json.NewEncoder(os.Stdout).Encode(map[string]any{
						"daemon": "running", "pid": pid,
						"mcp_addr": addrs.MCPAddr, "rest_addr": addrs.RESTAddr, "grpc_addr": addrs.GRPCAddr,
						"counts": counts,
					})
				}
				fmt.Printf("Daemon: running (pid %d, MCP %s)\n", pid, addrs.MCPAddr)
				if addrs.RESTAddr != "" {
					fmt.Printf("  REST %s\n", addrs.RESTAddr)
				}
				if addrs.GRPCAddr != "" {
					fmt.Printf("  gRPC %s\n", addrs.GRPCAddr)
				}
				if counts != "" {
					fmt.Printf("  %s\n", counts)
				}
				return nil
			}

			// Daemon not running — open the database directly for counts.
			cfg, db, err := openDB(dbPath)
			if err != nil {
				if asJSON {
					return json.NewEncoder(os.Stdout).Encode(map[string]any{"daemon": "stopped"})
				}
				fmt.Println("Daemon: not running")
				fmt.Printf("Database: %s not found (run 'go-rag init')\n", dbPath)
				return nil
			}
			defer db.Close()
			info := gatherStats(db, cfg)
			info.Health = pingHealth(cfg.OllamaURL)
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"daemon": "stopped", "database": info,
				})
			}
			fmt.Println("Daemon: not running")
			printStatus(info)
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output as JSON")
	return cmd
}

func gatherStats(db *storage.DB, cfg config.Config) statusInfo {
	info := statusInfo{
		Sources:   countPrefix(db, storage.PrefixSource),
		Documents: countPrefix(db, storage.PrefixDocument),
		Chunks:    countPrefix(db, storage.PrefixChunk),
		Provider:  cfg.OllamaURL,
	}

	embedded := 0
	var last time.Time
	_ = db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			if d.Status == "embedded" {
				embedded++
			}
			if d.UpdatedAt.After(last) {
				last = d.UpdatedAt
			}
		}
		return true
	})
	if info.Documents > 0 {
		info.EmbeddedPct = embedded * 100 / info.Documents
	}

	// Embedding profile (audit H03): report the STORED majority model/dim and
	// surface drift (mixed models/dims) so an operator sees it without querying.
	prof := engine.CorpusProfile(db)
	if prof.Total > 0 {
		info.EmbeddingModel = prof.MajorityModel
		info.Dimensions = prof.MajorityDim
		info.EmbeddingDrift = !prof.Consistent
		info.ModelCounts = prof.ModelCounts
		info.DimCounts = prof.DimCounts
	} else {
		info.EmbeddingModel = cfg.EmbeddingModel
	}

	if !last.IsZero() {
		info.LastActivity = last.Format(time.RFC3339)
	}
	info.StorageBytes = dirSize(filepath.Join(cfg.DBPath, "data"))
	return info
}

func countPrefix(db *storage.DB, prefix byte) int {
	n := 0
	_ = db.PrefixScanByte(prefix, func(_, _ []byte) bool {
		n++
		return true
	})
	return n
}

func dirSize(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
		}
		return nil
	})
	return size
}

func pingHealth(baseURL string) string {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL)
	if err != nil {
		return "degraded"
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "OK"
	}
	return "degraded"
}

func printStatus(s statusInfo) {
	fmt.Printf("Database: %s\n\n", dbPath)
	fmt.Printf("  Sources:    %d\n", s.Sources)
	fmt.Printf("  Documents:  %d\n", s.Documents)
	fmt.Printf("  Chunks:     %d\n", s.Chunks)
	fmt.Printf("  Embedded:   %d%%\n", s.EmbeddedPct)
	fmt.Printf("  Storage:    %d bytes\n", s.StorageBytes)
	fmt.Printf("  Model:      %s\n", s.EmbeddingModel)
	fmt.Printf("  Provider:   %s\n", s.Provider)
	fmt.Printf("  Dimensions: %d\n", s.Dimensions)
	if s.EmbeddingDrift {
		fmt.Printf("  Drift:      mixed embedding models/dims detected (%v)\n", s.ModelCounts)
	}
	fmt.Printf("  Health:     %s\n", s.Health)
	if s.LastActivity != "" {
		fmt.Printf("  Last:       %s\n", s.LastActivity)
	}
}
