package muninn

import (
	"github.com/madeinoz67/go-rag/internal/model"
)

// Mapper converts go-rag chunks + their parent document into the MuninnDB
// WriteParams the bridge promotes. It is the load-bearing translation for US1
// (data-model.md E4 / contracts/muninn-grpc-client.md). The maintainer's
// write invariants (research.md R4) are enforced HERE:
//
//   - Embedding is always nil — MuninnDB re-embeds via its own pipeline; passing
//     go-rag's vectors would double-store and silently break vector search when
//     dims differ.
//   - Stability is 30.0 — the Ebbinghaus anchor for reference material (the
//     default is tuned for conversational memory).
//   - IdempotentID is "chunk:"+chunkID — content-addressed, so re-promotion of an
//     unchanged chunk lands as a strict MuninnDB-side no-op (NFR-002).
//   - UpsertMode is always true (requires the non-empty IdempotentID above).
//
// Associations are intentionally nil in v1: a wikilink target is a page NAME, not
// an engram id, so it can't be a pre-declared Association.TargetID. The Hebbian
// wikilink→Link edge is a post-promotion step (both endpoints must exist first)
// and is deferred with the on-query hook (research.md R8).
type Mapper struct {
	SourceVault string // the go-rag vault being bridged (provenance tag)
	TargetVault string // the dedicated MuninnDB vault
}

// Map translates one chunk into a WriteParams.
func (m Mapper) Map(c model.Chunk, doc model.Document) WriteParams {
	return WriteParams{
		Concept:      concept(c, doc),
		Content:      c.Content,
		Vault:        m.TargetVault,
		Stability:    30.0, // maintainer invariant (research.md R4)
		Confidence:   1.0,
		IdempotentID: "chunk:" + c.ID,
		UpsertMode:   true,
		TypeLabel:    "go-rag-chunk",
		Embedding:    nil, // maintainer invariant — MuninnDB re-embeds
		Tags:         m.tags(c, doc),
	}
}

// MapAll translates a document's chunks. The doc is shared (title/filename for
// the concept cascade); each chunk maps independently.
func (m Mapper) MapAll(chunks []model.Chunk, doc model.Document) []WriteParams {
	out := make([]WriteParams, len(chunks))
	for i, c := range chunks {
		out[i] = m.Map(c, doc)
	}
	return out
}

// tags builds the provenance tag set: ["go-rag", sourceVault, fileType] plus the
// "low-quality" marker when the PDF reader's extraction_quality is below 0.5
// (spec 042 / BL-006) — so MuninnDB-side filtering can down-rank sparse extractions.
func (m Mapper) tags(c model.Chunk, doc model.Document) []string {
	tags := make([]string, 0, 5)
	tags = append(tags, "go-rag", m.SourceVault, doc.FileType)
	if 0 < c.ExtractionQuality && c.ExtractionQuality < 0.5 {
		tags = append(tags, "low-quality")
	}
	return tags
}
