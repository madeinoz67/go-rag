package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/madeinoz67/go-rag/internal/eval"
	"github.com/spf13/cobra"
)

// newEvalGenCmd implements `go-rag eval-gen`: emit candidate query→chunk_id
// pairs from a corpus for HUMAN TRIAGE only. Candidates are written to stdout as
// JSONL and are never auto-committed to the golden dataset — humans remain the
// source of truth for relevance labels (research.md D8, US3).
//
// The candidate "query" for each chunk is a short seed derived from the chunk's
// first line; a human rewrites it into a natural query and confirms relevance.
func newEvalGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval-gen",
		Short: "Emit candidate query→chunk pairs from a corpus for golden-set triage (stdout, not auto-committed)",
		Long: `Generate candidate {query, chunk_id} pairs from a corpus for human triage.

Reads the corpus, ingests it into a throwaway vault with the deterministic
offline embedder, and prints one JSONL candidate per chunk to stdout. Pipe the
output to a file, review it, and copy the agreed-upon pairs into
testdata/golden/v1.jsonl. Nothing is auto-committed — relevance labels are
always human-authored.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			corpus, _ := cmd.Flags().GetString("corpus")
			em := eval.NewDeterministicEmbedder()
			ctx := context.Background()
			_, db, cleanup, err := eval.ProvisionCorpus(ctx, corpus, em, "")
			if err != nil {
				return err
			}
			defer cleanup()
			refs, err := eval.ListChunks(db)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			for _, r := range refs {
				if err := enc.Encode(map[string]any{
					"chunk_id": r.ID,
					"file":     r.FilePath,
					"query":    r.Preview, // human rewrites this into a natural query
				}); err != nil {
					return err
				}
			}
			fmt.Fprintf(os.Stderr, "emitted %d candidate pairs for triage (stdout)\n", len(refs))
			return nil
		},
	}
	cmd.Flags().String("corpus", "testdata/golden/corpus/", "source corpus to derive candidate pairs from")
	return cmd
}
