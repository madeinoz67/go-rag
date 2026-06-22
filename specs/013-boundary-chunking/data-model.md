# Data Model: Boundary-Aware Chunking Cascade (H10)

> No persisted entity changes. The `Chunk` record (prefix 0x03), its identity
> hash, and all storage are untouched (Principle II). The `Segment` struct is
> unchanged; only how it is *produced* changes. This file documents the preserved
> `Split` contract and the internal boundary units.

## Preserved contract: `Splitter.Split`

```go
type Segment struct {
    Text         string
    StartCharIdx int
    EndCharIdx   int
    TokenCount   int
}

func (s *Splitter) Split(text string) []Segment   // signature UNCHANGED
```

| Aspect | Before (word-window) | After (cascade) |
|--------|----------------------|-----------------|
| Input | source text | source text (unchanged) |
| Output | `[]Segment` of ~Size tokens, word-window cuts | `[]Segment` of ~Size tokens, **boundary-aware** cuts |
| Offsets | char offsets into source | char offsets into source (preserved, accurate) |
| TokenCount | `EstimateTokens` (1.3× words) | `EstimateTokens` (unchanged) |
| Callers | `pipeline.processFile`, 4 CLI `NewSplitter` sites | **unchanged** — same signature |

The cascade is an internal rewrite of `Split`'s body; nothing the pipeline, CLI,
or transports see changes shape.

## Internal boundary units (not persisted)

### Paragraph

A block of text separated from neighbors by a blank line (`\n\n`). Detected by
`splitParagraphs`. The markdown reader's `stripMarkdown` preserves blank lines
(verified), so paragraph structure reaches the splitter. Preferred **flush point**:
a chunk does not span a paragraph boundary when the paragraph fits the size budget.

### Sentence

A run of text ending at a sentence terminator — ASCII `. ! ?` or CJK `。！？` —
followed by whitespace / end-of-paragraph. Detected by `splitSentences` (rule-based,
linear scan, char offsets tracked). The **fill unit**: chunks are assembled from
whole sentences up to the size budget.

### Word (fallback)

The existing whitespace-delimited token (`tokenizeWords`). Used only as the
**fallback** when a single sentence exceeds the size budget — that sentence is
word-windowed so no chunk ever exceeds the budget.

## Assembly (the cascade)

```text
text
  └─ splitParagraphs (\n\n) ──► []paragraph
        └─ splitSentences (terminator + CJK) ──► []sentence (with char offsets)
              └─ greedy-fill to Size budget:
                   • add whole sentences while under budget
                   • flush at sentence boundary when next would overflow
                   • flush at paragraph boundary (don't span paragraphs that fit)
                   • over-long sentence → word-window fallback (tokenizeWords)
                   • carry last sentence(s) within Overlap budget into next chunk
                   • merge sub-MinTokens final tail into predecessor
              ──► []Segment (Text + accurate offsets + TokenCount)
```

## Validation rules (from requirements)

- **FR-002**: chunk ends at a sentence boundary whenever one falls in the window.
- **FR-003**: paragraph breaks honored (no spanning when the paragraph fits).
- **FR-004**: over-long sentence → word-split (graceful), never one oversized chunk.
- **FR-005**: package doc accurately describes the cascade.
- **FR-006**: no retrieval regression (eval gate).
- **FR-007**: neighbor chunks share overlap (now sentence-granularity).

No persisted state machine; no migration.
