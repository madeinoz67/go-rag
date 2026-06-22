# Phase 0 — Research: Boundary-Aware Chunking Cascade (H10)

> Each item: Decision · Rationale · Alternatives rejected. Grounded in code read
> this session: `internal/chunk/chunk.go` (the word-window `Split`, `Splitter`,
> `Segment`, `EstimateTokens`, `tokenizeWords`, `buildSegment`, the false package
> doc), `internal/pipeline/pipeline.go:206` (`p.splitter.Split(content)`), and
> `internal/reader/markdown.go:142` (`stripMarkdown` — **confirmed it preserves
> `\n\n` paragraph breaks**, line-by-line rebuild keeps blank lines as `\n`).

## 1. Cascade structure: paragraph → sentence → word (greedy-fill)

**Decision**: `Split` becomes a structural cascade:
1. Split text into **paragraphs** on blank-line boundaries (`\n\n`, preserved by
   the markdown reader).
2. Within a paragraph, split into **sentences** at terminator boundaries.
3. **Greedily fill** a chunk with whole sentences until the next sentence would
   exceed the size budget; flush there.
4. Prefer flushing at **paragraph boundaries** — don't let a chunk silently span
   two paragraphs when the paragraph fits the budget.
5. **Word fallback**: a single sentence larger than the budget is split with the
   existing word-window (the cascade's word level) — never one oversized chunk.

**Rationale**: This is exactly the "paragraph → sentence → word cascade" the
package doc already claims, and the book §3.2 boundary-aware shape. Greedy-fill on
sentences with paragraph-boundary preference yields coherent chunks (whole ideas,
respecting structure). The word fallback guarantees no chunk ever exceeds the
budget (the over-long-sentence case the spec calls out).

**Alternatives rejected**:
- *Sentence-only (skip paragraph level)*: loses the paragraph-coherence win the
  doc claims and the audit wants. Rejected.
- *Fixed sentence count per chunk*: ignores token budget → uneven chunk sizes.
  Rejected (greedy-fill on the token budget is correct).
- *Recursive structural splitter (langchain-style)*: more machinery than needed;
  the greedy-fill cascade is simpler and sufficient. Rejected for v1.

## 2. Sentence detection: rule-based, terminator + CJK, no NLP

**Decision**: `splitSentences` scans for sentence terminators — `. ! ?` (ASCII)
and `。！？` (CJK) — followed by whitespace, end-of-paragraph, or another sentence.
Abbreviations are NOT special-cased (see tradeoff). Each sentence carries its char
offsets in the source (for `Segment.StartCharIdx`/`EndCharIdx`).

**Rationale**: Pure Go, O(text), no dependency (Principle III). CJK terminators
are included so CJK prose isn't mis-boundaried (the spec's CJK edge case).
Char-offset fidelity preserves the existing `Segment` contract.

**Tradeoff — abbreviations**: "Dr." / "e.g." / "Inc." will split early (treated as
sentence ends). This produces slightly more, shorter chunks for abbreviation-heavy
text but is **safe** (a chunk boundary mid-abbreviation is a minor coherence cost,
not a correctness bug) and keeps the detector dependency-free. A abbreviation
blocklist is explicitly out of scope (can be added later behind the same seam if
real corpora show it matters).

**Alternatives rejected**:
- *NLP sentence segmenter (prose/segmenter lib)*: adds a dependency, heavier,
  against the pure-Go/lightweight grain. Rejected.
- *Regex with backtracking*: catastrophic-backtracking risk on the sync ACK path
  (Principle IV). Rejected — a single linear scan is used instead.

## 3. Paragraph detection: blank-line split

**Decision**: `splitParagraphs` splits on `\n\n` (one or more blank lines). The
markdown reader's `stripMarkdown` rebuilds text line-by-line preserving blank lines
(verified), so paragraph structure reaches the splitter intact. Plain-text and
other readers that preserve blank lines get the same treatment; a document with no
blank lines is treated as one paragraph (degrades gracefully to sentence-level).

**Rationale**: Uses the structure the readers already preserve; no reader changes
needed. Paragraph boundaries are the cascade's preferred flush points.

**Alternatives rejected**:
- *Single-newline paragraph split*: too aggressive (every line break becomes a
  boundary, shattering lists/wrapped prose). Rejected — blank-line (`\n\n`) is the
  canonical paragraph separator.

## 4. Greedy-fill with paragraph-boundary preference

**Decision**: Accumulate whole sentences into the current chunk; when the next
sentence would exceed the size budget, flush. Additionally, at a paragraph
boundary, flush if the current chunk is non-trivially filled (so a chunk doesn't
span paragraphs unless a single paragraph already exceeds the budget, in which case
sentence-level filling continues within it). The minimum-tail-merge (sub-`MinTokens`
final segment merges into its predecessor) is preserved exactly as today.

**Rationale**: Yields coherent, budget-respecting chunks that respect structure.
Paragraph-boundary preference is the spec's US1 acceptance 2 ("chunks do not span
paragraph breaks when the paragraph fits"). Tail-merge preserves the existing
no-tiny-tail guarantee.

## 5. Overlap at structural boundaries

**Decision**: Neighbor chunks share overlap by carrying the **last sentence(s)**
whose token sum fits the overlap budget into the next chunk (sentence-granularity
overlap), rather than the current arbitrary word-window overlap.

**Rationale**: Overlap at sentence boundaries is more coherent than mid-word
overlap. Carrying whole sentences (not fractional words) keeps each chunk readable.
The overlap *budget* is still derived from `Splitter.Overlap` (tokens), so the
configurable knob and its semantics are preserved.

**Alternatives rejected**:
- *Word-level overlap at the boundary*: re-introduces mid-sentence cuts at the
  overlap seam. Rejected — sentence-granularity is cleaner.
- *No overlap*: loses the recall benefit of neighbor overlap (a known retrieval
  lever). Rejected.

## 6. Char-offset fidelity + Segment preservation

**Decision**: Each emitted `Segment` carries accurate `StartCharIdx`/`EndCharIdx`
into the ORIGINAL source text (not the normalized/joined form), and `TokenCount`
via the unchanged `EstimateTokens`. The `Segment` struct is unchanged.

**Rationale**: `Segment` offsets are consumed downstream (chunk char ranges). The
current `buildSegment` joins words with single spaces (normalizing internal
whitespace); the cascade keeps the source substring for offset accuracy and
readability rather than re-joining. `EstimateTokens` is reused as-is (the 1.3×
heuristic; fixing it is H26, out of scope).

## 7. Package doc correction (FR-005)

**Decision**: Rewrite the `internal/chunk` package doc to describe the cascade
truthfully (paragraph → sentence → word greedy-fill, ~Size tokens, Overlap at
sentence granularity, MinTokens tail-merge, the 1.3× token heuristic).

**Rationale**: Under Option A the cascade now EXISTS, so the doc becomes true
(rather than being deleted to match the old word-window). FR-005 (doc accurate)
is satisfied by describing the implemented behavior, verified by tests.

## 8. No regression gate + re-chunk identity note

**Decision**: SC-003 is the gate — `make test-eval` must show recall@10/MRR no
worse than the word-window baseline. Because chunk text changes (different
boundaries), chunk IDs change; existing vaults keep old chunking until
`Reprocess`/`migrate` (idempotent, no duplicates — Principle II). This is a
documented operational note, not a migration the feature runs.

**Rationale**: A chunking change that hurts retrieval is a regression; the gate
catches it. The identity-change note prevents surprise (a user re-adding a file
after the upgrade and wondering why chunk IDs differ).
