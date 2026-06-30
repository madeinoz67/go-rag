package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/spf13/cobra"
)

// chunk.go is the spec 035 (bridge backlog BL-001) point-fetch surface: resolve
// a content-addressed chunk_id to its chunk plus parent document metadata. It is
// a thin CLI projection of engine.GetChunk — parity-identical to the gRPC /
// REST / MCP projections (Constitution V). The document block is added in US2.

// chunkOut is the CLI JSON projection of a chunk. Its field names mirror the
// proto Chunk / REST chunkDTO 1:1 (snake_case) so a chunk printed by the CLI is
// byte-identical — after normalisation — to the same chunk over gRPC/REST/MCP
// (cross-transport parity, spec 035 US3). The Poisoning / NearDup sidecars reuse
// the model types directly (their JSON tags already match the proto messages).
type chunkOut struct {
	ChunkID         string               `json:"chunk_id"`
	DocumentID      string               `json:"document_id"`
	Content         string               `json:"content"`
	ChunkIndex      int                  `json:"chunk_index"`
	TotalChunks     int                  `json:"total_chunks"`
	PageNumber      int                  `json:"page_number"`
	StartChar       int                  `json:"start_char"`
	EndChar         int                  `json:"end_char"`
	TokenCount      int                  `json:"token_count"`
	PreviousChunkID string               `json:"previous_chunk_id"`
	NextChunkID     string               `json:"next_chunk_id"`
	Poisoning       *model.PoisonVerdict `json:"poisoning,omitempty"`
	SectionContext  []string             `json:"section_context,omitempty"`
	Wikilinks       []string             `json:"wikilinks,omitempty"` // spec 036 / BL-004
	NearDup         *model.NearDupInfo   `json:"near_dup,omitempty"`
	Kind            string               `json:"kind,omitempty"`
	CreatedAt       string               `json:"created_at,omitempty"`
}

func newChunkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chunk",
		Short: "Fetch a single chunk by its content-addressed ID (spec 035)",
	}
	cmd.AddCommand(newChunkGetCmd())
	return cmd
}

// newChunkGetCmd resolves a chunk_id to its full chunk. JSON is the default
// format (scripting/bridge friendly); --format text prints a readable block.
func newChunkGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <chunk_id>",
		Short: "Resolve a chunk_id to its full chunk (+ parent document metadata)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := engine.NewWithDB(cfg, db).GetChunk(args[0])
			if err != nil {
				return err
			}
			resp := getChunkResponseOut{Chunk: toChunkOut(res.Chunk)}
			if res.Document.ID != "" { // US2/spec 035: parent-document metadata (nil for an orphan chunk)
				resp.Document = toDocumentOut(res.Document, res.Source)
			}
			if format == "json" {
				return json.NewEncoder(os.Stdout).Encode(resp)
			}
			printChunkText(resp.Chunk)
			if resp.Document != nil {
				printDocumentText(*resp.Document)
			}
			return nil
		},
	}
	cmd.Flags().StringP("format", "f", "json", "output format: json|text")
	return cmd
}

func toChunkOut(c model.Chunk) chunkOut {
	created := ""
	if !c.CreatedAt.IsZero() {
		created = c.CreatedAt.UTC().Format(time.RFC3339)
	}
	return chunkOut{
		ChunkID:         c.ID,
		DocumentID:      c.DocumentID,
		Content:         c.Content,
		ChunkIndex:      c.ChunkIndex,
		TotalChunks:     c.TotalChunks,
		PageNumber:      c.PageNumber,
		StartChar:       c.StartCharIdx,
		EndChar:         c.EndCharIdx,
		TokenCount:      c.TokenCount,
		PreviousChunkID: c.PreviousChunkID,
		NextChunkID:     c.NextChunkID,
		Poisoning:       c.Poisoning,
		SectionContext:  c.SectionContext,
		Wikilinks:       c.Wikilinks, // spec 036 / BL-004
		NearDup:         c.NearDup,
		Kind:            c.Kind,
		CreatedAt:       created,
	}
}

// fmtRFC3339 renders a time as UTC RFC3339, "" for the zero value.
func fmtRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// getChunkResponseOut is the CLI JSON envelope — {chunk, document} — matching the
// proto GetChunkResponse / REST shape 1:1 (cross-transport parity, spec 035 US3).
type getChunkResponseOut struct {
	Chunk    chunkOut     `json:"chunk"`
	Document *documentOut `json:"document,omitempty"`
}

