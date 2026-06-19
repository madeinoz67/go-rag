package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/spf13/cobra"
)

type fileEntry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Chunks int    `json:"chunks"`
}

func newFilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files",
		Short: "List ingested file paths",
		RunE: func(cmd *cobra.Command, _ []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			_, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			var entries []fileEntry
			_ = db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
				var d model.Document
				if json.Unmarshal(val, &d) == nil {
					entries = append(entries, fileEntry{Path: d.FilePath, Type: d.FileType, Status: d.Status, Chunks: d.ChunkCount})
				}
				return true
			})
			sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(entries)
			}
			if len(entries) == 0 {
				fmt.Println("No files ingested.")
				return nil
			}
			fmt.Printf("%-8s %-9s %6s  %s\n", "TYPE", "STATUS", "CHUNKS", "PATH")
			for _, e := range entries {
				fmt.Printf("%-8s %-9s %6d  %s\n", e.Type, e.Status, e.Chunks, e.Path)
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output as JSON")
	return cmd
}
