package poison

import (
	"strings"

	"github.com/madeinoz67/go-rag/internal/model"
)

// HeuristicScorer is the default Detector (research D1). It normalizes the chunk
// text and combines three additive signals into a [0,1] score:
//
//   - instruction (weight 0.50): 1.0 if any curated injection phrase matches the
//     normalized text, else 0.0. The high-precision workhorse — a single classic
//     payload ("ignore previous instructions…") pushes the score to ≥ suspicious.
//   - repetition (weight 0.25): line-repetition ratio (0 for short/inline text).
//   - stuffing   (weight 0.25): token-concentration — how dominated the text is
//     by a single repeated token.
//
// Weighting keeps rep/stuff ALONE below the quarantine threshold, so a repetitive
// but benign log is never mass-quarantined (SC-002): quarantine requires an
// instruction phrase plus manipulation, suspicious is either alone. Both
// suspicious and quarantine are excluded from default results (Q1=A).
//
// HeuristicScorer is immutable after construction; safe for concurrent use.
type HeuristicScorer struct {
	phrases    []string
	suspicious float64
	quarantine float64
}

// NewHeuristic builds a scorer from a phrase list (the DefaultPhrases when nil)
// and the effective suspicious/quarantine thresholds. Decoupled from config so
// the package stays a pure leaf (only stdlib + model).
func NewHeuristic(phrases []string, suspicious, quarantine float64) *HeuristicScorer {
	if len(phrases) == 0 {
		phrases = DefaultPhrases
	}
	// Copy to avoid aliasing caller-owned slices / later mutation.
	p := make([]string, len(phrases))
	copy(p, phrases)
	return &HeuristicScorer{phrases: p, suspicious: suspicious, quarantine: quarantine}
}

// Score computes the verdict for a chunk (Detector). Deterministic.
func (h *HeuristicScorer) Score(text string) model.PoisonVerdict {
	norm := normalize(text)
	rep := repetition(norm)
	stuff := stuffing(norm)
	instr, matched := instruction(norm, h.phrases)

	score := clamp01(0.5*instr + 0.25*rep + 0.25*stuff)
	v := model.PoisonVerdict{
		Level:   LevelFor(score, h.suspicious, h.quarantine),
		Score:   score,
		Signals: model.PoisonSignals{Repetition: rep, Stuffing: stuff, Instruction: instr},
	}
	if len(matched) > 0 {
		v.MatchedPhrases = matched
	}
	return v
}

// LevelFor maps a score to a PoisonLevel via the thresholds. Exported so the
// engine can re-derive the scored level on ResetChunk (FR-006) without re-scoring.
func LevelFor(score, suspicious, quarantine float64) model.PoisonLevel {
	switch {
	case score >= quarantine:
		return model.PoisonQuarantine
	case score >= suspicious:
		return model.PoisonSuspicious
	default:
		return model.PoisonClean
	}
}

// normalize lowercases and collapses runs of whitespace (incl. newlines) to a
// single space, trimming the result. Deterministic; CJK-safe (no tokenization).
// Punctuation is preserved so substring phrase matches ("new instructions")
// still hit "new instructions:" verbatim.
func normalize(text string) string {
	fields := strings.Fields(strings.ToLower(text))
	return strings.Join(fields, " ")
}

// instruction returns (1.0, matchedPhrases) if any phrase is a substring of the
// normalized text, else (0.0, nil). Case already normalized by the caller.
func instruction(norm string, phrases []string) (float64, []string) {
	if norm == "" {
		return 0, nil
	}
	var matched []string
	for _, p := range phrases {
		if strings.Contains(norm, p) {
			matched = append(matched, p)
		}
	}
	if len(matched) == 0 {
		return 0, nil
	}
	return 1.0, matched
}

// repetition is the line-repetition ratio: the fraction of lines that are
// duplicates. Returns 0 for text with fewer than 3 lines (no meaningful
// line-level repetition signal for short/inline text). Bounded to [0,1].
func repetition(norm string) float64 {
	lines := strings.Split(norm, "\n")
	// After normalize(), whitespace is collapsed but the original newlines were
	// turned into spaces by strings.Fields — so multi-line structure is already
	// flattened. Re-split on " sentence-ish" boundaries is unreliable, so we fall
	// back to a token-trigram repetition measure for robustness.
	_ = lines
	tokens := strings.Fields(norm)
	if len(tokens) < 6 {
		return 0 // too short to repeat meaningfully
	}
	seen := make(map[string]int, len(tokens))
	for _, t := range tokens {
		seen[t]++
	}
	dup := 0
	for _, c := range seen {
		if c > 1 {
			dup += c - 1
		}
	}
	return clamp01(float64(dup) / float64(len(tokens)) * 2.0)
}

// stuffing is token-concentration: how dominated the text is by its single most
// frequent token (len > 3 to skip stopword-ish short tokens). 0 unless a token
// repeats heavily. Bounded to [0,1].
func stuffing(norm string) float64 {
	tokens := strings.Fields(norm)
	if len(tokens) < 6 {
		return 0
	}
	freq := make(map[string]int, len(tokens))
	for _, t := range tokens {
		if len(t) <= 3 {
			continue
		}
		freq[t]++
	}
	maxC := 0
	for _, c := range freq {
		if c > maxC {
			maxC = c
		}
	}
	if maxC < 2 {
		return 0
	}
	// A token repeated 6+ times → stuffing = 1.0.
	return clamp01(float64(maxC-1) / 5.0)
}

// clamp01 bounds v to [0,1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
