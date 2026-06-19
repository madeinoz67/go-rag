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
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/spf13/cobra"
)

type statusInfo struct {
	Sources        int    `json:"sources"`
	Documents      int    `json:"documents"`
	Chunks         int    `json:"chunks"`
	EmbeddedPct    int    `json:"embedded_pct"`
	StorageBytes   int64  `json:"storage_bytes"`
	EmbeddingModel string `json:"embedding_model"`
	Provider       string `json:"provider"`
	Dimensions     int    `json:"dimensions"`
	Health         string `json:"health"`
	LastActivity   string `json:"last_activity"`
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show database statistics and health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			info := gatherStats(db, cfg)
			info.Health = pingHealth(cfg.OllamaURL)

			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(info)
			}
			printStatus(info)
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output as JSON")
	return cmd
}

func gatherStats(db *storage.DB, cfg config.Config) statusInfo {
	info := statusInfo{
		Sources:        countPrefix(db, storage.PrefixSource),
		Documents:      countPrefix(db, storage.PrefixDocument),
		Chunks:         countPrefix(db, storage.PrefixChunk),
		EmbeddingModel: cfg.OllamaModel,
		Provider:       cfg.OllamaURL,
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

	// Dimensions from the first stored embedding (false => stop after one).
	_ = db.PrefixScanByte(storage.PrefixEmbedding, func(_, val []byte) bool {
		var v []float32
		if json.Unmarshal(val, &v) == nil {
			info.Dimensions = len(v)
		}
		return false
	})

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

// pingHealth reports "OK" if the embedding service responds 2xx, else "degraded".
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
	fmt.Printf("go-rag database: %s\n\n", dbPath)
	fmt.Printf("  Sources:    %d\n", s.Sources)
	fmt.Printf("  Documents:  %d\n", s.Documents)
	fmt.Printf("  Chunks:     %d\n", s.Chunks)
	fmt.Printf("  Embedded:   %d%%\n", s.EmbeddedPct)
	fmt.Printf("  Storage:    %d bytes\n", s.StorageBytes)
	fmt.Printf("  Model:      %s\n", s.EmbeddingModel)
	fmt.Printf("  Provider:   %s\n", s.Provider)
	fmt.Printf("  Dimensions: %d\n", s.Dimensions)
	fmt.Printf("  Health:     %s\n", s.Health)
	if s.LastActivity != "" {
		fmt.Printf("  Last:       %s\n", s.LastActivity)
	}
}
