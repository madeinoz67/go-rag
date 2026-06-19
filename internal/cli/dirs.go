package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/spf13/cobra"
)

type dirEntry struct {
	Dir    string `json:"dir"`
	Files  int    `json:"files"`
	Chunks int    `json:"chunks"`
}

func newDirsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dirs",
		Short: "List ingested directories with file and chunk counts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			_, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			counts := map[string]*dirEntry{}
			_ = db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
				var d model.Document
				if json.Unmarshal(val, &d) == nil {
					dir := filepath.Dir(d.FilePath)
					e := counts[dir]
					if e == nil {
						e = &dirEntry{Dir: dir}
						counts[dir] = e
					}
					e.Files++
					e.Chunks += d.ChunkCount
				}
				return true
			})

			entries := make([]dirEntry, 0, len(counts))
			for _, e := range counts {
				entries = append(entries, *e)
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].Dir < entries[j].Dir })

			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(entries)
			}
			if len(entries) == 0 {
				fmt.Println("No files ingested.")
				return nil
			}
			fmt.Printf("%6s %7s  %s\n", "FILES", "CHUNKS", "DIR")
			for _, e := range entries {
				fmt.Printf("%6d %7d  %s\n", e.Files, e.Chunks, e.Dir)
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output as JSON")
	return cmd
}
