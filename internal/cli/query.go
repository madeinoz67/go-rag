package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/index"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/spf13/cobra"
)

type queryResult struct {
	Source string  `json:"source"`
	Page   int     `json:"page"`
	Score  float64 `json:"score"`
	Chunk  string  `json:"chunk"`
}

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query [query]",
		Short: "Search the database (hybrid retrieval by default)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := strings.Join(args, " ")
			k, _ := cmd.Flags().GetInt("k")
			modeStr, _ := cmd.Flags().GetString("mode")
			format, _ := cmd.Flags().GetString("format")
			threshold, _ := cmd.Flags().GetFloat64("threshold")
			mode := index.ParseMode(modeStr)

			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			fts, vec, err := pipeline.LoadIndex(db)
			if err != nil {
				return err
			}
			em := embed.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
			r := index.NewRetrieval(fts, vec, em.Embed)

			hits, err := r.Search(context.Background(), q, k, mode, buildDocOf(db))
			if err != nil {
				return err
			}

			results := make([]queryResult, 0, len(hits))
			for _, h := range hits {
				if h.Score < threshold {
					continue
				}
				c, ok := lookupChunk(db, h.ChunkID)
				if !ok {
					continue
				}
				src := ""
				if d, ok := lookupDoc(db, c.DocumentID); ok {
					src = d.FileName
				}
				results = append(results, queryResult{Source: src, Page: c.PageNumber, Score: h.Score, Chunk: c.Content})
			}

			return renderResults(results, format)
		},
	}
	cmd.Flags().Int("k", 5, "number of results")
	cmd.Flags().String("mode", "hybrid", "retrieval mode: hybrid|semantic|keyword")
	cmd.Flags().StringP("format", "f", "text", "output format: text|json")
	cmd.Flags().String("source", "", "filter by source file glob")
	cmd.Flags().Float64("threshold", 0.0, "minimum relevance score")
	return cmd
}

func renderResults(results []queryResult, format string) error {
	if len(results) == 0 {
		fmt.Println("No results.")
		return nil
	}
	if format == "json" {
		return json.NewEncoder(os.Stdout).Encode(results)
	}
	for i, r := range results {
		page := ""
		if r.Page > 0 {
			page = fmt.Sprintf(" (page %d)", r.Page)
		}
		fmt.Printf("[%d] %s%s (score %.3f)\n", i+1, r.Source, page, r.Score)
		fmt.Printf("    %s\n", preview(r.Chunk, 200))
	}
	return nil
}

func preview(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
