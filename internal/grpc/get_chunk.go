package grpc

import (
	"time"

	"github.com/madeinoz67/go-rag/internal/model"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// get_chunk.go holds the spec 035 DocumentMeta projection for GetChunk (US2).
// toChunkPB + the GetChunk handler live in engine_adapter.go; this file adds the
// parent-document projection that makes resolving a chunk a single round-trip.

// formatRFC3339 renders a time as UTC RFC3339, returning "" for the zero value so
// unset timestamps serialize as absent (not "0001-01-01T00:00:00Z").
func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// toDocumentMetaPB maps a model.Document (+ its Source, for source_path) to the
// proto DocumentMeta projection (spec 035 US2). Identity hash (id) and change-
// detection hash (content_hash) are kept distinct (PRD §7.2). Enrichment
// (spec 029) is flattened from *EnrichInfo when present. Caller passes a zero
// Source when the source record is absent — source_path then serializes as "".
func toDocumentMetaPB(d model.Document, src model.Source) *goragpb.DocumentMeta {
	m := &goragpb.DocumentMeta{
		Id:          d.ID,
		ContentHash: d.ContentHash,
		SourceId:    d.SourceID,
		SourcePath:  src.Path,
		FilePath:    d.FilePath,
		FileName:    d.FileName,
		FileType:    d.FileType,
		MimeType:    d.MimeType,
		ChunkCount:  int32(d.ChunkCount),
		FileSize:    d.FileSize,
		Status:      d.Status,
		IngestedAt:  formatRFC3339(d.IngestedAt),
		UpdatedAt:   formatRFC3339(d.UpdatedAt),
	}
	if d.Enrichment != nil {
		m.Tags = d.Enrichment.Tags
		m.Summary = d.Enrichment.Summary
		m.EnrichmentStatus = d.Enrichment.Status
		m.EnrichmentModel = d.Enrichment.Model
		m.EnrichmentAt = formatRFC3339(d.Enrichment.GeneratedAt)
	}
	return m
}