// documentOut is the CLI JSON projection of model.Document (+ spec-029
// EnrichInfo flattened). Field names mirror proto DocumentMeta / the REST DTO.
type documentOut struct {
	ID               string   `json:"id"`
	ContentHash      string   `json:"content_hash"`
	SourceID         string   `json:"source_id"`
	SourcePath       string   `json:"source_path"`
	FilePath         string   `json:"file_path"`
	FileName         string   `json:"file_name"`
	FileType         string   `json:"file_type"`
	MimeType         string   `json:"mime_type"`
	ChunkCount       int      `json:"chunk_count"`
	FileSize         int64    `json:"file_size"`
	Status           string   `json:"status"`
	IngestedAt       string   `json:"ingested_at"`
	UpdatedAt        string   `json:"updated_at"`
	Tags             []string `json:"tags,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	EnrichmentStatus string   `json:"enrichment_status,omitempty"`
	EnrichmentModel  string   `json:"enrichment_model,omitempty"`
	EnrichmentAt     string   `json:"enrichment_at,omitempty"`
}

func toDocumentOut(d model.Document, src model.Source) *documentOut {
	o := &documentOut{
		ID:          d.ID,
		ContentHash: d.ContentHash,
		SourceID:    d.SourceID,
		SourcePath:  src.Path,
		FilePath:    d.FilePath,
		FileName:    d.FileName,
		FileType:    d.FileType,
		MimeType:    d.MimeType,
		ChunkCount:  d.ChunkCount,
		FileSize:    d.FileSize,
		Status:      d.Status,
		IngestedAt:  fmtRFC3339(d.IngestedAt),
		UpdatedAt:   fmtRFC3339(d.UpdatedAt),
	}
	if d.Enrichment != nil {
		o.Tags = d.Enrichment.Tags
		o.Summary = d.Enrichment.Summary
		o.EnrichmentStatus = d.Enrichment.Status
		o.EnrichmentModel = d.Enrichment.Model
		o.EnrichmentAt = fmtRFC3339(d.Enrichment.GeneratedAt)
	}
	return o
}

func printChunkText(c chunkOut) {
	fmt.Printf("chunk_id: %s\n", c.ChunkID)
	fmt.Printf("document_id: %s\n", c.DocumentID)
	if c.PageNumber > 0 {
		fmt.Printf("page: %d\n", c.PageNumber)
	}
	fmt.Printf("position: %d/%d (chars %d..%d, %d tokens)\n", c.ChunkIndex, c.TotalChunks, c.StartChar, c.EndChar, c.TokenCount)
	if c.Kind != "" {
		fmt.Printf("kind: %s\n", c.Kind)
	}
	if len(c.SectionContext) > 0 {
		fmt.Printf("section: %s\n", strings.Join(c.SectionContext, " / "))
	}
	if len(c.Wikilinks) > 0 { // spec 036 / BL-004
		fmt.Printf("wikilinks: %s\n", strings.Join(c.Wikilinks, ", "))
	}
	if c.Poisoning != nil {
		fmt.Printf("poisoning: %s (score %.2f)\n", c.Poisoning.Level, c.Poisoning.Score)
	}
	if c.PreviousChunkID != "" {
		fmt.Printf("prev: %s\n", c.PreviousChunkID)
	}
	if c.NextChunkID != "" {
		fmt.Printf("next: %s\n", c.NextChunkID)
	}
	fmt.Println("--- content ---")
	fmt.Println(c.Content)
}

func printDocumentText(d documentOut) {
	fmt.Println("--- document ---")
	fmt.Printf("id: %s\n", d.ID)
	if d.ContentHash != "" {
		fmt.Printf("content_hash: %s\n", d.ContentHash)
	}
	fmt.Printf("file: %s (%s)", d.FilePath, d.FileType)
	if d.MimeType != "" {
		fmt.Printf("; %s", d.MimeType)
	}
	fmt.Println()
	if d.SourcePath != "" {
		fmt.Printf("source: %s\n", d.SourcePath)
	}
	fmt.Printf("status: %s | chunks: %d\n", d.Status, d.ChunkCount)
	if d.Summary != "" {
		fmt.Printf("summary: %s\n", d.Summary)
	}
	if len(d.Tags) > 0 {
		fmt.Printf("tags: %s\n", strings.Join(d.Tags, ", "))
	}
}
