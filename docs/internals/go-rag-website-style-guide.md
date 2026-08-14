<!-- Author: Stephen Eaton -->

# go-rag — Style Guide
**v1.0 · Clean Technical Docs direction**

A visual identity for a single-binary local RAG database. The direction is
Stripe/Docusaurus-adjacent: light, precise, code-forward — but built around
go-rag's own content, not a generic docs template. The signature idea running
through the whole system is **two lists becoming one**: go-rag's hybrid
retrieval fuses a lexical (BM25) ranked list and a semantic (vector) ranked
list via Reciprocal Rank Fusion. Every place the identity needs a device —
color pairing, diagrams, section dividers — draws on that fusion, not on
decoration for its own sake.

---

## 1. Color

Two accents represent the two retrieval paths that go-rag fuses. They are
functional, not merely decorative — use them to *label* lexical vs. semantic
concepts consistently, never interchangeably.

| Token | Hex | Use |
|---|---|---|
| `--paper` | `#FAFAF8` | Page background |
| `--paper-alt` | `#F1F0EA` | Alternate section background |
| `--ink` | `#14171A` | Primary text, headings |
| `--ink-soft` | `#52585F` | Secondary/body text |
| `--line` | `#E3E1DA` | Hairline borders, dividers |
| `--line-strong` | `#C9C6BC` | Emphasized borders, table rules |
| `--lexical` | `#16A34A` | BM25 / keyword search — badges, diagram nodes |
| `--lexical-soft` | `#E7F6EC` | Lexical background tint |
| `--semantic` | `#4F46E5` | Vector / embedding search — badges, diagram nodes |
| `--semantic-soft` | `#EEEDFC` | Semantic background tint |
| `--fusion` | `#7C3AED` | The merged/final result — RRF output, primary CTA |
| `--fusion-soft` | `#F1EEFD` | Fusion background tint — icon fills, "fused" badges |
| `--amber` | `#B45309` | Status/alpha badges, drift warnings |
| `--amber-soft` | `#FDF2E3` | Amber background tint — alpha/status badge fills |
| `--code-bg` | `#14171A` | Code block background (inverted, Stripe-style contrast) |
| `--code-ink` | `#E7E5DD` | Code block text |

**Rule of thumb:** lexical is green, semantic is indigo, and the moment they
combine — a merged result, a completed query, a primary action — gets violet
(`--fusion`). Never use `--fusion` for anything that isn't the output of a
combination; it loses meaning if it becomes a generic accent.

Do not introduce a warm terracotta/clay accent or a near-black + acid-green
scheme — both read as generic "AI-generated" defaults and would blur go-rag's
identity into every other dev-tool landing page.

## 2. Typography

Three faces, three jobs. No face is used outside its role.

| Role | Face | Notes |
|---|---|---|
| Display / headings | **Space Grotesk** | Geometric, slightly technical, used at large sizes with tight tracking |
| Body copy | **IBM Plex Sans** | Legible at small sizes, quiet, does not compete with headings |
| Code, labels, eyebrows, data | **JetBrains Mono** | All-caps eyebrows get `letter-spacing: 0.08em`; code blocks keep default case |

**Type scale** (desktop, rem):

| Style | Size | Weight | Line-height |
|---|---|---|---|
| H1 | 3.25 | 600 | 1.05 |
| H2 | 2.25 | 600 | 1.15 |
| H3 | 1.375 | 600 | 1.3 |
| Body | 1.0625 | 400 | 1.65 |
| Small / caption | 0.875 | 400 | 1.5 |
| Eyebrow (mono, caps) | 0.75 | 500 | 1.4 |

Mobile: scale H1 down to 2.25rem, H2 to 1.625rem; body stays fixed for
readability.

## 3. Spacing & Layout

- Base unit: **8px**. All spacing is a multiple of it (8/16/24/32/48/64/96).
- Content max-width: **1120px**, centered, 24px side gutters below 768px.
- Section vertical rhythm: 96px desktop / 64px mobile between major sections.
- Cards and code blocks use **10px** corner radius — soft enough to feel
  approachable, sharp enough to stay technical. Avoid fully-rounded (pill)
  shapes except for status badges and buttons.
- Nested elements sit one step down from their container: diagram nodes,
  list rows, and small utility buttons (copy, tags) use **6–8px**. The 10px
  token marks a top-level container; anything inside one steps down rather
  than repeating it.
- The 8px base unit governs macro rhythm — section spacing, card padding,
  grid gaps. Compact UI chrome (badge padding, table cell padding, icon
  gaps) may use finer 2/4/6px increments where 8px would look heavy at that
  scale; this is the one place spacing intentionally departs from the grid.

## 4. Components

**Buttons**
- Primary: `--fusion` fill, white text, fully rounded (pill), 44px height minimum (touch target).
- Secondary: transparent fill, `--ink` 1px border, `--ink` text, same pill shape.
- Never use `--lexical` or `--semantic` as a button fill — they're reserved for labeling, not action.

**Inline code chip**
- For a short command, flag, or value referenced mid-sentence: `--paper` background, 1px `--line` border, 4px radius, 2px 6px padding, mono font at ~0.85em.
- Reserve the bare (unstyled) mono treatment for a single environment variable or address quoted in passing, where a chip would compete with the sentence around it — used sparingly, not as a second default.

**Badges** (`lexical`, `semantic`, `alpha`, `drift`)
- Pill shape, soft background tint (`-soft` token) + full-strength text/border in the matching hue.
- Mono font, uppercase, 0.75rem.

**Code blocks**
- Always `--code-bg`/`--code-ink`, JetBrains Mono, 10px radius.
- Terminal-style blocks (quickstart) show a three-dot window chrome only when representing an actual shell session — not as generic decoration on every snippet.

**Tables** (CLI reference, benchmarks)
- Hairline row dividers (`--line`), no vertical rules, mono for command/flag names, sans for descriptions.

**Diagrams** (architecture, RRF fusion)
- Nodes use `--lexical-soft`/`--semantic-soft` fills with full-strength borders; arrows/connectors are `--line-strong`; the terminal "fused" node uses `--fusion`.

## 5. Signature element

The **RRF fusion diagram**: two ranked lists (green, lexical / indigo,
semantic) visibly interleave into one ranked output list, labeled with the
actual formula go-rag uses (`score(d) = Σ 1/(k + rank)`). This is the one
place the page is allowed to be a little playful (subtle motion on
scroll/hover) — everywhere else, motion is restrained to hover/focus states
only. One signature moment, not several competing ones.

## 6. Voice & content

- Write from the person's side of the terminal: "you get source-cited
  results," not "the system returns citations."
- Name things by what they do, not by internal architecture — say "your
  document vault," not "the Pebble-backed corpus," in user-facing copy.
  Reserve architecture terms (Pebble, RRF, BM25) for developer-facing
  sections where precision matters more than approachability.
- Plain, active, unhurried. No exclamation points, no "supercharge/unlock/
  seamless." go-rag's own README voice — direct, slightly dry, confident
  without hype — is the model to follow.
- Status and errors are stated plainly: what happened, what to do next. No
  apologizing tone.

## 7. Accessibility & quality floor

- All interactive elements have visible keyboard focus (2px `--fusion`
  outline, 2px offset).
- Color is never the only signal — lexical/semantic labels always carry a
  text label alongside the color.
- Respect `prefers-reduced-motion`: disable the fusion diagram's animation
  and fall back to a static, fully-labeled state.
- Body text maintains 4.5:1 contrast minimum against `--paper`.
- Responsive down to 360px width.
