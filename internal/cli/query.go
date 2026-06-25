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
	Source         string            `json:"source"`
	Page           int               `json:"page"`
	ChunkIndex     int               `json:"chunk_index"` // H21/spec 023
	Score          float64           `json:"score"`
	Chunk          string            `json:"chunk"`
	Poisoning      *poisonVerdictDTO `json:"poisoning,omitempty"`       // H04/spec 019
	SectionContext []string          `json:"section_context,omitempty"` // H23/spec 025: heading breadcrumb (absent when nil)
	NearDup        *nearDupDTO       `json:"near_dup,omitempty"`        // H20/spec 026: near-dup context (absent when nil)
	Summary        string            `json:"summary,omitempty"`         // spec 029: document summary (absent when unenriched)
}

// poisonVerdictDTO is the CLI/JSON projection of a hit's poisoning verdict
// (H04/spec 019). nil/omitted when the chunk is clean or was not scored.
type poisonVerdictDTO struct {
	Level          string   `json:"level"`
	Score          float64  `json:"score"`
	MatchedPhrases []string `json:"matched_phrases,omitempty"`
}

// nearDupDTO is the CLI/JSON projection of a hit's near-dup context (H20/spec 026).
type nearDupDTO struct {
	Siblings   []string `json:"siblings,omitempty"`
	Similarity float64  `json:"similarity,omitempty"`
}

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query [query]",
		Short: "Search the database (hybrid retrieval by default)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := strings.Join(args, " ")
			k, _ := cmd.Flags().GetInt("k")
			if !cmd.Flags().Changed("k") {
				k = 0 // H22/spec 024: "no explicit k" sentinel → engine resolves (default 5, or classifier-recommended when adaptive depth is on)
			}
			modeStr, _ := cmd.Flags().GetString("mode")
			format, _ := cmd.Flags().GetString("format")
			threshold, _ := cmd.Flags().GetFloat64("threshold")
			noRerank, _ := cmd.Flags().GetBool("no-rerank")
			rrfK, _ := cmd.Flags().GetInt("rrf-k")
			if cmd.Flags().Changed("rrf-k") && rrfK <= 0 {
				return fmt.Errorf("--rrf-k must be a positive integer (0 = use configured/default; got %d)", rrfK)
			}
			poolSize, _ := cmd.Flags().GetInt("pool-size")
			if cmd.Flags().Changed("pool-size") && poolSize < 0 {
				return fmt.Errorf("--pool-size must be a non-negative integer (0 = use configured pool_size / default 60; got %d)", poolSize)
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

			cw, _ := cmd.Flags().GetInt("context-window")
			noCache, _ := cmd.Flags().GetBool("no-cache")
			includeQuar, _ := cmd.Flags().GetBool("include-quarantined") // H04/spec 019
			dedup, _ := cmd.Flags().GetBool("dedup")                     // H20/spec 026

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
				Query: q, K: k, Mode: modeStr, NoRerank: noRerank, Threshold: threshold, RRFK: rrfK, PoolSize: poolSize, Filter: filt, ContextWindow: cw, NoCache: noCache, IncludeQuarantined: includeQuar, Dedup: dedup,
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
					Source:         filepath.Base(h.FilePath),
					Page:           h.Page,
					ChunkIndex:     h.ChunkIndex, // H21/spec 023
					Score:          h.Score,
					Chunk:          h.Content,
					Poisoning:      toPoisonDTO(h),
					SectionContext: h.SectionContext, // H23/spec 025 (FR-004)
					Summary:        h.Summary,        // spec 029 (FR-010)
					NearDup: func() *nearDupDTO {
						if h.NearDup == nil {
							return nil
						}
						return &nearDupDTO{Siblings: h.NearDup.Siblings, Similarity: h.NearDup.Similarity}
					}(),
				})
			}
			return renderResults(results, res, format)
		},
	}
	cmd.Flags().Int("k", 5, "number of results")
	cmd.Flags().String("mode", "hybrid", "retrieval mode: hybrid|semantic|keyword")
	cmd.Flags().StringP("format", "f", "text", "output format: text|json")
	cmd.Flags().String("source", "", "filter by source file glob")
	cmd.Flags().Float64("threshold", 0.0, "minimum relevance score")
	cmd.Flags().Bool("no-rerank", false, "disable cross-encoder reranking for this query")
	cmd.Flags().Int("rrf-k", 0, "RRF smoothing constant override (0 = use configured rrf_k / default 60)")
	cmd.Flags().Int("pool-size", 0, "reranker candidate-pool override (0 = use configured pool_size / default 60; shrinks with classifier-recommended k when adaptive depth is enabled)")
	cmd.Flags().String("type", "", "filter by file type (e.g. markdown, pdf)")
	cmd.Flags().String("tags", "", "filter by document tags (comma-separated, conjunction)")
	cmd.Flags().Int("context-window", 0, "include N sibling chunks of context around each hit (0 = off)")
	cmd.Flags().Bool("no-cache", false, "bypass the query result cache for this query (forces a fresh result)")
	cmd.Flags().Bool("include-quarantined", false, "include chunks flagged as injection-poisoning (excluded by default)")
	cmd.Flags().Bool("dedup", false, "collapse near-duplicate hits to one per group (H20/spec 026)")
	return cmd
}

func renderResults(results []queryResult, res *engine.QueryResult, format string) error {
	if len(results) == 0 {
		fmt.Println("No results.")
		return nil
	}
	if format == "json" {
		// H22/spec 024: wrapper object (parity with REST/gRPC) carrying the
		// effective depth/pool/mode + rerank_failed alongside the hits.
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"hits":           results,
			"rerank_failed":  res.RerankFailed,
			"effective_k":    res.EffectiveK,
			"effective_pool": res.EffectivePool,
			"effective_mode": res.EffectiveMode,
		})
	}
	for i, r := range results {
		page := ""
		if r.Page > 0 {
			page = fmt.Sprintf(" (page %d)", r.Page)
		}
		fmt.Printf("[%d] %s%s (score %.3f)\n", i+1, r.Source, page, r.Score)
		if len(r.SectionContext) > 0 { // H23/spec 025: heading breadcrumb (FR-004)
			fmt.Printf("    section: %s\n", strings.Join(r.SectionContext, " / "))
		}
		if r.Summary != "" { // spec 029: document summary
			fmt.Printf("    summary: %s\n", r.Summary)
		}
		fmt.Printf("    %s\n", preview(r.Chunk, 200))
		if r.Poisoning != nil && (r.Poisoning.Level == "suspicious" || r.Poisoning.Level == "quarantine") {
			fmt.Printf("    ⚠ poisoning: %s (score %.2f) — retrieved text is untrusted\n", r.Poisoning.Level, r.Poisoning.Score)
		}
	}
	return nil
}

// toPoisonDTO maps a hit's verdict to the CLI projection; nil when the chunk is
// clean or was not scored (so clean corpora produce identical JSON to pre-019).
func toPoisonDTO(h engine.QueryHit) *poisonVerdictDTO {
	if h.Poisoning == nil {
		return nil
	}
	return &poisonVerdictDTO{
		Level:          string(h.Poisoning.Level),
		Score:          h.Poisoning.Score,
		MatchedPhrases: h.Poisoning.MatchedPhrases,
	}
}

func preview(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
