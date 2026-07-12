package cli

import (
	"context"
	"fmt"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/spf13/cobra"
)

// delete.go is the spec 050 (Slice 4) CLI projection of engine.DeleteDoc:
// `go-rag delete <docID>` removes a document and all its chunks/embeddings from
// the index by content-addressed ID. Index-only — the source file on disk is
// never touched (FR-011). Routes through engine.NewWithDB so the cfg-driven
// pipeline features fire consistently with the daemon and parity with
// REST/gRPC/MCP holds (Constitution V). Mirrors newReprocessCmd / newAddCmd.
func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <document_id>",
		Short: "Delete a document and its chunks from the index (index-only; source file preserved)",
		Long: `Delete a document by its content-addressed ID.

Removes the document and all of its chunks and embeddings from the index. The
source file on disk is NOT touched — removal is index-only (re-add the path to
restore it). Mirrors DELETE /v1/documents/{id} (REST), DeleteDocument (gRPC),
and go_rag_delete_document (MCP).`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			docID := args[0]
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			eng := engine.NewWithDB(cfg, db)
			if err := eng.DeleteDoc(context.Background(), docID); err != nil {
				eng.Close() // drain even on the error path
				return err
			}
			eng.Close()
			fmt.Printf("Deleted: %s\n", docID)
			return nil
		},
	}
	return cmd
}
