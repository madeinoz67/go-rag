# Research — Quarantine Management View

**Feature**: specs/053-quarantine-management | **Date**: 2026-07-13

Phase 0 output. The engine surface is complete; the research resolves the UI design decisions.

## R1 — UI reuses existing engine surface (no new capability)

**Decision**: `quarantine.go` handlers call `s.eng.ListPoisoned(vault)`,
`s.eng.ReleaseChunk(vault, chunkID)`, `s.eng.ResetChunk(vault, chunkID)`,
`s.eng.RescanPoisoning(vault)`, and `s.eng.GetChunk(vault, chunkID)` (for the full text in the
detail view). No new engine method.

**Grounding** (verified): `ListPoisoned` returns `[]PoisonedChunk{ChunkID, DocumentID, Preview,
Verdict}` where Verdict = `PoisonVerdict{Level, Score, Signals{Repetition,Stuffing,Instruction},
MatchedPhrases}`. The Preview is 160 chars; the detail view fetches `GetChunk` for full text.

## R2 — Detail view: GetChunk for full text + ListPoisoned for verdict

**Decision**: the list view uses ListPoisoned (lightweight — preview + verdict). The detail view
fetches GetChunk(vault, chunkID) for the full chunk text, then overlays the PoisonVerdict's
MatchedPhrases as highlighted spans.

## R3 — Matched-phrase highlighting (client-side)

**Decision**: highlight each MatchedPhrase in the chunk text with a signal-coloured background:
repetition=amber, stuffing=red, instruction=purple. Overlapping phrases blend. The highlighting
is client-side (Alpine renders the chunk text with `<mark>` spans around matched phrases). The
signals breakdown (Repetition/Stuffing/Instruction scores + thresholds) shown alongside.

## R4 — Release vs Reset distinction

**Decision**: two buttons. **Release** = permanent false-positive override (the chunk re-enters
retrieval; the score is preserved but the level is cleared). **Reset** = force re-scan (the
verdict is recomputed; may restore quarantine). Tooltips explain the difference. Both confirmed.

## R5 — Sidebar placement

**Decision**: new sidebar item "Quarantine" (expanding the original 8-view sidebar to 9). It's
a distinct triage workflow (browse → inspect → release), not a health metric (Operations).

## R6 — Rescan progress

**Decision**: simple "scanning..." state with a manual refresh button. No progress bar (the
rescan is a bounded operation; a loading state suffices).
