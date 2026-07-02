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
	ChunkID           string               `json:"chunk_id"`
	DocumentID        string               `json:"document_id"`
	Content           string               `json:"content"`
	ChunkIndex        int                  `json:"chunk_index"`
	TotalChunks       int                  `json:"total_chunks"`
	PageNumber        int                  `json:"page_number"`
	StartChar         int                  `json:"start_char"`
	EndChar           int                  `json:"end_char"`
	TokenCount        int                  `json:"token_count"`
	PreviousChunkID   string               `json:"previous_chunk_id"`
	NextChunkID       string               `json:"next_chunk_id"`
	Poisoning         *model.PoisonVerdict `json:"poisoning,omitempty"`
	SectionContext    []string             `json:"section_context,omitempty"`
	SectionDepth      int                  `json:"section_depth,omitempty"`      // spec 041 / BL-005
	ExtractionQuality float64              `json:"extraction_quality,omitempty"` // spec 042 / BL-006
	ExtractionMethod  string               `json:"extraction_method,omitempty"`  // spec 042 / BL-006
	Wikilinks         []string             `json:"wikilinks,omitempty"`          // spec 036 / BL-004
	NearDup           *model.NearDupInfo   `json:"near_dup,omitempty"`
	Kind              string               `json:"kind,omitempty"`
	CreatedAt         string               `json:"created_at,omitempty"`
}

func newChunkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chunk",
		Short: "Fetch a single chunk by its content-addressed ID (spec 035)",
	}
	cmd.AddCommand(newChunkGetCmd())
	cmd.AddCommand(newChunkContextCmd()) // spec 037
	cmd.AddCommand(newChunkBatchCmd())   // spec 038
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
		ChunkID:           c.ID,
		DocumentID:        c.DocumentID,
		Content:           c.Content,
		ChunkIndex:        c.ChunkIndex,
		TotalChunks:       c.TotalChunks,
		PageNumber:        c.PageNumber,
		StartChar:         c.StartCharIdx,
		EndChar:           c.EndCharIdx,
		TokenCount:        c.TokenCount,
		PreviousChunkID:   c.PreviousChunkID,
		NextChunkID:       c.NextChunkID,
		Poisoning:         c.Poisoning,
		SectionContext:    c.SectionContext,
		SectionDepth:      c.SectionLevel,      // spec 041 / BL-005
		ExtractionQuality: c.ExtractionQuality, // spec 042 / BL-006
		ExtractionMethod:  c.ExtractionMethod,  // spec 042 / BL-006
		Wikilinks:         c.Wikilinks,         // spec 036 / BL-004
		NearDup:           c.NearDup,
		Kind:              c.Kind,
		CreatedAt:         created,
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

// getContextResponseOut is the CLI JSON envelope for GetChunkContext — {chunks,
// target_index, document} — mirroring the proto GetChunkContextResponse / REST
// shape 1:1 (cross-transport parity, spec 037 US3).
type getContextResponseOut struct {
	Chunks      []chunkOut   `json:"chunks"`
	TargetIndex int          `json:"target_index"`
	Document    *documentOut `json:"document,omitempty"`
}

// newChunkContextCmd resolves a chunk_id to its chunk plus up to `window`
// neighbours on each side (spec 037 / BL-002). JSON is the default format;
// --format text prints the ordered window with the target marked (>>>), its
// index, the target's content, and the parent document. Default --window 2; 0
// returns exactly the target (≡ GetChunk); >10 exits non-zero with a message.
func newChunkContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context <chunk_id>",
		Short: "Resolve a chunk_id to its chunk plus up to N neighbours each side (spec 037)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			window, _ := cmd.Flags().GetInt("window")
			if window < 0 || window > engine.MaxChunkContextWindow() {
				return fmt.Errorf("window must be 0..%d, got %d", engine.MaxChunkContextWindow(), window)
			}
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := engine.NewWithDB(cfg, db).GetChunkContext(args[0], window)
			if err != nil {
				return err
			}
			resp := getContextResponseOut{Chunks: make([]chunkOut, len(res.Chunks)), TargetIndex: res.TargetIndex}
			for i, c := range res.Chunks {
				resp.Chunks[i] = toChunkOut(c)
			}
			if res.Document.ID != "" { // orphan chunk → document omitted (nil)
				resp.Document = toDocumentOut(res.Document, res.Source)
			}
			if format == "json" {
				return json.NewEncoder(os.Stdout).Encode(resp)
			}
			printChunkContextText(resp)
			return nil
		},
	}
	cmd.Flags().StringP("format", "f", "json", "output format: json|text")
	cmd.Flags().Int("window", engine.DefaultChunkContextWindow(), "neighbours per side (0..10; default 2; 0 = target only)")
	return cmd
}

