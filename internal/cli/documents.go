package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/spf13/cobra"
)

// documents.go is the spec 039 (BL-007) document-listing CLI surface: a new
// `documents` parent command with a `list` subcommand — `go-rag documents list
// [--page-size N] [--page-token TOK] [--after T] [--status embedded]`. Thin CLI
// projection of engine.ListDocuments — parity-identical to the gRPC/REST/MCP
// projections (cross-transport parity, Constitution V). The document DTO reuses
// `documentOut` (the GetChunk projection) so a listed document is byte-identical
// to a GetChunk document.

// documentsListOut is the CLI JSON envelope for `documents list` —
// { documents, next_page_token } — mirroring the proto/REST shape 1:1 (parity).
type documentsListOut struct {
	Documents     []documentOut `json:"documents"`
	NextPageToken string        `json:"next_page_token,omitempty"`
}

func newDocumentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "documents",
		Short: "List documents (spec 039 — ingested_at cursor + status filter + pagination)",
	}
	cmd.AddCommand(newDocumentsListCmd())
	return cmd
}

// newDocumentsListCmd lists documents ingested after a cursor, filtered by status,
// paginated. JSON default; --format text prints one line per document
// (ingested_at, status, file path) + a next_page_token line.
func newDocumentsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List documents ingested after a cursor, filtered by status, paginated",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pageSize, _ := cmd.Flags().GetInt("page-size")
			pageToken, _ := cmd.Flags().GetString("page-token")
			after, _ := cmd.Flags().GetString("after")
			status, _ := cmd.Flags().GetString("status")
			format, _ := cmd.Flags().GetString("format")
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := engine.NewWithDB(cfg, db).ListDocuments(vaultName, engine.ListDocumentsRequest{
				PageSize: pageSize, PageToken: pageToken, After: after, Status: status,
			})
			if err != nil {
				return err
			}
			out := documentsListOut{Documents: make([]documentOut, len(res.Documents)), NextPageToken: res.NextPageToken}
			for i, d := range res.Documents {
				out.Documents[i] = *toDocumentOut(d, model.Source{}) // listing has no per-doc source context
			}
			if format == "json" {
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			printDocumentsListText(out)
			return nil
		},
	}
	cmd.Flags().Int("page-size", engine.DefaultListPageSize(), "page size (1..200; default 50)")
	cmd.Flags().String("page-token", "", "opaque pagination cursor (next_page_token)")
	cmd.Flags().String("after", "", "RFC3339; only docs with ingested_at > after")
	cmd.Flags().String("status", "", "filter: embedded|pending|error (empty = all)")
	cmd.Flags().StringP("format", "f", "json", "output format: json|text")
	return cmd
}

func printDocumentsListText(out documentsListOut) {
	for _, d := range out.Documents {
		fmt.Printf("%s\t%s\t%s\n", d.IngestedAt, d.Status, d.FilePath)
	}
	if out.NextPageToken != "" {
		fmt.Printf("next_page_token: %s\n", out.NextPageToken)
	}
}
