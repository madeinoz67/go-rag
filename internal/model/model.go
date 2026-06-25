// Package model defines the go-rag data model (PRD §6).
//
// Entity chain: Source 1:N Document 1:N Chunk 1:1 Embedding.
// All identities are SHA-256 content-addressed (PRD §7.2) for idempotent ingestion.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// Source is a watched directory or file collection (PRD §6.2). Pebble prefix 0x01.
type Source struct {
	ID        string    `json:"id"`   // SHA-256 of canonical path
	Path      string    `json:"path"` // absolute directory path
	Kind      string    `json:"kind"` // "directory" | "file"
	AddedAt   time.Time `json:"added_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Document is a single ingested file, content-addressed (PRD §6.3). Pebble prefix 0x02.
type Document struct {
	ID          string         `json:"id"`        // GenerateID() — SHA-256(content + metadata)
	SourceID    string         `json:"source_id"` // FK -> Source
	FilePath    string         `json:"file_path"` // relative path from source root
	FileName    string         `json:"file_name"`
	FileType    string         `json:"file_type"` // pdf|text|markdown|docx|jpeg|png
	MimeType    string         `json:"mime_type"`
	ContentHash string         `json:"content_hash"` // ContentHash(raw bytes) — change detection
	Metadata    map[string]any `json:"metadata"`
	ChunkCount  int            `json:"chunk_count"`
	FileSize    int64          `json:"file_size"`
	IngestedAt  time.Time      `json:"ingested_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Status      string         `json:"status"` // pending|embedded|error

	// Enrichment is the per-document auto-tag + summary sidecar (spec 029),
	// produced async-after-ACK by the local model. nil for unenriched /
	// pre-feature / enrichment-off docs — treated as absent (never an error) at
	// retrieval. A non-identity sidecar (like the Chunk sidecars Poisoning /
	// SectionContext / NearDup): it is a SEPARATE struct field, NOT a Metadata
	// key, so it never enters GenerateID — document identity, content hash, and
	// idempotent re-add are unchanged. Tags feed the existing tag filter via a
	// bridge; the summary is surfaced on status/hits.
	Enrichment *EnrichInfo `json:"enrichment,omitempty"`
}

// GenerateID returns the SHA-256 over content + mime type + canonicalized (sorted)
// metadata — the canonical document identity (Principle II, PRD §7.2). Content is
// passed explicitly because the full text is not persisted on Document (it is split
// into Chunks). Order-independent: equal inputs produce equal IDs regardless of map
// iteration order.
func GenerateID(content, mimeType string, metadata map[string]any) string {
	h := sha256.New()
	h.Write([]byte(content))
	h.Write([]byte(mimeType))
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%v", k, metadata[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ContentHash returns the SHA-256 of raw file bytes — used for change detection.
// Distinct from GenerateID (which also covers metadata), so content can be
// re-embedded under a new model without creating a duplicate document.
func ContentHash(raw []byte) string {
	h := sha256.New()
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

// Chunk is a text segment split from a Document (PRD §6.4). Pebble prefix 0x03.
type Chunk struct {
	ID              string    `json:"id"`          // SHA-256(chunk text + metadata)
	DocumentID      string    `json:"document_id"` // FK -> Document
	Content         string    `json:"content"`
	ChunkIndex      int       `json:"chunk_index"` // 0-based position
	TotalChunks     int       `json:"total_chunks"`
	StartCharIdx    int       `json:"start_char_idx"`
	EndCharIdx      int       `json:"end_char_idx"`
	PageNumber      int       `json:"page_number"`       // PDF only, 0 otherwise
	PreviousChunkID string    `json:"previous_chunk_id"` // linked list
	NextChunkID     string    `json:"next_chunk_id"`
	TokenCount      int       `json:"token_count"`
	CreatedAt       time.Time `json:"created_at"`
	// Poisoning is the per-chunk injection-poisoning verdict (spec 019 / H04),
	// scored at ingest and persisted on this record. nil only on chunks ingested
	// before this feature or when detection is disabled — treated as clean at
	// retrieval. Surfaced on QueryHit across all transports.
	Poisoning *PoisonVerdict `json:"poisoning,omitempty"`
	// SectionContext is the ordered heading breadcrumb active at the chunk's
	// start position in the source document (top-level → governing heading),
	// e.g. ["Operations", "Backups", "Retention"]. Derived positionally from
	// the reader's heading structure during chunking (audit H23 / spec 025). nil
	// for chunks whose source has no headings and for chunks written before this
	// feature — treated as absent (never an error) at retrieval (FR-006). A
	// non-identity sidecar (like Poisoning): it does NOT participate in the
	// chunk ID (GenerateID folds text+mime+{doc,idx} only — pipeline.go:252) and
	// the span data is removed from document metadata before GenerateID, so
	// neither document nor chunk identity changes. Surfaced on QueryHit across
	// every transport (FR-004).
	SectionContext []string `json:"section_context,omitempty"`
	// NearDup describes this chunk's near-duplicate relationships (audit H20 /
	// spec 026): the chunkIDs within the configured SimHash Hamming distance
	// (pairwise siblings) and the closest sibling's similarity. nil for chunks
	// with no near-duplicates and for chunks ingested before the feature —
	// treated as absent (never an error) at retrieval (FR-008). A non-identity
	// sidecar (like Poisoning / SectionContext): it does NOT participate in chunk
	// or document identity. Populated async-after-ACK by the ingest worker's
	// clustering pass; opt-in query-time collapse reads it.
	NearDup *NearDupInfo `json:"near_dup,omitempty"`
}

// NearDupInfo is the per-chunk near-duplicate verdict (audit H20 / spec 026).
// Siblings are the chunkIDs within the configured Hamming distance (pairwise —
// no transitivity); Similarity is the closest sibling's normalised similarity in
// [0,1]. A chunk with no near-duplicates has NearDup == nil (never an empty
// NearDupInfo), so absent and "none" serialize identically (FR-008).
type NearDupInfo struct {
	Siblings   []string `json:"siblings,omitempty"`
	Similarity float64  `json:"similarity,omitempty"`
}

// EnrichInfo is the per-document enrichment sidecar (spec 029): auto-generated
// tags + summary from the local model, with provenance and a status that drives
// retry/visibility. Non-identity; stored on Document.Enrichment. Absent (nil)
// for unenriched / pre-feature / enrichment-off documents.
type EnrichInfo struct {
	Tags        []string  `json:"tags,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Model       string    `json:"model,omitempty"`
	GeneratedAt time.Time `json:"generated_at,omitempty"`
	Status      string    `json:"status,omitempty"` // enriched | failed | nothing-to-enrich
}

// Enrichment sidecar statuses (spec 029).
const (
	EnrichStatusDone    = "enriched"
	EnrichStatusFailed  = "failed"
	EnrichStatusNothing = "nothing-to-enrich"
)

// Embedding is a vector for a Chunk (PRD §6.5). Pebble prefix 0x04 (metadata only;
// the vector itself lives in chromem-go).
type Embedding struct {
	ChunkID    string    `json:"chunk_id"` // FK -> Chunk (1:1)
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
	Vector     []float32 `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
}
