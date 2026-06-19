// Package model defines the go-rag data model (PRD §6).
//
// Entity chain: Source 1:N Document 1:N Chunk 1:1 Embedding.
// All identities are SHA-256 content-addressed (PRD §7.2) for idempotent ingestion.
package model

import "time"

// Source is a watched directory or file collection (PRD §6.2). Pebble prefix 0x01.
type Source struct {
	ID        string    `json:"id"`         // SHA-256 of canonical path
	Path      string    `json:"path"`       // absolute directory path
	Kind      string    `json:"kind"`       // "directory" | "file"
	AddedAt   time.Time `json:"added_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Document is a single ingested file, content-addressed (PRD §6.3). Pebble prefix 0x02.
type Document struct {
	ID          string         `json:"id"`           // SHA-256(content + metadata)
	SourceID    string         `json:"source_id"`    // FK -> Source
	FilePath    string         `json:"file_path"`    // relative path from source root
	FileName    string         `json:"file_name"`
	FileType    string         `json:"file_type"`    // pdf|text|markdown|docx|jpeg|png
	MimeType    string         `json:"mime_type"`
	ContentHash string         `json:"content_hash"` // SHA-256 of raw bytes (change detection)
	Metadata    map[string]any `json:"metadata"`
	ChunkCount  int            `json:"chunk_count"`
	FileSize    int64          `json:"file_size"`
	IngestedAt  time.Time      `json:"ingested_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Status      string         `json:"status"` // pending|embedded|error
}

// Chunk is a text segment split from a Document (PRD §6.4). Pebble prefix 0x03.
type Chunk struct {
	ID              string    `json:"id"`               // SHA-256(chunk text + metadata)
	DocumentID      string    `json:"document_id"`      // FK -> Document
	Content         string    `json:"content"`
	ChunkIndex      int       `json:"chunk_index"`      // 0-based position
	TotalChunks     int       `json:"total_chunks"`
	StartCharIdx    int       `json:"start_char_idx"`
	EndCharIdx      int       `json:"end_char_idx"`
	PageNumber      int       `json:"page_number"`      // PDF only, 0 otherwise
	PreviousChunkID string    `json:"previous_chunk_id"` // linked list
	NextChunkID     string    `json:"next_chunk_id"`
	TokenCount      int       `json:"token_count"`
	CreatedAt       time.Time `json:"created_at"`
}

// Embedding is a vector for a Chunk (PRD §6.5). Pebble prefix 0x04 (metadata only;
// the vector itself lives in chromem-go).
type Embedding struct {
	ChunkID    string    `json:"chunk_id"` // FK -> Chunk (1:1)
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
	Vector     []float32 `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
}
