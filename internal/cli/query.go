package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/index"
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
			noRerank, _ := cmd.Flags().GetBool("no-rerank")
			rrfK, _ := cmd.Flags().GetInt("rrf-k")
			if cmd.Flags().Changed("rrf-k") && rrfK <= 0 {
				return fmt.Errorf("--rrf-k must be a positive integer (0 = use configured/default; got %d)", rrfK)
			}

			// H14/spec 014: optional metadata filter (source/type/tags).
			source, _ := cmd.Flags().GetString("source")
			filtType, _ := cmd.Flags().GetString("type")
			tagsStr, _ := cmd.Flags().GetString("tags")
			var filt *index.Filter
			if source != "" || filtType != "" || tagsStr != "" {
				var tags []string
				if tagsStr != "" {
					tags = strings.Split(tagsStr, ",")
				}
				filt = &index.Filter{Source: source, Type: filtType, Tags: tags}
			}

			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			// Route through the shared engine so the query path is identical to
			// MCP/REST/gRPC — including the embedding mismatch guard (audit H03),
			// which refuses a query whose model/dim doesn't match the corpus.
			eng := engine.NewWithDB(cfg, db)
			res, err := eng.Query(context.Background(), engine.QueryRequest{
				Query: q, K: k, Mode: modeStr, NoRerank: noRerank, Threshold: threshold, RRFK: rrfK, Filter: filt,
			})
			if err != nil {
				return err
			}
			if res.RerankFailed { // H09: results are valid but fallback-ordered; warn on stderr so stdout JSON stays clean.
				fmt.Fprintln(os.Stderr, "warning: reranking failed; results are in fallback order (see log for details)")
			}

			results := make([]queryResult, 0, len(res.Hits))
			for _, h := range res.Hits {
				results = append(results, queryResult{
					Source: filepath.Base(h.FilePath),
					Page:   h.Page,
					Score:  h.Score,
					Chunk:  h.Content,
				})
			}
			return renderResults(results, format)
		},
	}
	cmd.Flags().Int("k", 5, "number of results")
	cmd.Flags().String("mode", "hybrid", "retrieval mode: hybrid|semantic|keyword")
	cmd.Flags().StringP("format", "f", "text", "output format: text|json")
	cmd.Flags().String("source", "", "filter by source file glob")
	cmd.Flags().Float64("threshold", 0.0, "minimum relevance score")
	cmd.Flags().Bool("no-rerank", false, "disable cross-encoder reranking for this query")
	cmd.Flags().Int("rrf-k", 0, "RRF smoothing constant override (0 = use configured rrf_k / default 60)")
	cmd.Flags().String("type", "", "filter by file type (e.g. markdown, pdf)")
	cmd.Flags().String("tags", "", "filter by document tags (comma-separated, conjunction)")
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
