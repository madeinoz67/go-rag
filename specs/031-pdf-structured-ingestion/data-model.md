# Data Model — PDF Structured Ingestion (spec 031)

**Phase 1 output.** This feature adds no new persisted entity — it enhances the
existing `FileReader` implementations (PDF/DOCX/text) to produce richer `content`
+ `metadata` from the `Read()` call. The `HeadingSpan` mechanism (spec 025),
`Chunk.SectionContext`, and `Document.Metadata` are all existing surfaces, reused
unchanged. Image captioning adds a background `Captioner` interface (sibling of
`Enricher`, spec 029).

---

## 1. What each reader produces after enhancement

### PDF reader (`internal/reader/pdf.go`)
| Output | Source | Today | After |
|--------|--------|-------|-------|
| Text | content stream (Tj/TJ) | ✅ | ✅ (unchanged) |
| Metadata | Info dictionary | ❌ (format + page_count only) | Title, Author, Subject, Keywords, CreationDate, ModDate |
| Heading spans | bookmarks/outline or font-size heuristics | ❌ | `[]HeadingSpan` via `metadata["heading_spans"]` |
| Tables | content-stream text positions | ❌ (garbled) | Grid-detected → Markdown table in content |
| Images | `api.ExtractImages` | ❌ | `[]ImageData{Position, Bytes}` via `metadata["images"]` |

### DOCX reader (`internal/reader/docx.go`)
| Output | Source | Today | After |
|--------|--------|-------|-------|
| Text | document.xml text | ✅ | ✅ (unchanged) |
| Heading spans | `<w:pStyle w:val="HeadingN"/>` | ❌ | `[]HeadingSpan` (richest signal of any format) |

### Text reader (`internal/reader/text.go`)
| Output | Source | Today | After |
|--------|--------|-------|-------|
| Text | raw file | ✅ | ✅ (unchanged) |
| Heading spans | pattern heuristics | ❌ | `[]HeadingSpan` (ALL CAPS, underlines, `:` lines) — best-effort |

### Markdown reader (`internal/reader/markdown.go`)
Unchanged — already extracts heading spans (spec 025). ✅

---

## 2. The Captioner interface (NEW — sibling of Enricher, spec 029)

```go
// Captioner generates a text description for an image (spec 031 US4). The local
// multimodal model (e.g. llava) produces a caption that describes the image's
// content — for charts, describing the data trend/values. Background, opt-in.
type Captioner interface {
    Caption(ctx context.Context, imageBytes []byte) (string, error)
    Model() string
}
```

Called by the pipeline (background, post-ACK) for each extracted image. The caption
is spliced into the content at the image's position (before chunking). Circuit-
breaker-guarded (reuse the spec 029/030 primitive). The image bytes are discarded
after captioning.

---

## 3. Lifecycle

```text
Read (FileReader.Read):
  PDF  → text + metadata(Info dict) + heading_spans(outline/heuristics) + tables(grid detect) + images(extract)
  DOCX → text + heading_spans(pStyle)
  Text → text + heading_spans(heuristics)

processFile (pipeline):
  redact → resolve SectionContext from heading_spans (spec 025, unchanged) →
  IF captioner bound + images present:
    for each image → captioner.Caption(bytes) → splice caption into content at image position
  → chunk (content now includes table text + image captions) → store → ACK
```

No new persisted entity. No new Pebble prefix. The heading spans, table text, and
image captions all flow through the existing `content` + `metadata["heading_spans"]`
surfaces into chunks. The `Chunk.SectionContext`, `Chunk.Content`, and
`Document.Metadata` are unchanged in shape — just richer.
