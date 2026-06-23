package engine

// poison.go implements the H04/spec 019 quarantine MANAGEMENT surface (US2,
// FR-006): list flagged chunks (with the per-signal breakdown so a user can see
// WHY each was flagged), release a false positive back to normal retrieval, and
// reset (undo a release). All three are non-destructive — content is never
// deleted; only the verdict level and the 0x11 quarantine-index entry change.

import (
	"encoding/json"
	"fmt"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/poison"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// PoisonedChunk is one entry in the quarantine listing: the chunk's identity, a
// preview, and the full verdict (level/score/per-signal breakdown/matched phrases)
// — so a user can triage why a chunk was flagged (the standing quarantine-
// management preference: list, see-why, release).
type PoisonedChunk struct {
	ChunkID    string
	DocumentID string
	Preview    string
	Verdict    model.PoisonVerdict
}

// ListPoisoned returns all chunks currently flagged (suspicious/quarantine), via
// the 0x11 quarantine index populated async at ingest (O(flagged)). Each entry's
// verdict is read fresh from the chunk record (the source of truth), so a release
// is reflected even if the index briefly lags.
func (e *Engine) ListPoisoned() ([]PoisonedChunk, error) {
	var out []PoisonedChunk
	err := e.db.ScanQuarantine(func(chunkID string, _ []byte) bool {
		c, ok := lookupChunk(e.db, chunkID)
		if !ok || c.Poisoning == nil {
			return true // stale index entry (chunk gone/unscored) — skip
		}
		if c.Poisoning.Level.Quarantined() {
			out = append(out, PoisonedChunk{
				ChunkID:    c.ID,
				DocumentID: c.DocumentID,
				Preview:    preview(c.Content, 160),
				Verdict:    *c.Poisoning,
			})
		}
		return true
	})
	return out, err
}

// ReleaseChunk marks a flagged chunk as released — a false-positive override
// (FR-006): it re-enters default retrieval. Non-destructive: the original score is
// retained, only the level flips to `released` (sticky across rescans). Idempotent.
// Bumps the index epoch so cached default-query results invalidate (the released
// chunk may now appear).
func (e *Engine) ReleaseChunk(chunkID string) error {
	c, ok := lookupChunk(e.db, chunkID)
	if !ok {
		return fmt.Errorf("chunk not found: %s", chunkID)
	}
	if c.Poisoning == nil || c.Poisoning.Level == model.PoisonReleased {
		return nil // unscored/clean, or already released — idempotent no-op
	}
	c.Poisoning.Level = model.PoisonReleased
	if err := e.putChunk(c); err != nil {
		return err
	}
	_ = e.db.DeleteQuarantine(chunkID)
	e.markIndexChanged()
	return nil
}

// ResetChunk undoes a release (FR-006): re-derives the scored level from the stored
// score and re-quarantines if flagged. Non-destructive; idempotent.
func (e *Engine) ResetChunk(chunkID string) error {
	c, ok := lookupChunk(e.db, chunkID)
	if !ok {
		return fmt.Errorf("chunk not found: %s", chunkID)
	}
	if c.Poisoning == nil {
		return nil
	}
	c.Poisoning.Level = poison.LevelFor(c.Poisoning.Score,
		e.cfg.EffectivePoisonThresholdSuspicious(), e.cfg.EffectivePoisonThresholdQuarantine())
	if err := e.putChunk(c); err != nil {
		return err
	}
	if c.Poisoning.Level.Quarantined() {
		if vj, merr := json.Marshal(c.Poisoning); merr == nil {
			_ = e.db.PutQuarantine(chunkID, vj)
		}
	} else {
		_ = e.db.DeleteQuarantine(chunkID)
	}
	e.markIndexChanged()
	return nil
}

// putChunk writes a chunk record back to 0x03 (used by Release/Reset).
func (e *Engine) putChunk(c model.Chunk) error {
	bj, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return e.db.SetWithPrefix(storage.PrefixChunk, []byte(c.ID), bj)
}

// RescanPoisoning re-scores every stored chunk against the current detector
// (US3, FR-007): pre-feature chunks get verdicts, and a detector/threshold change
// is applied to the back-catalog WITHOUT re-ingesting source files. Idempotent
// (Constitution II): a chunk whose score is unchanged is a no-op write. A released
// chunk STAYS released (sticky across rescans, D11) — only its score/signals
// refresh, for transparency. No-op (0,0,nil) when detection is disabled.
//
// This is the single re-score operation behind the manual `poison rescan` surface
// (US3/US4 T031) and the threat-list-change background rescan (US4 T029). It bumps
// the index epoch so cached query results invalidate (verdicts may have changed).
func (e *Engine) RescanPoisoning() (rescored, flagged int, err error) {
	if !e.cfg.EffectivePoisoningEnabled() {
		return 0, 0, nil
	}
	det := e.poisonDetector() // US4: built-in + managed sources (merged)

	// Pass 1: load chunks (scan-then-write; avoid mutating keys during iteration).
	var chunks []model.Chunk
	if serr := e.db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil {
			chunks = append(chunks, c)
		}
		return true
	}); serr != nil {
		return 0, 0, serr
	}

	// Pass 2: re-score + persist (idempotent; released is sticky).
	for i := range chunks {
		c := &chunks[i]
		fresh := det.Score(c.Content)
		if c.Poisoning != nil && c.Poisoning.Score == fresh.Score {
			continue // identical verdict — idempotent no-op
		}
		if c.Poisoning != nil && c.Poisoning.Level == model.PoisonReleased {
			// Sticky: keep released, refresh score/signals for transparency only.
			c.Poisoning.Score = fresh.Score
			c.Poisoning.Signals = fresh.Signals
			c.Poisoning.MatchedPhrases = fresh.MatchedPhrases
		} else {
			c.Poisoning = &fresh
			if fresh.Level.Quarantined() {
				if vj, merr := json.Marshal(fresh); merr == nil {
					_ = e.db.PutQuarantine(c.ID, vj)
				}
				flagged++
			} else {
				_ = e.db.DeleteQuarantine(c.ID)
			}
		}
		if bj, merr := json.Marshal(c); merr == nil {
			_ = e.db.SetWithPrefix(storage.PrefixChunk, []byte(c.ID), bj)
		}
		rescored++
	}
	e.markIndexChanged()
	return rescored, flagged, nil
}