// printChunkContextText renders the ordered window as a numbered list with the
// target chunk marked (>>>), its target_index, the target's content, and the
// parent document line. Mirrors the MCP text render (cross-transport parity).
func printChunkContextText(resp getContextResponseOut) {
	for i, c := range resp.Chunks {
		marker := "   "
		if i == resp.TargetIndex {
			marker = ">>>"
		}
		fmt.Printf("%s [%d] %s\n", marker, i, c.ChunkID)
	}
	fmt.Printf("target_index: %d\n", resp.TargetIndex)
	if resp.TargetIndex >= 0 && resp.TargetIndex < len(resp.Chunks) {
		fmt.Println("--- target content ---")
		fmt.Println(resp.Chunks[resp.TargetIndex].Content)
	}
	if resp.Document != nil {
		printDocumentText(*resp.Document)
	}
}

// batchItemOut is one positional CLI result entry (the requested chunk_id, the
// resolved chunk + document, or a non-empty error when not found). Field names
// mirror the proto BatchGetChunksResult / REST DTO (snake_case) for parity.
type batchItemOut struct {
	ChunkID  string       `json:"chunk_id"`
	Chunk    *chunkOut    `json:"chunk,omitempty"`
	Error    string       `json:"error,omitempty"`
	Document *documentOut `json:"document,omitempty"`
}

// batchResponseOut is the CLI JSON envelope for BatchGetChunks — {results} —
// mirroring the proto BatchGetChunksResponse / REST shape 1:1 (parity, spec 038).
type batchResponseOut struct {
	Results []batchItemOut `json:"results"`
}

// newChunkBatchCmd resolves up to 100 chunk_ids (positional args) in one call
// (spec 038 / BL-003). JSON default; --format text prints one line per result
// (chunk_id, ok / "not found", document). Rejects >100 args with a non-zero exit.
// Missing ids are per-id errors — the command exits 0 as long as the request is
// structurally valid (mirrors the per-id-error model).
func newChunkBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch <chunk_id> [<chunk_id>...]",
		Short: "Resolve up to 100 chunk_ids in one call (spec 038)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > engine.MaxBatchGetChunks() {
				return fmt.Errorf("max %d chunk_ids, got %d", engine.MaxBatchGetChunks(), len(args))
			}
			format, _ := cmd.Flags().GetString("format")
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := engine.NewWithDB(cfg, db).BatchGetChunks(args)
			if err != nil {
				return err
			}
			resp := batchResponseOut{Results: make([]batchItemOut, len(res.Results))}
			for i, it := range res.Results {
				out := batchItemOut{ChunkID: it.ChunkID, Error: it.Err}
				if it.Err == "" {
					c := toChunkOut(it.Chunk)
					out.Chunk = &c
					if it.Document.ID != "" {
						out.Document = toDocumentOut(it.Document, it.Source)
					}
				}
				resp.Results[i] = out
			}
			if format == "json" {
				return json.NewEncoder(os.Stdout).Encode(resp)
			}
			printBatchText(resp)
			return nil
		},
	}
	cmd.Flags().StringP("format", "f", "json", "output format: json|text")
	return cmd
}

func printBatchText(resp batchResponseOut) {
	for _, r := range resp.Results {
		if r.Error != "" {
			fmt.Printf("%s: %s\n", r.ChunkID, r.Error)
			continue
		}
		fmt.Printf("%s: ok", r.ChunkID)
		if r.Document != nil {
			fmt.Printf(" (%s)", r.Document.FilePath)
		}
		fmt.Println()
	}
}
